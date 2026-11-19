package pgstore

import (
	"context"
	"database/sql"
	"sort"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// touched are the numbers of the entities one change number wrote, by what happened to them. A changed or a retired
// entity has a row in the history that ends at the number; an added one has its row opened at it.
type touched struct {
	addedObjects, backObjects []int64 // back: changed or retired, to be opened again from the history
	changedObjects            []int64
	addedLinks, backLinks     []int64
	changedLinks              []int64
}

// entriesOf reads what the change entries say the change number wrote.
func entriesOf(ctx context.Context, tx *sql.Tx, a *kgAccess, seq int64) (touched, error) {
	type entry struct {
		kind, change int16
		no           int64
	}
	rows, err := collect(ctx, tx, func(sc scanner) (entry, error) {
		var e entry
		return e, sc.Scan(&e.kind, &e.no, &e.change)
	}, "read the entries of a change", `SELECT entity_kind, entity_no, change_kind FROM `+a.t(pgschema.TableChangeEntry)+` WHERE change_seq = $1`, seq)
	var t touched
	for _, e := range rows {
		object, added, changed := e.kind == pgschema.EntityKindObject, e.change == pgschema.ChangeKindAdded, e.change == pgschema.ChangeKindChanged
		switch {
		case object && added:
			t.addedObjects = append(t.addedObjects, e.no)
		case object:
			t.backObjects = append(t.backObjects, e.no)
			if changed {
				t.changedObjects = append(t.changedObjects, e.no)
			}
		case added:
			t.addedLinks = append(t.addedLinks, e.no)
		default:
			t.backLinks = append(t.backLinks, e.no)
			if changed {
				t.changedLinks = append(t.changedLinks, e.no)
			}
		}
	}
	return t, err
}

// revertChange takes one change number back out: the rows it opened go, the rows it closed are open again, and
// every record of the number goes. An abandon also lets go of the keys the number bound; a restore keeps them (a
// key that was bound stays with its object).
func revertChange(ctx context.Context, tx *sql.Tx, a *kgAccess, seq int64, dropKeys bool) error {
	t, err := entriesOf(ctx, tx, a, seq)
	if err != nil {
		return err
	}
	steps := []func() error{
		func() error { return revertObjects(ctx, tx, a, seq, t) },
		func() error { return revertLinks(ctx, tx, a, seq, t) },
	}
	if dropKeys {
		steps = append(steps, func() error {
			return execAll(ctx, tx, []step{{"let go of the keys", a.sub(`DELETE FROM @ WHERE bound_seq = $1`, pgschema.TableObjectKey)}}, seq)
		})
	}
	for _, f := range steps {
		if err := f(); err != nil {
			return err
		}
	}
	return execAll(ctx, tx, []step{
		{"drop the light changes", a.sub(`DELETE FROM @ WHERE change_seq = $1`, pgschema.TableLightChange)},
		{"drop the identifiers", a.sub(`DELETE FROM @ WHERE seq_from = $1`, pgschema.TableObjectIdentifier)},
		{"reopen identifiers", a.sub(`UPDATE @ SET seq_to = NULL WHERE seq_to = $1`, pgschema.TableObjectIdentifier)},
		{"drop the identity events", a.sub(`DELETE FROM @ WHERE change_seq = $1`, pgschema.TableIdentityEvent)},
		{"drop the entries", a.sub(`DELETE FROM @ WHERE change_seq = $1`, pgschema.TableChangeEntry)},
		{"drop the batch", a.sub(`DELETE FROM @ WHERE change_seq = $1`, pgschema.TableChangeBatch)},
	}, seq)
}

type step struct{ what, query string }

func execAll(ctx context.Context, tx *sql.Tx, steps []step, args ...any) error {
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, st.query, args...); err != nil {
			return dbErr(err, st.what)
		}
	}
	return nil
}

// typesOf groups object numbers by the type the registry says they have.
func typesOf(ctx context.Context, tx *sql.Tx, a *kgAccess, nos []int64) (map[int][]int64, error) {
	out := map[int][]int64{}
	if len(nos) == 0 {
		return out, nil
	}
	type pair struct {
		no  int64
		typ int
	}
	rows, err := collect(ctx, tx, func(sc scanner) (pair, error) {
		var p pair
		return p, sc.Scan(&p.no, &p.typ)
	}, "find the types", `SELECT obj_no, type_no FROM `+a.t(pgschema.TableObject)+` WHERE obj_no = ANY($1)`, pq.Array(nos))
	for _, p := range rows {
		out[p.typ] = append(out[p.typ], p.no)
	}
	return out, err
}

