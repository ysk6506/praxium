package ingest

import (
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"
)

// xsPeakHeap runs f and gives the most the heap held above what it held before, sampled while f runs. The collector is made to
// run often (a quarter more than what is alive), so that what is counted is mostly what f holds and not what it left behind. It is
// the way the memory of the reads of a source is measured elsewhere (hsfPeakHeap), and the limit below is that of a read of a workbook:
// 160 MiB at this setting is 256 MiB at the default one (ADR-0103).
func xsPeakHeap(f func()) uint64 {
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

const xsHeapLimit = 160 << 20

// xsWorstPeak reads the sheet three times and gives the largest of the peaks and the last table.
func xsWorstPeak(c xsCase) (uint64, Table) {
	var worst uint64
	var t Table
	for range 3 {
		worst = max(worst, xsPeakHeap(func() { t = ParseSheet(c.ds, c.wb) }))
	}
	return worst, t
}

// xsDistinct gives n texts of about size bytes that differ from each other, so that none of them is shared.
func xsDistinct(n, size int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = strings.Repeat(ColumnName(i+1)+" ", size/3)
	}
	return out
}

// [REQ-801]
// A header cell with a long text that is merged over a very wide range gives that text to no more columns than a sheet may
// have, once and shared, whatever the address of the range says: the memory of reading the sheet does not follow the width of the
// merge.
func TestXsAHeaderMergedOverAHugeRangeIsReadInTheMemoryOfAFewNames(t *testing.T) {
	long := strings.Repeat("a long title ", 1300) // about 16 KiB
	for _, end := range []int{8192, 1 << 20} {
		c := xsCase{ds: xsDataset(nil, []string{"asset"}, xsColumn("asset", false, "Asset")), wb: xsBook(xsSheet("S", xsRows(
			xsRow(1, "Asset", long), xsRow(2, "#1", "a")), []Merge{{Row1: 1, Col1: 2, Row2: 1, Col2: end}}, nil))}
		peak, tb := xsWorstPeak(c)
		if tb.Reason != "" || len(tb.Rows) != 1 || len(tb.Extra) != DefaultMaxColumnsPerSheet-1 {
			t.Fatalf("merged to column %d: reason %q, %d rows, %d extra names", end, tb.Reason, len(tb.Rows), len(tb.Extra))
		}
		if peak > xsHeapLimit {
			t.Errorf("merged to column %d: the peak of the heap is %d MiB, more than %d MiB", end, peak>>20, xsHeapLimit>>20)
		}
		t.Logf("merged to column %d: the peak of the heap above its start is %d MiB", end, peak>>20)
	}
}

// [REQ-801] [REQ-804]
// A header of two rows whose second row has a name for each column makes a name of its own of the text of the merged cell above
// each of them: a thousand names of 16 KiB. It is over what a scan keeps and is refused before the names are made.
func TestXsAHeaderThatJoinsALongMergedTextWithManyNamesIsRefusedInSmallMemory(t *testing.T) {
	long := strings.Repeat("a long title ", 1300)
	wide := make([]string, 1024)
	for i := range wide {
		wide[i] = "c" + ColumnName(i+1)
	}
	c := xsCase{ds: xsDataset(&SheetOptions{HeaderRows: 2}, []string{"asset"}, xsColumn("asset", false, "Asset")), wb: xsBook(xsSheet("S", xsRows(
		xsRow(1, long), xsRow(2, wide...), xsRow(3, "#1")), []Merge{{Row1: 1, Col1: 1, Row2: 1, Col2: 1 << 20}}, nil))}
	peak, tb := xsWorstPeak(c)
	if !strings.Contains(tb.Reason, "max_kept_bytes") || len(tb.Rows) != 0 {
		t.Fatalf("reason %q, %d rows", tb.Reason, len(tb.Rows))
	}
	if peak > xsHeapLimit {
		t.Errorf("the peak of the heap is %d MiB, more than %d MiB", peak>>20, xsHeapLimit>>20)
	}
	t.Logf("the peak of the heap above its start is %d MiB", peak>>20)
}

// [REQ-804]
// What a scan keeps of a sheet while it reads the rows is bounded: the cells of a header, and the first cells of the merged ranges
// that give their value to the data, are refused when they hold more than the limit. A first cell is given back when its range has
// gone by, so any number of short ranges one after another is read.
func TestXsWhatAScanKeepsIsBoundedAndGivenBack(t *testing.T) {
	zone := xsColumn("zone", false, "Zone")
	key := []string{"asset"}
	texts := xsDistinct(200, 128<<10)

	wideHeader := xsRow(1, append([]string{"Asset"}, texts[:100]...)...)
	over := xsCase{ds: xsDataset(nil, key, xsColumn("asset", false, "Asset")), wb: xsBook(xsSheet("S", xsRows(wideHeader, xsRow(2, "#1")), nil, nil))}
	if tb := ParseSheet(over.ds, over.wb); !strings.Contains(tb.Reason, "max_kept_bytes") {
		t.Errorf("a header of 100 cells of 128 KiB: reason %q", tb.Reason)
	}

	var rows []xsRowSpec
	var long, short []Merge
	rows = append(rows, xsRow(1, "Asset", "Zone"))
	for i := range 200 {
		n := 2 + 2*i
		rows = append(rows, xsRow(n, "#"+ColumnName(i+1), texts[i]), xsRow(n+1, "#"+ColumnName(i+1)+"b"))
		short = append(short, Merge{Row1: n, Col1: 2, Row2: n + 1, Col2: 2})
		if i < 100 {
			long = append(long, Merge{Row1: n, Col1: 2, Row2: 1000, Col2: 2})
		}
	}
	fill := &SheetOptions{MergedCells: MergeFill}
	if tb := ParseSheet(xsDataset(fill, key, xsColumn("asset", false, "Asset"), zone), xsBook(xsSheet("S", rows, long, nil))); !strings.Contains(tb.Reason, "max_kept_bytes") {
		t.Errorf("100 merged ranges of 128 KiB that all reach the data: reason %q", tb.Reason)
	}
	tb := ParseSheet(xsDataset(fill, key, xsColumn("asset", false, "Asset"), zone), xsBook(xsSheet("S", rows, short, nil)))
	if tb.Reason != "" || len(tb.Rows) != 400 {
		t.Fatalf("200 merged ranges of 128 KiB one after another: reason %q, %d rows", tb.Reason, len(tb.Rows))
	}
	if tb.Rows[1].Fields["zone"] != texts[0] {
		t.Errorf("the second row of the first range did not get the text of its first cell")
	}
}
