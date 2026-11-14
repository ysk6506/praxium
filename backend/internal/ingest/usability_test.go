package ingest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

func datasetReport(rep Report, id string) DatasetReport {
	for _, d := range rep.Datasets {
		if d.Dataset == id {
			return d
		}
	}
	return DatasetReport{}
}

// [REQ-320]
func TestASheetTheWorkbookLacksIsReportedWithTheSheetsItHas(t *testing.T) {
	r, books := bookRig(t)
	books.wb.Sheets[0].Name = "Rack sheet"
	rep := r.run()
	racks := datasetReport(rep, "ds_racks")
	if racks.NotRead == "" || !strings.Contains(racks.NotRead, `no sheet "Racks"`) || !strings.Contains(racks.NotRead, `"Rack sheet", "Servers"`) {
		t.Fatalf("the report must say the sheet is missing and which sheets there are: %+v", racks)
	}
	if rep.ProblemCounts[ProblemShapeChanged] != 1 || racks.Collected != true || racks.Applied {
		t.Fatalf("a missing sheet is a shape change of the dataset: %+v %v", racks, rep.ProblemCounts)
	}
	if rep.Problems[0].Kind != ProblemShapeChanged || rep.Problems[0].Reason != racks.NotRead {
		t.Fatalf("the problem comes first and says the same: %+v", rep.Problems[0])
	}
	if datasetReport(rep, "ds_servers").NotRead != "" {
		t.Fatal("the other dataset is read")
	}
}

// [REQ-320]
func TestMissingColumnsAreReportedWithTheHeaderThatWasRead(t *testing.T) {
	r := newRig(t, sourceJSON, mappingJSON)
	r.files["ds_racks"] = "Asset,Name,Height\nRK1,rack-a,42\n"
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\n"
	r.files["ds_vms"] = ""
	rep := r.run()
	got := datasetReport(rep, "ds_racks").NotRead
	for _, want := range []string{"lacks the columns [asset_id rack_name total_u]", `"Asset", "Name", "Height"`} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is not in %q", want, got)
		}
	}
	// a sheet with the header in another row names that row
	b, books := bookRig(t)
	books.wb.Sheets[0].Rows = books.wb.Sheets[0].Rows[:1]
	read := datasetReport(b.run(), "ds_racks").NotRead
	if !strings.Contains(read, "(row 2)") || !strings.Contains(read, "header_row") {
		t.Fatalf("%q", read)
	}
	// a title in a merged cell is listed once
	if got := nonBlank([]string{"Title", "Title", "Title", "A"}); len(got) != 2 {
		t.Fatalf("%v", got)
	}
}

func compileWith(t *testing.T, mapping string) error {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseSource(strings.NewReader(sourceJSON))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseMapping(strings.NewReader(mapping))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Compile(reg, s, m)
	return err
}

func feedJSON(typ, attrs, identity, extra string) string {
	return `{"feeds":[{"id":"f","dataset":"ds_racks","object_type":"` + typ + `","creates_objects":true,"basis":"declared","attributes":[` + attrs + `],"identity":[` + identity + `]` + extra + `}]}`
}

// [REQ-321]
func TestRefusedKeysAndSchemesListWhatTheModelAllows(t *testing.T) {
	name := `{"attribute":"name","column":"rack_name"}`
	cases := []struct {
		what, mapping string
		want          []string
	}{
		{"a key of properties the type does not have", feedJSON("Rack", name, `{"components":[{"property":"name","column":"rack_name"}],"priority":1}`, ""),
			[]string{"identity keys of Rack are", "link LOCATED_IN + property name (strong)", "property asset_tag (weak)", `"scheme"`}},
		{"a scheme the model lacks", feedJSON("PhysicalServer", name, `{"scheme":"asset_tag","column":"asset_id"}`, ""),
			[]string{"is no identifier scheme", "that are an identity key of PhysicalServer: bmc_mac, redfish_system_id", "vm_instance_uuid"}},
		{"a scheme that is no key of the type", feedJSON("Rack", name, `{"scheme":"bmc_mac","column":"asset_id"}`, ""),
			[]string{"scheme bmc_mac is not an identity key of type Rack", "property asset_tag (weak)"}},
		{"an identifier with a scheme the model lacks", feedJSON("Rack", name, `{"components":[{"property":"asset_tag","column":"asset_id"}]}`, `,"identifiers":[{"scheme":"serial","column":"asset_id"}]`),
			[]string{`"serial", which is no identifier scheme`, "bmc_mac"}},
		{"an attribute the type does not have", feedJSON("Rack", `{"attribute":"nmae","column":"rack_name"}`, `{"components":[{"property":"asset_tag","column":"asset_id"}]}`, ""),
			[]string{`no attribute "nmae"; its attributes are:`, "total_u", "asset_tag"}},
		{"a column the dataset lacks", feedJSON("Rack", `{"attribute":"name","column":"rack"}`, `{"components":[{"property":"asset_tag","column":"asset_id"}]}`, ""),
			[]string{`column "rack"`, `"asset_id", "rack_name", "total_u"`}},
		{"a type with a small mistake", feedJSON("rack", name, `{"components":[{"property":"asset_tag","column":"asset_id"}]}`, ""),
			[]string{"did you mean Rack?"}},
	}
	for _, c := range cases {
		err := compileWith(t, c.mapping)
		if err == nil {
			t.Fatalf("%s: no error", c.what)
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: %q is not in %q", c.what, w, err.Error())
			}
		}
	}
	if got := Similar("PhysicalServr", []string{"PhysicalServer", "Rack"}); len(got) != 1 || got[0] != "PhysicalServer" {
		t.Fatalf("%v", got)
	}
	if got := Similar("zzzz", []string{"Rack"}); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}

