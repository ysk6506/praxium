package pgstore_test

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// [REQ-557] [REQ-555]
func TestAnApplyWhoseCommitGotNoAnswerIsCheckedAgainstTheHeadAndCanBeRepeated(t *testing.T) {
	s, arm := pgstore.NewTestStoreThatLosesACommit(t)
	r := newRigOn(t, s, graph.Options{})
	arm()
	_, err := r.a.Apply(r.ctx, r.kg.ID, r.lease, r.fx.World(1, 1, 3))
	wantCode(t, "a commit that never happened", err, pgconn.CodeConnectionLost)
	h0, err := s.Head(r.ctx, r.kg.ID)
	if err != nil || h0.AppliedSeq != 0 {
		t.Fatalf("the head after the lost commit: %+v %v", h0, err)
	}
	res := r.apply(r.fx.World(1, 1, 3))
	if res.Duplicate {
		t.Fatal("the repeat was taken for a duplicate of a write that never happened")
	}
	h1, err := s.Head(r.ctx, r.kg.ID)
	if err != nil || h1.AppliedSeq == 0 {
		t.Fatalf("the head after the repeat: %+v %v", h1, err)
	}
	unknown := kernel.Errorf(pgconn.CodeOutcomeUnknown, "no answer")
	if err := s.ResolveForTest(r.ctx, r.kg.ID, h1, unknown); err != nil {
		t.Fatalf("a head that shows the write: %v", err)
	}
	wantCode(t, "a head that does not", s.ResolveForTest(r.ctx, r.kg.ID, h0, unknown), pgconn.CodeConnectionLost)
	other := kernel.Errorf(kernel.CodeConflict, "mine")
	if got := s.ResolveForTest(r.ctx, r.kg.ID, h0, other); got != other {
		t.Fatalf("an error that is not unknown changed: %v", got)
	}
	ghost := graph.KGID{}
	if got := s.ResolveForTest(r.ctx, ghost, h0, unknown); got != unknown {
		t.Fatalf("a head that cannot be read left the answer unknown: %v", got)
	}
}
