package ingest

import (
	"fmt"
	"strings"
	"testing"
)

// The cells of a test sheet are written as text: "x" is text, "#5" a number, "@2026-03-14" a date, "!#N/A" an error
// cell, "?" a formula with no stored value, "" no cell.
func cellOf(col int, s string) (Cell, bool) {
	c := Cell{Col: col}
	switch {
	case s == "":
		return c, false
	case strings.HasPrefix(s, "#"):
		c.Kind, c.Text = CellNumber, s[1:]
	case strings.HasPrefix(s, "@"):
		c.Kind, c.Text = CellDate, s[1:]
	case strings.HasPrefix(s, "!"):
		c.Kind, c.Text = CellError, s[1:]
	case s == "?":
		c.Kind, c.Formula, c.NoValue = CellText, true, true
	default:
		c.Kind, c.Text = CellText, s
	}
	return c, true
}

// sheetOf makes a sheet from rows given by row number; a hidden row has its number in hidden.
func sheetOf(name string, rows map[int][]string, hidden ...int) Sheet {
	sh := Sheet{Name: name, Index: 1, Kind: SheetKindWorksheet, State: SheetVisible}
	for n, cells := range rows {
		r := SheetRow{Number: n}
		for i, s := range cells {
			if c, ok := cellOf(i+1, s); ok {
				r.Cells = append(r.Cells, c)
			}
		}
		for _, h := range hidden {
			if h == n {
				r.Hidden = true
			}
		}
		if len(r.Cells) > 0 {
			sh.Rows = append(sh.Rows, r)
		}
	}
	sh.HiddenRows = len(hidden)
	sh.Sorted()
	return sh
}

func xlsxDataset(opts *SheetOptions, cols ...Column) Dataset {
	d := Dataset{ID: "ds", Parser: ParserXLSX, NaturalKey: []string{cols[0].Name}, Columns: cols, Sheet: opts}
	d, err := d.normalized("src")
	if err != nil {
		panic(err)
	}
	return d
}

func col(name string, paths ...string) Column { return Column{Name: name, Paths: paths} }

func keysOf(t Table) string {
	var out []string
	for _, r := range t.Rows {
		out = append(out, fmt.Sprintf("%s@%s", strings.ReplaceAll(r.Key, keySep, "|"), r.Where))
	}
	return strings.Join(out, " ")
}

// [REQ-286]
func TestTitleAboveTheHeaderAndBlankRowsInTheMiddle(t *testing.T) {
	sh := sheetOf("Servers", map[int][]string{
		1: {"Server list 2031"},
		3: {"Asset", "Name", "Zone"},
		4: {"#10001", "web-01", "A"},
		6: {"#10002", "web-02", "B"},
		7: {" ", "\t"}, // white space only
		9: {"#10003", "db-01", "B"},
	})
	d := xlsxDataset(&SheetOptions{HeaderRow: 3}, col("asset", "Asset"), col("name", "Name"), col("zone", "Zone"))
	tb := ParseSheet(d, &Workbook{Sheets: []Sheet{sh}})
	if got := keysOf(tb); got != "10001@Servers!A4:C4 10002@Servers!A6:C6 10003@Servers!A9:C9" {
		t.Fatalf("rows %s", got)
	}
	if tb.Rows[0].Fields["asset"] != "10001" || tb.Rows[0].Line != 4 || tb.Received() != 3 || len(tb.Unreadable) != 0 {
		t.Fatalf("%+v", tb)
	}
	if tb.Skipped["blank_row"] != 3 || len(tb.Skipped) != 1 { // row 5, row 7 (white space), row 8
		t.Fatalf("skipped %v", tb.Skipped)
	}
}

// [REQ-286]
func TestHeadersAreNormalizedAndAliasesAreUsed(t *testing.T) {
	sh := sheetOf("S", map[int][]string{
		1: {"  Asset\nNo. ", "Rack  Name", "Other"},
		2: {"1", "r1", "x"},
	})
	d := xlsxDataset(nil, col("asset", "Asset No."), col("rack", "Rack name", "Rack Name"))
	tb := ParseSheet(d, &Workbook{Sheets: []Sheet{sh}})
	if len(tb.Missing) != 0 || len(tb.Rows) != 1 || tb.Rows[0].Fields["rack"] != "r1" {
		t.Fatalf("%+v", tb)
	}
	if len(tb.Extra) != 1 || tb.Extra[0] != "Other" {
		t.Fatalf("extra %v", tb.Extra)
	}
	// a header the sheet has no name for is missing
	d = xlsxDataset(nil, col("asset", "Asset No."), col("serial", "Serial"))
	if tb := ParseSheet(d, &Workbook{Sheets: []Sheet{sh}}); len(tb.Missing) != 1 || tb.Missing[0] != "serial" {
		t.Fatalf("missing %v", tb.Missing)
	}
}

