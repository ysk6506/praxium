package ingest

import (
	"errors"
	"strings"
	"testing"
)

// xsScan feeds the rows of a sheet to a scanner as a reader would and gives the steps.
func xsScan(d Dataset, sh Sheet) (*SheetScanner, []ScanStep, error) {
	o := SheetOptions{HeaderRow: 1, HeaderRows: 1, MergedCells: MergeFirstOnly}
	if d.Sheet != nil {
		o = *d.Sheet
	}
	sc := NewSheetScanner(d, o, SheetHead{Index: 1, Name: sh.Name, Kind: sh.Kind, State: sh.State}, SheetLayout{Merges: sh.Merges, HiddenCols: sh.HiddenCols})
	var steps []ScanStep
	for _, r := range sh.Rows {
		st, err := sc.Row(r)
		if err != nil {
			return sc, steps, err
		}
		steps = append(steps, st)
		if st.Done {
			break
		}
	}
	return sc, steps, nil
}

// [REQ-800]
// The scanner gives the record of each row as the row goes by, sets the header at the first data row, and says when the data is
// over so that a reader stops.
func TestXsScannerGivesTheRecordOfEachRowAndSaysWhenTheDataIsOver(t *testing.T) {
	d := xsDataset(&SheetOptions{Range: "A2:C4"}, []string{"asset"}, xsStd()...)
	sh := xsSheet("S", xsRows(xsRow(1, "Asset", "Name", "Zone"), xsRow(2, "#1", "a", "A"), xsRow(4, "#2", "b", "B"), xsRow(5, "#3", "c", "C")), nil, nil)
	sc := NewSheetScanner(d, *d.Sheet, SheetHead{Name: "S"}, SheetLayout{})
	if _, ok := sc.Header(); ok {
		t.Fatal("the header is set before a row of the data came")
	}
	if st, err := sc.Row(sh.Rows[0]); err != nil || st.Done || len(st.Table.Rows) != 0 {
		t.Fatalf("the header row gave %+v, %v", st, err)
	}
	if _, ok := sc.Header(); ok {
		t.Fatal("the header is set by the header row")
	}
	st, err := sc.Row(sh.Rows[1])
	if shape, ok := sc.Header(); !ok || len(shape.Missing) != 0 || strings.Join(shape.Header, ",") != "Asset,Name,Zone" {
		t.Fatalf("the shape is %+v", shape)
	}
	if err != nil || len(st.Table.Rows) != 1 || st.Table.Rows[0].Where != "S!A2:C2" {
		t.Fatalf("the first data row gave %+v, %v", st, err)
	}
	if st, _ = sc.Row(sh.Rows[2]); st.Table.Skipped["blank_row"] != 1 || len(st.Table.Rows) != 1 {
		t.Fatalf("a row after a gap gave %+v", st.Table)
	}
	if st, _ = sc.Row(sh.Rows[3]); !st.Done || len(st.Table.Rows) != 0 {
		t.Fatalf("the row past the range gave %+v", st)
	}
	if st, err = sc.Row(sh.Rows[3]); !st.Done || err != nil {
		t.Fatalf("a row after the end gave %+v, %v", st, err)
	}
	if end := sc.Finish(); end.Header != nil || end.Err != nil || end.HiddenColsRead != 0 {
		t.Fatalf("the end gave %+v", end)
	}
}

// [REQ-800]
// A sheet that has a header and no data row is given its header by the end, and a header that lacks a column ends the data at
// once.
func TestXsScannerSetsTheHeaderAtTheEndOfASheetWithoutData(t *testing.T) {
	d := xsDataset(nil, []string{"asset"}, xsStd()...)
	sc, _, err := xsScan(d, xsSheet("S", xsRows(xsRow(1, "Asset", "Name", "Rack")), nil, []ColSpan{{Min: 2, Max: 2}}))
	if err != nil {
		t.Fatal(err)
	}
	end := sc.Finish()
	if end.Header == nil || strings.Join(end.Header.Extra, ",") != "Rack" || end.HiddenColsRead != 1 {
		t.Fatalf("the end gave %+v", end)
	}
	if again := sc.Finish(); again.Header != nil || again.HiddenColsRead != 1 {
		t.Fatalf("the end asked twice gave %+v", again)
	}

	sc, steps, err := xsScan(d, xsSheet("S", xsRows(xsRow(1, "Asset"), xsRow(2, "#1"), xsRow(3, "#2")), nil, nil))
	if err != nil || len(steps) != 2 || !steps[1].Done {
		t.Fatalf("a header that lacks a column: %+v, %v", steps, err)
	}
	if shape, _ := sc.Header(); strings.Join(shape.Missing, ",") != "name" {
		t.Fatalf("the shape is %+v", shape)
	}
}

