package ingestcheck

import (
	"sort"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/xlsx"
)

// messySourceJSON reads a workbook as people keep them: a title above the header, a classification column made of
// merged cells, a header of two rows, a hidden sheet, a subtotal row, blank rows and a note after the data.
const messySourceJSON = `{"id":"src_messy","adapter":"file","datasets":[
 {"id":"ds_racks","parser":"xlsx","sheet":{"name":"랙"},"natural_key":["asset_id"],
  "columns":[{"name":"asset_id","paths":["자산번호"]},{"name":"rack_name","paths":["이름"]},{"name":"total_u","paths":["높이(U)"]}]},
 {"id":"ds_servers","parser":"xlsx","sheet":{"name":"서버 목록","header_row":3,"merged_cells":"MERGED","stop_after_blank_rows":2},"natural_key":["asset_id"],
  "columns":[{"name":"asset_id","paths":["자산번호"]},{"name":"serial","paths":["시리얼"]},{"name":"manufacturer","paths":["제조사"]},{"name":"rack_asset_id","paths":["랙"]},{"name":"power_state","paths":["전원"],"optional":true}]},
 {"id":"ds_batteries","parser":"xlsx","sheet":{"name":"Batteries","header_rows":2},"natural_key":["serial"],
  "columns":[{"name":"maker","paths":["Unit Maker"]},{"name":"serial","paths":["Unit Serial"]},{"name":"installed","paths":["Install Date"]}]}
]}`

const messyMappingJSON = `{"refinements":[{"id":"rf_servers","input":"ds_servers","ops":[{"op":"filter","column":"asset_id","not_in":["합계"]}]}],
"feeds":[
 {"id":"f_rack","dataset":"ds_racks","object_type":"Rack","creates_objects":true,"basis":"declared",
  "attributes":[{"attribute":"name","column":"rack_name"},{"attribute":"total_u","column":"total_u"},{"attribute":"asset_tag","column":"asset_id"}],
  "identity":[{"key_name":"asset_tag","components":[{"property":"asset_tag","column":"asset_id"}],"priority":1}]},
 {"id":"f_server","dataset":"rf_servers","object_type":"PhysicalServer","creates_objects":true,"basis":"observed",
  "attributes":[{"attribute":"serial_number","column":"serial"},{"attribute":"manufacturer","column":"manufacturer"},{"attribute":"power_state","column":"power_state"},{"attribute":"name","column":"asset_id"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]},
 {"id":"f_battery","dataset":"ds_batteries","object_type":"BatteryBank","creates_objects":true,"basis":"declared",
  "attributes":[{"attribute":"manufacturer","column":"maker"},{"attribute":"serial_number","column":"serial"},{"attribute":"install_date","column":"installed"}],
  "identity":[{"components":[{"property":"manufacturer","column":"maker"},{"property":"serial_number","column":"serial"}],"priority":1}]}
],
"links":[{"id":"l_mounted","link_type":"MOUNTED_IN","from":{"feed":"f_server","columns":["rack_asset_id"]},"to":{"feed":"f_rack","columns":["asset_id"]},"condition":"same_key","basis":"declared"}]}`