// [REQ-323]
func TestAFileThatIsNotUTF8StopsWithWhatToDo(t *testing.T) {
	euckr := []byte("\xc0\xda\xbb\xea\xb9\xf8\xc8\xa3,\xc0\xcc\xb8\xa7\nA-1,x\n")
	utf16 := []byte{0xFF, 0xFE, 'a', 0, ',', 0, 'b', 0}
	for what, data := range map[string][]byte{"a local code page": euckr, "UTF-16": utf16} {
		err := CheckEncoding(data, ParserCSV)
		if err == nil || !strings.Contains(err.Error(), "CSV UTF-8") || !strings.Contains(err.Error(), ".xlsx") {
			t.Errorf("%s: %v", what, err)
		}
		if _, ierr := Inspect(data, InspectOptions{Format: FormatCSV}, nil); ierr == nil {
			t.Errorf("%s: inspect reads it", what)
		}
	}
	if err := CheckEncoding(euckr, ParserJSONLines); err == nil || strings.Contains(err.Error(), "Excel") {
		t.Errorf("JSON Lines is told to save UTF-8, not to use Excel: %v", err)
	}
	if err := CheckEncoding(append([]byte("\xef\xbb\xbf"), []byte("자산번호,이름\n")...), ParserCSV); err != nil {
		t.Fatalf("a UTF-8 byte order mark is fine: %v", err)
	}
	r := newRig(t, sourceJSON, mappingJSON)
	r.files["ds_racks"] = string(euckr)
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\n"
	r.files["ds_vms"] = ""
	_, err := r.runner.Run(context.Background(), r.plan, r.kg.ID)
	if err == nil || !strings.Contains(err.Error(), "dataset ds_racks") || !strings.Contains(err.Error(), "not UTF-8") {
		t.Fatalf("the run must stop and name the dataset: %v", err)
	}
}

func csvSource(delim string) string {
	d := ""
	if delim != "" {
		d = `,"delimiter":` + delim
	}
	return `{"id":"s","adapter":"file","datasets":[{"id":"ds","parser":"csv","natural_key":["id"]` + d + `,"columns":[{"name":"id"},{"name":"name"}]}]}`
}

