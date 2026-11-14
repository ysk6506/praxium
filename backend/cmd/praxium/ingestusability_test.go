package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cmdRun is one command line of the program with the files of the machine.
func cmdRun(t *testing.T, args ...string) (code int, out, errText string) {
	t.Helper()
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	d.open = systemOpen
	var stdout, stderr bytes.Buffer
	code = execute(args, &stdout, &stderr, d)
	return code, stdout.String(), stderr.String()
}

// writeFile writes a file of a test and gives its path.
func putFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// initExamples runs `ingest init` into a new folder and gives the folder.
func initExamples(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "ex")
	if code, so, se := cmdRun(t, "ingest", "init", "-out", out); code != exitOK {
		t.Fatalf("init: %d %s %s", code, so, se)
	}
	return out
}

// [REQ-322]
func TestTheExamplesGiveTheirServersAnIdentifierThatFindsThem(t *testing.T) {
	ex := initExamples(t)
	data := filepath.Join(t.TempDir(), "data")
	book := ex + "/excel/datacenter.xlsx"
	if code, so, se := cmdRun(t, "ingest", "run", "-data-dir", data, "-source", ex+"/excel/source.json", "-mapping", ex+"/excel/mapping.json",
		"-input", "ds_racks="+book, "-input", "ds_servers="+book); code != exitOK || !strings.Contains(so, "completed without problems") {
		t.Fatalf("the Excel example: %d %s %s", code, so, se)
	}
	csv := ex + "/csv/"
	if code, so, se := cmdRun(t, "ingest", "run", "-data-dir", data, "-source", csv+"source.json", "-mapping", csv+"mapping.json",
		"-input", "ds_racks="+csv+"racks.csv", "-input", "ds_servers="+csv+"servers.csv", "-input", "ds_hosts="+csv+"hosts.jsonl",
		"-input", "ds_vms="+csv+"vms.jsonl", "-input", "ds_vm_power="+csv+"vm-power.csv"); code != exitOK || !strings.Contains(so, "completed without problems") {
		t.Fatalf("the CSV example: %d %s %s", code, so, se)
	}
	r := startLocal(t, true, data)
	for scheme, want := range map[string]string{"bmc_mac:02:00:00:00:40:02": "XS-4002", "bmc_mac:02-00-00-00-20-03": "SV-2003", "bmc_mac:020000004003": "XS-4003"} {
		code, out := demoCLI(&r.demoRun, "query", "get", "--identifier", scheme)
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("--identifier %s must find %s: %d %s", scheme, want, code, out)
		}
	}
	code, out := demoCLI(&r.demoRun, "query", "find", "--identifier", "bmc_mac:02:00:00:00:20:01")
	if code != 0 || !strings.Contains(out, "SV-2001") {
		t.Fatalf("find by identifier: %d %s", code, out)
	}
	if code, out = demoCLI(&r.demoRun, "query", "get", "--identifier", "asset_tag:XR-3001"); code == 0 || !strings.Contains(out, "bmc_mac") {
		t.Fatalf("a scheme that does not exist is answered with the schemes that do: %d %s", code, out)
	}
}

// [REQ-320]
func TestAWorkbookWithoutTheSheetFailsTheRunAndSaysWhichSheetsItHas(t *testing.T) {
	ex := initExamples(t)
	src, err := os.ReadFile(ex + "/excel/source.json")
	if err != nil {
		t.Fatal(err)
	}
	wrong := putFile(t, t.TempDir(), "source.json", []byte(strings.Replace(string(src), `"name": "랙"`, `"name": "랙 목록"`, 1)))
	book := ex + "/excel/datacenter.xlsx"
	args := []string{"ingest", "run", "-source", wrong, "-mapping", ex + "/excel/mapping.json", "-input", "ds_racks=" + book, "-input", "ds_servers=" + book}
	code, out, _ := cmdRun(t, args...)
	if code != exitOK || !strings.Contains(out, "not_read=") || !strings.Contains(out, `no sheet "랙 목록"`) || !strings.Contains(out, `it has "랙", "서버 목록"`) || strings.Contains(out, "completed without problems") {
		t.Fatalf("the missing sheet must be on the dataset line and in the problems: %d\n%s", code, out)
	}
	if code, _, _ = cmdRun(t, append(args, "-fail-on-problem")...); code != exitProblems {
		t.Fatalf("-fail-on-problem: %d", code)
	}
	cols := strings.Replace(string(src), `"paths": ["이름"]`, `"paths": ["명칭"]`, 1)
	_, out, _ = cmdRun(t, append([]string{"ingest", "run", "-source", putFile(t, t.TempDir(), "source.json", []byte(cols))}, args[4:]...)...)
	if !strings.Contains(out, `the header it read (row 1) has the columns: "자산번호", "이름", "높이(U)"`) {
		t.Fatalf("a missing column is answered with the header that was read:\n%s", out)
	}
}

