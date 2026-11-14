package ingest

import (
	"context"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// fakeBooks is a reader of workbooks that gives a workbook it was made with, and counts its reads.
type fakeBooks struct {
	wb    *Workbook
	err   error
	reads int
	limit Limits
}

func (f *fakeBooks) ReadWorkbook(_ []byte, l Limits) (*Workbook, error) {
	f.reads++
	f.limit = l
	return f.wb, f.err
}

const xlsxSourceJSON = `{"id":"src_book","adapter":"file","limits":{"max_rows_per_sheet":1000},"datasets":[
 {"id":"ds_racks","parser":"xlsx","natural_key":["asset_id"],"sheet":{"name":"Racks","header_row":2},
  "columns":[{"name":"asset_id","paths":["Asset"]},{"name":"rack_name","paths":["Rack"]},{"name":"total_u","paths":["U"]}]},
 {"id":"ds_servers","parser":"xlsx","natural_key":["asset_id"],"sheet":{"name":"Servers","merged_cells":"fill"},
  "columns":[{"name":"asset_id","paths":["Asset No."]},{"name":"serial","paths":["Serial"]},{"name":"manufacturer","paths":["Maker"]},{"name":"rack_asset_id","paths":["Rack"]},{"name":"power_state","paths":["Power"],"optional":true}]}
]}`

// xlsxMappingJSON is the mapping of the two datasets of the workbook: racks, servers and the link between them.
const xlsxMappingJSON = `{"feeds":[
 {"id":"f_rack","dataset":"ds_racks","object_type":"Rack","creates_objects":true,"retires_objects":true,"basis":"declared",
  "attributes":[{"attribute":"name","column":"rack_name"},{"attribute":"total_u","column":"total_u"},{"attribute":"asset_tag","column":"asset_id"}],
  "identity":[{"key_name":"asset_tag","components":[{"property":"asset_tag","column":"asset_id"}],"priority":1}]},
 {"id":"f_server","dataset":"ds_servers","object_type":"PhysicalServer","creates_objects":true,"retires_objects":true,"basis":"observed",
  "attributes":[{"attribute":"serial_number","column":"serial"},{"attribute":"manufacturer","column":"manufacturer"},{"attribute":"power_state","column":"power_state"},{"attribute":"name","column":"asset_id"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]}
],
"links":[{"id":"l_mounted","link_type":"MOUNTED_IN","from":{"feed":"f_server","columns":["rack_asset_id"]},"to":{"feed":"f_rack","columns":["asset_id"]},"condition":"same_key","basis":"declared"}]}`

func bookRig(t *testing.T) (*rig, *fakeBooks) {
	r := newRig(t, xlsxSourceJSON, xlsxMappingJSON)
	wb := &Workbook{Sheets: []Sheet{
		sheetOf("Racks", map[int][]string{1: {"Rack list"}, 2: {"Asset", "Rack", "U"}, 3: {"RK1", "rack-a", "#42"}, 4: {"RK2", "rack-b", "#45"}}),
		sheetOf("Servers", map[int][]string{
			1: {"Asset No.", "Serial", "Maker", "Rack", "Power"},
			2: {"#2001", "SN1", "Zorvane", "RK1", "on"},
			3: {"#2002", "SN2", "", "RK1", "!#N/A"},
			5: {"", "", "", "RK9"}, // no asset number
		}),
	}}
	wb.Sheets[1].Merges = []Merge{{2, 3, 3, 3}}
	books := &fakeBooks{wb: wb}
	r.runner.Workbooks = books
	r.files["ds_racks"], r.files["ds_servers"] = "the bytes of one workbook", "the bytes of one workbook"
	return r, books
}

// [REQ-287]
func TestWorkbookRunReportsWhatWasReadAndWhereTheProblemsAre(t *testing.T) {
	r, books := bookRig(t)
	rep := r.run()
	if books.reads != 1 || books.limit.MaxRowsPerSheet != 1000 {
		t.Fatalf("the workbook is read once for both datasets with the limits of the source: %d %+v", books.reads, books.limit)
	}
	if rep.Objects != 4 || rep.Links != 2 {
		t.Fatalf("objects %d links %d: %+v", rep.Objects, rep.Links, rep.Problems)
	}
	var servers DatasetReport
	for _, d := range rep.Datasets {
		if d.Dataset == "ds_servers" {
			servers = d
		}
		if !d.Balanced() {
			t.Errorf("%s does not add up: %+v", d.Dataset, d)
		}
	}
	if servers.Sheet != "Servers" || servers.Received != 3 || servers.Unreadable != 1 || servers.Read != 2 || servers.Used != 2 {
		t.Fatalf("%+v", servers)
	}
	where := map[string]string{}
	for _, p := range rep.Problems {
		where[p.Kind] = p.Where
	}
	if where["cell_error"] != "Servers!E3" || where["unreadable_line"] != "Servers!A5" {
		t.Fatalf("%+v", rep.Problems)
	}
	for _, p := range rep.Problems {
		if p.Kind == "cell_error" && (p.Line != 3 || !strings.Contains(p.Reason, "#N/A") || !strings.Contains(p.Reason, "power_state")) {
			t.Errorf("%+v", p)
		}
	}
	// the merged maker filled the second row, so both servers have it and no row lacks an identity
	if rep.ProblemCounts[ProblemMissingIdentity] != 0 {
		t.Fatalf("%v", rep.ProblemCounts)
	}
	again := r.run()
	if again.Changed || again.NewVersion || !again.Datasets[0].SameCapture {
		t.Fatalf("a second run of the same workbook changed something: %+v", again)
	}
}

// [REQ-287]
func TestRowProblemsOfAWorkbookCarryTheRangeOfTheRow(t *testing.T) {
	r, books := bookRig(t)
	books.wb.Sheets[1] = sheetOf("Servers", map[int][]string{
		1: {"Asset No.", "Serial", "Maker", "Rack", "Power"},
		2: {"#2001", "SN1", "Zorvane", "RK1", "on"},
		3: {"#2002", "", "Zorvane", "RK1", "on"}, // no serial: the server has no identity
	})
	rep := r.run()
	found := false
	for _, p := range rep.Problems {
		if p.Kind == ProblemMissingIdentity {
			found = p.Where == "Servers!A3:E3" && p.Line == 3
		}
	}
	if !found {
		t.Fatalf("%+v", rep.Problems)
	}
}

// [REQ-287]
func TestWhereARowStandsIsKeptWhenOnlyItsPlaceChanged(t *testing.T) {
	r, books := bookRig(t)
	r.run()
	// a row is added above the servers, which moves them down one row; their content is the same
	books.wb.Sheets[1] = sheetOf("Servers", map[int][]string{
		1: {"Asset No.", "Serial", "Maker", "Rack", "Power"},
		2: {"#2001", "SN1", "Zorvane", "RK1", "on"},
		3: {"#2003", "SN3", "Zorvane", "RK2", "on"},
		4: {"#2002", "SN2", "Zorvane", "RK1", ""},
	})
	r.files["ds_servers"] = "other bytes"
	rep := r.run()
	st, _ := r.runner.States.Load(context.Background(), "src_book")
	if got := st.Datasets["ds_servers"].Open["2002"].Where; got != "Servers!A4:E4" || rep.Datasets[1].Raw.Added != 1 {
		t.Fatalf("where %q, %+v", got, rep.Datasets[1].Raw)
	}
}

// [REQ-285]
func TestAWorkbookThatCannotBeReadStopsTheRunWithTheDatasetAndTheReason(t *testing.T) {
	r, books := bookRig(t)
	books.err = kernel.Errorf(kernel.CodeUnsupported, "the file is in the old binary Excel format (.xls)")
	_, err := r.runner.Run(context.Background(), r.plan, r.kg.ID)
	if err == nil || kernel.CodeOf(err) != kernel.CodeUnsupported || !strings.Contains(err.Error(), "dataset ds_racks: the file is in the old binary Excel format") || strings.Count(err.Error(), "unsupported") != 1 {
		t.Fatalf("%v", err)
	}
	r.runner.Workbooks = nil
	if _, err := r.runner.Run(context.Background(), r.plan, r.kg.ID); err == nil || !strings.Contains(err.Error(), "no reader of workbook files") {
		t.Fatalf("%v", err)
	}
	if plainMessage(kernel.Wrap(kernel.CodeInvalidValue, context.Canceled, "m")) != "m: context canceled" || plainMessage(context.Canceled) != "context canceled" {
		t.Fatal("plain message")
	}
}

// [REQ-286]
func TestASheetThatIsMissingIsAChangedShapeAndTheOtherDatasetIsStillRun(t *testing.T) {
	r, books := bookRig(t)
	books.wb.Sheets[0].Name = "Racks (old)"
	rep := r.run()
	if rep.ProblemCounts[ProblemShapeChanged] != 1 || !strings.Contains(rep.Problems[0].Reason, `no sheet "Racks"`) {
		t.Fatalf("%+v", rep.Problems)
	}
	if rep.Datasets[0].Applied || !rep.Datasets[1].Applied {
		t.Fatalf("%+v", rep.Datasets)
	}
}

// [REQ-286]
func TestADateWithoutATimeIsMidnightForAPropertyOfTime(t *testing.T) {
	r, _ := bookRig(t)
	var spec valueSpec
	for _, ot := range r.runner.Reg.ObjectTypes() {
		for _, p := range ot.Properties {
			if p.Name == "release_date" {
				spec = valueSpec{prop: p, base: model.BaseTime}
			}
		}
	}
	if spec.prop.Name == "" {
		t.Fatal("the standard model has no property named release_date")
	}
	v, err := convertCell(spec, "2026-03-14")
	if err != nil || v != "2026-03-14T00:00:00Z" {
		t.Fatalf("%v %v", v, err)
	}
	if v, err := convertCell(spec, "2026-03-14T09:30:00Z"); err != nil || v != "2026-03-14T09:30:00Z" {
		t.Fatalf("%v %v", v, err)
	}
	if _, err := convertCell(spec, "14.03.2026"); err == nil {
		t.Fatal("another form of a date is still no time")
	}
}