// [REQ-286]
func TestHeaderOfTwoRowsIsJoinedAndMergedGroupsSpanTheirColumns(t *testing.T) {
	sh := sheetOf("S", map[int][]string{
		1: {"Asset", "Spec", "", "Owner"},
		2: {"", "CPU", "Memory", ""},
		3: {"1", "8", "64", "ops"},
	})
	sh.Merges = []Merge{{1, 2, 1, 3}}
	d := xlsxDataset(&SheetOptions{HeaderRows: 2}, col("asset", "Asset"), col("cpu", "Spec CPU"), col("mem", "Spec Memory"), col("owner", "Owner"))
	tb := ParseSheet(d, &Workbook{Sheets: []Sheet{sh}})
	if len(tb.Missing) != 0 || len(tb.Rows) != 1 {
		t.Fatalf("%+v", tb)
	}
	if f := tb.Rows[0].Fields; f["cpu"] != "8" || f["mem"] != "64" || f["owner"] != "ops" {
		t.Fatalf("%v", f)
	}
}

// [REQ-286]
func TestMergedCellsAreFilledOrKeptInTheFirstCell(t *testing.T) {
	rows := map[int][]string{1: {"Zone", "Asset"}, 2: {"A", "1"}, 3: {"", "2"}, 4: {"", "3"}, 5: {"B", "4"}}
	for _, mode := range []string{MergeFill, MergeFirstOnly, ""} {
		sh := sheetOf("S", rows)
		sh.Merges = []Merge{{2, 1, 4, 1}}
		d := xlsxDataset(&SheetOptions{MergedCells: mode}, col("asset", "Asset"), col("zone", "Zone"))
		tb := ParseSheet(d, &Workbook{Sheets: []Sheet{sh}})
		var zones []string
		for _, r := range tb.Rows {
			zones = append(zones, r.Fields["zone"])
		}
		want := "A|||B"
		if mode == MergeFill {
			want = "A|A|A|B"
		}
		if got := strings.Join(zones, "|"); got != want {
			t.Errorf("%q: zones %q, want %q", mode, got, want)
		}
	}
}

// [REQ-286]
func TestADataRangeLimitsTheRowsAndColumnsRead(t *testing.T) {
	sh := sheetOf("S", map[int][]string{
		1: {"note"},
		2: {"x", "Asset", "Name", "Extra"},
		3: {"x", "1", "a", "1"},
		4: {"x", "2", "b", "2"},
		5: {"x", "3", "c", "3"},
	})
	d := xlsxDataset(&SheetOptions{HeaderRow: 2, Range: "B3:C4"}, col("asset", "Asset"), col("name", "Name"))
	tb := ParseSheet(d, &Workbook{Sheets: []Sheet{sh}})
	if got := keysOf(tb); got != "1@S!B3:C3 2@S!B4:C4" || len(tb.Extra) != 0 {
		t.Fatalf("%s %v", got, tb.Extra)
	}
	// a column outside the range is a missing column
	d = xlsxDataset(&SheetOptions{HeaderRow: 2, Range: "B3:C4"}, col("asset", "Asset"), col("extra", "Extra"))
	if tb := ParseSheet(d, &Workbook{Sheets: []Sheet{sh}}); len(tb.Missing) != 1 {
		t.Fatalf("%v", tb.Missing)
	}
}

