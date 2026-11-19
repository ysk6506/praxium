package httpsource

import (
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// hsfPeakHeap runs f and gives the most the heap held above what it held before, sampled while f runs. The collector is
// made to run often, so that what is counted is mostly what f holds and not what it left behind.
func hsfPeakHeap(f func()) uint64 {
	old := debug.SetGCPercent(25)
	defer debug.SetGCPercent(old)
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	base := m.HeapAlloc
	var mu sync.Mutex
	peak := base
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Millisecond):
				var s runtime.MemStats
				runtime.ReadMemStats(&s)
				mu.Lock()
				peak = max(peak, s.HeapAlloc)
				mu.Unlock()
			}
		}
	}()
	f()
	close(stop)
	<-done
	mu.Lock()
	defer mu.Unlock()
	if peak < base {
		return 0
	}
	return peak - base
}

// hsfSwollen is the body that a parser that holds the document whole pays about ninety times its size for: millions of
// empty arrays that are the records.
func hsfSwollen() string {
	return `{"items":[` + strings.Repeat("[],", (15<<20)/3) + `[]]}`
}

// hsfDrain reads every page of the first dataset and keeps none, and gives how many rows and unreadable lines came.
func hsfDrain(g *hsRig) (lines int, err error) {
	for p, e := range g.hsReader.ReadPages(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PageOptions{}) {
		if e != nil {
			return lines, e
		}
		lines += p.Received()
	}
	return lines, nil
}

// [REQ-692]
// The memory of reading an answer does not depend on how big the answer is: an answer of a few million records that each
// would be ninety times their size once parsed is read with a heap that holds a record and a page. The peak is measured
// three times, with the collector run often, and the limit is wide (read as a whole the answer took 300 MiB at this setting of the collector, and more than 1,300 MiB at the default).
func TestHsfTheMemoryOfAReadDoesNotFollowTheSizeOfTheAnswer(t *testing.T) {
	body := hsfSwollen()
	var peaks []uint64
	for range 3 {
		g := hsNew(t, hsDoc("", "", "", ""), func(int, hsRequest) hsReply { return hsJSON(body) })
		var lines int
		var err error
		peaks = append(peaks, hsfPeakHeap(func() { lines, err = hsfDrain(g) }))
		if err != nil || lines != (15<<20)/3+1 {
			t.Fatalf("%d lines, %v", lines, err)
		}
	}
	sort.Slice(peaks, func(i, j int) bool { return peaks[i] < peaks[j] })
	t.Logf("the peaks of the heap above its start, in MiB: %d %d %d", peaks[0]>>20, peaks[1]>>20, peaks[2]>>20)
	const limit = 64 << 20
	if peaks[1] > limit {
		t.Fatalf("the median peak is %d MiB, more than %d MiB", peaks[1]>>20, limit>>20)
	}
}
