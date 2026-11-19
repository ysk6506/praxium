package pgstore

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// provEntry is what a volatile value keeps in the prov column of its row besides the typed copy: the exact value
// (a typed column cannot hold every value without loss), the grade, the basis, the time the source saw it and the
// reference. The key of the entry is the attribute number.
type provEntry struct {
	Value any    `json:"v"`
	Grade string `json:"g"`
	Basis string `json:"b"`
	Seen  string `json:"s"`
	Ref   string `json:"r"`
}

// liveAt is the objects of a registry answer that have a row valid in the state.
func (sn *kgSnapshot) liveAt(ctx context.Context, tx *sql.Tx, reg []regEntry, newest bool) (map[int64]bool, error) {
	rows, err := sn.fetchRows(ctx, tx, reg, newest)
	live := map[int64]bool{}
	for no := range rows {
		live[no] = true
	}
	return live, err
}

// fetchRows is the rows of the objects of the answer by object number.
func (sn *kgSnapshot) fetchRows(ctx context.Context, tx *sql.Tx, reg []regEntry, newest bool) (map[int64]graph.ObjectRow, error) {
	byType := map[int][]int64{}
	for _, e := range reg {
		byType[e.typeNo] = append(byType[e.typeNo], e.objNo)
	}
	out := map[int64]graph.ObjectRow{}
	for typeNo, nos := range byType {
		rows, err := sn.objectRowsOfType(ctx, tx, typeNo, nos, newest)
		if err != nil {
			return nil, err
		}
		for no, r := range rows {
			out[no] = r
		}
	}
	return out, nil
}

