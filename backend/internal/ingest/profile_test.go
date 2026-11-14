package ingest

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-288]
func TestProfileOfACSVFileHasTheColumnsOfTheDesign(t *testing.T) {
	data := "id,name,qty,when,flag,code\nA-1,web,3,2026-03-14,true,00123\nA-2,web,4.5,2026-03-15T09:30:00Z,false,007\nA-3,,5,2026-03-16,true,\n\nA-4,db,x\n"
	fp, err := Inspect([]byte(data), InspectOptions{Format: FormatCSV, SampleRows: 2}, nil)
	if err != nil || len(fp.Tables) != 1 {
		t.Fatalf("%v %+v", err, fp)
	}
	tb := fp.Tables[0]
	if tb.Rows != 3 || tb.Unreadable != 1 || len(tb.SampleRows) != 2 || len(tb.Columns) != 6 {
		t.Fatalf("%+v", tb)
	}
	want := map[string]struct {
		typ      string
		empty    float64
		distinct int
		shape    string
	}{
		"id":   {TypeText, 0, 3, "A-9"},
		"name": {TypeText, 0.3333, 1, "a+"},
		"qty":  {TypeNumber, 0, 3, "9"},
		"when": {TypeDateTime, 0, 3, "9+-9+-9+"},
		"flag": {TypeBoolean, 0, 2, "a+"},
		"code": {TypeText, 0.3333, 2, "9+"},
	}
	for _, c := range tb.Columns {
		w := want[c.Name]
		if c.ValueType != w.typ || c.EmptyRatio != w.empty || c.DistinctCount != w.distinct || c.Shape != w.shape {
			t.Errorf("%s: %+v, want %+v", c.Name, c, w)
		}
	}
	if top := tb.Columns[1].TopValues; len(top) != 1 || top[0].Value != "web" || top[0].Count != 2 {
		t.Errorf("top values %+v", top)
	}
}

// [REQ-288]
func TestProfileOfAJSONLinesFileFlattensPaths(t *testing.T) {
	data := `{"id":"a","host":{"id":"h1"},"n":1,"tags":["x","y"]}` + "\n" + `{"id":"b","host":{"id":null},"n":2.5}` + "\nnot json\n"
	fp, err := Inspect([]byte(data), InspectOptions{Format: FormatJSONLines}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tb := fp.Tables[0]
	var names []string
	for _, c := range tb.Columns {
		names = append(names, c.Name+":"+c.ValueType)
	}
	if strings.Join(names, " ") != "host.id:text id:text n:number tags.0:text tags.1:text" || tb.Rows != 2 || tb.Unreadable != 1 {
		t.Fatalf("%v %+v", names, tb)
	}
	if tb.Columns[0].EmptyRatio != 0.5 || len(tb.SampleRows) != 2 {
		t.Fatalf("%+v", tb.Columns[0])
	}
}

// [REQ-288]
func TestProfileOfAWorkbookListsSheetsAndGuessesTheHeader(t *testing.T) {
	servers := sheetOf("서버", map[int][]string{
		1: {"Server list"},
		3: {"Asset", "Name", "Spec"},
		4: {"#1", "web-01", "!#N/A"},
		5: {"#2", "web-02", "?"},
		6: {"합계"},
	}, 5)
	servers.Merges = []Merge{{1, 1, 1, 3}}
	servers.HiddenCols = []ColSpan{{3, 3}}
	hidden := sheetOf("Old", map[int][]string{1: {"k", "v"}, 2: {"a", "#1"}})
	hidden.State, hidden.Index = SheetHidden, 2
	chart := Sheet{Name: "Chart", Kind: "other", Index: 3, State: SheetVisible}
	wb := &Workbook{Sheets: []Sheet{servers, hidden, chart}}
	books := &fakeBooks{wb: wb}
	fp, err := Inspect(nil, InspectOptions{Format: FormatXLSX}, books)
	if err != nil {
		t.Fatal(err)
	}
	if len(fp.Sheets) != 3 || len(fp.Tables) != 2 {
		t.Fatalf("%+v", fp)
	}
	s := fp.Sheets[0]
	if s.Name != "서버" || s.UsedRange != "A1:C6" || s.Rows != 6 || s.Columns != 3 || s.HiddenRows != 1 || s.HiddenColumns != 1 || s.MergedRanges != 1 || s.HeaderRow != 3 {
		t.Fatalf("%+v", s)
	}
	if fp.Sheets[1].State != SheetHidden || fp.Sheets[2].Kind != "other" || fp.Sheets[2].UsedRange != "" {
		t.Fatalf("%+v", fp.Sheets)
	}
	tb := fp.Tables[0]
	if !tb.HeaderGuess || tb.HeaderRow != 3 || tb.Rows != 3 || tb.Columns[0].Name != "Asset" || tb.Columns[0].Column != "A" || !tb.Columns[2].Hidden {
		t.Fatalf("%+v", tb)
	}
	if c := tb.Columns[0]; c.ValueType != TypeMixed || c.DistinctCount != 3 {
		t.Fatalf("Asset: %+v", c)
	}
	if c := tb.Columns[2]; c.ErrorCells != 1 || c.UncalculatedCells != 1 || c.ValueType != TypeEmpty || c.EmptyRatio != 1 {
		t.Fatalf("Spec: %+v", c)
	}
	if got := tb.SampleRows[0]; strings.Join(got, "|") != "1|web-01|#N/A" || tb.SampleRows[1][2] != "(never calculated)" {
		t.Fatalf("%v", tb.SampleRows)
	}
	// a header named by the caller, over two rows, and one sheet picked
	one, err := Inspect(nil, InspectOptions{Format: FormatXLSX, Sheet: "2", HeaderRow: 1, HeaderRows: 2}, books)
	if err != nil || len(one.Tables) != 1 || one.Tables[0].Name != "Old" || one.Tables[0].HeaderGuess || one.Tables[0].Columns[0].Name != "k a" || one.Tables[0].Rows != 0 {
		t.Fatalf("%v %+v", err, one.Tables)
	}
}

// [REQ-288]
func TestInspectRefusesWhatItCannotReadAndNamesTheSheetsThereAre(t *testing.T) {
	wb := &Workbook{Sheets: []Sheet{sheetOf("A", map[int][]string{1: {"k"}})}}
	if _, err := Inspect(nil, InspectOptions{Format: FormatXLSX, Sheet: "B"}, &fakeBooks{wb: wb}); kernel.CodeOf(err) != kernel.CodeNotFound || !strings.Contains(err.Error(), `no sheet "B" (it has "A")`) {
		t.Fatalf("%v", err)
	}
	if _, err := Inspect(nil, InspectOptions{Format: FormatXLSX}, nil); kernel.CodeOf(err) != kernel.CodeUnsupported {
		t.Fatalf("%v", err)
	}
	if _, err := Inspect(nil, InspectOptions{Format: FormatXLSX}, &fakeBooks{err: kernel.Errorf(kernel.CodeUnsupported, "old format")}); kernel.CodeOf(err) != kernel.CodeUnsupported {
		t.Fatalf("%v", err)
	}
	if _, err := Inspect(nil, InspectOptions{Format: "pdf"}, nil); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("%v", err)
	}
	empty, err := Inspect(nil, InspectOptions{Format: FormatCSV}, nil)
	if err != nil || empty.Tables[0].Rows != 0 || len(empty.Tables[0].SampleRows) != 0 {
		t.Fatalf("%v %+v", err, empty)
	}
}

