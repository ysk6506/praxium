package pgstore

import (
	"context"
	"database/sql"
	"math"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// snapshot is one state of a KG: the change number it shows is fixed when it is taken. Each read is a
// read-only repeatable-read transaction of its own that first checks that the KG is still in the generation the
// snapshot was taken in, since a restore removes rows that the snapshot's state may have had.
type snapshot struct {
	sh   *shard
	info graph.SnapshotInfo
}

var (
	_ graph.Snapshot         = (*snapshot)(nil)
	_ graph.IdentifierLister = (*snapshot)(nil)
)

// Snapshot implements graph.Reader.
func (s *Store) Snapshot(ctx context.Context, rp graph.ReadPoint) (graph.Snapshot, error) {
	sh, err := s.rt.locate(rp.KG)
	if err != nil {
		return nil, err
	}
	var info graph.SnapshotInfo
	err = sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		cur, err := loadHead(ctx, tx, rp.KG, false)
		if err != nil {
			return err
		}
		if rp.Generation != 0 && rp.Generation != cur.generation {
			return kernel.Errorf(graph.CodeGenerationChanged, "generation %d, the KG is at %d", rp.Generation, cur.generation)
		}
		info = graph.SnapshotInfo{KG: rp.KG, Generation: cur.generation, Volatile: graph.VolatileOmitted}
		return readPoint(ctx, tx, rp, cur.head, &info)
	})
	if err != nil {
		return nil, err
	}
	return &snapshot{sh: sh, info: info}, nil
}

// readPoint fills in the change number and the volatile mode a read point means.
func readPoint(ctx context.Context, tx *sql.Tx, rp graph.ReadPoint, head graph.Head, info *graph.SnapshotInfo) error {
	switch rp.Kind {
	case graph.ReadLatest:
		info.Seq, info.Volatile = head.ReadableThrough, graph.VolatileLatest
	case graph.ReadApplied:
		info.Seq, info.Volatile = head.AppliedSeq, graph.VolatileLatest
	case graph.ReadSeq:
		if rp.Seq < 0 || rp.Seq > head.ReadableThrough {
			return kernel.Errorf(graph.CodeNotReadable, "change number %d is not readable: readable_through is %d", rp.Seq, head.ReadableThrough)
		}
		info.Seq = rp.Seq
	case graph.ReadVersion:
		err := tx.QueryRowContext(ctx, `SELECT sealed_seq FROM kg_version WHERE kg_id = $1 AND version_id = $2`, rp.KG.String(), rp.Version.String()).Scan(&info.Seq)
		if isNoRows(err) {
			return kernel.Errorf(graph.CodeKGNotFound, "no version %q", rp.Version)
		}
		if err != nil {
			return dbErr(err, "find the version")
		}
		info.Version = rp.Version
	default:
		return kernel.Errorf(kernel.CodeInvalidValue, "unknown read kind %d", rp.Kind)
	}
	return nil
}

func (sn *snapshot) Info() graph.SnapshotInfo { return sn.info }

// read runs fn in a reading transaction after the generation check.
func (sn *snapshot) read(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return sn.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		cur, err := loadHead(ctx, tx, sn.info.KG, false)
		if err != nil {
			return err
		}
		if cur.generation != sn.info.Generation {
			return kernel.Errorf(graph.CodeGenerationChanged, "the KG moved to generation %d", cur.generation)
		}
		return fn(tx)
	})
}

// collect reads every row of a query with the scan function.
func collect[T any](ctx context.Context, tx *sql.Tx, scan func(scanner) (T, error), what, query string, args ...any) ([]T, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, dbErr(err, what)
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, dbErr(err, what)
		}
		out = append(out, v)
	}
	return out, dbErr(rows.Err(), what)
}

func (sn *snapshot) kg() string { return sn.info.KG.String() }

func (sn *snapshot) Object(ctx context.Context, id graph.ObjectID) (graph.ObjectRow, bool, error) {
	var row graph.ObjectRow
	var found bool
	err := sn.read(ctx, func(tx *sql.Tx) error {
		got, err := collect(ctx, tx, scanObject, "read an object", `SELECT `+objectColumns+`, 0::bigint FROM (
			(SELECT `+objectColumns+` FROM object_live WHERE kg_id = $1 AND object_id = $2 AND seq_from <= $3)
			UNION ALL
			(SELECT `+objectColumns+` FROM object_hist WHERE kg_id = $1 AND object_id = $2 AND seq_from <= $3 AND seq_to > $3)) o LIMIT 1`,
			sn.kg(), id.String(), sn.info.Seq)
		if len(got) > 0 {
			row, found = got[0], true
		}
		return err
	})
	return row, found, err
}

