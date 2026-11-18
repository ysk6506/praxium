package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/definitions"
	"github.com/ysk6506/praxium/backend/internal/definitionsfile"
	"github.com/ysk6506/praxium/backend/internal/model"
)

const (
	ledgerCSV = "asset_id,serial_number,manufacturer,power_state,management_department,introduced_year\n" +
		"SV-3001,SN-C100,Zorvane,on,Infra Team 1,2021\n" +
		"SV-3002,SN-C200,Zorvane,on,Infra Team 2,2022\n" +
		"SV-3003,SN-C300,Quillon,off,Infra Team 1,2019\n"
	ledgerSource = `{"id":"src_asset_ledger","name":"asset ledger","adapter":"file","datasets":[{"id":"ds_ledger","parser":"csv","natural_key":["asset_id"],
	  "columns":[{"name":"asset_id"},{"name":"serial_number"},{"name":"manufacturer"},{"name":"power_state"},{"name":"management_department"},{"name":"introduced_year"}]}]}`
	// ledgerMapping reads the department and the year into organization properties. %s is the name of the first one.
	ledgerMapping = `{"feeds":[{"id":"f_server","dataset":"ds_ledger","object_type":"PhysicalServer","creates_objects":true,"retires_objects":true,"basis":"declared",
	  "attributes":[{"attribute":"name","column":"asset_id"},{"attribute":"manufacturer","column":"manufacturer"},{"attribute":"serial_number","column":"serial_number"},
	    {"attribute":"power_state","column":"power_state"},{"attribute":"%s","column":"management_department"},{"attribute":"org.introduced_year","column":"introduced_year"}],
	  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial_number"}],"priority":1}]}]}`
)

// ledgerFiles are the asset ledger, its source definition and a mapping that names the department attribute as given.
type ledgerFiles struct{ csv, source, mapping string }

func writeLedger(t *testing.T, departmentAttribute string) ledgerFiles {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	return ledgerFiles{csv: write("ledger.csv", ledgerCSV), source: write("source.json", ledgerSource),
		mapping: write("mapping.json", strings.Replace(ledgerMapping, "%s", departmentAttribute, 1))}
}

// ingestLedger is `praxium ingest run -data-dir` of the ledger.
func ingestLedger(t *testing.T, d wiring, dir string, l ledgerFiles) (code int, out, errOut string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code = execute([]string{"ingest", "run", "-data-dir", dir, "-source", l.source, "-mapping", l.mapping, "-input", "ds_ledger=" + l.csv}, &stdout, &stderr, d)
	return code, stdout.String(), stderr.String()
}

// [REQ-373] [REQ-371]
func TestOrgPropertiesGoFromTheFileThroughIngestionToQueriesAndSurviveARestart(t *testing.T) {
	d := modelWiring(t)
	dir, _ := appliedFolder(t, d)
	ledger := writeLedger(t, "org.management_department")
	code, out, errOut := ingestLedger(t, d, dir, ledger)
	if code != exitOK || !strings.Contains(out, "objects=3") || !strings.Contains(out, "completed without problems") {
		t.Fatalf("the ledger with its own columns: %d\n%s%s", code, out, errOut)
	}
	for round := 1; round <= 2; round++ {
		r := startLocal(t, true, dir)
		for name, c := range map[string]struct {
			args       []string
			have, lack string
		}{
			"equals": {[]string{"--attr", "org.management_department.equals=Infra Team 1"}, "SV-3001", "SV-3002"},
			"in":     {[]string{"--attr", "org.management_department.in=Infra Team 2,Infra Team 9"}, "SV-3002", "SV-3001"},
			"prefix": {[]string{"--attr", "org.management_department.prefix=Infra"}, "SV-3003", "nothing"},
			"range":  {[]string{"--attr", "org.introduced_year.range=2020..2023"}, "SV-3002", "SV-3003"},
			"two":    {[]string{"--attr", "org.management_department.equals=Infra Team 1", "--attr", "org.introduced_year.range=2020.."}, "SV-3001", "SV-3003"},
		} {
			code, got := demoCLI(&r.demoRun, append([]string{"query", "find", "--type", "PhysicalServer"}, c.args...)...)
			if code != 0 || !strings.Contains(got, c.have) || strings.Contains(got, c.lack) {
				t.Errorf("round %d, %s: %d\n%s", round, name, code, got)
			}
		}
		_, shown := demoCLI(&r.demoRun, "query", "find", "--type", "PhysicalServer", "--attr", "org.introduced_year.equals=2019", "--json")
		id := firstObjectID(t, shown)
		if code, got := demoCLI(&r.demoRun, "object", "get", id); code != 0 || !strings.Contains(got, "org.management_department") || !strings.Contains(got, "Infra Team 1") || !strings.Contains(got, "2019") {
			t.Errorf("round %d: the object shows the organization attributes like the standard ones: %d\n%s", round, code, got)
		}
		if code, got := demoCLI(&r.demoRun, "types", "get", "PhysicalServer", "--json"); code != 0 || !strings.Contains(got, `"org.management_department"`) || !strings.Contains(got, `"org.introduced_year"`) {
			t.Errorf("round %d: the type lists its organization properties: %d\n%s", round, code, got)
		}
		if code := demoStop(&r.demoRun); code != 0 {
			t.Fatalf("round %d: exit %d", round, code)
		}
	}
	if code, out, errOut = ingestLedger(t, d, dir, ledger); code != exitOK || !strings.Contains(out, "changed=false new_version=false") {
		t.Fatalf("the same files again change nothing: %d\n%s%s", code, out, errOut)
	}
}

