package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/xlsx"
)

const bookDir = "../../internal/ingestcheck/testdata/xlsx/"

// sampleWorkbook is the workbook the sample definitions in testdata/xlsx read: a Korean sheet name, a title above the
// header, a merged maker, a number cell for the asset number, a subtotal row, blank rows, a note, a hidden sheet.
func sampleWorkbook(t *testing.T) string {
	t.Helper()
	b := &xlsx.Book{SharedStrings: true, RichText: true}
	s := b.Sheet("서버 목록")
	s.Set("A1", xlsx.Str("서버 현황"))
	s.Merges = []string{"A1:F1", "C4:C6"}
	s.Row(3, xlsx.Str("자산번호"), xlsx.Str("서버명"), xlsx.Str("제조사"), xlsx.Str("시리얼"), xlsx.Str("랙"), xlsx.Str("전원"))
	s.Row(4, xlsx.Num("20001"), xlsx.Str("web-01"), xlsx.Str("Zorvane"), xlsx.Str("SN-A1"), xlsx.Str("RK-1"), xlsx.Str("on"))
	s.Row(5, xlsx.Num("20002"), xlsx.Str("web-02"), xlsx.C{}, xlsx.Str("SN-A2"), xlsx.Str("RK-1"), xlsx.Str("off"))
	s.Row(6, xlsx.Num("20003"), xlsx.Str("web-03"), xlsx.C{}, xlsx.Str("SN-A3"), xlsx.Str("RK-1"), xlsx.Err("#N/A"))
	s.Row(8, xlsx.Num("20004"), xlsx.Str("db-01"), xlsx.Str("Other Maker"), xlsx.Str("SN-B1"), xlsx.Str("RK-2"), xlsx.Uncalculated(`IF(1,"on")`))
	s.Row(9, xlsx.Str("합계"))
	s.Row(12, xlsx.Str("※ 견본"))
	r := b.Sheet("랙")
	r.State = "hidden"
	r.Row(1, xlsx.Str("자산번호"), xlsx.Str("이름"), xlsx.Str("높이(U)"))
	r.Row(2, xlsx.Str("RK-1"), xlsx.Str("rack-1"), xlsx.Num("42"))
	r.Row(3, xlsx.Str("RK-2"), xlsx.Str("rack-2"), xlsx.Num("45"))
	return writeBook(t, b, "book.xlsx")
}

func writeBook(t *testing.T, b *xlsx.Book, name string) string {
	t.Helper()
	data, err := b.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return writeFile(t, name, data)
}

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runCLI(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	d.open = systemOpen
	var o, e bytes.Buffer
	code = execute(args, &o, &e, d)
	return code, o.String(), e.String()
}

// [REQ-288]
func TestIngestInspectShowsTheSheetsColumnsAndFirstRowsOfAWorkbook(t *testing.T) {
	path := sampleWorkbook(t)
	code, out, errOut := runCLI(t, "ingest", "inspect", "-input", path, "-rows", "2")
	if code != exitOK {
		t.Fatalf("%d: %s", code, errOut)
	}
	for _, want := range []string{"format: xlsx", "sheets (2):", "서버 목록", "랙", "hidden", "A1:F12", `table "서버 목록": 6 rows, header in row 3 (guessed; -header-row sets it)`,
		"자산번호", "integer", "FREQUENT VALUES", "first rows:", "web-01", `table "랙": 2 rows, header in row 1`} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
	// one sheet by name, with the header named, as JSON
	code, out, errOut = runCLI(t, "ingest", "inspect", "-input", path, "-sheet", "서버 목록", "-header-row", "3", "-o", "json")
	if code != exitOK {
		t.Fatalf("%d: %s", code, errOut)
	}
	var fp ingest.FileProfile
	if err := json.Unmarshal([]byte(out), &fp); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if fp.Format != "xlsx" || len(fp.Sheets) != 2 || len(fp.Tables) != 1 || fp.Tables[0].HeaderGuess || fp.Tables[0].HeaderRow != 3 || fp.Sheets[1].State != "hidden" {
		t.Fatalf("%+v", fp)
	}
	col := fp.Tables[0].Columns[0]
	if col.Name != "자산번호" || col.Column != "A" || col.ValueType != "mixed" || col.DistinctCount != 6 || col.TopValues[0].Count != 1 {
		t.Fatalf("%+v", col)
	}
}