// messyBook writes the workbook of the source above.
func messyBook() *xlsx.Book {
	b := &xlsx.Book{SharedStrings: true, RichText: true}
	s := b.Sheet("서버 목록")
	s.Set("A1", xlsx.Str("서버 현황 (2031-03)"))
	s.Merges = []string{"A1:F1", "C4:C6"}
	s.Row(3, xlsx.Str("자산번호"), xlsx.Str("서버명"), xlsx.Str("제조사"), xlsx.Str("시리얼"), xlsx.Str("랙"), xlsx.Str("전원"))
	s.Row(4, xlsx.Num("20001"), xlsx.Str("web-01"), xlsx.Str("Zorvane"), xlsx.Str("SN-A1"), xlsx.Str("RK-1"), xlsx.Str("on"))
	s.Row(5, xlsx.Num("20002"), xlsx.Str("web-02"), xlsx.C{}, xlsx.Str("SN-A2"), xlsx.Str("RK-1"), xlsx.Str("off"))
	s.Row(6, xlsx.Num("20003"), xlsx.Str("web-03"), xlsx.C{}, xlsx.Formula(`"SN-"&"A3"`, xlsx.Str("SN-A3")), xlsx.Str("RK-1"), xlsx.Err("#N/A"))
	s.Row(8, xlsx.Num("20004"), xlsx.Str("db-01"), xlsx.Str("Other Maker"), xlsx.Str("SN-B1"), xlsx.Str("RK-2"), xlsx.Uncalculated(`IF(1,"on")`))
	s.Row(9, xlsx.Str("합계"), xlsx.C{}, xlsx.C{}, xlsx.C{}, xlsx.C{}, xlsx.Formula("COUNTA(A4:A8)", xlsx.Num("4")))
	s.Row(12, xlsx.Str("※ 위 자료는 견본"))
	r := b.Sheet("랙")
	r.State = "hidden"
	r.Row(1, xlsx.Str("자산번호"), xlsx.Str("이름"), xlsx.Str("높이(U)"))
	r.Row(2, xlsx.Str("RK-1"), xlsx.Str("rack-1"), xlsx.Num("42"))
	r.Row(3, xlsx.Str("RK-2"), xlsx.Str("rack-2"), xlsx.Num("45"))
	bt := b.Sheet("Batteries")
	bt.Merges = []string{"A1:B1", "C1:D1"}
	bt.Row(1, xlsx.Str("Unit"), xlsx.C{}, xlsx.Str("Install"))
	bt.Row(2, xlsx.Str("Maker"), xlsx.Str("Serial"), xlsx.Str("Date"), xlsx.Str("Note"))
	bt.Row(3, xlsx.Str("Voltek"), xlsx.Str("BB-1"), xlsx.Dated("46095", `yyyy\-mm\-dd`), xlsx.Str("main hall"))
	bt.Row(4, xlsx.Str("Voltek"), xlsx.Str("BB-2"), xlsx.Dated("46096.3958333333", `yyyy\-mm\-dd\ h:mm:ss`), xlsx.Str("annex"))
	return b
}

func messyPipeline(t *testing.T, merged string) *pipeline {
	t.Helper()
	data, err := messyBook().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	p := newPipeline(t, exports{"book.xlsx": string(data)})
	src, err := ingest.ParseSource(strings.NewReader(strings.Replace(messySourceJSON, "MERGED", merged, 1)))
	if err != nil {
		t.Fatal(err)
	}
	mp, err := ingest.ParseMapping(strings.NewReader(messyMappingJSON))
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := model.LoadStandard()
	if p.plan, err = ingest.Compile(reg, src, mp); err != nil {
		t.Fatal(err)
	}
	return wireBook(p)
}

func wireBook(p *pipeline) *pipeline {
	files := map[string]string{}
	for _, d := range p.plan.Source.Datasets {
		files[d.ID] = "book.xlsx"
	}
	p.runner.Fetcher = filesourceOf(p, files)
	p.runner.Workbooks = xlsx.Reader{}
	return p
}

func datasetOf(rep ingest.Report, id string) ingest.DatasetReport {
	for _, d := range rep.Datasets {
		if d.Dataset == id {
			return d
		}
	}
	return ingest.DatasetReport{}
}

func problemsBy(rep ingest.Report) map[string][]string {
	out := map[string][]string{}
	for _, p := range rep.Problems {
		out[p.Kind] = append(out[p.Kind], p.Where)
	}
	for _, v := range out {
		sort.Strings(v)
	}
	return out
}