// firstObjectID finds the first object id in the JSON of a find.
func firstObjectID(t *testing.T, body string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, `"object_id": "`)
	if !ok {
		t.Fatalf("no object in %s", body)
	}
	id, _, _ := strings.Cut(rest, `"`)
	return id
}

// [REQ-370]
func TestARenamedOrgPropertyKeepsItsOldNameInMappingsAndADeprecatedOneIsRefused(t *testing.T) {
	d := modelWiring(t)
	dir, _ := appliedFolder(t, d)
	_, exported, _ := modelCmd(t, d, "export", "-data-dir", dir)
	renamed := strings.Replace(exported, `"org.management_department"`, `"org.owning_department"`, 1)
	if code, out, errOut := modelCmd(t, d, "apply", "-file", writeModelFile(t, renamed), "-data-dir", dir); code != exitOK {
		t.Fatalf("rename: %d %s%s", code, out, errOut)
	}
	if code, out, errOut := ingestLedger(t, d, dir, writeLedger(t, "org.management_department")); code != exitOK || !strings.Contains(out, "objects=3") {
		t.Fatalf("a mapping that still has the old name goes on working: %d %s%s", code, out, errOut)
	}
	if code, out, errOut := ingestLedger(t, d, dir, writeLedger(t, "org.owning_department")); code != exitOK {
		t.Fatalf("and the new name works: %d %s%s", code, out, errOut)
	}
	retired := strings.Replace(renamed, `"value_type": "integer"`, `"value_type": "integer", "status": "deprecated"`, 1)
	if code, out, errOut := modelCmd(t, d, "apply", "-file", writeModelFile(t, retired), "-data-dir", dir); code != exitOK {
		t.Fatalf("deprecate: %d %s%s", code, out, errOut)
	}
	code, out, errOut := ingestLedger(t, d, dir, writeLedger(t, "org.owning_department"))
	if code != exitFailure || !strings.Contains(errOut, "org.introduced_year is deprecated") || !strings.Contains(errOut, "remove it from the mapping") {
		t.Fatalf("a mapping onto a deprecated attribute is a definition error: %d %s%s", code, out, errOut)
	}
}

