package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// [REQ-293]
func TestIngestInitWritesExamplesThatRunAndSaysWhatToTypeNext(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ex")
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	var stdout, stderr bytes.Buffer
	if code := execute([]string{"ingest", "init", "-out", out}, &stdout, &stderr, d); code != exitOK {
		t.Fatalf("%d %s", code, stderr.String())
	}
	for _, want := range []string{"ingest inspect -input " + filepath.ToSlash(out) + "/excel/datacenter.xlsx", "ingest run -data-dir data", "praxium local -data-dir data", "-input ds_vm_power="} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("the next steps must say %q:\n%s", want, stdout.String())
		}
	}
	d.open = systemOpen
	data := filepath.Join(t.TempDir(), "data")
	csvRun := []string{"ingest", "run", "-data-dir", data, "-source", out + "/csv/source.json", "-mapping", out + "/csv/mapping.json",
		"-input", "ds_racks=" + out + "/csv/racks.csv", "-input", "ds_servers=" + out + "/csv/servers.csv", "-input", "ds_hosts=" + out + "/csv/hosts.jsonl",
		"-input", "ds_vms=" + out + "/csv/vms.jsonl", "-input", "ds_vm_power=" + out + "/csv/vm-power.csv"}
	stdout.Reset()
	if code := execute(csvRun, &stdout, &stderr, d); code != exitOK || !strings.Contains(stdout.String(), "objects=10 links=8") || !strings.Contains(stdout.String(), "completed without problems") {
		t.Fatalf("the CSV example must run clean: %d\n%s%s", code, stdout.String(), stderr.String())
	}
	book := out + "/excel/datacenter.xlsx"
	xlRun := []string{"ingest", "run", "-data-dir", data, "-source", out + "/excel/source.json", "-mapping", out + "/excel/mapping.json", "-input", "ds_racks=" + book, "-input", "ds_servers=" + book}
	stdout.Reset()
	if code := execute(xlRun, &stdout, &stderr, d); code != exitOK || !strings.Contains(stdout.String(), "objects=5 links=3") || !strings.Contains(stdout.String(), "completed without problems") {
		t.Fatalf("the Excel example must run clean into the same KG: %d\n%s%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := execute([]string{"ingest", "inspect", "-input", book}, &stdout, &stderr, d); code != exitOK || !strings.Contains(stdout.String(), "서버 목록") {
		t.Fatalf("the workbook must be inspectable: %d %s", code, stdout.String())
	}
}

// [REQ-293]
func TestIngestInitNeverOverwritesAndRefusesBadArguments(t *testing.T) {
	out := t.TempDir()
	target := filepath.Join(out, "csv", "racks.csv")
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	var stderr bytes.Buffer
	if code := execute([]string{"ingest", "init", "-out", out}, &bytes.Buffer{}, &stderr, d); code != exitFailure || !strings.Contains(stderr.String(), "does not overwrite") {
		t.Fatalf("%d %s", code, stderr.String())
	}
	if raw, _ := os.ReadFile(target); string(raw) != "mine" {
		t.Fatal("a file of the person was overwritten")
	}
	if _, err := os.Stat(filepath.Join(out, "excel")); err == nil {
		t.Fatal("it must write all of the files or none")
	}
	for _, args := range [][]string{{"ingest", "init", "extra"}, {"ingest", "init", "-nope"}} {
		stderr.Reset()
		if code := execute(args, &bytes.Buffer{}, &stderr, d); code != exitUsage || !strings.Contains(stderr.String(), "usage: praxium ingest init") {
			t.Errorf("%v: %d %s", args, code, stderr.String())
		}
	}
}