// [REQ-324]
func TestACSVDelimiterIsTheDatasetsAndAFileThatNeedsAnotherIsSaid(t *testing.T) {
	for delim, sep := range map[string]string{`"tab"`: "\t", `"\t"`: "\t", `"semicolon"`: ";", `";"`: ";", `"pipe"`: "|", `"|"`: "|", `"comma"`: ","} {
		s, err := ParseSource(strings.NewReader(csvSource(delim)))
		if err != nil {
			t.Fatalf("%s: %v", delim, err)
		}
		tb := Parse(s.Datasets[0], []byte("id"+sep+"name\n1"+sep+"a b\n"))
		if len(tb.Rows) != 1 || tb.Rows[0].Fields["name"] != "a b" || len(tb.Missing) > 0 {
			t.Errorf("%s: %+v", delim, tb)
		}
	}
	if _, err := ParseSource(strings.NewReader(csvSource(`"colon"`))); err == nil || !strings.Contains(err.Error(), "none of comma, tab, semicolon, pipe") {
		t.Fatalf("%v", err)
	}
	if _, err := ParseSource(strings.NewReader(strings.Replace(xlsxSourceJSON, `"parser":"xlsx"`, `"parser":"xlsx","delimiter":"tab"`, 1))); err == nil || !strings.Contains(err.Error(), "csv parser") {
		t.Fatalf("a workbook has no delimiter: %v", err)
	}
	// the default comma reads a tab file as one column: the report says why
	s, _ := ParseSource(strings.NewReader(csvSource("")))
	tb := Parse(s.Datasets[0], []byte("id\tname\n1\ta\n"))
	if reason := shapeReason(tb); !strings.Contains(reason, `"delimiter": "tab"`) {
		t.Fatalf("%q", reason)
	}
	// inspect says so too, and reads the file with the delimiter it is given
	fp, err := Inspect([]byte("id;name\n1;a\n2;b\n"), InspectOptions{Format: FormatCSV}, nil)
	if err != nil || len(fp.Notes) != 1 || !strings.Contains(fp.Notes[0], "-delimiter semicolon") {
		t.Fatalf("%v %+v", err, fp.Notes)
	}
	fp, err = Inspect([]byte("id;name\n1;a\n2;b\n"), InspectOptions{Format: FormatCSV, Delimiter: "semicolon"}, nil)
	if err != nil || len(fp.Notes) != 0 || len(fp.Tables[0].Columns) != 2 {
		t.Fatalf("%v %+v", err, fp)
	}
	if _, err := Inspect(nil, InspectOptions{Format: FormatCSV, Delimiter: "colon"}, nil); err == nil {
		t.Fatal("a delimiter that is none of the four is refused")
	}
}

// [REQ-325]
func TestUndeclaredColumnsCanBeLeftOutAndTheDeclaredOnesStayRequired(t *testing.T) {
	src := func(ignore string) string {
		return strings.Replace(sourceJSON, `"id":"ds_racks","parser":"csv"`, `"id":"ds_racks","parser":"csv"`+ignore, 1)
	}
	files := func(r *rig, racks string) {
		r.files["ds_racks"] = racks
		r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\n"
		r.files["ds_vms"] = ""
	}
	r := newRig(t, src(""), mappingJSON)
	files(r, "asset_id,rack_name,total_u,color,owner\nRK1,a,42,red,me\n")
	if n := r.run().ProblemCounts[ProblemExtraColumn]; n != 2 {
		t.Fatalf("by default every undeclared column is reported: %d", n)
	}
	r = newRig(t, src(`,"ignore_undeclared_columns":true`), mappingJSON)
	files(r, "asset_id,rack_name,total_u,color,owner\nRK1,a,42,red,me\n")
	rep := r.run()
	if rep.ProblemCounts[ProblemExtraColumn] != 0 || datasetReport(rep, "ds_racks").Read != 1 {
		t.Fatalf("%+v", rep)
	}
	files(r, "asset_id,total_u,color\nRK1,42,red\n")
	if rep = r.run(); !strings.Contains(datasetReport(rep, "ds_racks").NotRead, "rack_name") {
		t.Fatalf("a declared column that is gone is still reported: %+v", rep.Problems)
	}
}