// [REQ-286] [REQ-287]
func TestMessyWorkbookGivesTheRowsExpectedAndReportsWhereTheProblemsAre(t *testing.T) {
	reg, _ := model.LoadStandard()
	p := messyPipeline(t, "fill")
	rep := p.run()

	servers := datasetOf(rep, "ds_servers")
	if servers.Sheet != "서버 목록" || servers.SheetState != "visible" || servers.Received != 5 || servers.Read != 5 || servers.Used != 4 || servers.Filtered != 1 ||
		servers.Problem != 0 || servers.Unused != 0 || !servers.Balanced() {
		t.Fatalf("servers: %+v", servers)
	}
	// row 7 is blank, rows 10 and 11 are two blank rows in a row, which end the data: the note in row 12 is not read
	if servers.Skipped["blank_row"] != 3 || servers.Skipped["after_end"] != 1 || len(servers.Skipped) != 2 {
		t.Fatalf("skipped: %v", servers.Skipped)
	}
	racks := datasetOf(rep, "ds_racks")
	if racks.SheetState != "hidden" || racks.Received != 2 || racks.Used != 2 || !racks.Balanced() {
		t.Fatalf("racks: %+v", racks)
	}
	batteries := datasetOf(rep, "ds_batteries")
	if batteries.Received != 2 || batteries.Used != 2 {
		t.Fatalf("batteries: %+v", batteries)
	}

	got := problemsBy(rep)
	want := map[string][]string{
		"cell_error":            {"'서버 목록'!F6"},
		"formula_without_value": {"'서버 목록'!F8"},
	}
	for kind, wheres := range want {
		if strings.Join(got[kind], ",") != strings.Join(wheres, ",") {
			t.Errorf("%s: %v, want %v", kind, got[kind], wheres)
		}
	}
	if rep.ProblemCounts["undeclared_column"] != 2 || len(rep.ProblemCounts) != 3 {
		t.Errorf("problem counts: %v", rep.ProblemCounts)
	}

	// the objects: the merged maker filled down, a formula gave its stored value, a number cell named the server
	servs := attrsOf(t, reg, p, "PhysicalServer", "serial_number")
	if len(servs) != 4 || servs["SN-A2"]["manufacturer"] != "Zorvane" || servs["SN-A3"]["manufacturer"] != "Zorvane" || servs["SN-B1"]["manufacturer"] != "Other Maker" {
		t.Fatalf("servers: %v", servs)
	}
	if servs["SN-A1"]["name"] != "20001" || servs["SN-A1"]["power_state"] != "on" || servs["SN-A3"]["power_state"] != "" || servs["SN-B1"]["power_state"] != "" {
		t.Fatalf("server values: %v", servs)
	}
	bats := attrsOf(t, reg, p, "BatteryBank", "serial_number")
	if len(bats) != 2 || bats["BB-1"]["install_date"] != "2026-03-14T00:00:00Z" || bats["BB-2"]["install_date"] != "2026-03-15T09:30:00Z" {
		t.Fatalf("batteries: %v", bats)
	}
	if rep.Objects != 8 || rep.Links != 4 {
		t.Fatalf("objects %d links %d", rep.Objects, rep.Links)
	}
	if again := p.run(); again.Changed || again.NewVersion {
		t.Fatalf("a second run of the same workbook changed the KG: %+v", again)
	}
}

// [REQ-286] [REQ-287]
func TestMergedCellsKeptInTheFirstCellLeaveTheOthersWithoutAnIdentityAndSaySo(t *testing.T) {
	rep := messyPipeline(t, "first_only").run()
	servers := datasetOf(rep, "ds_servers")
	if servers.Used != 2 || servers.Problem != 2 || servers.Filtered != 1 || !servers.Balanced() {
		t.Fatalf("servers: %+v", servers)
	}
	got := problemsBy(rep)
	if strings.Join(got["missing_identity"], ",") != "'서버 목록'!A5:F5,'서버 목록'!A6:F6" {
		t.Fatalf("%v", got)
	}
}