// [REQ-288]
func TestProfileHelpers(t *testing.T) {
	shapes := map[string]string{"SN-A100": "A+-A9+", "web-01": "a+-9+", "R1-U10": "A9-A9+", "웹": "L", "": "", "2026-03-14": "9+-9+-9+", "a b": "a a"}
	for in, want := range shapes {
		if got := shapeOf(in); got != want {
			t.Errorf("shape of %q: %q, want %q", in, got, want)
		}
	}
	kinds := map[string]string{"12": TypeInteger, "-3": TypeInteger, "1.5": TypeNumber, "007": TypeText, "0": TypeInteger, "true": TypeBoolean, "2026-03-14": TypeDate,
		"2026-03-14T01:02:03Z": TypeDateTime, "NaN": TypeText, "Inf": TypeText, "abc": TypeText, "0.5": TypeNumber}
	for in, want := range kinds {
		if got := textKind(in); got != want {
			t.Errorf("kind of %q: %q, want %q", in, got, want)
		}
	}
	for c, want := range map[Cell]string{{Kind: CellNumber, Text: "5"}: TypeInteger, {Kind: CellNumber, Text: "5.5"}: TypeNumber, {Kind: CellBool}: TypeBoolean,
		{Kind: CellDate, Text: "2026-03-14"}: TypeDate, {Kind: CellDate, Text: "2026-03-14T00:00:00Z"}: TypeDateTime, {Kind: CellDate, Text: "09:30:00"}: TypeTime, {Kind: CellText}: TypeText} {
		if got := cellType(c); got != want {
			t.Errorf("%+v: %q, want %q", c, got, want)
		}
	}
	if got := typeOfKinds(map[string]int{TypeDate: 2, TypeDateTime: 1}); got != TypeDateTime {
		t.Error(got)
	}
	if got := typeOfKinds(map[string]int{TypeInteger: 2, TypeNumber: 1}); got != TypeNumber {
		t.Error(got)
	}
	if got := typeOfKinds(map[string]int{TypeInteger: 2, TypeText: 1}); got != TypeMixed {
		t.Error(got)
	}
	if clip("abcdefgh", 4) != "abc…" || clip("abc", 4) != "abc" || unnamed(" ", 2) != "(column 3)" {
		t.Error("clip")
	}
}
