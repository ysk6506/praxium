package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleDir = "../../internal/ingestcheck/testdata/"

func ingestArgs(extra ...string) []string {
	args := []string{"ingest", "run", "-source", sampleDir + "source.json", "-mapping", sampleDir + "mapping.json",
		"-input", "ds_racks=" + sampleDir + "sample/racks.csv", "-input", "ds_servers=" + sampleDir + "sample/servers.csv",
		"-input", "ds_hosts=" + sampleDir + "sample/hosts.jsonl", "-input", "ds_vms=" + sampleDir + "sample/vms.jsonl",
		"-input", "ds_vm_power=" + sampleDir + "sample/vm-power.csv"}
	return append(args, extra...)
}

// [REQ-269]
func TestIngestRunReadsFilesAndSecondRunChangesNothing(t *testing.T) {
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	d.open = systemOpen
	var out, errOut bytes.Buffer
	if code := execute(ingestArgs("-times", "2"), &out, &errOut, d); code != exitOK {
		t.Fatalf("code %d: %s", code, errOut.String())
	}
	text := out.String()
	for _, want := range []string{"run=1 objects=10 links=8", "changed=true new_version=true", "run=2", "changed=false new_version=false", "dataset ds_servers collected=true received=3"} {
		if !strings.Contains(text, want) {
			t.Errorf("the output lacks %q:\n%s", want, text)
		}
	}
	// the opener falls back to the environment's reader
	d.open = nil
	d.env.ReadFile = os.ReadFile
	if code := execute(ingestArgs(), &bytes.Buffer{}, &errOut, d); code != exitOK {
		t.Fatalf("with the environment's reader: %d: %s", code, errOut.String())
	}
}

// [REQ-269] [REQ-268]
func TestIngestRunReportsBadRows(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "servers.csv")
	data := "asset_id,serial_number,manufacturer,rack_asset_id,power_state\nSV-1,,Zorvane,RK-1001,on\nSV-2,SN-9,Zorvane,RK-1001,on\n"
	if err := os.WriteFile(bad, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	d.open = systemOpen
	var out, errOut bytes.Buffer
	args := []string{"ingest", "run", "-source", sampleDir + "source.json", "-mapping", sampleDir + "mapping.json",
		"-input", "ds_racks=" + sampleDir + "sample/racks.csv", "-input", "ds_servers=" + bad}
	if code := execute(args, &out, &errOut, d); code != exitOK {
		t.Fatalf("code %d: %s", code, errOut.String())
	}
	for _, want := range []string{"problems missing_identity=1", "problem missing_identity dataset=ds_servers", "unused="} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the output lacks %q:\n%s", want, out.String())
		}
	}
}

// [REQ-269]
func TestIngestRunRefusesWhatItCannotRun(t *testing.T) {
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	d.open = systemOpen
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no subcommand", []string{"ingest"}, exitUsage},
		{"an unknown subcommand", []string{"ingest", "walk"}, exitUsage},
		{"no inputs", []string{"ingest", "run", "-source", "a", "-mapping", "b"}, exitUsage},
		{"a flag without value", []string{"ingest", "run", "-source"}, exitUsage},
		{"a bad input", []string{"ingest", "run", "-source", "a", "-mapping", "b", "-input", "nofile"}, exitUsage},
		{"a bad count", append(ingestArgs(), "-times", "0"), exitUsage},
		{"an unknown flag", append(ingestArgs(), "-x", "1"), exitUsage},
		{"a missing source file", []string{"ingest", "run", "-source", "/nonexistent.json", "-mapping", sampleDir + "mapping.json", "-input", "ds_racks=x"}, exitFailure},
		{"a missing mapping file", []string{"ingest", "run", "-source", sampleDir + "source.json", "-mapping", "/nonexistent.json", "-input", "ds_racks=x"}, exitFailure},
		{"a missing input file", []string{"ingest", "run", "-source", sampleDir + "source.json", "-mapping", sampleDir + "mapping.json", "-input", "ds_racks=/nonexistent.csv"}, exitFailure},
	}
	for _, c := range cases {
		var out, errOut bytes.Buffer
		if code := execute(c.args, &out, &errOut, d); code != c.want {
			t.Errorf("%s: code %d, want %d (%s)", c.name, code, c.want, errOut.String())
		}
	}
	dir := t.TempDir()
	wrong := filepath.Join(dir, "mapping.json")
	if err := os.WriteFile(wrong, []byte(`{"feeds":[{"id":"f","dataset":"ds_racks","object_type":"Nope"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	args := []string{"ingest", "run", "-source", sampleDir + "source.json", "-mapping", wrong, "-input", "ds_racks=" + sampleDir + "sample/racks.csv"}
	if code := execute(args, &bytes.Buffer{}, &errOut, d); code != exitFailure || !strings.Contains(errOut.String(), "Nope") {
		t.Errorf("a wrong mapping: code %d: %s", code, errOut.String())
	}
}
