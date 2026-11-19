package pgingest

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// pgiReceive receives the texts into a new run of a KG with a tee, and says what the pages were.
func pgiReceive(t *testing.T, e *pgiEnv, plan *ingest.Plan, ref SourceRef, lease ingest.RunLease, texts pgiTexts, opts ingest.PageOptions) (ingest.IntakeReport, []ingest.StagePage) {
	t.Helper()
	sink, err := e.st.newSink(e.ctx, lease, plan)
	if err != nil {
		t.Fatal(err)
	}
	tee := &pgiTee{inner: sink}
	rep, err := ingest.Intake{Reader: texts, Sink: tee, Plan: plan, Options: opts}.Run(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.saveCounts(e.ctx, sink, rep); err != nil {
		t.Fatal(err)
	}
	return rep, tee.pages
}

func pgiJSONStrings(t *testing.T, e *pgiEnv, query string, args ...any) []string {
	t.Helper()
	var out []string
	if err := json.Unmarshal([]byte(e.text(t, query, args...)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// [REQ-701]
func TestPgiReceiveStoresEveryPageWithItsPlaceAndMovesTheRunToComputing(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	ref := e.attach(t, kg, plan)
	lease := e.open(t, kg, plan.Source.ID, "server-a")
	in := ingest.Intake{Reader: pgiAll(), Plan: plan, Options: ingest.PageOptions{MaxRows: 1}}
	rep, err := e.st.Receive(e.ctx, lease, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Datasets) != 3 || rep.Datasets[0].Pages != 3 || rep.Datasets[1].Pages != 2 || rep.Datasets[2].Pages != 2 {
		t.Fatalf("%+v", rep.Datasets)
	}
	place, err := e.st.ReceivePlace(e.ctx, lease.No)
	if err != nil || place.Stage != ingest.RunComputing || place.Dataset != "ds_vms" || place.Page != 1 || !place.Last || place.Token != nil {
		t.Fatalf("the place after the last page: %+v %v", place, err)
	}
	q := func(table, where string) int {
		return e.count(t, `SELECT count(*) FROM `+pgiTable(ref, table)+` WHERE `+where)
	}
	if n := q(pgschema.TableStageRecord, "true"); n != 7 {
		t.Fatalf("%d records", n)
	}
	for _, c := range []struct {
		dataset string
		rows    int
	}{{"ds_places", 3}, {"ds_servers", 2}, {"ds_vms", 2}} {
		no := ref.Datasets[c.dataset]
		if n := q(pgschema.TableStageRecord, fmt.Sprintf("dataset_no = %d AND row_no = 0 AND change_kind = %d AND raw_hash IS NOT NULL AND fields IS NOT NULL", no, pgschema.ReceiveNew)); n != c.rows {
			t.Errorf("%s: %d first rows of a page", c.dataset, n)
		}
	}
	// 3 rack rows give two keys each (the context key and the tag), 2 servers one, 2 VMs one; the claims are the scheme of the VMs.
	if k, c, en := q(pgschema.TableStageKey, "true"), q(pgschema.TableStageClaim, "true"), q(pgschema.TableStageEnd, "true"); k != 10 || c != 2 || en != 5 {
		t.Errorf("%d keys, %d claims, %d ends", k, c, en)
	}
	if got := e.text(t, `SELECT counts::text FROM sync_run WHERE run_no = $1`, lease.No); !strings.Contains(got, `"complete": true`) || !strings.Contains(got, `"dataset": "ds_vms"`) {
		t.Errorf("the counts of the run: %s", got)
	}
	if got := e.text(t, `SELECT stage FROM sync_run WHERE run_no = $1`, lease.No); got != "computing" {
		t.Errorf("the stage of the run is %s", got)
	}
}

// [REQ-701]
func TestPgiTheRowsOfAPageAreTheRowsThePreparerGaveWithTheCellsAndTheKeysIntact(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	ref := e.attach(t, kg, plan)
	lease := e.open(t, kg, plan.Source.ID, "server-a")
	texts := pgiAll()
	texts["ds_places"] = "row_id,site,building,floor,room,rack,tag\n1,HQ,B1,F1,R1,\"rack, \"\"a\"\"\\{x}\",RK1\n2,HQ,B1,F1,R1,\"two\nlines\ttab\",RK2\n"
	_, pages := pgiReceive(t, e, plan, ref, lease, texts, ingest.PageOptions{})
	var places ingest.StagePage
	for _, p := range pages {
		if p.Dataset == "ds_places" {
			places = p
		}
	}
	if len(places.Records) != 2 || len(places.Parents) == 0 {
		t.Fatalf("%+v", places)
	}
	tbl := pgiTable(ref, pgschema.TableStageRecord)
	for _, r := range places.Records {
		got := pgiJSONStrings(t, e, `SELECT to_jsonb(fields)::text FROM `+tbl+` WHERE dataset_no = $1 AND row_no = $2`, ref.Datasets["ds_places"], r.No)
		if strings.Join(got, "\x00") != strings.Join(r.Fields, "\x00") {
			t.Errorf("the cells of the row %d: %q, want %q", r.No, got, r.Fields)
		}
		if hash := e.text(t, `SELECT encode(raw_hash, 'hex') FROM `+tbl+` WHERE dataset_no = $1 AND row_no = $2`, ref.Datasets["ds_places"], r.No); hash != r.RawHash {
			t.Errorf("the hash of the row %d is %s, want %s", r.No, hash, r.RawHash)
		}
		if got := e.text(t, `SELECT key || '|' || place || '|' || filtered FROM `+tbl+` WHERE dataset_no = $1 AND row_no = $2`, ref.Datasets["ds_places"], r.No); got != fmt.Sprintf("%s|%d|false", r.Key, r.Line) {
			t.Errorf("the row %d: %s", r.No, got)
		}
	}
	// the context key of a rack has the four parents the model names, nearest first, as arrays on its row.
	keys := pgiTable(ref, pgschema.TableStageKey)
	for _, k := range places.Keys {
		if k.LinkRef == "" {
			continue
		}
		var want []string
		var types []string
		for _, p := range places.Parents {
			if p.No == k.No && p.Ord == k.Ord {
				want = append(want, fmt.Sprintf("%x", p.SigHash[:]))
				types = append(types, fmt.Sprint(p.TypeNo))
			}
		}
		row := fmt.Sprintf(`dataset_no = %d AND row_no = %d AND key_ord = %d`, ref.Datasets["ds_places"], k.No, k.Ord)
		if got := e.text(t, `SELECT array_to_string(ARRAY(SELECT encode(x, 'hex') FROM unnest(anc_sigs) x), ',') FROM `+keys+` WHERE `+row); got != strings.Join(want, ",") || len(want) != 4 {
			t.Errorf("the parents of the key %d.%d: %q, want %q", k.No, k.Ord, got, want)
		}
		if got := e.text(t, `SELECT array_to_string(anc_types, ',') FROM `+keys+` WHERE `+row); got != strings.Join(types, ",") {
			t.Errorf("the types of the parents: %q, want %q", got, types)
		}
		if got := e.text(t, `SELECT encode(sig_hash, 'hex') || '|' || coalesce(cand_ref, '-') FROM `+keys+` WHERE `+row); got != fmt.Sprintf("%x|-", k.SigHash[:]) {
			t.Errorf("the key: %s", got)
		}
	}
	for _, k := range places.Keys {
		if k.LinkRef == "" && k.Cand != "" {
			row := fmt.Sprintf(`dataset_no = %d AND row_no = %d AND key_ord = %d`, ref.Datasets["ds_places"], k.No, k.Ord)
			if got := e.text(t, `SELECT cand_ref || '|' || coalesce(anc_types::text, '-') FROM `+keys+` WHERE `+row); got != k.Cand+"|-" {
				t.Errorf("the candidate id of a key with no context: %s, want %s", got, k.Cand)
			}
		}
	}
}

// [REQ-701]
func TestPgiARecordIsNewChangedOrTheSameAndOnlyNewAndChangedOnesCarryCellsKeysEndsAndClaims(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	ref := e.attach(t, kg, plan)
	first := e.open(t, kg, plan.Source.ID, "server-a")
	pgiReceive(t, e, plan, ref, first, pgiAll(), ingest.PageOptions{})
	// what a run that was applied leaves: the raw records of the three datasets
	e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate)
		SELECT src_no, dataset_no, key, raw_hash, fields, 0, 1, 0 FROM %s`, pgiTable(ref, pgschema.TableRawRecord), pgiTable(ref, pgschema.TableStageRecord)))
	if err := e.st.EndRun(e.ctx, first, ingest.RunEnd{Stage: ingest.RunCanceled}); err != nil {
		t.Fatal(err)
	}
	raw := pgiTable(ref, pgschema.TableRawRecord)
	places := ref.Datasets["ds_places"]
	e.exec(t, `UPDATE `+raw+` SET raw_hash = decode(repeat('ab', 32), 'hex') WHERE dataset_no = $1 AND key = '2'`, places)            // changed
	e.exec(t, `DELETE FROM `+raw+` WHERE dataset_no = $1 AND key = '3'`, places)                                                      // new
	e.exec(t, `UPDATE `+raw+` SET fate = $2 WHERE dataset_no = $1 AND key = '1'`, places, pgschema.FateFiltered)                      // a rule filtered it before and not now: changed
	e.exec(t, `UPDATE `+raw+` SET fate = $2 WHERE dataset_no = $1 AND key = 'SV1'`, ref.Datasets["ds_servers"], pgschema.FateProblem) // the same
	second := e.open(t, kg, plan.Source.ID, "server-a")
	pgiReceive(t, e, plan, ref, second, pgiAll(), ingest.PageOptions{})
	rec := pgiTable(ref, pgschema.TableStageRecord)
	kinds := e.text(t, fmt.Sprintf(`SELECT string_agg(key || '=' || change_kind || ':' || (raw_hash IS NOT NULL) || ':' || (fields IS NOT NULL), ' ' ORDER BY dataset_no, key) FROM %s`, rec))
	want := "1=1:true:true 2=1:true:true 3=0:true:true SV1=2:false:false SV2=2:false:false u-1=2:false:false u-2=2:false:false"
	if kinds != want {
		t.Fatalf("what the receiving found:\n got %s\nwant %s", kinds, want)
	}
	// the key, the ends and the claims of a record that is the same are not stored again; the ones of the others are.
	byRecord := func(table string) string {
		return e.text(t, fmt.Sprintf(`SELECT string_agg(r.key || ':' || n::text, ' ' ORDER BY r.dataset_no, r.key) FROM %s r JOIN
			(SELECT dataset_no, row_no, count(*) n FROM %s GROUP BY 1, 2) c USING (dataset_no, row_no)`, rec, pgiTable(ref, table)))
	}
	if got := byRecord(pgschema.TableStageKey); got != "1:2 2:2 3:2" {
		t.Errorf("the keys: %s", got)
	}
	if got := byRecord(pgschema.TableStageEnd); got != "1:1 2:1 3:1" {
		t.Errorf("the ends: %s", got)
	}
	if got := byRecord(pgschema.TableStageClaim); got != "" {
		t.Errorf("the claims of VMs that did not change: %s", got)
	}
	// a record that comes back in an unused state (fate 3) and is not filtered is read again as changed.
	if err := e.st.EndRun(e.ctx, second, ingest.RunEnd{Stage: ingest.RunCanceled}); err != nil {
		t.Fatal(err)
	}
	e.exec(t, `UPDATE `+raw+` SET fate = $2 WHERE dataset_no = $1 AND key = 'u-2'`, ref.Datasets["ds_vms"], pgschema.FateUnused)
	third := e.open(t, kg, plan.Source.ID, "server-a")
	pgiReceive(t, e, plan, ref, third, pgiAll(), ingest.PageOptions{})
	if got := e.text(t, `SELECT string_agg(key, ',' ORDER BY key) FROM `+rec+` WHERE dataset_no = $1 AND change_kind = $2`, ref.Datasets["ds_vms"], pgschema.ReceiveChanged); got != "u-2" {
		t.Errorf("the VMs that changed: %q", got)
	}
	if n := e.count(t, `SELECT count(*) FROM `+pgiTable(ref, pgschema.TableStageClaim)+` WHERE dataset_no = $1`, ref.Datasets["ds_vms"]); n != 1 {
		t.Errorf("%d claims of the VMs", n)
	}
}

// [REQ-701]
func TestPgiAKeyAnEndAndAClaimOfARowThatARuleFilteredOutAreHeldAndKeepTheNumberOfTheirFeedAsItsComplement(t *testing.T) {
	filter := `{"op":"filter","column":"power_state","in":["on"]}`
	mapping := strings.Replace(pgiMappingJSON, `{"feeds":[`, `{"refinements":[{"id":"r_on","input":"ds_servers","ops":[`+filter+`]}],"feeds":[`, 1)
	mapping = strings.Replace(mapping, `"id":"f_server","dataset":"ds_servers"`, `"id":"f_server","dataset":"r_on"`, 1)
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, mapping)
	kg := e.kg(t, 1)
	ref := e.attach(t, kg, plan)
	lease := e.open(t, kg, plan.Source.ID, "server-a")
	_, pages := pgiReceive(t, e, plan, ref, lease, pgiAll(), ingest.PageOptions{})
	servers := ref.Datasets["ds_servers"]
	if got := e.text(t, fmt.Sprintf(`SELECT string_agg(key || '=' || filtered, ' ' ORDER BY key) FROM %s WHERE dataset_no = %d`, pgiTable(ref, pgschema.TableStageRecord), servers)); got != "SV1=false SV2=true" {
		t.Fatalf("the rows a rule filtered out: %s", got)
	}
	var feed, def int
	for _, p := range pages {
		for _, k := range p.Keys {
			if k.Held && p.Dataset == "ds_servers" {
				feed = k.Feed
			}
		}
		for _, en := range p.Ends {
			if en.Held && p.Dataset == "ds_servers" {
				def = en.Def
			}
		}
	}
	keys := e.text(t, fmt.Sprintf(`SELECT string_agg(row_no || ':' || feed_no, ' ' ORDER BY row_no) FROM %s WHERE dataset_no = %d`, pgiTable(ref, pgschema.TableStageKey), servers))
	if want := fmt.Sprintf("0:%d 1:%d", feed, ^feed); keys != want || feed != 1 {
		t.Errorf("the keys of the servers: %q, want %q", keys, want)
	}
	ends := e.text(t, fmt.Sprintf(`SELECT string_agg(row_no || ':' || def_no, ' ' ORDER BY row_no) FROM %s WHERE dataset_no = %d`, pgiTable(ref, pgschema.TableStageEnd), servers))
	if want := fmt.Sprintf("0:%d 1:%d", def, ^def); ends != want {
		t.Errorf("the ends of the servers: %q, want %q", ends, want)
	}
	if held(3, false) != 3 || held(3, true) != -4 {
		t.Error("the number of a held feed is its complement")
	}
}

// [REQ-701]
func TestPgiTheProblemsOfARecordAreOnItsRowAndTheProblemsOfAPageOnTheRowMinusOneAndAnIncompleteDatasetIsToldToo(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	ref := e.attach(t, kg, plan)
	lease := e.open(t, kg, plan.Source.ID, "server-a")
	texts := pgiAll()
	// a line with no key, a row with no manufacturer (no identity), and a header that lacks a column.
	texts["ds_places"] = "row_id,site,building,floor,room,rack,tag\n1,HQ,B1,F1,R1,rack a,RK1\n,HQ,B1,F1,R1,rack b,RK2\n3,HQ,B1,F1,R1,rack c,RK3\n"
	texts["ds_servers"] = "asset_id,serial,manufacturer,rack_tag,power_state\nSV1,,Zorvane,RK1,on\n"
	texts["ds_vms"] = "{\"uuid\":\"u-1\",\"name\":\"a\"}\n{\"uuid\":\"u-2\",\"name\":\"b\"}\n{\"uuid\":\"u-3\",\"name\":\"c\"}\n"
	rep, pages := pgiReceive(t, e, plan, ref, lease, texts, ingest.PageOptions{MaxRows: 10, MaxDatasetRows: 2})
	// ds_places is cut at 2 data lines (the 3rd is not read): the line with no key is one of them, so one record only is read.
	if len(rep.Datasets) != 3 || rep.Datasets[0].Complete || rep.Datasets[2].Complete || !rep.Datasets[1].Complete {
		t.Fatalf("%+v", rep.Datasets)
	}
	pageRows := pgiTable(ref, pgschema.TableStageRecord)
	got := e.text(t, fmt.Sprintf(`SELECT string_agg(dataset_no || ':' || page || ':' || problems::text, ' | ' ORDER BY dataset_no) FROM %s WHERE row_no = -1`, pageRows))
	for _, want := range []string{`"kind": "unreadable_line"`, `"kind": "incomplete_capture"`, `"line": 3`} {
		if !strings.Contains(got, want) {
			t.Errorf("the problems of the pages lack %s:\n%s", want, got)
		}
	}
	if n := e.count(t, fmt.Sprintf(`SELECT count(*) FROM %s WHERE row_no = -1 AND key = '' AND change_kind = %d AND raw_hash IS NULL AND fields IS NULL AND NOT filtered`, pageRows, pgschema.ReceiveSame)); n != 2 {
		t.Errorf("%d rows of pages that have problems of the page", n)
	}
	// the problem of a record is on its row, with the feed and the field.
	row := e.text(t, fmt.Sprintf(`SELECT problems::text FROM %s WHERE dataset_no = %d AND row_no = 0`, pageRows, ref.Datasets["ds_servers"]))
	if !strings.Contains(row, `"kind": "missing_identity"`) || !strings.Contains(row, `"feed": "f_server"`) || !strings.Contains(row, `"field": "serial"`) {
		t.Errorf("the problem of the server: %s", row)
	}
	// the facts about the datasets are in the counts of the run, where the step that compares a source reads them.
	counts := e.text(t, `SELECT counts #>> '{receive,datasets}' FROM sync_run WHERE run_no = $1`, lease.No)
	var lines []datasetCount
	if err := json.Unmarshal([]byte(counts), &lines); err != nil || len(lines) != 3 {
		t.Fatalf("%v %s", err, counts)
	}
	if lines[0].Dataset != "ds_places" || lines[0].Complete || lines[0].Reason == "" || lines[0].Unreadable != 1 || lines[0].Records != 1 || lines[0].DatasetNo != ref.Datasets["ds_places"] {
		t.Errorf("ds_places: %+v", lines[0])
	}
	if lines[1].Dataset != "ds_servers" || !lines[1].Complete || lines[1].Reason != "" {
		t.Errorf("ds_servers: %+v", lines[1])
	}
	if got := e.text(t, `SELECT counts #>> '{receive,complete}' FROM sync_run WHERE run_no = $1`, lease.No); got != "false" {
		t.Errorf("a source with a dataset that was not read to its end is not complete: %s", got)
	}
	if len(pages) == 0 {
		t.Fatal("no page")
	}
}
