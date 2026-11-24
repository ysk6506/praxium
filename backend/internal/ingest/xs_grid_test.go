package ingest

import (
	"strings"
	"testing"
)

// The merged ranges of a sheet are kept once by the grid that names the columns of ingest inspect, and not copied to every row they
// cover (ADR-0104).

func xsGridSheet(rows int, merges []Merge) Sheet {
	sh := Sheet{Name: "S", Kind: SheetKindWorksheet, Merges: merges}
	for r := 1; r <= rows; r++ {
		sh.Rows = append(sh.Rows, SheetRow{Number: r, Cells: []Cell{{Col: 1, Kind: CellText, Text: "x"}}})
	}
	return sh
}

// [REQ-810]
func TestXsTheGridFindsTheFirstRangeThatCoversAPlaceAndNoneBeyondTheLastRow(t *testing.T) {
	sh := Sheet{Name: "S", Kind: SheetKindWorksheet, Merges: []Merge{
		{Row1: 3, Col1: 1, Row2: 3, Col2: 4},
		{Row1: 1, Col1: 2, Row2: 2, Col2: 3},
		{Row1: 1, Col1: 1, Row2: 3, Col2: 2},
		{Row1: 9, Col1: 1, Row2: 12, Col2: 2}, // past the last row that has a cell
	}}
	for r, cells := range map[int][]string{1: {"a", "b"}, 2: {"c", "d"}, 3: {"e", "f"}} {
		row := SheetRow{Number: r}
		for i, text := range cells {
			row.Cells = append(row.Cells, Cell{Col: i + 1, Kind: CellText, Text: text})
		}
		sh.Rows = append(sh.Rows, row)
	}
	sortRowsForGrid(&sh)
	g := newGrid(&sh)
	for _, c := range []struct {
		row, col int
		want     string
	}{
		{1, 2, "a"}, // B1 is the first cell of B1:C2, so the next range that covers it counts: A1:B3
		{1, 3, "b"}, // the range B1:C2
		{2, 1, "a"}, // the range A1:B3
		{2, 2, "b"}, // B2 is in B1:C2 (second in the sheet) and in A1:B3 (third): the second counts
		{3, 2, "e"}, // B3 is in A3:D3 (first in the sheet) and in A1:B3: the first counts
		{3, 4, "e"}, // D3 is in A3:D3
		{3, 1, "a"}, // A3 is the first cell of A3:D3, so the next range that covers it counts: A1:B3
		{10, 1, ""}, // below the last row that has a cell
		{4, 1, ""},
	} {
		got, ok := g.originCell(c.row, c.col)
		if (c.want == "") == ok || (ok && got.Text != c.want) {
			t.Errorf("(%d,%d): got %q (%v), want %q", c.row, c.col, got.Text, ok, c.want)
		}
	}
}

func sortRowsForGrid(sh *Sheet) { sh.Sorted() }

// [REQ-810]
func TestXsTheGridOfAManyRowsManyRangesSheetIsReadInSmallMemory(t *testing.T) {
	const rows = 200_000
	merges := make([]Merge, 0, 1000)
	for i := range 1000 {
		merges = append(merges, Merge{Row1: 1 + i, Col1: 2, Row2: rows, Col2: 3})
	}
	sh := xsGridSheet(rows, merges)
	var tab TableProfile
	var worst uint64
	for range 3 {
		worst = max(worst, xsPeakHeap(func() { tab = profileSheet(&sh, 1, 1, 3) }))
	}
	if len(tab.Columns) != 1 || !strings.Contains(tab.Columns[0].Name, "x") {
		t.Fatalf("the columns are %+v", tab.Columns)
	}
	if worst > xsHeapLimit {
		t.Errorf("the peak of the heap is %d MiB, more than %d MiB", worst>>20, xsHeapLimit>>20)
	}
	t.Logf("the peak of the heap above its start is %d MiB", worst>>20)
}
