package pgstore

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The work an abandon or a restore leaves half done is ended by the server when it starts (ResumeAllWork) and by the
// call that gives out a lease (AcquireLease), ADR-0094. The names of these tests and their helpers start with rsm.

// rsmCut leaves an abandon half done on the rig: the first chunks of an apply are in, the abandon took one number out.
func rsmCut(t *testing.T, r *v2wRig) {
	t.Helper()
	v2wHalfDone(t, r)
	r.st.StopWorkAfter(1, kernel.Errorf(kernel.CodeInternal, "the server died"))
	wantCode(t, "an abandon that is cut", r.app.Abandon(r.ctx, r.kg.ID, r.lease), kernel.CodeInternal)
	r.st.hook = nil
	if work := rsmWork(t, r); work != workAbandoning {
		t.Fatalf("the head says %q after the cut", work)
	}
}

// rsmCutRestore leaves a restore to the first change number half done on the rig.
func rsmCutRestore(t *testing.T, r *v2wRig) {
	t.Helper()
	r.st.StopWorkAfter(1, kernel.Errorf(kernel.CodeInternal, "the server died"))
	_, err := r.app.Restore(r.ctx, r.kg.ID, 1)
	wantCode(t, "a restore that is cut", err, kernel.CodeInternal)
	r.st.hook = nil
	if work := rsmWork(t, r); work != workRestoring {
		t.Fatalf("the head says %q after the cut", work)
	}
}

func rsmWork(t *testing.T, r *v2wRig) string {
	t.Helper()
	var work string
	noErr(t, "work", r.admin.QueryRow(`SELECT work FROM `+r.v2wTable("head")).Scan(&work))
	return work
}

func rsmRequest(r *v2wRig) graph.LeaseRequest {
	return graph.LeaseRequest{KG: r.kg.ID, Owner: r.lease.Owner, TTL: r.lease.Until.Sub(r.clock.Now()), Now: r.clock.Now(), Semantic: 1}
}

func TestRsmALeaseIsGivenAfterTheAbandonThatWasCutIsEnded(t *testing.T) {
	r := v2wRigOf(t, graph.Options{ChunkRows: 7})
	r.v2wApply(t, r.fx.World(1, 1, 1))
	before := v2wState(t, r, true)
	rsmCut(t, r)
	lease, err := r.st.AcquireLease(r.ctx, rsmRequest(r))
	noErr(t, "acquire", err)
	if lease.Attempt != r.lease.Attempt+1 {
		t.Fatalf("the attempt is %d after %d", lease.Attempt, r.lease.Attempt)
	}
	if work := rsmWork(t, r); work != workNone {
		t.Fatalf("the lease was given with the work %q half done", work)
	}
	if got := v2wState(t, r, true); got != before {
		t.Fatalf("the abandon ended in another state:\n%s\n%s", got, before)
	}
}

func TestRsmALeaseIsGivenAfterTheRestoreThatWasCutIsEndedAndTheKGIsOpenAgain(t *testing.T) {
	r := v2wRigOf(t, graph.Options{ChunkRows: 1000})
	r.v2wApply(t, r.fx.World(1, 1, 1))
	mid := v2wState(t, r, false)
	r.v2wApply(t, r.fx.World(1, 2, 2))
	rsmCutRestore(t, r)
	_, err := r.st.Head(r.ctx, r.kg.ID)
	wantCode(t, "the KG while its restore is half done", err, CodeKGRestoring)
	lease, err := r.st.AcquireLease(r.ctx, rsmRequest(r))
	noErr(t, "acquire", err)
	if lease.Attempt != r.lease.Attempt+1 || rsmWork(t, r) != workNone {
		t.Fatalf("lease %+v, work %q", lease, rsmWork(t, r))
	}
	head, err := r.st.Head(r.ctx, r.kg.ID)
	if err != nil || head.AppliedSeq != 1 {
		t.Fatalf("head %+v %v", head, err)
	}
	if got := v2wState(t, r, false); got != mid {
		t.Fatalf("the restore ended in another state:\n%s\n%s", got, mid)
	}
}

func TestRsmALeaseOfAKGWithNoWorkHalfDoneIsGivenAtOnce(t *testing.T) {
	r := v2wRigOf(t, graph.Options{ChunkRows: 7})
	r.v2wApply(t, r.fx.World(1, 1, 1))
	lease, err := r.st.AcquireLease(r.ctx, rsmRequest(r))
	noErr(t, "acquire", err)
	if lease.Attempt != r.lease.Attempt+1 {
		t.Fatalf("the attempt is %d", lease.Attempt)
	}
	other := rsmRequest(r)
	other.Owner = "someone else"
	_, err = r.st.AcquireLease(r.ctx, other)
	wantCode(t, "a lease that another owner holds", err, graph.CodeLeaseHeld)
}

func TestRsmTheServerEndsTheWorkOfEveryKGAndOneThatCannotBeEndedDoesNotStopTheOthers(t *testing.T) {
	r := v2wRigOf(t, graph.Options{ChunkRows: 7})
	r.v2wApply(t, r.fx.World(1, 1, 1))
	rsmCut(t, r)
	second, err := graph.NewTestKG(r.ctx, r.st, r.fx.Reg, kernel.NewRandomIDs(kernel.NewSeededRng(21)), r.clock, graph.KGSpec{Name: "second", Role: graph.RoleCandidate})
	noErr(t, "second KG", err)
	none, err := r.st.ResumeAllWork(r.ctx)
	if err != nil || len(none) != 1 || none[0] != r.kg.ID {
		t.Fatalf("resumed %v, %v: only the first KG has work half done", none, err)
	}
	if rsmWork(t, r) != workNone {
		t.Fatal("the work of the first KG is not ended")
	}
	again, err := r.st.ResumeAllWork(r.ctx)
	if err != nil || len(again) != 0 {
		t.Fatalf("resumed %v, %v: nothing is left", again, err)
	}
	r.v2wApply(t, r.fx.World(1, 2, 2))
	rsmCutRestore(t, r)
	v2rExec(t, r.admin, `ALTER TABLE `+r.v2wTable("object_hist")+` RENAME TO object_hist_away`)
	done, err := r.st.ResumeAllWork(r.ctx)
	if err == nil || len(done) != 0 || kernel.CodeOf(err) == graph.CodeKGNotFound {
		t.Fatalf("resumed %v, %v: the KG whose table is gone must be an error", done, err)
	}
	if msg := err.Error(); !strings.Contains(msg, r.kg.ID.String()) || strings.Contains(msg, second.ID.String()) {
		t.Fatalf("the error names the wrong KG: %v", err)
	}
	v2rExec(t, r.admin, `ALTER TABLE `+r.v2wTable("object_hist_away")+` RENAME TO object_hist`)
	done, err = r.st.ResumeAllWork(r.ctx)
	if err != nil || len(done) != 1 || rsmWork(t, r) != workNone {
		t.Fatalf("after the repair: resumed %v, %v, work %q", done, err, rsmWork(t, r))
	}
}