// [REQ-369]
func TestStartsWithoutDefinitionsAreAsBeforeAndNothingIsWrittenForThem(t *testing.T) {
	d := modelWiring(t)
	dir := filepath.Join(t.TempDir(), "data")
	l := writeLedger(t, "management_department")
	code, out, errOut := ingestLedger(t, d, dir, l)
	if code != exitFailure || !strings.Contains(errOut, `no attribute "management_department"`) {
		t.Fatalf("without definitions the standard model alone is used, and it has no such attribute: %d %s%s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, definitionsfile.FileName)); err == nil {
		t.Fatalf("reading definitions must never create the file")
	}
	if code, out, errOut = ingestLedger(t, d, dir, writeLedger(t, "org.management_department")); code != exitFailure || !strings.Contains(errOut, `no attribute "org.management_department"`) {
		t.Fatalf("the standard model alone: %d %s%s", code, out, errOut)
	}
}

// [REQ-370]
func TestAnOrgPropertyNamedWithoutItsPrefixIsPointedTo(t *testing.T) {
	d := modelWiring(t)
	dir, _ := appliedFolder(t, d)
	code, out, errOut := ingestLedger(t, d, dir, writeLedger(t, "management_department"))
	if code != exitFailure || !strings.Contains(errOut, `no attribute "management_department"`) || !strings.Contains(errOut, "org.management_department") ||
		!strings.Contains(errOut, "write that") {
		t.Fatalf("the hint names the organization property: %d %s%s", code, out, errOut)
	}
}

// [REQ-369]
func TestOnlyTheFileStoreReadsDefinitions(t *testing.T) {
	for store, want := range map[string]string{storeMemory: "", storePostgres: "", storeFile: "data"} {
		if got := modelFolder(serveOptions{store: store, storeDataDir: "data"}); got != want {
			t.Errorf("-store %s reads the definitions of %q, want %q", store, got, want)
		}
	}
}

// putOverlay stores an overlay body, as stored definitions that came from somewhere else could be.
func putOverlay(t *testing.T, d wiring, dir, target, body string) {
	t.Helper()
	disk, err := openDefinitionsDisk(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	store, err := definitionsfile.Open(disk)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := definitions.New(definitions.Config{Store: store, Clock: d.clock, IDs: d.ids})
	if err != nil {
		t.Fatal(err)
	}
	head, _, err := svc.Head(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Apply(context.Background(), definitions.ApplyInput{ExpectHead: head.No, Actor: definitions.Actor{ID: "test"}, Changes: []definitions.Change{
		{ItemID: "ov." + target, Kind: model.OverlayKind, Name: target, SchemaVersion: 1, Body: []byte(body)}}})
	if err != nil {
		t.Fatal(err)
	}
}

// startAttempts are the three ways to start that read a data folder, as command lines for a folder.
func startAttempts(t *testing.T, dir string) map[string][]string {
	t.Helper()
	l := writeLedger(t, "org.management_department")
	return map[string][]string{
		"ingest run": {"ingest", "run", "-data-dir", dir, "-source", l.source, "-mapping", l.mapping, "-input", "ds_ledger=" + l.csv},
		"local":      {"local", "-data-dir", dir, "-addr", "127.0.0.1:0"},
		"api serve":  {"api", "serve", "-mode", "development", "-dev-token-file", "/dev-token", "-addr", "127.0.0.1:0", "-store", "file", "-store-data-dir", dir},
	}
}

// [REQ-369]
func TestAllThreeStartsRefuseDefinitionsThatCannotBeUsedAndSayHowToGoOn(t *testing.T) {
	d, _, stop := apiDeps(map[string]string{"/dev-token": "dev-token-not-a-secret\n"}, nil)
	defer stop()
	d.open = systemOpen
	const noTarget = `{"schema_version":1,"kind":"type_overlay","target":{"kind":"object_type","id":"ot.no_such_type"},"properties":[
	  {"id":"pr.org.no_such_type.x","attr_no":100000,"name":"org.x","value_type":"vt.string","change_class":"slow"}]}`
	for name, prepare := range map[string]func(dir string){
		"a file that is damaged": func(dir string) {
			if err := os.WriteFile(filepath.Join(dir, definitionsfile.FileName), []byte(`{"format":"praxium-definitions","vers`), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"an overlay with no target": func(dir string) { putOverlay(t, d, dir, "ot.no_such_type", noTarget) },
	} {
		for command := range startAttempts(t, "unused") {
			dir := filepath.Join(t.TempDir(), "data")
			if err := os.MkdirAll(dir, 0o750); err != nil {
				t.Fatal(err)
			}
			prepare(dir)
			var stdout, stderr bytes.Buffer
			code := execute(startAttempts(t, dir)[command], &stdout, &stderr, d)
			if code != exitFailure || !strings.Contains(stderr.String(), "cannot start") || !strings.Contains(stderr.String(), "praxium model show -data-dir "+dir) ||
				!strings.Contains(stderr.String(), "move definitions.json away") {
				t.Errorf("%s with %s: %d\n%s%s", command, name, code, stdout.String(), stderr.String())
			}
		}
	}
}