// volatileOf reads the value of one volatile attribute of an object from the table that has it: the common one or
// the one of the type.
func (sn *kgSnapshot) volatileOf(ctx context.Context, tx *sql.Tx, e regEntry, id graph.ObjectID, attrNo int) (graph.VolatileRow, bool, error) {
	table := pgschema.TableObjectState
	if _, ok := sn.a.common[attrNo]; !ok {
		if _, ok := sn.a.state[e.typeNo][attrNo]; !ok {
			return graph.VolatileRow{}, false, nil
		}
		table = pgschema.StateTable(e.typeNo)
	}
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT prov -> $2 FROM `+sn.a.t(table)+` WHERE obj_no = $1`, e.objNo, strconv.Itoa(attrNo)).Scan(&raw)
	if isNoRows(err) || (err == nil && len(raw) == 0) {
		return graph.VolatileRow{}, false, nil
	}
	if err != nil {
		return graph.VolatileRow{}, false, dbErr(err, "read a volatile value")
	}
	var p provEntry
	if err := unmarshal(raw, &p); err != nil {
		return graph.VolatileRow{}, false, err
	}
	v, err := plainValue(p.Value)
	if err != nil {
		return graph.VolatileRow{}, false, err
	}
	return graph.VolatileRow{Object: id, AttrNo: attrNo, Value: v, Grade: model.Grade(p.Grade), Basis: model.Basis(p.Basis), SeenAt: p.Seen, Ref: p.Ref}, true, nil
}

const kgIdentifierSelect = `SELECT o.ref, i.scheme, i.value, i.scope, i.seq_from, i.seq_to FROM `

func (sn *kgSnapshot) identifierQuery(cond, order string) string {
	return kgIdentifierSelect + sn.a.t(pgschema.TableObjectIdentifier) + ` i JOIN ` + sn.a.t(pgschema.TableObject) + ` o ON o.obj_no = i.obj_no WHERE ` +
		cond + ` ORDER BY ` + order
}

func (sn *kgSnapshot) Identifiers(ctx context.Context, scheme, value string) ([]graph.IdentifierRecord, error) {
	return sn.identifiers(ctx, `i.scheme = $1 AND i.value = $2 AND i.seq_from <= $3 AND (i.seq_to IS NULL OR $3 < i.seq_to)`, scheme, value)
}

func (sn *kgSnapshot) IdentifierHistory(ctx context.Context, scheme, value string) ([]graph.IdentifierRecord, error) {
	return sn.identifiers(ctx, `i.scheme = $1 AND i.value = $2 AND i.seq_from <= $3`, scheme, value)
}

func (sn *kgSnapshot) identifiers(ctx context.Context, cond, scheme, value string) ([]graph.IdentifierRecord, error) {
	var out []graph.IdentifierRecord
	err := sn.read(ctx, func(tx *sql.Tx, _ bool) error {
		var err error
		out, err = collect(ctx, tx, scanIdentifier, "read identifiers", sn.identifierQuery(cond, `i.seq_from, o.ref, i.scope`), scheme, value, sn.info.Seq)
		return err
	})
	return out, err
}

// IdentifiersOf implements graph.IdentifierLister.
func (sn *kgSnapshot) IdentifiersOf(ctx context.Context, id graph.ObjectID) ([]graph.IdentifierRecord, error) {
	var out []graph.IdentifierRecord
	err := sn.read(ctx, func(tx *sql.Tx, _ bool) error {
		var err error
		out, err = collect(ctx, tx, scanIdentifier, "read the identifiers of an object", sn.identifierQuery(
			`o.ref = $1 AND i.seq_from <= $2 AND (i.seq_to IS NULL OR $2 < i.seq_to)`, `i.seq_from, i.scheme, i.value, i.scope`), id.String(), sn.info.Seq)
		return err
	})
	return out, err
}

func (sn *kgSnapshot) IdentityEvents(ctx context.Context) ([]graph.IdentityEvent, error) {
	var out []graph.IdentityEvent
	err := sn.read(ctx, func(tx *sql.Tx, _ bool) error {
		var err error
		out, err = collect(ctx, tx, scanEvent, "read identity events", `SELECT e.change_seq, e.event_kind, r.ref, i.ref, e.link_ref_map FROM `+
			sn.a.t(pgschema.TableIdentityEvent)+` e JOIN `+sn.a.t(pgschema.TableObject)+` r ON r.obj_no = e.retired_obj_no JOIN `+
			sn.a.t(pgschema.TableObject)+` i ON i.obj_no = e.into_obj_no WHERE e.change_seq <= $1 ORDER BY e.change_seq, e.ord`, sn.info.Seq)
		return err
	})
	return out, err
}

// Versions implements graph.Reader: the sealed versions in order.
func (s *Store) Versions(ctx context.Context, kg graph.KGID) ([]graph.Version, error) {
	a, err := s.access(ctx, kg)
	if err != nil {
		return nil, err
	}
	var out []graph.Version
	err = a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		if _, err := loadKGHead(ctx, tx, a, false); err != nil {
			return err
		}
		var err error
		if out, err = collect(ctx, tx, scanVersion(kg), "list the versions", `SELECT `+versionColumns+` FROM `+a.t(pgschema.TableVersion)+` ORDER BY version_no`); err != nil {
			return err
		}
		return fillKGParts(ctx, tx, a, out)
	})
	return out, err
}

func fillKGParts(ctx context.Context, tx *sql.Tx, a *kgAccess, versions []graph.Version) error {
	byNo := make(map[int]*graph.Version, len(versions))
	for i := range versions {
		byNo[versions[i].No] = &versions[i]
	}
	type part struct {
		no int
		k  graph.PartKey
		p  graph.Part
	}
	parts, err := collect(ctx, tx, func(sc scanner) (part, error) {
		var x part
		var sum []byte
		if err := sc.Scan(&x.no, &x.k.Entity, &x.k.TypeID, &sum, &x.p.Rows); err != nil {
			return x, err
		}
		var err error
		x.p.Sum, err = sumFrom(sum)
		return x, err
	}, "read the version parts", `SELECT version_no, entity_kind, type_id, partial_sum, row_count FROM `+a.t(pgschema.TableVersionHashPart))
	for _, x := range parts {
		if v := byNo[x.no]; v != nil {
			v.Parts[x.k] = x.p
		}
	}
	return err
}
