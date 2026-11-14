package ingestcheck

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/filesource"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/xlsx"
)

// bookSourceJSON is the same five datasets as the CSV and JSON Lines source, each on a sheet of one workbook.
const bookSourceJSON = `{"id":"src_example_files","name":"example workbook","adapter":"file","datasets":[
 {"id":"ds_racks","parser":"xlsx","sheet":{"name":"Racks"},"natural_key":["asset_id"],
  "columns":[{"name":"asset_id"},{"name":"rack_name"},{"name":"total_u"}]},
 {"id":"ds_servers","parser":"xlsx","sheet":{"name":"Servers"},"natural_key":["asset_id"],
  "columns":[{"name":"asset_id"},{"name":"serial","paths":["serial_number","serial_no"]},{"name":"manufacturer"},{"name":"rack_asset_id"},{"name":"power_state","optional":true}]},
 {"id":"ds_hosts","parser":"xlsx","sheet":{"name":"Hosts"},"natural_key":["host_id"],
  "columns":[{"name":"host_id"},{"name":"hostname","paths":["name"]},{"name":"serial"},{"name":"connection","optional":true}]},
 {"id":"ds_vms","parser":"xlsx","sheet":{"name":"VMs"},"natural_key":["vm_id"],
  "columns":[{"name":"vm_id"},{"name":"instance_uuid"},{"name":"name"},{"name":"host_id"}]},
 {"id":"ds_vm_power","parser":"xlsx","sheet":{"name":"VM Power"},"natural_key":["instance_uuid"],
  "columns":[{"name":"instance_uuid"},{"name":"power_state"}]}
]}`

// csvCells reads a CSV export into rows of cells; the columns named as numbers are written as number cells, as a
// person who typed them into a spreadsheet would have them.
func csvCells(t testing.TB, text string, numeric ...string) [][]xlsx.C {
	t.Helper()
	recs, err := csv.NewReader(strings.NewReader(text)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]xlsx.C
	for i, rec := range recs {
		row := make([]xlsx.C, len(rec))
		for j, v := range rec {
			switch {
			case v == "":
			case i > 0 && contains(numeric, recs[0][j]):
				row[j] = xlsx.Num(v)
			default:
				row[j] = xlsx.Str(v)
			}
		}
		out = append(out, row)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// jsonCells flattens the lines of a JSON Lines export into rows with the given headers; a header is a dotted path.
func jsonCells(t testing.TB, text string, paths map[string]string, order ...string) [][]xlsx.C {
	t.Helper()
	header := make([]xlsx.C, len(order))
	for i, h := range order {
		header[i] = xlsx.Str(h)
	}
	out := [][]xlsx.C{header}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		var doc map[string]any
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Fatal(err)
		}
		row := make([]xlsx.C, len(order))
		for i, h := range order {
			var v any = doc
			for _, seg := range strings.Split(paths[h], ".") {
				m, _ := v.(map[string]any)
				v = m[seg]
			}
			if s, ok := v.(string); ok && s != "" {
				row[i] = xlsx.Str(s)
			}
		}
		out = append(out, row)
	}
	return out
}