func revertObjects(ctx context.Context, tx *sql.Tx, a *kgAccess, seq int64, t touched) error {
	added, err := typesOf(ctx, tx, a, append(append([]int64(nil), t.addedObjects...), t.changedObjects...))
	if err != nil {
		return err
	}
	types := sortedTypes(added)
	for _, typ := range types {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+a.typeTable(typ)+` WHERE obj_no = ANY($1) AND seq_from = $2`, pq.Array(added[typ]), seq); err != nil {
			return dbErr(err, "drop the rows a change opened")
		}
	}
	if len(t.addedObjects) > 0 {
		// An object that was first seen at the number never was; one that came back is retired as it was before.
		if _, err := tx.ExecContext(ctx, a.sub(`UPDATE @ o SET retired_seq = COALESCE((SELECT max(h.seq_to) FROM `, pgschema.TableObject)+a.t(pgschema.TableObjectHist)+
			` h WHERE h.obj_no = o.obj_no AND h.seq_to < $1), $1) WHERE o.obj_no = ANY($2)`, seq, pq.Array(t.addedObjects)); err != nil {
			return dbErr(err, "retire the objects a change added")
		}
	}
	back, err := typesOf(ctx, tx, a, t.backObjects)
	if err != nil {
		return err
	}
	for _, typ := range sortedTypes(back) {
		if err := reopenObjects(ctx, tx, a, typ, back[typ], seq); err != nil {
			return err
		}
	}
	return nil
}

func sortedTypes(m map[int][]int64) []int {
	out := make([]int, 0, len(m))
	for t := range m {
		out = append(out, t)
	}
	sort.Ints(out)
	return out
}

// reopenObjects moves the rows that a change closed back from the history, and gives the registry their name and
// grade again.
func reopenObjects(ctx context.Context, tx *sql.Tx, a *kgAccess, typ int, nos []int64, seq int64) error {
	hist := a.t(pgschema.TableObjectHist)
	if _, err := tx.ExecContext(ctx, a.sub(`UPDATE @ o SET name = h.name, min_grade = h.min_grade, retired_seq = NULL FROM `, pgschema.TableObject)+hist+
		` h WHERE h.seq_to = $1 AND h.obj_no = o.obj_no AND h.obj_no = ANY($2)`, seq, pq.Array(nos)); err != nil {
		return dbErr(err, "give the registry the old names")
	}
	_, err := tx.ExecContext(ctx, `WITH re AS (DELETE FROM `+hist+` WHERE seq_to = $1 AND obj_no = ANY($2) RETURNING obj_no, name, min_grade, doc, source_refs, content_hash, row_hash, seq_from)
		INSERT INTO `+a.typeTable(typ)+` (obj_no, ref, name, min_grade, doc, source_refs, content_hash, row_hash, seq_from)
		SELECT re.obj_no, o.ref, re.name, re.min_grade, re.doc, re.source_refs, re.content_hash, re.row_hash, re.seq_from FROM re JOIN `+a.t(pgschema.TableObject)+` o ON o.obj_no = re.obj_no`,
		seq, pq.Array(nos))
	return dbErr(err, "open the rows a change closed")
}

func revertLinks(ctx context.Context, tx *sql.Tx, a *kgAccess, seq int64, t touched) error {
	link, hist := a.t(pgschema.TableLink), a.t(pgschema.TableLinkHist)
	gone := append(append([]int64(nil), t.addedLinks...), t.changedLinks...)
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+link+` WHERE link_no = ANY($1) AND seq_from = $2`, pq.Array(gone), seq); err != nil {
		return dbErr(err, "drop the links a change opened")
	}
	_, err := tx.ExecContext(ctx, `WITH re AS (DELETE FROM `+hist+` WHERE seq_to = $1 AND link_no = ANY($2) RETURNING `+kgLinkInsertColumns+`)
		INSERT INTO `+link+` (`+kgLinkInsertColumns+`) SELECT `+kgLinkInsertColumns+` FROM re`, seq, pq.Array(t.backLinks))
	return dbErr(err, "open the links a change closed")
}
