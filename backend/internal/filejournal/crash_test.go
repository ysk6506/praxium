package filejournal

import (
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// bigPlan is long enough for three checkpoints and segments in between.
func bigPlan() plan {
	return plan{items: payloads(14, 5), ckpt: map[int]bool{3: true, 7: true, 11: true}}
}

// dryRun plays a plan on a disk that never fails and says how many operations and bytes it took.
func dryRun(t *testing.T, p plan) (ticks, bytes int) {
	t.Helper()
	d := newFakeDisk()
	j := openEmpty(t, d)
	if acked := p.run(j); acked != len(p.items) {
		t.Fatalf("a disk that never fails acked %d of %d", acked, len(p.items))
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	return d.tick, d.written
}

// checkAfterCrash recovers the folder a power cut left and checks the one promise: every record that was answered
// is there, and past them at most the one record that was in flight. Then it appends once more and recovers again,
// which shows the log is whole behind a cut record.
func checkAfterCrash(t *testing.T, label string, p plan, acked int, crashed *fakeDisk, deep bool) {
	t.Helper()
	j, got, rep, err := recoverState(crashed)
	if err != nil {
		t.Fatalf("%s: recovery failed: %v", label, err)
	}
	want := p.items[:acked]
	if !got.equal(want) {
		inflight := state(nil)
		if acked < len(p.items) {
			inflight = p.items[:acked+1]
		}
		if !got.equal(inflight) {
			t.Fatalf("%s: recovered %d items, want the %d answered (or %d with the one in flight); report %+v", label, len(got), acked, acked+1, rep)
		}
	}
	if !deep {
		_ = j.Close()
		return
	}
	extra := []byte("after the crash")
	if _, err := j.Append(extra); err != nil {
		t.Fatalf("%s: append after recovery: %v", label, err)
	}
	if err := j.Close(); err != nil {
		t.Fatalf("%s: close: %v", label, err)
	}
	j2, again, _, err := recoverState(crashed)
	if err != nil {
		t.Fatalf("%s: second recovery failed: %v", label, err)
	}
	if want := append(append(state(nil), got...), extra); !again.equal(want) {
		t.Fatalf("%s: second recovery has %d items, want %d", label, len(again), len(want))
	}
	if err := j2.Close(); err != nil {
		t.Fatal(err)
	}
}

// [REQ-272]
func TestPowerCutAtEveryOperation(t *testing.T) {
	p := bigPlan()
	ticks, _ := dryRun(t, p)
	cases := 0
	for at := 0; at < ticks; at++ {
		for _, after := range []bool{false, true} {
			d := newFakeDisk()
			d.crashAt, d.crashAfter = at, after
			j := openEmptyOn(t, d)
			acked := p.run(j)
			for keep := 0; keep <= len(d.pending); keep++ {
				for mode := tailLost; mode < tailModes; mode++ {
					rng := kernel.NewSeededRng(uint64(at*1000 + keep*10 + int(mode)))
					label := fmt.Sprintf("operation %d after=%v keep=%d mode=%d", at, after, keep, mode)
					checkAfterCrash(t, label, p, acked, d.crash(keep, mode, rng), mode == tailPrefix)
					cases++
				}
			}
		}
	}
	if cases < 500 {
		t.Fatalf("only %d cases ran", cases)
	}
}

// openEmptyOn opens a fresh folder that is set to fail later; recovery of an empty folder does not touch the plan.
func openEmptyOn(t *testing.T, d *fakeDisk) *Journal {
	t.Helper()
	at, after, budget := d.crashAt, d.crashAfter, d.byteBudget
	d.crashAt, d.byteBudget = -1, -1
	j := openEmpty(t, d)
	d.tick, d.written = 0, 0
	d.crashAt, d.crashAfter, d.byteBudget = at, after, budget
	return j
}

// [REQ-272]
func TestWriteCutAtEveryByte(t *testing.T) {
	p := bigPlan()
	_, total := dryRun(t, p)
	rng := kernel.NewSeededRng(77)
	cases := 0
	for cut := 0; cut < total; cut++ {
		d := newFakeDisk()
		d.byteBudget = cut
		j := openEmptyOn(t, d)
		acked := p.run(j)
		keeps := []int{len(d.pending)}
		if cut%4 == 0 {
			keeps = append(keeps, 0)
		}
		for _, keep := range keeps {
			mode := tailMode(rng.Uint64() % uint64(tailModes))
			checkAfterCrash(t, fmt.Sprintf("byte %d keep=%d mode=%d", cut, keep, mode), p, acked, d.crash(keep, mode, rng), mode == tailPrefix)
			cases++
		}
	}
	if cases < 700 {
		t.Fatalf("only %d cases ran", cases)
	}
}

// A journal that failed a write takes no more records, and a restart finds the folder in a state it can read.
// [REQ-272]
func TestJournalRefusesWritesAfterAFailure(t *testing.T) {
	d := newFakeDisk()
	j := openEmpty(t, d)
	if _, err := j.Append([]byte("one")); err != nil {
		t.Fatal(err)
	}
	d.crashAt, d.crashAfter = d.tick, false
	_, err := j.Append([]byte("two"))
	wantCode(t, err, CodeIO)
	d.crashAt = -1
	d.dead = false
	_, err = j.Append([]byte("three"))
	wantCode(t, err, CodeIO)
	wantCode(t, j.Checkpoint(nil), CodeIO)
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	_, got, _ := mustRecover(t, d)
	if !got.equal(state{[]byte("one")}) {
		t.Fatalf("recovered %d items after a failed write", len(got))
	}
}