// [REQ-323]
func TestACSVInALocalCodePageStopsInspectAndRunWithWhatToDo(t *testing.T) {
	ex := initExamples(t)
	euckr := putFile(t, t.TempDir(), "racks.csv", []byte("\xc0\xda\xbb\xea\xb9\xf8\xc8\xa3,rack_name,total_u\nRK1,a,42\n"))
	code, _, se := cmdRun(t, "ingest", "inspect", "-input", euckr)
	if code != exitFailure || !strings.Contains(se, "not UTF-8") || !strings.Contains(se, "CSV UTF-8") {
		t.Fatalf("inspect: %d %s", code, se)
	}
	csv := ex + "/csv/"
	code, out, se := cmdRun(t, "ingest", "run", "-source", csv+"source.json", "-mapping", csv+"mapping.json", "-input", "ds_racks="+euckr)
	if code != exitFailure || !strings.Contains(se, "dataset ds_racks") || !strings.Contains(se, "not UTF-8") || strings.Contains(out, "completed") {
		t.Fatalf("run must stop: %d %s %s", code, out, se)
	}
	bom := putFile(t, t.TempDir(), "bom.csv", []byte("\xef\xbb\xbfasset_id,rack_name,total_u\nRK1,a,42\n"))
	if code, so, se := cmdRun(t, "ingest", "inspect", "-input", bom); code != exitOK || !strings.Contains(so, "rack_name") {
		t.Fatalf("a UTF-8 byte order mark is fine: %d %s %s", code, so, se)
	}
}

// [REQ-324]
func TestATabSeparatedFileIsSaidToNeedTheDelimiterAndReadWithIt(t *testing.T) {
	dir := t.TempDir()
	tsv := putFile(t, dir, "racks.csv", []byte("asset_id\track_name\ttotal_u\nRK1\ta\t42\nRK2\tb\t45\n"))
	code, out, _ := cmdRun(t, "ingest", "inspect", "-input", tsv)
	if code != exitOK || !strings.Contains(out, "note: ") || !strings.Contains(out, "-delimiter tab") {
		t.Fatalf("%d %s", code, out)
	}
	if code, out, _ = cmdRun(t, "ingest", "inspect", "-input", tsv, "-delimiter", "tab"); code != exitOK || strings.Contains(out, "note: ") || !strings.Contains(out, "total_u") {
		t.Fatalf("%d %s", code, out)
	}
	if code, _, se := cmdRun(t, "ingest", "inspect", "-input", tsv, "-delimiter", "colon"); code != exitUsage || !strings.Contains(se, "none of comma, tab, semicolon, pipe") {
		t.Fatalf("%d %s", code, se)
	}
	ex := initExamples(t)
	src, _ := os.ReadFile(ex + "/csv/source.json")
	with := func(delimiter string) string {
		return putFile(t, t.TempDir(), "source.json", []byte(strings.Replace(string(src), `"id": "ds_racks",`, `"id": "ds_racks", `+delimiter, 1)))
	}
	run := func(source string) string {
		_, so, _ := cmdRun(t, "ingest", "run", "-source", source, "-mapping", ex+"/csv/mapping.json", "-input", "ds_racks="+tsv)
		return so
	}
	if so := run(with("")); !strings.Contains(so, `"delimiter": "tab"`) {
		t.Fatalf("a run with the comma says the file may use tabs:\n%s", so)
	}
	if so := run(with(`"delimiter": "tab",`)); !strings.Contains(so, "dataset ds_racks collected=true received=2 unreadable=0 read=2") {
		t.Fatalf("a run with the delimiter reads both rows:\n%s", so)
	}
}