// [REQ-802]
// A scan that failed stays failed: the next row gives the same error, and the end tells it.
func TestXsScannerStaysFailedAfterAnError(t *testing.T) {
	d := xsDataset(nil, []string{"asset"}, xsStd()...)
	sh := xsSheet("S", xsRows(xsRow(1, "Asset", "Name"), xsRow(3, "#1", "a"), xsRow(2, "#2", "b")), nil, nil)
	sc, _, err := xsScan(d, sh)
	if err == nil || !strings.Contains(err.Error(), "row 2 after row 3") {
		t.Fatalf("the error is %v", err)
	}
	if _, again := sc.Row(sh.Rows[1]); !errors.Is(again, err) && again.Error() != err.Error() {
		t.Fatalf("the next row gave %v, want %v", again, err)
	}
	if end := sc.Finish(); end.Err == nil || end.Header != nil {
		t.Fatalf("the end gave %+v", end)
	}
}

// [REQ-804]
// A header that is over what a scan keeps is refused where it is made: at the first data row, or at the end of a sheet that has none.
func TestXsScannerRefusesAHeaderOverWhatItKeepsAtTheFirstDataRowOrTheEnd(t *testing.T) {
	big := strings.Repeat("ab  ", 1<<20+1<<19) // 6 MiB, and 4.5 MiB once its white space is gone
	d := xsDataset(nil, []string{"asset"}, xsColumn("asset", false, "Asset"))
	sh := xsSheet("S", xsRows(xsRow(1, "Asset", big), xsRow(2, "#1")), nil, nil)

	if tb := ParseSheet(d, xsBook(sh)); !strings.Contains(tb.Reason, "max_kept_bytes") {
		t.Errorf("at the first data row: reason %q", tb.Reason)
	}
	sh.Rows = sh.Rows[:1]
	if tb := ParseSheet(d, xsBook(sh)); !strings.Contains(tb.Reason, "max_kept_bytes") {
		t.Errorf("at the end: reason %q", tb.Reason)
	}
	sc, _, err := xsScan(d, sh)
	if err != nil {
		t.Fatal(err)
	}
	if end := sc.Finish(); end.Err == nil || end.Header != nil {
		t.Errorf("the end gave %+v", end)
	}

	// The text of a range is kept once with the row it is in and once more when it is made a name: 3.75 MiB, and 2.8 MiB once its white
	// space is gone. The columns of the range of the data take their names from a range that starts left of them.
	mid := strings.Repeat("ab  ", 1<<20-1<<16)
	merged := xsSheet("S", xsRows(xsRow(1, mid), xsRow(2, "", "#1")), []Merge{{Row1: 1, Col1: 1, Row2: 1, Col2: 3}}, nil)
	ranged := xsDataset(&SheetOptions{Range: "B2:C3"}, []string{"asset"}, xsColumn("asset", false, "Asset"))
	if tb := ParseSheet(ranged, xsBook(merged)); !strings.Contains(tb.Reason, "max_kept_bytes") {
		t.Errorf("a merged range whose text is made a name: reason %q", tb.Reason)
	}
}

