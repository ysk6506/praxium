package ingest

import (
	_ "embed"
	"encoding/json"
	"strings"
	"testing"
)

// The cases whose answer the scanner changes on purpose (ADR-0103). Their answers are written by hand in
// testdata/xs_changed_scan.json from the rules of the ADR, not made by the code before the scanner.

//go:embed testdata/xs_changed_scan.json
var xsChangedFile []byte

type xsChanged struct {
	name string
	ds   Dataset
	wb   *Workbook
}

func xsChangedCases() []xsChanged {
	key := []string{"asset"}
	std := xsStd()
	asset := []Column{xsColumn("asset", false, "Asset")}
	long := strings.Repeat("a long title ", 1300) // about 16 KiB
	wide := make([]string, 1024)
	for i := range wide {
		wide[i] = "c" + ColumnName(i+1)
	}
	two := &SheetOptions{HeaderRows: 2}
	unordered := xsSheet("S", xsRows(xsRow(1, "Asset", "Name"), xsRow(2, "a", "b")), nil, nil)
	unordered.Rows[1].Cells[0], unordered.Rows[1].Cells[1] = unordered.Rows[1].Cells[1], unordered.Rows[1].Cells[0]
	return []xsChanged{
		{"a header merged past the last column that has a cell names every column it covers", xsDataset(nil, key, asset...), xsBook(xsSheet("S", xsRows(
			xsRow(1, "Asset", "Notes"), xsRow(2, "#1", "a"), xsRow(3, "#2", "b")), []Merge{{Row1: 1, Col1: 2, Row2: 1, Col2: 4}}, nil))},
		{"a merged header is cut at the last column a cell may have", xsDataset(nil, key, asset...), xsBook(xsSheet("S", xsRows(
			xsRow(1, "Asset", "Notes"), xsRow(2, "#1", "a")), []Merge{{Row1: 1, Col1: 2, Row2: 1, Col2: 16384}}, nil))},
		{"rows out of order are an unusable sheet", xsDataset(nil, key, std...), xsBook(xsSheet("S", xsRows(
			xsRow(1, "Asset", "Name"), xsRow(3, "#1", "a"), xsRow(2, "#2", "b")), nil, nil))},
		{"two rows with the same number are an unusable sheet", xsDataset(nil, key, std...), xsBook(xsSheet("S", xsRows(
			xsRow(1, "Asset", "Name"), xsRow(2, "#1", "a"), xsRow(2, "#2", "b")), nil, nil))},
		{"cells out of order in a row are an unusable sheet", xsDataset(nil, key, std...), xsBook(unordered)},
		{"a header that spans many columns over a header of many names is over what a scan keeps", xsDataset(two, key, asset...), xsBook(xsSheet("S", xsRows(
			xsRow(1, long), xsRow(2, wide...), xsRow(3, "#1")), []Merge{{Row1: 1, Col1: 1, Row2: 1, Col2: 1 << 20}}, nil))},
	}
}

// [REQ-800] [REQ-801] [REQ-802] [REQ-804]
func TestXsScanChangesTheAnswerWhereTheAdrSays(t *testing.T) {
	var want []struct {
		Name       string
		Reason     string
		Header     []string
		Extra      []string
		ExtraCount int `json:"extra_count"`
		Rows       int
	}
	if err := json.Unmarshal(xsChangedFile, &want); err != nil {
		t.Fatal(err)
	}
	cases := xsChangedCases()
	if len(want) != len(cases) {
		t.Fatalf("%d answers written, %d cases", len(want), len(cases))
	}
	for i, c := range cases {
		w := want[i]
		if w.Name != c.name {
			t.Fatalf("case %d is %q, written as %q", i, c.name, w.Name)
		}
		got := ParseSheet(c.ds, c.wb)
		if got.Reason != w.Reason {
			t.Errorf("%s: the reason is %q, want %q", c.name, got.Reason, w.Reason)
		}
		if len(got.Rows) != w.Rows {
			t.Errorf("%s: %d rows, want %d", c.name, len(got.Rows), w.Rows)
		}
		if w.Header != nil && strings.Join(got.Header, "|") != strings.Join(w.Header, "|") {
			t.Errorf("%s: the header is %q, want %q", c.name, got.Header, w.Header)
		}
		if w.Extra != nil && strings.Join(got.Extra, "|") != strings.Join(w.Extra, "|") {
			t.Errorf("%s: the extra names are %q, want %q", c.name, got.Extra, w.Extra)
		}
		if w.ExtraCount > 0 && len(got.Extra) != w.ExtraCount {
			t.Errorf("%s: %d extra names, want %d", c.name, len(got.Extra), w.ExtraCount)
		}
	}
}