// [REQ-326]
func TestInspectDraftsASourceThatRunsOnceTheNaturalKeyIsChosen(t *testing.T) {
	ex := initExamples(t)
	servers := ex + "/csv/servers.csv"
	code, out, se := cmdRun(t, "ingest", "inspect", "-input", servers, "-emit-source")
	var draft map[string]any
	if code != exitOK || json.Unmarshal([]byte(out), &draft) != nil || !strings.Contains(se, "note: ds_servers: natural_key is left to you") || !strings.Contains(se, "asset_id") {
		t.Fatalf("the standard output is the definition and the standard error the notes: %d\n%s\n%s", code, out, se)
	}
	mapping := putFile(t, t.TempDir(), "mapping.json", []byte(`{"feeds":[{"id":"f","dataset":"ds_servers","object_type":"PhysicalServer","creates_objects":true,"basis":"observed",
	"attributes":[{"attribute":"name","column":"asset_id"},{"attribute":"manufacturer","column":"manufacturer"},{"attribute":"serial_number","column":"serial_number"}],
	"identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial_number"}],"priority":1}]}]}`))
	source := putFile(t, t.TempDir(), "source.json", []byte(out))
	code, so, se := cmdRun(t, "ingest", "run", "-source", source, "-mapping", mapping, "-input", "ds_servers="+servers)
	if code != exitFailure || !strings.Contains(se, `"asset_id", "serial_number", "manufacturer"`) || !strings.Contains(se, "natural_key") {
		t.Fatalf("the draft as it is tells what to fill in: %d %s %s", code, so, se)
	}
	edited := putFile(t, t.TempDir(), "source.json", []byte(strings.Replace(out, "FILL IN: the column that tells one row from another", "asset_id", 1)))
	code, so, se = cmdRun(t, "ingest", "run", "-source", edited, "-mapping", mapping, "-input", "ds_servers="+servers)
	if code != exitOK || !strings.Contains(so, "objects=3 ") || !strings.Contains(so, "completed without problems") {
		t.Fatalf("the edited draft collects the file: %d %s %s", code, so, se)
	}
	if code, _, se = cmdRun(t, "ingest", "inspect", "-input", servers, "-emit-source", "-o", "json"); code != exitUsage || !strings.Contains(se, "-emit-source") {
		t.Fatalf("%d %s", code, se)
	}
	book := ex + "/excel/datacenter.xlsx"
	if code, so, _ = cmdRun(t, "ingest", "inspect", "-input", book, "-emit-source", "-sheet", "랙"); code != exitOK || strings.Contains(so, "서버 목록") || !strings.Contains(so, `"header_row": 1`) {
		t.Fatalf("one sheet: %d %s", code, so)
	}
}

// [REQ-327]
func TestGlobalFlagsBeforeIngestAreExplained(t *testing.T) {
	code, _, se := cmdRun(t, "--endpoint", "http://127.0.0.1:1", "--token", "t", "ingest", "run")
	if code != exitUsage || !strings.Contains(se, "ingest does not ask a server") || !strings.Contains(se, "--endpoint, --token") {
		t.Fatalf("%d %s", code, se)
	}
	if code, _, se = cmdRun(t, "--endpoint", "http://127.0.0.1:1", "--token", "t", "nothing"); code != exitUsage || !strings.Contains(se, `unknown command "nothing"`) {
		t.Fatalf("%d %s", code, se)
	}
}

// [REQ-328]
func TestTheTableFormShowsNestedValuesAndTheJSONFormIsUnchanged(t *testing.T) {
	r := startDemo(t, true)
	code, out := demoCLI(r, "health")
	if code != 0 || strings.Contains(out, "{...}") || !strings.Contains(out, "live") || !strings.Contains(out, "ok (200)") {
		t.Fatalf("health: %d %s", code, out)
	}
	code, out = demoCLI(r, "types", "get", "Rack")
	for _, want := range []string{"1. strong: link located_in + property name", "2. weak: property asset_tag", "total_u (integer, required)", "width_mm (quantity, unit mm)"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("types get must show %q: %d\n%s", want, code, out)
		}
	}
	if strings.Contains(out, "{...}") {
		t.Errorf("no value is left as {...}:\n%s", out)
	}
	if code, out = demoCLI(r, "types", "get", "PhysicalServer"); !strings.Contains(out, "3. weak: scheme bmc_mac") {
		t.Fatalf("a scheme key is shown by its scheme: %d\n%s", code, out)
	}
	code, out = demoCLI(r, "--json", "types", "get", "Rack")
	var doc map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &doc) != nil || doc["identity_keys"] == nil || !strings.Contains(out, `"ref": "pr.rack.asset_tag"`) {
		t.Fatalf("--json keeps the whole structure: %d %s", code, out)
	}
	if code, out = demoCLI(r, "--json", "health"); !strings.Contains(out, `"live": 200`) {
		t.Fatalf("%s", out)
	}
}

// [REQ-329]
func TestIngestHelpPointsToTheGuideAndExplainsTheSummaryLines(t *testing.T) {
	_, run, _ := cmdRun(t, "ingest", "run", "--help")
	for _, want := range []string{"docs/guides/ingest-definitions.md", "skipped_blank_row", "skipped_hidden_row", "skipped_after_end", "not_read=", "undeclared_column", "ignore_undeclared_columns", "formula_without_value"} {
		if !strings.Contains(run, want) {
			t.Errorf("ingest run --help must say %q:\n%s", want, run)
		}
	}
	_, group, _ := cmdRun(t, "ingest", "--help")
	if !strings.Contains(group, "docs/guides/ingest-definitions.md") || !strings.Contains(group, "-emit-source") {
		t.Errorf("ingest --help:\n%s", group)
	}
	_, inspect, _ := cmdRun(t, "ingest", "inspect", "--help")
	for _, want := range []string{"-delimiter", "-emit-source", "UTF-8"} {
		if !strings.Contains(inspect, want) {
			t.Errorf("ingest inspect --help must say %q:\n%s", want, inspect)
		}
	}
}
