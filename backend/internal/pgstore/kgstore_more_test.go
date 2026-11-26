package pgstore

import (
	"context"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
)

// More refusals of the writes of the new storage. The names start with v2m.

func v2mGuarded(t *testing.T, r *v2wRig) graph.ChunkWrite {
	t.Helper()
	h, err := r.st.Head(r.ctx, r.kg.ID)
	noErr(t, "head", err)
	return graph.ChunkWrite{KG: r.kg.ID, Generation: 1, Lease: r.lease, ExpectedSeq: h.AppliedSeq, Now: r.clock.Now(), Head: h}
}

func TestV2mAChunkWithAValueThatHasNoPlaceIsRefused(t *testing.T) {
	r := v2wRigOf(t, graph.Options{})
	r.v2wApply(t, r.fx.World(1, 1, 1))
	w := v2mGuarded(t, r)
	sn := r.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadLatest})
	rows, err := sn.ScanObjects(r.ctx, graph.ObjectID{}, 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("scan: %v", err)
	}
	w.Volatile = []graph.VolatileRow{{Object: rows[0].ID, AttrNo: 999999, Value: "x", Grade: "A", Basis: "observed"}}
	wantCode(t, "a value of an attribute without a table", r.st.ApplyChunk(r.ctx, w), kernel.CodeInvalidValue)
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(77))
	other, err := kernel.NewID[graph.ObjectKind](ids, "obj")
	noErr(t, "id", err)
	w.Volatile = []graph.VolatileRow{{Object: other, AttrNo: 1, Value: "x"}}
	wantCode(t, "a value of an object that is not in the registry", r.st.ApplyChunk(r.ctx, w), graph.CodeHeadMoved)
	w.Volatile = nil
	w.Lease = graph.Lease{}
	if err := r.st.ApplyChunk(r.ctx, w); err == nil {
		t.Fatal("a chunk without a lease was taken")
	}
}

func TestV2mASealThatIsNotReadyOrDoesNotFollowIsRefused(t *testing.T) {
	r := v2wRigOf(t, graph.Options{ChunkRows: 7})
	r.v2wApply(t, r.fx.World(1, 1, 1))
	h, err := r.st.Head(r.ctx, r.kg.ID)
	noErr(t, "head", err)
	seal := graph.SealWrite{KG: r.kg.ID, Generation: 1, ExpectedSeq: h.AppliedSeq, Lease: r.lease, Now: r.clock.Now(), Head: h}
	seal.Version.No, seal.Version.SealedSeq = 5, h.ReadableThrough
	wantCode(t, "a seal of a number that does not follow", r.st.Seal(r.ctx, seal), kernel.CodeInvalidValue)
	v2wHalfDone(t, r)
	h2, err := r.st.Head(r.ctx, r.kg.ID)
	noErr(t, "head", err)
	seal.ExpectedSeq, seal.Head = h2.AppliedSeq, h2
	wantCode(t, "a seal of a half done apply", r.st.Seal(r.ctx, seal), graph.CodeSealNotReady)
	missing := graph.KGID{}
	wantCode(t, "a seal of no KG", r.st.Seal(r.ctx, graph.SealWrite{KG: missing}), graph.CodeKGNotFound)
	wantCode(t, "a chunk of no KG", r.st.ApplyChunk(r.ctx, graph.ChunkWrite{KG: missing}), graph.CodeKGNotFound)
	wantCode(t, "an abandon of no KG", r.st.AbandonApply(r.ctx, graph.AbandonWrite{KG: missing}), graph.CodeKGNotFound)
	_, err = r.st.Restore(r.ctx, missing, 0, h2, r.clock.Now())
	wantCode(t, "a restore of no KG", err, graph.CodeKGNotFound)
	wantCode(t, "a resume of no KG", r.st.ResumeWork(r.ctx, missing), graph.CodeKGNotFound)
}

func TestV2mAnUnknownOutcomeOfTheStartOfAWorkIsDecidedByTheHead(t *testing.T) {
	r := v2wRigOf(t, graph.Options{ChunkRows: 7})
	r.v2wApply(t, r.fx.World(1, 1, 1))
	lost := kernel.Errorf(pgconn.CodeOutcomeUnknown, "no answer to the COMMIT")
	ctx := context.Background()
	if err := r.st.resolveWork(ctx, r.a, workRestoring, kernel.Errorf(kernel.CodeInternal, "x")); kernel.CodeOf(err) != kernel.CodeInternal {
		t.Fatalf("an error that is not an unknown outcome was changed: %v", err)
	}
	if err := r.st.resolveWork(ctx, r.a, workRestoring, lost); kernel.CodeOf(err) != pgconn.CodeOutcomeUnknown {
		t.Fatalf("the work did not begin and the error must stay: %v", err)
	}
	h, err := r.st.Head(ctx, r.kg.ID)
	noErr(t, "head", err)
	r.st.hook = func(string, int64) error { return kernel.Errorf(kernel.CodeInternal, "the server died") }
	_, err = r.st.Restore(ctx, r.kg.ID, 0, h, r.clock.Now())
	wantCode(t, "a restore that is cut", err, kernel.CodeInternal)
	r.st.hook = nil
	noErr(t, "the work did begin", r.st.resolveWork(ctx, r.a, workRestoring, lost))
	var gen int
	var work string
	noErr(t, "head row", r.admin.QueryRow(`SELECT generation, work FROM `+r.v2wTable("head")).Scan(&gen, &work))
	if gen != 2 || work != workNone {
		t.Fatalf("the restore was not ended: generation %d, work %q", gen, work)
	}
}

// A table that is gone makes every write and every feed read that needs it fail with an error, not a wrong answer.
func TestV2mWritesAndFeedsFailWhenATableIsGone(t *testing.T) {
	for _, table := range []string{"object", "link", "object_hist", "link_hist", "object_key", "change_batch", "change_entry",
		"light_change", "object_identifier", "identity_event", "ingest_order", "version", "version_hash_part", "generation_event"} {
		t.Run(table, func(t *testing.T) {
			t.Parallel() // every table has a world of its own (ADR-0120)
			r := v2wRigOf(t, graph.Options{ChunkRows: 7})
			r.v2wApply(t, r.fx.World(1, 1, 1))
			v2wHalfDone(t, r)
			h, err := r.st.Head(r.ctx, r.kg.ID)
			noErr(t, "head", err)
			v2rExec(t, r.admin, `DROP TABLE `+r.v2wTable(table)+` CASCADE`)
			failed := 0
			note := func(err error) {
				if err != nil {
					failed++
				}
			}
			_, err = r.st.AppliedChanges(r.ctx, r.kg.ID, 0, 10)
			note(err)
			_, err = r.st.LightChanges(r.ctx, r.kg.ID, 0, 10)
			note(err)
			_, err = r.st.OrderEvents(r.ctx, r.kg.ID, 0, 10)
			note(err)
			note(r.app.Abandon(r.ctx, r.kg.ID, r.lease))
			_, err = r.st.Restore(r.ctx, r.kg.ID, 0, h, r.clock.Now())
			note(err)
			note(r.st.ResumeWork(r.ctx, r.kg.ID))
			_, err = r.app.Apply(r.ctx, r.kg.ID, r.lease, r.fx.World(2, 2, 2))
			note(err)
			if failed == 0 {
				t.Fatalf("nothing failed without the table %s", table)
			}
		})
	}
}
