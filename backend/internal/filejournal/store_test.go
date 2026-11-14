package filejournal

import (
	"context"
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/graphcheck"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The conformance suite of the storage port, on a store that keeps its state in a journal: the default settings, and
// settings that take a snapshot after every write, so the suite also runs with the log cut into pieces all the time.
// [REQ-271]
func TestFileStorePassesTheSuite(t *testing.T) {
	for _, variant := range []struct {
		name string
		ckpt int64
	}{{"log only", 0}, {"snapshot after every write", 1}} {
		factory := func(tb graphcheck.TB) graph.Store {
			s, _, err := openStore(newFakeDisk(), variant.ckpt)
			if err != nil {
				tb.Fatalf("open: %v", err)
			}
			tb.Cleanup(func() {
				if err := s.Close(); err != nil {
					tb.Errorf("close: %v", err)
				}
			})
			return s
		}
		for _, c := range graphcheck.Cases() {
			t.Run(variant.name+"/"+c.Name, func(t *testing.T) { c.Run(t, factory) })
		}
	}
}

// playClean plays the flow on a store of the disk, taking a fingerprint after every record.
func playClean(t *testing.T, d *fakeDisk, ckpt int64, print func(context.Context, graph.Store) string) (refs []string, final string) {
	t.Helper()
	ctx := context.Background()
	s, _, err := openStore(d, ckpt)
	if err != nil {
		t.Fatal(err)
	}
	refs = []string{print(ctx, s)}
	obs := observed{Store: s, after: func() { refs = append(refs, print(ctx, s)) }}
	if err := (flow{ctx, obs}).play(); err != nil {
		t.Fatalf("the flow failed on a disk that never fails: %v", err)
	}
	final = print(ctx, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return refs, final
}

// A store that is closed and opened again on the same folder is the same store: the same heads, hashes, versions,
// every past state and the feeds. Whether the folder is read from a snapshot, from the log or from both.
// [REQ-271]
func TestAStoreReopenedOnTheSameFolderIsTheSameStore(t *testing.T) {
	for _, ckpt := range []int64{0, 1, 3000} {
		t.Run(fmt.Sprintf("checkpoint at %d", ckpt), func(t *testing.T) {
			d := newFakeDisk()
			_, final := playClean(t, d, ckpt, fingerprint)
			s, _, err := openStore(d, ckpt)
			if err != nil {
				t.Fatal(err)
			}
			if got := fingerprint(context.Background(), s); got != final {
				t.Fatalf("the reopened store differs from the closed one")
			}
			verifyStillWorks(t, s)
		})
	}
}

// verifyStillWorks recomputes the graph hash from the rows and applies one more batch: the rows came back whole.
func verifyStillWorks(t *testing.T, s graph.Store) {
	t.Helper()
	ctx := context.Background()
	reg, err := sharedFixture()
	if err != nil {
		t.Fatal(err)
	}
	kgs, _ := s.ListKGs(ctx)
	if len(kgs) != 1 {
		t.Fatalf("%d KGs", len(kgs))
	}
	clock := kernel.NewManualClock(kgs[0].CreatedAt)
	a, err := graph.NewApplier(s, reg.Reg, graph.NewTestKeyProvider("fs"), clock, kernel.NewRandomIDs(kernel.NewSeededRng(99)), graph.Options{Semantic: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Verify(ctx, kgs[0].ID); err != nil {
		t.Fatalf("the graph hash is not what the recovered rows give: %v", err)
	}
	lease, err := a.Acquire(ctx, kgs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Apply(ctx, kgs[0].ID, lease, reg.World(3, 1, 1)); err != nil {
		t.Fatalf("a recovered store takes a new batch: %v", err)
	}
	if err := a.Verify(ctx, kgs[0].ID); err != nil {
		t.Fatal(err)
	}
}

// A store that is not closed (the process was killed) is read back from the log alone.
// [REQ-271]
func TestAStoreThatWasNotClosedIsReadBackFromTheLog(t *testing.T) {
	d := newFakeDisk()
	ctx := context.Background()
	s, _, err := openStore(d, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := (flow{ctx, s}).play(); err != nil {
		t.Fatal(err)
	}
	want := fingerprint(ctx, s)
	killed := d.crash(len(d.pending), tailLost, kernel.NewSeededRng(1))
	s2, _, err := openStore(killed, 0)
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint(ctx, s2) != want {
		t.Fatalf("the store read from the log differs")
	}
	if err := s2.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if fingerprint(ctx, s2) != want {
		t.Fatalf("a checkpoint changed the state")
	}
}

// The store ends in the state of the records that were answered, plus at most the one that was in flight, whenever
// the power goes: at any operation of the disk, and at any byte of any write.
// [REQ-272]
func TestTheStoreAfterAPowerCutHoldsEveryAnsweredWrite(t *testing.T) {
	const ckpt = 2500
	refs, _ := playClean(t, newFakeDisk(), ckpt, fingerprintLite)
	probe := newFakeDisk()
	playClean2(t, probe, ckpt)
	ticks, total := probe.tick, probe.written
	rng := kernel.NewSeededRng(2026)
	type cut struct {
		at, budget int
		after      bool
	}
	var cuts []cut
	for at := 0; at < ticks; at++ {
		cuts = append(cuts, cut{at, -1, false}, cut{at, -1, true})
	}
	for i := 0; i < 60; i++ {
		cuts = append(cuts, cut{-1, int(rng.Uint64() % uint64(total)), false})
	}
	for n, c := range cuts {
		d := newFakeDisk()
		d.crashAt, d.crashAfter, d.byteBudget = c.at, c.after, c.budget
		s, cj, err := openStore(d, ckpt)
		if err != nil {
			t.Fatalf("cut %d: open: %v", n, err)
		}
		ctx := context.Background()
		_ = (flow{ctx, s}).play()
		acked := cj.acked
		for _, keep := range []int{0, len(d.pending)} {
			mode := tailMode(rng.Uint64() % uint64(tailModes))
			crashed := d.crash(keep, mode, rng)
			s2, _, err := openStore(crashed, ckpt)
			if err != nil {
				t.Fatalf("cut %d (%+v keep %d mode %d): recovery failed: %v", n, c, keep, mode, err)
			}
			got := fingerprintLite(ctx, s2)
			if got != refs[acked] && (acked+1 >= len(refs) || got != refs[acked+1]) {
				t.Fatalf("cut %d (%+v keep %d mode %d): the recovered store is neither the %d answered writes nor one more", n, c, keep, mode, acked)
			}
		}
	}
}

// playClean2 plays the flow and keeps the disk, to read its counters.
func playClean2(t *testing.T, d *fakeDisk, ckpt int64) {
	t.Helper()
	s, _, err := openStore(d, ckpt)
	if err != nil {
		t.Fatal(err)
	}
	if err := (flow{context.Background(), s}).play(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