// [REQ-800]
// A cell that shows an error or has no stored value gives no text to a name, and a place that has such a cell of its own is not
// given the text of a range over it; a range whose first cell is such a cell gives none.
func TestXsAHeaderTakesNoTextFromACellThatHasNoValue(t *testing.T) {
	d := xsDataset(&SheetOptions{HeaderRows: 2}, []string{"asset"}, xsColumn("asset", false, "Asset Tag"))
	sh := xsSheet("S", xsRows(xsRow(1, "Asset", "!#N/A", "?", "Group"), xsRow(2, "Tag", "x", "y", "z"), xsRow(3, "#1")), []Merge{
		{Row1: 1, Col1: 1, Row2: 1, Col2: 1}, {Row1: 1, Col1: 2, Row2: 1, Col2: 3}, {Row1: 1, Col1: 4, Row2: 1, Col2: 5}}, nil)
	tb := ParseSheet(d, xsBook(sh))
	if strings.Join(tb.Header, "|") != "Asset Tag|x|y|Group z|Group" || len(tb.Rows) != 1 {
		t.Fatalf("the header is %q, %d rows", tb.Header, len(tb.Rows))
	}
	for _, first := range []string{"!#N/A", "?"} {
		sh = xsSheet("S", xsRows(xsRow(1, "Asset", first), xsRow(2, "Tag", "x"), xsRow(3, "#1")), []Merge{{Row1: 1, Col1: 2, Row2: 1, Col2: 3}}, nil)
		if tb = ParseSheet(d, xsBook(sh)); strings.Join(tb.Header, "|") != "Asset Tag|x" {
			t.Errorf("a range over a cell that is %q: the header is %q", first, tb.Header)
		}
	}
}

// [REQ-800]
// The profile of a sheet (ingest inspect) names the columns by the same rule as the scanner: a merged header is every column's.
func TestXsTheProfileOfASheetNamesAMergedHeaderLikeTheScanner(t *testing.T) {
	sh := xsSheet("S", xsRows(xsRow(1, "", "Spec", "", "!#N/A", "?"), xsRow(2, "ID", "CPU", "RAM", "x", "y"), xsRow(3, "#1", "8", "32", "a", "b")),
		[]Merge{{Row1: 1, Col1: 2, Row2: 1, Col2: 3}, {Row1: 1, Col1: 4, Row2: 1, Col2: 5}}, nil)
	tab := profileSheet(&sh, 1, 2, 3)
	var names []string
	for _, c := range tab.Columns {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, "|"); got != "ID|Spec CPU|Spec RAM|x|y" {
		t.Fatalf("the columns are %q", got)
	}
}

// [REQ-803]
// A cell that was not given its text is blank when the rule of a blank cell says so, whatever is left of it.
func TestXsACellThatIsNotGivenItsTextIsJudgedByWhatTheReaderSaidOfIt(t *testing.T) {
	for _, c := range []struct {
		cell  Cell
		blank bool
	}{
		{Cell{Kind: CellText, Text: " \t"}, true},
		{Cell{Kind: CellText, NoValue: true}, true},
		{Cell{Kind: CellText, Text: "x"}, false},
		{Cell{Kind: CellError, Text: "#N/A"}, false},
		{Cell{Kind: CellNumber, Text: "0"}, false},
		{Cell{Skipped: true, Blank: true}, true},
		{Cell{Skipped: true}, false},
	} {
		if got := c.cell.blank(); got != c.blank {
			t.Errorf("%+v is blank: %v, want %v", c.cell, got, c.blank)
		}
	}
}

// [REQ-800]
// A dataset that has no sheet options reads the first sheet, with the header in row 1.
func TestXsADatasetWithoutSheetOptionsReadsTheFirstSheetWithTheHeaderInRowOne(t *testing.T) {
	d := Dataset{ID: "ds", Parser: ParserXLSX, NaturalKey: []string{"asset"}, Columns: []Column{xsColumn("asset", false, "Asset")}}
	tb := ParseSheet(d, xsBook(xsSheet("S", xsRows(xsRow(1, "Asset"), xsRow(2, "#1")), nil, nil)))
	if tb.Reason != "" || len(tb.Rows) != 1 || tb.HeaderRow != 1 {
		t.Fatalf("reason %q, %d rows, header row %d", tb.Reason, len(tb.Rows), tb.HeaderRow)
	}
}