// [REQ-326]
func TestADraftSourceIsADefinitionThatNeedsOnlyItsNaturalKey(t *testing.T) {
	csvData := []byte("Asset No,이름,Rack Name\nA-1,가,r1\nA-2,나,r2\n")
	fp, err := Inspect(csvData, InspectOptions{Format: FormatCSV}, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := DraftSource(fp, "/x/My Ledger.csv", "")
	data, err := json.Marshal(d.Source)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParseSource(strings.NewReader(string(data)))
	if err == nil || !strings.Contains(err.Error(), `"asset_no", "이름", "rack_name"`) {
		t.Fatalf("running a draft names the columns to choose the key from: %v", err)
	}
	if d.Source.ID != "src_my_ledger" || d.Source.Datasets[0].ID != "ds_my_ledger" || len(d.Notes) != 1 || !strings.Contains(d.Notes[0], "asset_no") {
		t.Fatalf("%+v", d)
	}
	d.Source.Datasets[0].NaturalKey = []string{"asset_no"}
	data, _ = json.Marshal(d.Source)
	src, err := ParseSource(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("a draft with its natural key is a source: %v", err)
	}
	tb := Parse(src.Datasets[0], csvData)
	if len(tb.Rows) != 2 || tb.Rows[1].Fields["이름"] != "나" || tb.Rows[1].Fields["rack_name"] != "r2" {
		t.Fatalf("the draft reads the file: %+v", tb)
	}
	if d := DraftSource(fp, "x.csv", "tab"); d.Source.Datasets[0].Delimiter != "tab" {
		t.Fatalf("%+v", d.Source.Datasets[0])
	}
}

// [REQ-326]
func TestADraftSourceOfAWorkbookHasADatasetForEveryVisibleSheet(t *testing.T) {
	r, books := bookRig(t)
	books.wb.Sheets = append(books.wb.Sheets, sheetOf("Scratch", map[int][]string{1: {"a", "b"}, 2: {"1", "2"}}))
	books.wb.Sheets[2].State = SheetHidden
	for i := range books.wb.Sheets {
		books.wb.Sheets[i].Index = i + 1
	}
	fp, err := Inspect([]byte("x"), InspectOptions{Format: FormatXLSX}, books)
	if err != nil {
		t.Fatal(err)
	}
	d := DraftSource(fp, "Book.xlsx", "")
	if len(d.Source.Datasets) != 2 {
		t.Fatalf("the hidden sheet has none: %+v", d.Source.Datasets)
	}
	racks := d.Source.Datasets[0]
	if racks.ID != "ds_racks" || racks.Sheet.Name != "Racks" || racks.Sheet.HeaderRow != 2 || racks.Columns[0].Name != "asset" {
		t.Fatalf("%+v %+v", racks, racks.Sheet)
	}
	hidden := false
	for _, n := range d.Notes {
		hidden = hidden || strings.Contains(n, `"Scratch" is hidden`)
	}
	if !hidden {
		t.Fatalf("%v", d.Notes)
	}
	// with the natural keys chosen the draft is a source the pipeline reads
	for i := range d.Source.Datasets {
		d.Source.Datasets[i].NaturalKey = []string{d.Source.Datasets[i].Columns[0].Name}
	}
	data, _ := json.Marshal(d.Source)
	src, err := ParseSource(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if tb := ParseSheet(src.Datasets[0], books.wb); len(tb.Rows) != 2 || len(tb.Missing) != 0 {
		t.Fatalf("%+v", tb)
	}
	_ = r
}

// [REQ-327]
func TestAFieldThatIsNotAllowedIsReportedWithTheFieldsThatAre(t *testing.T) {
	bad := `{"refinements":[{"id":"r","input":"ds","ops":[{"op":"replace","column":"c","function":"map","values":{"a":"b"}}]}],"feeds":[]}`
	_, err := ParseMapping(strings.NewReader(bad))
	if err == nil || !strings.Contains(err.Error(), `"values" is not a field of refinements[0].ops[0]`) || !strings.Contains(err.Error(), "op, column, in, not_in, not_empty, value, function, map") {
		t.Fatalf("%v", err)
	}
	_, err = ParseSource(strings.NewReader(`{"id":"s","adapter":"file","datasets":[{"id":"d","parser":"csv","natural_key":["a"],"columns":[{"name":"a","path":"x"}]}]}`))
	if err == nil || !strings.Contains(err.Error(), `"path" is not a field of datasets[0].columns[0]`) || !strings.Contains(err.Error(), "name, paths, optional") {
		t.Fatalf("%v", err)
	}
	_, err = ParseSource(strings.NewReader(`{"id":"s","adapters":"file"}`))
	if err == nil || !strings.Contains(err.Error(), "the top level") || !strings.Contains(err.Error(), "adapter") {
		t.Fatalf("%v", err)
	}
	if _, err = ParseSource(strings.NewReader(`{"id":`)); err == nil {
		t.Fatal("a broken document is still an error")
	}
}

// [REQ-327]
func TestAnEmptyNaturalKeyIsReportedWithTheColumnThatIsEmpty(t *testing.T) {
	r := newRig(t, sourceJSON, mappingJSON)
	r.files["ds_racks"] = "asset_id,rack_name,total_u\n,rack-a,42\nRK2,rack-b,45\n"
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\n"
	r.files["ds_vms"] = ""
	rep := r.run()
	var got []string
	for _, p := range rep.Problems {
		if p.Kind == ProblemUnreadable {
			got = append(got, p.Reason)
		}
	}
	if len(got) != 1 || !strings.HasPrefix(got[0], "missing_natural_key") || !strings.Contains(got[0], `"asset_id"`) {
		t.Fatalf("%v", got)
	}
	if _, err := ParseSource(strings.NewReader(strings.Replace(sourceJSON, `"natural_key":["asset_id"]`, `"natural_key":[]`, 1))); err == nil || !strings.Contains(err.Error(), `"asset_id", "rack_name", "total_u"`) {
		t.Fatalf("an empty natural key lists the columns: %v", err)
	}
}