// [REQ-286]
func TestDataEndsAfterEnoughBlankRows(t *testing.T) {
	sh := sheetOf("S", map[int][]string{1: {"Asset"}, 2: {"1"}, 3: {"2"}, 6: {"3"}, 7: {"later notes"}, 9: {"more"}})
	d := xlsxDataset(&SheetOptions{StopAfterBlankRows: 3}, col("asset", "Asset"))
	tb := ParseSheet(d, &Workbook{Sheets: []Sheet{sh}})
	// rows 4 and 5 are two blank rows: not enough to end the data
	if len(tb.Rows) != 5 || tb.Skipped["after_end"] != 0 {
		t.Fatalf("stop 3: %d rows, %v", len(tb.Rows), tb.Skipped)
	}
	d = xlsxDataset(&SheetOptions{StopAfterBlankRows: 2}, col("asset", "Asset"))
	tb = ParseSheet(d, &Workbook{Sheets: []Sheet{sh}})
	if got := keysOf(tb); got != "1@S!A2:A2 2@S!A3:A3" || tb.Skipped["after_end"] != 3 || tb.Skipped["blank_row"] != 2 {
		t.Fatalf("stop 2: %s %v", got, tb.Skipped)
	}
}

// [REQ-286]
func TestHiddenRowsAreReadAndCountedOrSkipped(t *testing.T) {
	sh := sheetOf("S", map[int][]string{1: {"Asset", "Name"}, 2: {"1", "a"}, 3: {"2", "b"}, 4: {"3", "c"}}, 3)
	sh.State = SheetHidden
	sh.HiddenCols = []ColSpan{{2, 2}}
	d := xlsxDataset(nil, col("asset", "Asset"), col("name", "Name"))
	tb := ParseSheet(d, &Workbook{Sheets: []Sheet{sh}})
	if len(tb.Rows) != 3 || tb.HiddenRowsRead != 1 || tb.HiddenColsRead != 1 || tb.Sheet != "S" || tb.SheetState != SheetHidden {
		t.Fatalf("%+v", tb)
	}
	d = xlsxDataset(&SheetOptions{SkipHiddenRows: true}, col("asset", "Asset"), col("name", "Name"))
	tb = ParseSheet(d, &Workbook{Sheets: []Sheet{sh}})
	if len(tb.Rows) != 2 || tb.HiddenRowsRead != 0 || tb.Skipped["hidden_row"] != 1 {
		t.Fatalf("%+v", tb)
	}
}

// [REQ-287]
func TestCellsThatGaveNoValueAreListedWithTheirAddress(t *testing.T) {
	sh := sheetOf("My Sheet", map[int][]string{
		1: {"Asset", "Qty", "Total"},
		2: {"#1", "!#N/A", "?"},
		3: {"", "#4", "#8"},
		4: {"#3", "#5", "#6"},
	})
	d := xlsxDataset(nil, col("asset", "Asset"), col("qty", "Qty"), col("total", "Total"))
	tb := ParseSheet(d, &Workbook{Sheets: []Sheet{sh}})
	var issues []string
	for _, c := range tb.CellIssues {
		issues = append(issues, c.Kind+"@"+c.Where)
	}
	if strings.Join(issues, " ") != "cell_error@'My Sheet'!B2 formula_without_value@'My Sheet'!C2" {
		t.Fatalf("%v", issues)
	}
	if tb.Rows[0].Fields["qty"] != "" || tb.Rows[0].Fields["total"] != "" {
		t.Fatalf("%v", tb.Rows[0].Fields)
	}
	// row 3 has no asset: unreadable, at the cell of the key
	if len(tb.Unreadable) != 1 || tb.Unreadable[0].Where != "'My Sheet'!A3" || tb.Unreadable[0].Reason != "missing_natural_key" || tb.Received() != 3 {
		t.Fatalf("%+v", tb.Unreadable)
	}
}

