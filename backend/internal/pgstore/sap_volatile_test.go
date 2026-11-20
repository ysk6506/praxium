package pgstore

import (
	"context"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
)

// sapInterval writes one interval of the staged volatile values.
func sapInterval(t *testing.T, k *sapKG, after graph.StagedVolatileCursor, limit int) (graph.StagedVolatileStep, error) {
	t.Helper()
	return k.st.ApplyStagedVolatile(context.Background(), graph.StagedVolatile{KG: k.kg.ID, Generation: k.kg.Generation, Lease: k.lease,
		Now: k.clock.Now(), ApplyID: "sap-run", Kind: graph.BatchSync, SourceID: "sap", After: after, Limit: limit})
}

// The volatile values are written after the apply, in intervals: every staged value is written once, an interval with
// values that differ from the kept ones has one change number of its own with their light change lines, and an
// interval that is done again (the same values) writes nothing and uses no change number. A value that changes is
// written alone.
//
// [REQ-727]
func TestSapVolatileValuesGoInIntervalsAndAreWrittenOnlyWhenTheyDiffer(t *testing.T) {
	p := sapStaged(t)
	ctx := context.Background()
	k := p.staged
	_, err := sapInterval(t, k, graph.StagedVolatileCursor{}, 100)
	wantCode(t, "an interval before the apply that makes the objects", err, graph.CodeHeadMoved)
	if h, _ := k.st.Head(ctx, k.kg.ID); h.AppliedSeq != 0 {
		t.Fatalf("the refused interval moved the head: %+v", h)
	}
	sapRun(t, k, "sap-run")
	staged := sapCount(t, k, `SELECT count(*) FROM {stage_volatile}`)
	head, err := k.st.Head(ctx, k.kg.ID)
	noErr(t, "head", err)
	const limit = 300
	var after graph.StagedVolatileCursor
	var seqs []int64
	written, steps := 0, 0
	for {
		step, err := sapInterval(t, k, after, limit)
		noErr(t, "interval", err)
		steps++
		written += step.Written
		if step.Written > 0 {
			seqs = append(seqs, step.Seq)
		}
		if step.Done {
			break
		}
		if step.Next == after {
			t.Fatal("an interval that is not the last did not move the cursor")
		}
		after = step.Next
	}
	if want := (staged + limit - 1) / limit; steps < want || written != staged {
		t.Fatalf("%d intervals and %d values written of %d staged", steps, written, staged)
	}
	for i, s := range seqs {
		if s != head.AppliedSeq+int64(i)+1 {
			t.Fatalf("the change numbers of the intervals: %v after %d", seqs, head.AppliedSeq)
		}
	}
	if n := sapCount(t, k, `SELECT count(DISTINCT change_seq) FROM {light_change}`); n != len(seqs) {
		t.Fatalf("%d change numbers hold light change lines, %d intervals wrote", n, len(seqs))
	}
	if n := sapCount(t, k, `SELECT count(*) FROM {light_change}`); n != staged {
		t.Fatalf("%d light change lines for %d values", n, staged)
	}
	sapVolatileAgainWritesNothing(t, k, limit)
	sapOneVolatileValueChanges(t, k)
}

// sapVolatileAgainWritesNothing does every interval again: no value is written and the head does not move.
func sapVolatileAgainWritesNothing(t *testing.T, k *sapKG, limit int) {
	t.Helper()
	ctx := context.Background()
	before, err := k.st.Head(ctx, k.kg.ID)
	noErr(t, "head", err)
	var after graph.StagedVolatileCursor
	for {
		step, err := sapInterval(t, k, after, limit)
		noErr(t, "interval again", err)
		if step.Written != 0 || step.Seq != 0 {
			t.Fatalf("an interval that is done again wrote %d values at %d", step.Written, step.Seq)
		}
		if step.Done {
			break
		}
		after = step.Next
	}
	now, err := k.st.Head(ctx, k.kg.ID)
	noErr(t, "head", err)
	if now.AppliedSeq != before.AppliedSeq || now.ReadableThrough != before.ReadableThrough || !now.Hash.Equal(before.Hash) {
		t.Fatalf("the head moved: %+v then %+v", before, now)
	}
}

// sapOneVolatileValueChanges stages a new value for one object and attribute and runs the intervals: one value is
// written, under one change number.
func sapOneVolatileValueChanges(t *testing.T, k *sapKG) {
	t.Helper()
	ctx := context.Background()
	before, err := k.st.Head(ctx, k.kg.ID)
	noErr(t, "head", err)
	sapExec(t, k, `UPDATE {stage_volatile} SET {entry} = jsonb_set({entry}, '{v}', '"sap-changed"') WHERE ({obj_no}, {attr_no}) = (SELECT {obj_no}, {attr_no} FROM {stage_volatile} ORDER BY 1, 2 OFFSET 5 LIMIT 1)`)
	written, seq := 0, int64(0)
	var after graph.StagedVolatileCursor
	for {
		step, err := sapInterval(t, k, after, 50)
		noErr(t, "interval", err)
		written += step.Written
		if step.Seq != 0 {
			seq = step.Seq
		}
		if step.Done {
			break
		}
		after = step.Next
	}
	now, err := k.st.Head(ctx, k.kg.ID)
	noErr(t, "head", err)
	if written != 1 || seq != before.AppliedSeq+1 || now.AppliedSeq != seq || now.ReadableThrough != seq {
		t.Fatalf("one value changed: written %d at %d, head %+v after %+v", written, seq, now, before)
	}
}
