package pgstore

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The volatile values of a run (ADR-0099) are written after the apply, when the read watermark has moved: they have no
// history and a revert does not touch them, so they do not ride on the chunks. The staged values are cut into
// intervals by the object number and the attribute number, and each interval is a small apply of its own: the values
// that differ from the kept ones are written in place and one change number holds the light change record of them. A
// value whose value, grade and basis are what is kept is not written, so an interval that is done again changes
// nothing and uses no change number.

// stagedValue is one staged volatile value: the object, its type, the attribute and the entry the source gave (provEntry).
type stagedValue struct {
	objNo  int64
	typeNo int
	attrNo int
	entry  provEntry
	mark   string
}

// ApplyStagedVolatile implements graph.StagedApplier.
func (s *Store) ApplyStagedVolatile(ctx context.Context, v graph.StagedVolatile) (graph.StagedVolatileStep, error) {
	a, err := s.access(ctx, v.KG)
	if err != nil {
		return graph.StagedVolatileStep{}, err
	}
	if err := a.checked(); err != nil {
		return graph.StagedVolatileStep{}, err
	}
	if v.Limit <= 0 {
		return graph.StagedVolatileStep{}, kernel.Errorf(kernel.CodeInvalidValue, "an interval of volatile values needs a limit above 0")
	}
	var step graph.StagedVolatileStep
	var want graph.Head
	err = a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		var err error
		step, want, err = s.stagedVolatileIn(ctx, tx, a, v)
		return err
	})
	if err = s.resolve(ctx, a, want, err); err != nil {
		return graph.StagedVolatileStep{}, err
	}
	return step, nil
}

func (s *Store) stagedVolatileIn(ctx context.Context, tx *sql.Tx, a *kgAccess, v graph.StagedVolatile) (graph.StagedVolatileStep, graph.Head, error) {
	cur, err := s.kgGuard(ctx, tx, a, planned{v.KG, v.Generation, anyHead, v.Lease, v.Now})
	if err != nil {
		return graph.StagedVolatileStep{}, graph.Head{}, err
	}
	if cur.head.Open() {
		return graph.StagedVolatileStep{}, graph.Head{}, kernel.Errorf(graph.CodeApplyInProgress, "apply %s is half done: the volatile values are written after it", cur.head.ApplyID)
	}
	values, err := readStaged(ctx, tx, a, v)
	if err != nil {
		return graph.StagedVolatileStep{}, graph.Head{}, err
	}
	step := graph.StagedVolatileStep{Next: v.After, Done: len(values) < v.Limit}
	if len(values) > 0 {
		last := values[len(values)-1]
		step.Next = graph.StagedVolatileCursor{ObjNo: last.objNo, AttrNo: last.attrNo}
	}
	changed, err := differing(ctx, tx, a, values)
	if err != nil || len(changed) == 0 {
		return step, cur.head, err
	}
	seq := cur.head.AppliedSeq + 1
	c := &kgChunk{writer: newWriter(ctx, tx, v.KG), a: a, seq: seq}
	writeStaged(c, changed)
	kind := v.Kind
	if kind == "" {
		kind = graph.BatchSync
	}
	c.exec("write the change batch", a.sub(kgBatchSQL, pgschema.TableChangeBatch), seq, string(kind), v.SourceID, v.ApplyID, 0, 1,
		cur.head.IngestSeq, v.Lease.Attempt, pgTime(v.Now), 0, 0)
	if c.err != nil {
		return graph.StagedVolatileStep{}, graph.Head{}, c.err
	}
	nh := cur.head.Clone()
	nh.AppliedSeq, nh.ReadableThrough = seq, seq
	if err := saveKGHead(ctx, tx, a, nh); err != nil {
		return graph.StagedVolatileStep{}, graph.Head{}, err
	}
	step.Written, step.Seq = len(changed), seq
	return step, nh, nil
}

// readStaged reads the interval: the limit values after the cursor, in the order of the object and the attribute. The
// value of an object that the registry of the KG does not have is the sign that the apply that makes the objects was not
// done (or not this one): it is refused, and nothing is written.
func readStaged(ctx context.Context, tx *sql.Tx, a *kgAccess, v graph.StagedVolatile) ([]stagedValue, error) {
	values, err := collect(ctx, tx, func(sc scanner) (stagedValue, error) {
		var sv stagedValue
		var entry []byte
		var known bool
		if err := sc.Scan(&sv.objNo, &sv.typeNo, &sv.attrNo, &entry, &known); err != nil {
			return sv, err
		}
		if !known {
			return sv, kernel.Errorf(graph.CodeHeadMoved, "the staged volatile values name the object %d, which the KG does not have", sv.objNo)
		}
		if err := unmarshal(entry, &sv.entry); err != nil {
			return sv, err
		}
		val, err := plainValue(sv.entry.Value)
		if err != nil {
			return sv, err
		}
		sv.entry.Value = val
		sv.mark, err = graph.ValueMark(val)
		return sv, err
	}, "read the staged volatile values", a.stagedSQL(`SELECT sv.{obj_no}, sv.{type_no}, sv.{attr_no}, sv.{entry}, o.obj_no IS NOT NULL FROM {stage_volatile} sv
		LEFT JOIN {object} o ON o.obj_no = sv.{obj_no} AND o.type_no = sv.{type_no}
		WHERE (sv.{obj_no}, sv.{attr_no}) > ($1, $2) ORDER BY sv.{obj_no}, sv.{attr_no} LIMIT $3`), v.After.ObjNo, v.After.AttrNo, v.Limit)
	return values, err
}