// [REQ-288]
func TestIngestInspectReadsCSVAndJSONLinesAndSaysWhatItDoesNotUnderstand(t *testing.T) {
	code, out, errOut := runCLI(t, "ingest", "inspect", "-input", sampleDir+"sample/servers.csv")
	if code != exitOK || !strings.Contains(out, "format: csv") || !strings.Contains(out, "serial_number") || strings.Contains(out, "sheets (") {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	code, out, errOut = runCLI(t, "ingest", "inspect", "-input", sampleDir+"sample/vms.jsonl", "-o", "json")
	if code != exitOK || !strings.Contains(out, `"format": "jsonl"`) || !strings.Contains(out, "host.host_id") {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	for name, args := range map[string][]string{
		"no input":               {"ingest", "inspect"},
		"a flag without a value": {"ingest", "inspect", "-input"},
		"an unknown flag":        {"ingest", "inspect", "-input", "x", "-sheets", "1"},
		"a bad format":           {"ingest", "inspect", "-input", "x", "-format", "pdf"},
		"a bad output":           {"ingest", "inspect", "-input", "x", "-o", "yaml"},
		"a bad number":           {"ingest", "inspect", "-input", "x", "-rows", "0"},
	} {
		if code, _, errOut := runCLI(t, args...); code != exitUsage || !strings.Contains(errOut, "usage: praxium ingest inspect") {
			t.Errorf("%s: %d %s", name, code, errOut)
		}
	}
	if code, _, errOut := runCLI(t, "ingest", "inspect", "-input", filepath.Join(t.TempDir(), "missing.xlsx")); code != exitFailure || !strings.Contains(errOut, "cannot read") {
		t.Errorf("a file that is not there: %d %s", code, errOut)
	}
	if code, _, errOut := runCLI(t, "ingest", "inspect", "-input", sampleDir+"sample/servers.csv", "-format", "xlsx"); code != exitFailure || !strings.Contains(errOut, "does not start like a ZIP archive") {
		t.Errorf("a CSV file read as a workbook: %d %s", code, errOut)
	}
}

// [REQ-285] [REQ-288]
func TestFilesThatAreNoWorkbookGiveAClearErrorWhateverTheyAreNamed(t *testing.T) {
	ole := append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 1024)...)
	encrypted := append(append([]byte{}, ole...), []byte("E\x00n\x00c\x00r\x00y\x00p\x00t\x00e\x00d\x00P\x00a\x00c\x00k\x00a\x00g\x00e\x00")...)
	good, err := (&xlsx.Book{}).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, file string
		data       []byte
		want       string
	}{
		{"old binary format", "old.xls", ole, "old binary Excel format (.xls)"},
		{"binary format named csv", "old.csv", ole, "old binary Excel format (.xls)"},
		{"password", "locked.xlsx", encrypted, "has a password"},
		{"cut short", "cut.xlsx", good[:len(good)-20], "damaged or cut short"},
		{"text named xlsx", "notes.xlsx", []byte("a,b\n1,2\n"), "does not start like a ZIP archive"},
	}
	for _, c := range cases {
		path := writeFile(t, c.file, c.data)
		code, _, errOut := runCLI(t, "ingest", "inspect", "-input", path)
		if code != exitFailure || !strings.Contains(errOut, c.want) {
			t.Errorf("inspect %s: %d %s", c.name, code, errOut)
		}
		code, _, errOut = runCLI(t, "ingest", "run", "-source", bookDir+"source.json", "-mapping", bookDir+"mapping.json", "-input", "ds_racks="+path, "-input", "ds_servers="+path)
		if code != exitFailure || !strings.Contains(errOut, "dataset ds_racks: ") || !strings.Contains(errOut, c.want) {
			t.Errorf("run %s: %d %s", c.name, code, errOut)
		}
	}
}

// [REQ-286] [REQ-287]
func TestIngestRunReadsAWorkbookAndShowsWhereWhatWentWrongIs(t *testing.T) {
	path := sampleWorkbook(t)
	args := []string{"ingest", "run", "-source", bookDir + "source.json", "-mapping", bookDir + "mapping.json", "-times", "2",
		"-input", "ds_racks=" + path, "-input", "ds_servers=" + path}
	code, out, errOut := runCLI(t, args...)
	if code != exitOK {
		t.Fatalf("%d: %s", code, errOut)
	}
	for _, want := range []string{"run=1 objects=6 links=4", "changed=true new_version=true", "run=2", "changed=false new_version=false",
		`dataset ds_servers collected=true received=5 unreadable=0 read=5 used=4 filtered=1 problem=0 unused=0`,
		`sheet="서버 목록" sheet_state=visible skipped_after_end=1 skipped_blank_row=3`,
		`dataset ds_racks collected=true received=2`, `sheet="랙" sheet_state=hidden`,
		"problems cell_error=1", "problems formula_without_value=1", "problems undeclared_column=1",
		"problem cell_error dataset=ds_servers key= line=6 where='서버 목록'!F6: the cell of column power_state shows the error #N/A",
		"where='서버 목록'!F8"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
}

// [REQ-285]
func TestIngestRunStopsAtALimitOfTheSourceAndNamesIt(t *testing.T) {
	path := sampleWorkbook(t)
	src, err := os.ReadFile(bookDir + "source.json")
	if err != nil {
		t.Fatal(err)
	}
	tight := writeFile(t, "source.json", []byte(strings.Replace(string(src), `"max_rows_per_sheet": 50000`, `"max_rows_per_sheet": 6`, 1)))
	code, _, errOut := runCLI(t, "ingest", "run", "-source", tight, "-mapping", bookDir+"mapping.json", "-input", "ds_racks="+path, "-input", "ds_servers="+path)
	if code != exitFailure || !strings.Contains(errOut, "dataset ds_racks: ") || !strings.Contains(errOut, "max_rows_per_sheet (6)") || !strings.Contains(errOut, `sheet "서버 목록"`) || !strings.Contains(errOut, "raise limits.max_rows_per_sheet") {
		t.Fatalf("%d: %s", code, errOut)
	}
}

// [REQ-288]
func TestFormatIsToldByTheContentFirstThenTheNameThenTheFirstCharacter(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"a.csv", "PK\x03\x04", "xlsx"}, {"a.txt", "\xd0\xcf\x11\xe0", "xlsx"}, {"a.XLSX", "x", "xlsx"}, {"a.xlsm", "x", "xlsx"},
		{"a.jsonl", "x", "jsonl"}, {"a.ndjson", "x", "jsonl"}, {"a.csv", "{", "csv"}, {"a.dat", " {\"a\":1}", "jsonl"}, {"a.dat", "a,b", "csv"},
	}
	for _, c := range cases {
		if got := detectFormat(c.name, []byte(c.data)); got != c.want {
			t.Errorf("%s %q: %s, want %s", c.name, c.data, got, c.want)
		}
	}
}