func (sn *snapshot) Link(ctx context.Context, id graph.LinkID) (graph.LinkRow, bool, error) {
	var row graph.LinkRow
	var found bool
	err := sn.read(ctx, func(tx *sql.Tx) error {
		got, err := collect(ctx, tx, scanLink, "read a link", `SELECT `+linkSelectColumns+`, 0::bigint FROM (
			(SELECT `+linkSelectColumns+` FROM link_live WHERE kg_id = $1 AND link_id = $2 AND seq_from <= $3)
			UNION ALL
			(SELECT `+linkSelectColumns+` FROM link_hist WHERE kg_id = $1 AND link_id = $2 AND seq_from <= $3 AND seq_to > $3)) l LIMIT 1`,
			sn.kg(), id.String(), sn.info.Seq)
		if len(got) > 0 {
			row, found = got[0], true
		}
		return err
	})
	return row, found, err
}

func (sn *snapshot) ScanObjects(ctx context.Context, after graph.ObjectID, limit int) ([]graph.ObjectRow, error) {
	var out []graph.ObjectRow
	err := sn.read(ctx, func(tx *sql.Tx) error {
		if limit <= 0 {
			return nil
		}
		var err error
		out, err = collect(ctx, tx, scanObject, "scan objects", objectSelect, sn.kg(), afterText(after), sn.info.Seq, limit)
		return err
	})
	return out, err
}

func (sn *snapshot) ScanLinks(ctx context.Context, after graph.LinkID, limit int) ([]graph.LinkRow, error) {
	var out []graph.LinkRow
	err := sn.read(ctx, func(tx *sql.Tx) error {
		if limit <= 0 {
			return nil
		}
		var err error
		out, err = collect(ctx, tx, scanLink, "scan links", linkSelect("true"), sn.kg(), afterText(after), sn.info.Seq, limit)
		return err
	})
	return out, err
}

// afterText is the id to scan after; an empty text is below every id.
func afterText[K any](id kernel.ID[K]) string {
	if id.IsZero() {
		return ""
	}
	return id.String()
}

var endConditions = map[graph.Direction]string{
	graph.Out:  `src_object_id = $5`,
	graph.In:   `dst_object_id = $5`,
	graph.Both: `(src_object_id = $5 OR dst_object_id = $5)`,
}

func (sn *snapshot) LinksOf(ctx context.Context, id graph.ObjectID, dir graph.Direction) ([]graph.LinkRow, error) {
	cond, ok := endConditions[dir]
	if !ok {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "unknown direction %d", dir)
	}
	var out []graph.LinkRow
	err := sn.read(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = collect(ctx, tx, scanLink, "read the links of an object", linkSelect(cond), sn.kg(), "", sn.info.Seq, math.MaxInt32, id.String())
		return err
	})
	return out, err
}

func (sn *snapshot) Volatile(ctx context.Context, id graph.ObjectID, attrNo int) (graph.VolatileRow, bool, error) {
	var row graph.VolatileRow
	var found bool
	err := sn.read(ctx, func(tx *sql.Tx) error {
		if sn.info.Volatile != graph.VolatileLatest {
			return kernel.Errorf(graph.CodeVolatileOmitted, "volatile values are only in latest reads")
		}
		got, err := collect(ctx, tx, func(sc scanner) (graph.VolatileRow, error) { return scanVolatile(sc, id) }, "read a volatile value",
			`SELECT attr_no, value, grade, basis, seen_at, ref FROM attr_volatile v WHERE kg_id = $1 AND object_id = $2 AND attr_no = $3 AND (
				EXISTS (SELECT 1 FROM object_live WHERE kg_id = $1 AND object_id = $2 AND seq_from <= $4)
				OR EXISTS (SELECT 1 FROM object_hist WHERE kg_id = $1 AND object_id = $2 AND seq_from <= $4 AND seq_to > $4))`,
			sn.kg(), id.String(), attrNo, sn.info.Seq)
		if len(got) > 0 {
			row, found = got[0], true
		}
		return err
	})
	return row, found, err
}

const identifierColumns = `object_id, scheme, value, scope, seq_from, seq_to`

func (sn *snapshot) Identifiers(ctx context.Context, scheme, value string) ([]graph.IdentifierRecord, error) {
	return sn.identifiers(ctx, `scheme = $2 AND value = $3 AND seq_from <= $4 AND (seq_to IS NULL OR $4 < seq_to)`, scheme, value)
}

func (sn *snapshot) IdentifierHistory(ctx context.Context, scheme, value string) ([]graph.IdentifierRecord, error) {
	return sn.identifiers(ctx, `scheme = $2 AND value = $3 AND seq_from <= $4`, scheme, value)
}

func (sn *snapshot) identifiers(ctx context.Context, where, scheme, value string) ([]graph.IdentifierRecord, error) {
	var out []graph.IdentifierRecord
	err := sn.read(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = collect(ctx, tx, scanIdentifier, "read identifiers", `SELECT `+identifierColumns+` FROM object_identifier
			WHERE kg_id = $1 AND `+where+` ORDER BY seq_from, object_id, scope`, sn.kg(), scheme, value, sn.info.Seq)
		return err
	})
	return out, err
}