// workbookOf puts the export files of the world on the sheets of one workbook.
func workbookOf(t testing.TB, files exports, book *xlsx.Book) []byte {
	t.Helper()
	put := func(name string, rows [][]xlsx.C) {
		p := book.Sheet(name)
		for i, r := range rows {
			p.Row(i+1, r...)
		}
	}
	put("Racks", csvCells(t, files["ds_racks"], "total_u"))
	put("Servers", csvCells(t, files["ds_servers"]))
	put("Hosts", jsonCells(t, files["ds_hosts"], map[string]string{"host_id": "host_id", "name": "name", "serial": "hardware.serial", "connection": "connection"}, "host_id", "name", "serial", "connection"))
	put("VMs", jsonCells(t, files["ds_vms"], map[string]string{"vm_id": "vm_id", "instance_uuid": "instance_uuid", "name": "name", "host_id": "host.host_id"}, "vm_id", "instance_uuid", "name", "host_id"))
	put("VM Power", csvCells(t, files["ds_vm_power"]))
	data, err := book.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// newBookPipeline is the pipeline of the workbook source: the same mapping as the CSV one, every dataset read from
// the one workbook file.
func newBookPipeline(t *testing.T, workbook []byte, sourceDef string) *pipeline {
	t.Helper()
	p := newPipeline(t, exports{"book.xlsx": string(workbook)})
	src, err := ingest.ParseSource(strings.NewReader(sourceDef))
	if err != nil {
		t.Fatal(err)
	}
	mp, err := ingest.ParseMapping(strings.NewReader(mappingJSON))
	if err != nil {
		t.Fatal(err)
	}
	if p.plan, err = ingest.Compile(p.reg, src, mp); err != nil {
		t.Fatal(err)
	}
	return wireBook(p)
}

// filesourceOf reads every dataset from the files the pipeline has.
func filesourceOf(p *pipeline, files map[string]string) filesource.Source {
	return filesource.Source{Open: p.open, Files: files}
}

// toolBooks are the ways of writing the same workbook that the real tools were found to have (ADR-0040).
func toolBooks() map[string]func() *xlsx.Book {
	return map[string]func() *xlsx.Book{
		"inline strings, absolute targets (openpyxl)": func() *xlsx.Book { return &xlsx.Book{AbsoluteTargets: true, ExplicitN: true} },
		"shared strings in runs (LibreOffice)":        func() *xlsx.Book { return &xlsx.Book{SharedStrings: true, RichText: true, WordBool: true} },
		"prefixed elements":                           func() *xlsx.Book { return &xlsx.Book{Prefix: "x", SharedStrings: true} },
	}
}

// [REQ-284] [REQ-286]
func TestWorkbookOfTheWorldGivesTheSameKGAsTheCSVAndJSONLinesFiles(t *testing.T) {
	reg, _ := model.LoadStandard()
	files, _ := world(t, reg, 7, 500, clean)
	csvRun := newPipeline(t, files)
	want := csvRun.run()
	if want.Objects < 40 || want.Links < 40 {
		t.Fatalf("the world is too small to compare: %d objects, %d links", want.Objects, want.Links)
	}
	wantView := readView(t, reg, csvRun.store, csvRun.kg)
	for name, mk := range toolBooks() {
		p := newBookPipeline(t, workbookOf(t, files, mk()), bookSourceJSON)
		got := p.run()
		if len(got.ProblemCounts) != len(want.ProblemCounts) {
			t.Errorf("%s: problems %v, the files gave %v", name, got.ProblemCounts, want.ProblemCounts)
		}
		if got.GraphHash != want.GraphHash || got.Objects != want.Objects || got.Links != want.Links {
			t.Errorf("%s: graph hash %s objects %d links %d, the files gave %s %d %d", name, got.GraphHash, got.Objects, got.Links, want.GraphHash, want.Objects, want.Links)
		}
		if d := wantView.diff(readView(t, reg, p.store, p.kg)); len(d) != 0 {
			t.Errorf("%s: %d differences, first %v", name, len(d), d)
		}
		for i, d := range got.Datasets {
			w := want.Datasets[i]
			if d.Received != w.Received || d.Read != w.Read || d.Used != w.Used || !d.Balanced() || d.Sheet == "" {
				t.Errorf("%s: dataset %s counts %+v, the files gave %+v", name, d.Dataset, d, w)
			}
		}
	}
}

// [REQ-284] [REQ-286]
func TestReingestingTheSameWorkbookChangesNothing(t *testing.T) {
	reg, _ := model.LoadStandard()
	files, _ := world(t, reg, 7, 500, clean)
	p := newBookPipeline(t, workbookOf(t, files, &xlsx.Book{SharedStrings: true}), bookSourceJSON)
	first := p.run()
	headBefore, _ := p.store.Head(context.Background(), p.kg.ID)
	again := p.run()
	headAfter, _ := p.store.Head(context.Background(), p.kg.ID)
	if again.Changed || again.NewVersion || again.GraphHash != first.GraphHash || headAfter.AppliedSeq != headBefore.AppliedSeq {
		t.Fatalf("the second run changed the KG: %+v", again)
	}
	for _, d := range again.Datasets {
		if d.Raw.Added+d.Raw.Modified+d.Raw.Deleted != 0 || !d.SameCapture {
			t.Errorf("%s: %+v", d.Dataset, d)
		}
	}
	// the same content written by another tool is another file with the same rows: nothing changes either
	p.files["book.xlsx"] = string(workbookOf(t, files, &xlsx.Book{AbsoluteTargets: true, ExplicitN: true}))
	other := p.run()
	if other.Changed || other.NewVersion || other.Datasets[0].SameCapture {
		t.Fatalf("a workbook of the same rows from another tool: %+v", other)
	}
	if other.Datasets[0].Raw.Added+other.Datasets[0].Raw.Modified+other.Datasets[0].Raw.Deleted != 0 {
		t.Fatalf("%+v", other.Datasets[0].Raw)
	}
}

// attrsOf lists the objects of a type with the values of their properties, by the value of the property that names
// them.
func attrsOf(t testing.TB, reg *model.Registry, p *pipeline, typeName, align string) map[string]map[string]string {
	t.Helper()
	ctx := context.Background()
	snap, err := p.store.Snapshot(ctx, graph.ReadPoint{KG: p.kg.ID, Kind: graph.ReadLatest})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := snap.ScanObjects(ctx, graph.ObjectID{}, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]string{}
	for _, o := range rows {
		ot, _ := reg.ObjectTypeByNo(o.TypeNo)
		if ot.Name != typeName {
			continue
		}
		vals := map[string]string{}
		for no, a := range o.Attrs {
			if ref, ok := reg.PropertyByNo(no); ok && a.Value != nil {
				vals[ref.Property.Name] = fmt.Sprint(a.Value)
			}
		}
		for _, pr := range reg.EffectiveProperties(ot.ID) {
			if pr.EffectiveHistory() != model.HistoryVolatile {
				continue
			}
			if vr, ok, err := snap.Volatile(ctx, o.ID, pr.AttrNo); err != nil {
				t.Fatal(err)
			} else if ok && vr.Value != nil {
				vals[pr.Name] = fmt.Sprint(vr.Value)
			}
		}
		out[vals[align]] = vals
	}
	return out
}
