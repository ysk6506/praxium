package worldgen

import (
	"io"
	"runtime"
	"testing"
)

func heapAfterRun(t *testing.T, scale int64) (records int64, heapMB float64) {
	t.Helper()
	w := smallWorld(t, 51, scale, func(p *Params) { p.Sites = 4 })
	runtime.GC()
	tw := NewTruthWriter(io.Discard)
	var peak uint64
	probe := &heapProbe{peak: &peak}
	if err := w.Generate(tw, probe); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return tw.TotalObjects + tw.TotalLinks, float64(peak) / (1 << 20)
}

// heapProbe samples the heap in use now and then while the world streams.
type heapProbe struct {
	n    int
	peak *uint64
}

func (h *heapProbe) Scope(Scope, int, int, bool) error { return nil }
func (h *heapProbe) Link(*Link) error                  { return nil }
func (h *heapProbe) Object(*Object) error {
	h.n++
	if h.n%5000 == 0 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		*h.peak = max(*h.peak, m.HeapInuse)
	}
	return nil
}

// Memory does not grow with the size of the world: a world ten times larger
// needs about the same heap, and the heap stays small in absolute terms.
func TestStreamingMemoryIsBounded(t *testing.T) {
	small, smallMB := heapAfterRun(t, 100000)
	big, bigMB := heapAfterRun(t, 1000000)
	if big < 8*small {
		t.Fatalf("the big world has %d records, the small one %d", big, small)
	}
	if bigMB > 64 || bigMB > 2*smallMB+8 {
		t.Fatalf("heap grew from %.1f MB to %.1f MB for %d -> %d records", smallMB, bigMB, small, big)
	}
	t.Logf("heap in use: %.1f MB for %d records, %.1f MB for %d records", smallMB, small, bigMB, big)
}