// item is the staged value on its way into a table of values.
func (sv stagedValue) item() volatileItem {
	return volatileItem{objNo: sv.objNo, typeNo: sv.typeNo, attrNo: sv.attrNo, value: sv.entry.Value,
		doc: provDoc{Grade: sv.entry.Grade, Basis: sv.entry.Basis, Seen: sv.entry.Seen, Ref: sv.entry.Ref}}
}

// differing keeps the values of the interval that are not what is kept: no kept entry for the attribute, or the kept value (the
// column), grade or basis differs. The comparison is made in the type of the column, so a value that is the same number in
// another form (5 and "5.0") is the same value and is not written again; it is made by the database, a column at a time.
func differing(ctx context.Context, tx *sql.Tx, a *kgAccess, values []stagedValue) ([]stagedValue, error) {
	items := make([]volatileItem, len(values))
	for i, sv := range values {
		items[i] = sv.item()
	}
	groups, err := a.groupValues(items)
	if err != nil {
		return nil, err
	}
	changed := map[[2]int64]bool{}
	for _, g := range groups {
		nos, err := differingOf(ctx, tx, a, g)
		if err != nil {
			return nil, err
		}
		for _, no := range nos {
			changed[[2]int64{no, int64(g.attrNo)}] = true
		}
	}
	var out []stagedValue
	for _, sv := range values {
		if changed[[2]int64{sv.objNo, int64(sv.attrNo)}] {
			out = append(out, sv)
		}
	}
	return out, nil
}

// differingOf is the numbers of the objects of the group whose kept value, grade or basis is not the one of the group.
func differingOf(ctx context.Context, tx *sql.Tx, a *kgAccess, g valueGroup) ([]int64, error) {
	val, err := valueCast(g.kind, "u.v")
	if err != nil {
		return nil, err
	}
	var arr valueArrays
	var grades, bases []string
	for _, it := range g.items {
		if err := arr.add(g.kind, it); err != nil {
			return nil, err
		}
		grades, bases = append(grades, it.doc.Grade), append(bases, it.doc.Basis)
	}
	col, key := pgschema.StateColumn(g.attrNo), strconv.Itoa(g.attrNo)
	return collect(ctx, tx, func(sc scanner) (int64, error) {
		var no int64
		return no, sc.Scan(&no)
	}, "read the kept volatile values", `SELECT u.obj_no FROM unnest($1::bigint[], $2::text[], $3::boolean[], $4::text[], $5::text[]) AS u(obj_no, v, has, g, b)
		LEFT JOIN `+a.t(g.table)+` t ON t.obj_no = u.obj_no
		WHERE t.obj_no IS NULL OR NOT jsonb_exists(t.prov, '`+key+`') OR t.`+col+` IS DISTINCT FROM (CASE WHEN u.has THEN `+val+` END)
			OR t.prov->'`+key+`'->>'g' IS DISTINCT FROM u.g OR t.prov->'`+key+`'->>'b' IS DISTINCT FROM u.b`,
		pq.Array(arr.nos), pq.Array(arr.vals), pq.Array(arr.has), pq.Array(grades), pq.Array(bases))
}

// writeStaged writes the values that differ into the columns of their attributes and the entries of prov (merged into what is
// there), and the light change lines of them under the change number of the interval.
func writeStaged(c *kgChunk, values []stagedValue) {
	items := make([]volatileItem, len(values))
	var nos, attrs []int64
	var marks []string
	for i, sv := range values {
		items[i] = sv.item()
		nos, attrs, marks = append(nos, sv.objNo), append(attrs, int64(sv.attrNo)), append(marks, sv.mark)
	}
	c.writeItems(items)
	slices(len(nos), func(lo, hi int) {
		c.exec("write light changes", `INSERT INTO `+c.a.t(pgschema.TableLightChange)+` (change_seq, obj_no, attr_no, value_mark)
			SELECT $1, u.obj_no, u.attr_no, u.value_mark FROM unnest($2::bigint[], $3::bigint[], $4::text[]) AS u(obj_no, attr_no, value_mark)
			ON CONFLICT (change_seq, obj_no, attr_no) DO UPDATE SET value_mark = EXCLUDED.value_mark`,
			c.seq, pq.Array(nos[lo:hi]), pq.Array(attrs[lo:hi]), pq.Array(marks[lo:hi]))
	})
}