// IdentifiersOf implements graph.IdentifierLister.
func (sn *snapshot) IdentifiersOf(ctx context.Context, id graph.ObjectID) ([]graph.IdentifierRecord, error) {
	var out []graph.IdentifierRecord
	err := sn.read(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = collect(ctx, tx, scanIdentifier, "read the identifiers of an object", `SELECT `+identifierColumns+` FROM object_identifier
			WHERE kg_id = $1 AND object_id = $2 AND seq_from <= $3 AND (seq_to IS NULL OR $3 < seq_to) ORDER BY seq_from, scheme, value, scope`,
			sn.kg(), id.String(), sn.info.Seq)
		return err
	})
	return out, err
}

func (sn *snapshot) IdentityEvents(ctx context.Context) ([]graph.IdentityEvent, error) {
	var out []graph.IdentityEvent
	err := sn.read(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = collect(ctx, tx, scanEvent, "read identity events", `SELECT change_seq, event_kind, retired_object_id, into_object_id, link_id_map
			FROM identity_event WHERE kg_id = $1 AND change_seq <= $2 ORDER BY change_seq, ord`, sn.kg(), sn.info.Seq)
		return err
	})
	return out, err
}

// Head implements graph.Reader.
func (s *Store) Head(ctx context.Context, kg graph.KGID) (graph.Head, error) {
	sh, err := s.rt.locate(kg)
	if err != nil {
		return graph.Head{}, err
	}
	var out graph.Head
	err = sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		cur, err := loadHead(ctx, tx, kg, false)
		out = cur.head
		return err
	})
	return out, err
}

const versionColumns = `version_no, version_id, sealed_seq, generation, prev_version_id, definition_set_hash, engine_version,
	hash_rule_version, input_digest, graph_hash, row_count, sealed_at, seal_kind`

func scanVersion(kg graph.KGID) func(scanner) (graph.Version, error) {
	return func(sc scanner) (graph.Version, error) {
		v := graph.Version{KG: kg}
		var id, prev, kind string
		var definitions, digest, sum []byte
		if err := sc.Scan(&v.No, &id, &v.SealedSeq, &v.Generation, &prev, &definitions, &v.EngineVersion, &v.HashRuleVersion, &digest, &sum, &v.Rows, &v.SealedAt, &kind); err != nil {
			return v, err
		}
		var err error
		if v.ID, err = parseID[graph.VersionKind](id); err != nil {
			return v, err
		}
		if prev != "" {
			if v.Prev, err = parseID[graph.VersionKind](prev); err != nil {
				return v, err
			}
		}
		if v.DefinitionSetHash, err = hashOf(definitions); err != nil {
			return v, err
		}
		if v.InputDigest, err = hashOf(digest); err != nil {
			return v, err
		}
		if v.GraphHash, err = sumFrom(sum); err != nil {
			return v, err
		}
		v.SealedAt, v.Kind = v.SealedAt.UTC(), graph.SealKind(kind)
		v.Parts = map[graph.PartKey]graph.Part{}
		return v, nil
	}
}

// Versions implements graph.Reader: the sealed versions in order.
func (s *Store) Versions(ctx context.Context, kg graph.KGID) ([]graph.Version, error) {
	sh, err := s.rt.locate(kg)
	if err != nil {
		return nil, err
	}
	var out []graph.Version
	err = sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		if _, err := loadHead(ctx, tx, kg, false); err != nil {
			return err
		}
		var err error
		if out, err = collect(ctx, tx, scanVersion(kg), "list the versions", `SELECT `+versionColumns+` FROM kg_version WHERE kg_id = $1 ORDER BY version_no`, kg.String()); err != nil {
			return err
		}
		return fillParts(ctx, tx, kg, out)
	})
	return out, err
}

// fillParts adds the partial sums to the versions.
func fillParts(ctx context.Context, tx *sql.Tx, kg graph.KGID, versions []graph.Version) error {
	rows, err := tx.QueryContext(ctx, `SELECT version_no, entity_kind, type_id, partial_sum, row_count FROM kg_version_hash_part WHERE kg_id = $1`, kg.String())
	if err != nil {
		return dbErr(err, "read the version parts")
	}
	defer rows.Close()
	byNo := make(map[int]*graph.Version, len(versions))
	for i := range versions {
		byNo[versions[i].No] = &versions[i]
	}
	for rows.Next() {
		var no int
		var k graph.PartKey
		var sum []byte
		var p graph.Part
		if err := rows.Scan(&no, &k.Entity, &k.TypeID, &sum, &p.Rows); err != nil {
			return dbErr(err, "read a version part")
		}
		if p.Sum, err = sumFrom(sum); err != nil {
			return err
		}
		if v := byNo[no]; v != nil {
			v.Parts[k] = p
		}
	}
	return dbErr(rows.Err(), "read the version parts")
}
