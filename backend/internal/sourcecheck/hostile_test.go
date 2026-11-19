package sourcecheck

import (
	"context"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// scWaitGoroutines waits for the goroutines of the test to go back to the count they had before the read.
func scWaitGoroutines(t *testing.T, base int) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if runtime.NumGoroutine() <= base {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	buf := make([]byte, 1<<16)
	t.Fatalf("goroutines are left after the read: %d, were %d\n%s", runtime.NumGoroutine(), base, buf[:runtime.Stack(buf, true)])
}

// [REQ-674] A source that sends a message of a length that cannot be, a data row that is cut short, rows with fewer
// columns than the statement has, or a count of rows without a key that names a column the statement does not have,
// ends the read with the kind response_format. The driver panics on some of these; the panic ends this read and nothing
// else, and no text of the answer is in the error.
func TestScHostileAnswersEndTheReadAsResponseFormat(t *testing.T) {
	for _, scenario := range []string{"neglen", "shortmsg", "fewcols", "nullidx"} {
		t.Run(scenario, func(t *testing.T) {
			f := scNewFake(t)
			rd, ds := scReader(t, scFakeSource(f, scenario), "SELECT 1 AS id", &scLoopback{})
			base := runtime.NumGoroutine()
			res := scRun(scCtx, rd, ds, ingest.PageOptions{})
			scSrcErr(t, res.err, ingest.SrcResponseFormat)
			if strings.Contains(res.err.Error(), "panic") || strings.Contains(res.err.Error(), "runtime") {
				t.Fatalf("the error tells of the panic: %v", res.err)
			}
			scWaitGoroutines(t, base)
		})
	}
}

// [REQ-674] A key that fills a page and has no row when its rows are asked for (the sql of a dataset gives other rows in
// one statement than in another, review R1) is a source that changed: the dataset ends as one that is not complete, at
// the page before, and the read does not fail or loop.
func TestScRowsOfAKeyThatAreGoneEndTheDatasetIncomplete(t *testing.T) {
	f := scNewFake(t)
	rd, ds := scReader(t, scFakeSource(f, "nonebatch"), "SELECT 5 AS id", &scLoopback{})
	res := scRun(scCtx, rd, ds, ingest.PageOptions{MaxRows: 2})
	if res.err != nil {
		t.Fatalf("the read failed: %v", res.err)
	}
	last := res.pages[len(res.pages)-1]
	if !last.Last || last.End.Complete || !strings.Contains(last.End.Reason, "changed") {
		t.Fatalf("the dataset is not told as incomplete because the source changed: %+v", last.End)
	}
}

// [REQ-674] Every transaction of a read is REPEATABLE READ READ ONLY.
func TestScEveryTransactionIsRepeatableReadReadOnly(t *testing.T) {
	f := scNewFake(t)
	rd, ds := scReader(t, scFakeSource(f, "nonebatch"), "SELECT 5 AS id", &scLoopback{})
	scRun(scCtx, rd, ds, ingest.PageOptions{MaxRows: 2})
	begins := 0
	for _, q := range f.queries() {
		if !strings.HasPrefix(q, "BEGIN") {
			continue
		}
		begins++
		if up := strings.ToUpper(q); !strings.Contains(up, "REPEATABLE READ") || !strings.Contains(up, "READ ONLY") {
			t.Fatalf("a transaction is not repeatable read and read only: %s", q)
		}
	}
	if begins == 0 {
		t.Fatalf("no transaction was begun: %v", f.queries())
	}
}

// [REQ-673] A source that goes silent in the middle of a page does not hold the read past the time of the transaction: the
// read returns with the kind timeout, and no goroutine of it is left.
func TestScSilentSourceReturnsWithinTheDeadlineAndLeavesNoGoroutine(t *testing.T) {
	f := scNewFake(t)
	src := scFakeSource(f, "silent")
	src.statement = 1 // the transaction may take 5 statements and 10 seconds
	rd, ds := scReader(t, src, "SELECT 1 AS id", &scLoopback{})
	base := runtime.NumGoroutine()
	res := scRun(scCtx, rd, ds, ingest.PageOptions{})
	scSrcErr(t, res.err, ingest.SrcTimeout)
	scWaitGoroutines(t, base)
}

// [REQ-673] A row that is larger than the bytes one transaction may read is cut: the read ends with the kind
// response_format. The peak of the heap is measured and reported (the driver makes the buffer of a message of the size
// the message says before it reads it).
func TestScRowLargerThanTheBudgetIsCut(t *testing.T) {
	f := scNewFake(t)
	f.big = scDataRow(strings.Repeat("x", 40<<20), "1")
	rd, ds := scReader(t, scFakeSource(f, "bigrow"), "SELECT 1 AS id", &scLoopback{})
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var peak atomic.Uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peak.Load() {
				peak.Store(m.HeapAlloc)
			}
			select {
			case <-stop:
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
	res := scRun(scCtx, rd, ds, ingest.PageOptions{MaxBytes: 1 << 20})
	close(stop)
	<-done
	scSrcErr(t, res.err, ingest.SrcResponseFormat)
	grown := int64(peak.Load()) - int64(before.HeapAlloc)
	t.Logf("a row of 40 MiB, a budget of %d bytes: the peak of the heap grew by %d MiB (the message of the fake is not in it)", 4*(1<<20)+(1<<20), grown>>20)
	if grown > 200<<20 {
		t.Fatalf("the heap grew by %d MiB for one row", grown>>20)
	}
}

// [REQ-673] The request that cancels a statement of a source without TLS is a connection of its own, and it goes through
// the policy of the dialer like the first one: the name is looked up again.
func TestScPlaintextCancelRequestGoesThroughThePolicy(t *testing.T) {
	f := scNewFake(t)
	res := &scLoopback{}
	rd, ds := scReader(t, scFakeSource(f, "silent"), "SELECT 1 AS id", res)
	ctx, cancel := context.WithCancel(scCtx)
	defer cancel()
	out := make(chan scResult, 1)
	go func() { out <- scRun(ctx, rd, ds, ingest.PageOptions{}) }()
	<-f.silent
	cancel()
	<-out
	for i := 0; i < 500 && f.cancels() == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	cancels := int64(f.cancels())
	if cancels < 1 {
		t.Fatal("the source got no cancel request")
	}
	if n := res.lookups.Load(); n < 1+cancels {
		t.Fatalf("the name was looked up %d times for %d cancel requests and the first connection: a cancel request did not go through the dialer", n, cancels)
	}
}
