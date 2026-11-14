package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/filejournal"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// keepRun is one `praxium ingest run -data-dir` of the sample files in this process; racks names the file of the
// racks dataset, so a test can take a row away.
func keepRun(t *testing.T, d wiring, dir, racks string, extra ...string) (code int, out, errText string) {
	t.Helper()
	d.open = systemOpen
	args := []string{"ingest", "run", "-data-dir", dir, "-source", sampleDir + "source.json", "-mapping", sampleDir + "mapping.json",
		"-input", "ds_racks=" + racks, "-input", "ds_servers=" + sampleDir + "sample/servers.csv",
		"-input", "ds_hosts=" + sampleDir + "sample/hosts.jsonl", "-input", "ds_vms=" + sampleDir + "sample/vms.jsonl",
		"-input", "ds_vm_power=" + sampleDir + "sample/vm-power.csv"}
	var stdout, stderr bytes.Buffer
	code = execute(append(args, extra...), &stdout, &stderr, d)
	return code, stdout.String(), stderr.String()
}

const keepRacks = sampleDir + "sample/racks.csv"

// keepOneRack writes the racks file with only its first rack, as a file from which a row was taken away.
func keepOneRack(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(keepRacks)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(raw), "\n")
	path := filepath.Join(t.TempDir(), "racks.csv")
	if err := os.WriteFile(path, []byte(strings.Join(lines[:2], "")), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func keepClock(d wiring) *kernel.ManualClock { return d.clock.(*kernel.ManualClock) }

// [REQ-291]
func TestIngestDataDirKeepsTheKGAndTheStateAcrossRuns(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	code, out, errText := keepRun(t, d, dir, keepRacks)
	if code != exitOK || !strings.Contains(out, "changed=true new_version=true") || !strings.Contains(out, "kept in "+dir) {
		t.Fatalf("the first run: %d\n%s%s", code, out, errText)
	}
	if _, err := os.Stat(filepath.Join(dir, ingest.StateFileName)); err != nil {
		t.Fatalf("the state must be a file of the folder: %v", err)
	}
	if code, out, _ = keepRun(t, d, dir, keepRacks); code != exitOK || !strings.Contains(out, "run=2") || !strings.Contains(out, "changed=false new_version=false") {
		t.Fatalf("the same files in a second process change nothing: %d\n%s", code, out)
	}
	one := keepOneRack(t)
	for i := 0; i < 3; i++ {
		_, _ = keepClock(d).Advance(time.Hour)
		if code, out, _ = keepRun(t, d, dir, one); code != exitOK || !strings.Contains(out, "raw_unseen=1") || !strings.Contains(out, "retired_objects=0") {
			t.Fatalf("a row that stays away is counted, not retired, at once (run %d): %d\n%s", i+3, code, out)
		}
	}
	_, _ = keepClock(d).Advance(25 * time.Hour)
	if code, out, _ = keepRun(t, d, dir, one); code != exitOK || !strings.Contains(out, "retired_objects=1") || !strings.Contains(out, "raw_deleted=1") {
		t.Fatalf("the rule must go on across runs and retire the row: %d\n%s", code, out)
	}
}

// [REQ-291]
func TestIngestDataDirRefusesAFolderThatDoesNotFitItsState(t *testing.T) {
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	fresh := func() string {
		dir := filepath.Join(t.TempDir(), "data")
		if code, out, errText := keepRun(t, d, dir, keepRacks); code != exitOK {
			t.Fatalf("setup: %d %s%s", code, out, errText)
		}
		return dir
	}
	noState := fresh()
	if err := os.Remove(filepath.Join(noState, ingest.StateFileName)); err != nil {
		t.Fatal(err)
	}
	noKG := fresh()
	for _, pattern := range []string{"log-*", "snap-*"} {
		files, _ := filepath.Glob(filepath.Join(noKG, pattern))
		for _, f := range files {
			if err := os.Remove(f); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, c := range map[string]struct{ dir, want string }{
		"a KG without its state": {noState, "did not build"},
		"a state without its KG": {noKG, "holds no KG"},
	} {
		code, _, errText := keepRun(t, d, c.dir, keepRacks)
		if code != exitFailure || !strings.Contains(errText, c.want) {
			t.Errorf("%s: %d %s", name, code, errText)
		}
	}
	if code, _, errText := keepRun(t, d, noState, keepRacks); code != exitFailure {
		t.Errorf("a refused folder stays refused: %d %s", code, errText)
	}
}

// [REQ-291]
func TestIngestDataDirRefusesADemoFolderAndALockedOne(t *testing.T) {
	demoDir := filepath.Join(t.TempDir(), "demo")
	r := startDemo(t, true, "-data-dir", demoDir)
	if code := demoStop(r); code != exitOK {
		t.Fatalf("demo stop: %d", code)
	}
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	if code, _, errText := keepRun(t, d, demoDir, keepRacks); code != exitFailure || !strings.Contains(errText, "did not build") {
		t.Fatalf("a folder of the demo is not an ingestion folder: %d %s", code, errText)
	}
	locked := filepath.Join(t.TempDir(), "locked")
	disk, err := openDataDir(locked)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := disk.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unlock() }()
	code, _, errText := keepRun(t, d, locked, keepRacks)
	if code != exitFailure || !strings.Contains(errText, "stop it and run this again") || kernel.CodeOf(lockHint(kernel.Errorf(filejournal.CodeLocked, "x"))) != filejournal.CodeLocked {
		t.Fatalf("a folder another process has open must say what to do: %d %s", code, errText)
	}
}

// [REQ-292]
func TestIngestRunRefusesAnInputOfAnUndefinedDataset(t *testing.T) {
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	d.open = systemOpen
	args := ingestArgs("-input", "ds_nope="+sampleDir+"sample/racks.csv")
	var out, errOut bytes.Buffer
	code := execute(args, &out, &errOut, d)
	text := errOut.String()
	for _, want := range []string{`dataset "ds_nope"`, "src_example_files", "ds_racks, ds_servers, ds_hosts, ds_vms, ds_vm_power", "For details: praxium ingest run --help"} {
		if !strings.Contains(text, want) {
			t.Errorf("the error must say %q: %s", want, text)
		}
	}
	if code != exitUsage || out.Len() != 0 || strings.Count(text, "\n") > 4 {
		t.Fatalf("an undefined dataset is a short usage error, got %d (%d lines) %s", code, strings.Count(text, "\n"), out.String())
	}
}

// [REQ-292]
func TestIngestRunSaysWhatItDidNotCollectAndWhetherItKeptTheResult(t *testing.T) {
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	d.open = systemOpen
	args := []string{"ingest", "run", "-source", sampleDir + "source.json", "-mapping", sampleDir + "mapping.json", "-input", "ds_racks=" + sampleDir + "sample/racks.csv"}
	var out, errOut bytes.Buffer
	if code := execute(args, &out, &errOut, d); code != exitOK {
		t.Fatalf("%d %s", code, errOut.String())
	}
	for _, want := range []string{"note: dataset ds_servers has no -input", "completed without problems", "Pass -data-dir <folder> to keep it"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the output must say %q:\n%s", want, out.String())
		}
	}
}

// [REQ-292]
func TestIngestRunExitCodesAndTheLastLine(t *testing.T) {
	data := "asset_id,serial_number,manufacturer,rack_asset_id,power_state\nSV-1,,Zorvane,RK-1001,on\nSV-2,SN-9,Zorvane,RK-1001,on\n"
	bad := filepath.Join(t.TempDir(), "servers.csv")
	if err := os.WriteFile(bad, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	d.open = systemOpen
	args := []string{"ingest", "run", "-source", sampleDir + "source.json", "-mapping", sampleDir + "mapping.json", "-input", "ds_servers=" + bad}
	var out bytes.Buffer
	if code := execute(args, &out, &bytes.Buffer{}, d); code != exitOK {
		t.Fatalf("problems that were set aside do not fail the run by default: %d", code)
	}
	if !strings.Contains(out.String(), `completed with 2 problems (see "problem" lines above)`) {
		t.Fatalf("the verdict must be a line of its own:\n%s", out.String())
	}
	out.Reset()
	if code := execute(append(args, "-fail-on-problem"), &out, &bytes.Buffer{}, d); code != exitProblems || exitProblems == exitFailure || exitProblems == exitUsage {
		t.Fatalf("-fail-on-problem has its own exit code, got %d", code)
	}
	if !strings.Contains(out.String(), "completed with 2 problems") {
		t.Fatalf("the report is printed before the code is returned:\n%s", out.String())
	}
	var clean bytes.Buffer
	if code := execute(ingestArgs("-fail-on-problem"), &clean, &bytes.Buffer{}, d); code != exitOK {
		t.Fatalf("-fail-on-problem with no problem is 0, got %d", code)
	}
}

// [REQ-292]
func TestIngestRunHelpExplainsTheOutputAndTheExitCodes(t *testing.T) {
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	var out, errOut bytes.Buffer
	if code := execute([]string{"ingest", "run", "--help"}, &out, &errOut, d); code != exitOK || errOut.Len() != 0 {
		t.Fatalf("%d %s", code, errOut.String())
	}
	for _, want := range []string{"volatile", "graph_hash", "raw_added", "raw_unseen", "Exit codes", "-fail-on-problem", "-data-dir", "(default", "example:"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the help must say %q:\n%s", want, out.String())
		}
	}
}

// [REQ-292]
func TestIngestVerdictLineIsGrammatical(t *testing.T) {
	var out bytes.Buffer
	printEnd(&out, 1, "")
	printEnd(&out, 0, "data")
	for _, want := range []string{`completed with 1 problem (see "problem" lines above)`, "completed without problems", "kept in data (KG main)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q:\n%s", want, out.String())
		}
	}
}
