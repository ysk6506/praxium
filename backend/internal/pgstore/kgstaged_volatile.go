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

// stagedValue is one staged volatile value: the object, its type, the attribute and the entry of the prov document.
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

// table is the table of values that has the attribute of a staged value: the common one or the one of its type.
func (sv stagedValue) table(a *kgAccess) (kgProvTarget, error) {
	if _, common := a.common[sv.attrNo]; common {
		return kgProvTarget{pgschema.TableObjectState, sv.typeNo}, nil
	}
	if _, own := a.state[sv.typeNo][sv.attrNo]; !own {
		return kgProvTarget{}, kernel.Errorf(kernel.CodeInvalidValue, "the attribute %d of the type %d has no table of values", sv.attrNo, sv.typeNo)
	}
	return kgProvTarget{pgschema.StateTable(sv.typeNo), sv.typeNo}, nil
}

// keptProv is the prov documents of the rows that hold the values of the interval, by table and object number.
type keptProv map[kgProvTarget]map[int64]map[string]provEntry

// keptOf reads the prov documents of the objects of the interval from the tables that hold their values.
func keptOf(ctx context.Context, tx *sql.Tx, a *kgAccess, values []stagedValue) (keptProv, error) {
	byTable := map[kgProvTarget][]int64{}
	for _, sv := range values {
		t, err := sv.table(a)
		if err != nil {
			return nil, err
		}
		byTable[t] = append(byTable[t], sv.objNo)
	}
	kept := keptProv{}
	for t, nos := range byTable {
		kept[t] = map[int64]map[string]provEntry{}
		rows, err := tx.QueryContext(ctx, `SELECT obj_no, prov FROM `+a.t(t.table)+` WHERE obj_no = ANY($1)`, pq.Array(nos))
		if err != nil {
			return nil, dbErr(err, "read the kept volatile values")
		}
		err = scanProv(rows, kept[t])
		if err != nil {
			return nil, err
		}
	}
	return kept, nil
}

func scanProv(rows *sql.Rows, into map[int64]map[string]provEntry) error {
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var no int64
		var doc []byte
		if err := rows.Scan(&no, &doc); err != nil {
			return dbErr(err, "read the kept volatile values")
		}
		var m map[string]provEntry
		if err := unmarshal(doc, &m); err != nil {
			return err
		}
		into[no] = m
	}
	return dbErr(rows.Err(), "read the kept volatile values")
}

// sameAsKept reports whether the kept entry has the value, the grade and the basis of the staged value: the
// comparison of the apply by rows (the mark of the value, the grade, the basis).
func sameAsKept(old provEntry, sv stagedValue) (bool, error) {
	val, err := plainValue(old.Value)
	if err != nil {
		return false, err
	}
	mark, err := graph.ValueMark(val)
	return err == nil && mark == sv.mark && old.Grade == sv.entry.Grade && old.Basis == sv.entry.Basis, err
}

// differing keeps the values of the interval that are not what is kept: the value, the grade or the basis differs,
// or there is no kept value.
func differing(ctx context.Context, tx *sql.Tx, a *kgAccess, values []stagedValue) ([]stagedValue, error) {
	kept, err := keptOf(ctx, tx, a, values)
	if err != nil {
		return nil, err
	}
	var out []stagedValue
	for _, sv := range values {
		t, _ := sv.table(a)
		if old, ok := kept[t][sv.objNo][strconv.Itoa(sv.attrNo)]; ok {
			same, err := sameAsKept(old, sv)
			if err != nil {
				return nil, err
			}
			if same {
				continue
			}
		}
		out = append(out, sv)
	}
	return out, nil
}

// writeStaged writes the values that differ into the prov documents of their rows (merged into what is there), and
// the light change lines of them under the change number of the interval.
func writeStaged(c *kgChunk, values []stagedValue) {
	provs := map[kgProvTarget]map[int64]map[string]provEntry{}
	var nos, attrs []int64
	var marks []string
	for _, sv := range values {
		t, err := sv.table(c.a)
		if err != nil {
			c.fail(err)
			return
		}
		if provs[t] == nil {
			provs[t] = map[int64]map[string]provEntry{}
		}
		if provs[t][sv.objNo] == nil {
			provs[t][sv.objNo] = map[string]provEntry{}
		}
		provs[t][sv.objNo][strconv.Itoa(sv.attrNo)] = sv.entry
		nos, attrs, marks = append(nos, sv.objNo), append(attrs, int64(sv.attrNo)), append(marks, sv.mark)
	}
	for t, byObj := range provs {
		c.writeProv(t.table, t.typeNo, t.table == pgschema.TableObjectState, byObj)
	}
	slices(len(nos), func(lo, hi int) {
		c.exec("write light changes", `INSERT INTO `+c.a.t(pgschema.TableLightChange)+` (change_seq, obj_no, attr_no, value_mark)
			SELECT $1, u.obj_no, u.attr_no, u.value_mark FROM unnest($2::bigint[], $3::bigint[], $4::text[]) AS u(obj_no, attr_no, value_mark)
			ON CONFLICT (change_seq, obj_no, attr_no) DO UPDATE SET value_mark = EXCLUDED.value_mark`,
			c.seq, pq.Array(nos[lo:hi]), pq.Array(attrs[lo:hi]), pq.Array(marks[lo:hi]))
	})
}