// [REQ-287]
func TestSheetAddressesAreQuotedLikeExcelDoes(t *testing.T) {
	cases := map[string]string{"Servers": "Servers!C12", "서버목록": "서버목록!C12", "My Sheet": "'My Sheet'!C12", "O'Brien": "'O''Brien'!C12", "a-b": "'a-b'!C12", "S_1": "S_1!C12"}
	for name, want := range cases {
		if got := QualifiedRef(name, "C12"); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
	if CellRef(12, 3) != "C12" || ColumnName(27) != "AA" || ColumnNumber("aa") != 27 || ColumnNumber("A1") != 0 {
		t.Fatal("column names")
	}
	if r, c, ok := ParseCellRef("$B$7"); !ok || r != 7 || c != 2 {
		t.Fatal("cell ref")
	}
	for _, bad := range []string{"", "7", "B", "B0", "1B"} {
		if _, _, ok := ParseCellRef(bad); ok {
			t.Errorf("%q", bad)
		}
	}
}

// [REQ-286]
func TestTheSheetIsPickedByNameByPlaceOrFirstAndMissingOnesAreSaid(t *testing.T) {
	wb := &Workbook{Sheets: []Sheet{
		sheetOf("A", map[int][]string{1: {"K"}, 2: {"a"}}),
		sheetOf("서버", map[int][]string{1: {"K"}, 2: {"b"}}),
		{Name: "Chart", Kind: "other", Index: 3},
	}}
	pick := func(o *SheetOptions) Table { return ParseSheet(xlsxDataset(o, col("k", "K")), wb) }
	if tb := pick(nil); tb.Rows[0].Key != "a" {
		t.Fatalf("first: %+v", tb)
	}
	if tb := pick(&SheetOptions{Name: "서버"}); tb.Rows[0].Key != "b" || tb.Sheet != "서버" {
		t.Fatalf("by name: %+v", tb)
	}
	if tb := pick(&SheetOptions{Index: 2}); tb.Rows[0].Key != "b" {
		t.Fatalf("by place: %+v", tb)
	}
	for _, c := range []struct {
		o    SheetOptions
		want string
	}{{SheetOptions{Name: "Nope"}, `no sheet "Nope" (it has "A", "서버", "Chart")`}, {SheetOptions{Index: 9}, "no sheet number 9"}, {SheetOptions{Index: 3}, "not a worksheet"}} {
		tb := pick(&c.o)
		if !strings.Contains(tb.Reason, c.want) || len(tb.Missing) != 1 {
			t.Errorf("%+v: %q", c.o, tb.Reason)
		}
	}
	if !strings.Contains(shapeReason(Table{Missing: []string{"x"}}), "lacks the columns [x]") {
		t.Fatal("shape reason")
	}
	if sheetNames(&Workbook{}) != "no sheet" {
		t.Fatal("no sheets")
	}
	many := &Workbook{}
	for i := 0; i < 12; i++ {
		many.Sheets = append(many.Sheets, Sheet{Name: fmt.Sprint(i)})
	}
	if !strings.HasSuffix(sheetNames(many), `"9", ...`) {
		t.Fatal(sheetNames(many))
	}
}

// [REQ-286]
func TestSheetOptionsAreChecked(t *testing.T) {
	bad := map[string]*SheetOptions{
		"both name and index": {Name: "a", Index: 1}, "negative index": {Index: -1}, "header row": {HeaderRow: -2},
		"merged_cells": {MergedCells: "spread"}, "range": {Range: "A5"}, "backwards range": {Range: "C5:A9"},
		"range in the header": {HeaderRow: 3, HeaderRows: 2, Range: "A4:C9"}, "stop": {StopAfterBlankRows: -1},
	}
	for name, o := range bad {
		d := Dataset{ID: "ds", Parser: ParserXLSX, NaturalKey: []string{"k"}, Columns: []Column{{Name: "k"}}, Sheet: o}
		if _, err := d.normalized("src"); err == nil || !strings.Contains(err.Error(), "ds") {
			t.Errorf("%s: %v", name, err)
		}
	}
	d := Dataset{ID: "ds", Parser: ParserCSV, NaturalKey: []string{"k"}, Columns: []Column{{Name: "k"}}, Sheet: &SheetOptions{}}
	if _, err := d.normalized("src"); err == nil || !strings.Contains(err.Error(), "belong to the xlsx parser") {
		t.Errorf("sheet options on a csv dataset: %v", err)
	}
	if _, err := ParseSource(strings.NewReader(`{"id":"s","adapter":"file","limits":{"max_rows_per_sheet":-1},"datasets":[{"id":"d","parser":"xlsx","natural_key":["k"],"columns":[{"name":"k"}]}]}`)); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("negative limit: %v", err)
	}
	src, err := ParseSource(strings.NewReader(`{"id":"s","adapter":"file","limits":{"max_rows_per_sheet":5},"datasets":[{"id":"d","parser":"xlsx","natural_key":["k"],"columns":[{"name":"k"}],"sheet":{"name":"X","header_row":2,"merged_cells":"fill"}}]}`))
	if err != nil || src.Limits.MaxRowsPerSheet != 5 || src.Datasets[0].Sheet.HeaderRows != 1 || src.Datasets[0].Sheet.HeaderRow != 2 {
		t.Fatalf("%+v %v", src, err)
	}
}
