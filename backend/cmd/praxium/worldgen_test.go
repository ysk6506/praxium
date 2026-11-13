package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/worldgen"
)

func runWG(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code = run(append([]string{"worldgen"}, args...), &stdout, &stderr, fixedDeps())
	return code, stdout.String(), stderr.String()
}

func TestWorldgenCountsWithoutFiles(t *testing.T) {
	code, out, errOut := runWG(t, "-seed", "5", "-scale", "20000")
	if code != exitOK || !strings.HasPrefix(out, "worldgen seed=5 ") || !strings.Contains(out, "content_hash=") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	code2, out2, _ := runWG(t, "-seed", "5", "-scale", "20000")
	hash := func(s string) string { return s[strings.Index(s, "content_hash="):strings.Index(s, " elapsed_ms")] }
	if code2 != exitOK || hash(out) != hash(out2) {
		t.Fatalf("same parameters, different output:\n%s%s", out, out2)
	}
}

func TestWorldgenWritesEveryFile(t *testing.T) {
	dir := t.TempDir()
	code, out, errOut := runWG(t, "-seed", "6", "-scale", "30000", "-out", dir, "-views", "-changes", "50", "-shape", "-set", "sites=2")
	if code != exitOK {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	for _, name := range []string{"manifest.json", "truth.jsonl", "shape-card.json", "racks.csv", "changes/events.jsonl", "changes/expected.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m worldgen.Manifest
	if err := json.Unmarshal(raw, &m); err != nil || m.Layout.Sites != 2 || m.Format != worldgen.ManifestFormat || len(m.Files) < 10 {
		t.Fatalf("manifest: %v %+v", err, m.Layout)
	}
	code, out, errOut = runWG(t, "validate", filepath.Join(dir, "truth.jsonl"))
	if code != exitOK || !strings.Contains(out, "problems=0") {
		t.Fatalf("validate: %d %q %q", code, out, errOut)
	}
}

func TestWorldgenSetsParametersFromAShapeCard(t *testing.T) {
	dir := t.TempDir()
	card := filepath.Join(dir, "card.json")
	body := `{"service_calls_per_service":2.5,"service_depth":{"count":3,"max":5},"cross_site":{"call_edges":10,"call_ratio":0.2}}`
	if err := os.WriteFile(card, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runWG(t, "-scale", "10000", "-shape-in", card)
	if code != exitOK || !strings.Contains(out, "shape: set cross_site_ratio") || !strings.Contains(out, "shape: not supported: out_degree") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
}

func TestWorldgenErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte("{"), 0o600)
	badTruth := filepath.Join(dir, "bad.jsonl")
	_ = os.WriteFile(badTruth, []byte("not json\n"), 0o600)
	flawed := filepath.Join(dir, "flawed.jsonl")
	_ = os.WriteFile(flawed, []byte(`{"kind":"header","format":"praxium-worldgen-truth","version":1}`+"\n"+`{"kind":"object","id":"x","type":"ot.nope"}`+"\n"), 0o600)
	cases := []struct {
		name string
		args []string
		code int
		msg  string
	}{
		{"unknown flag", []string{"-nope"}, exitUsage, "usage: praxium worldgen"},
		{"extra argument", []string{"extra"}, exitUsage, "bad arguments"},
		{"bad set", []string{"-set", "novalue"}, exitUsage, "name=value"},
		{"unknown parameter", []string{"-set", "nope=1"}, exitUsage, "unknown parameter"},
		{"out of range", []string{"-scale", "5"}, exitUsage, "outside"},
		{"missing card", []string{"-shape-in", filepath.Join(dir, "none.json")}, exitUsage, "no such file"},
		{"broken card", []string{"-shape-in", bad}, exitUsage, "shape card"},
		{"validate without file", []string{"validate"}, exitUsage, "usage: praxium worldgen"},
		{"validate missing file", []string{"validate", filepath.Join(dir, "none")}, 1, "no such file"},
		{"validate bad file", []string{"validate", badTruth}, 1, "truth line"},
		{"validate problems", []string{"validate", flawed}, 1, "unknown type"},
		{"output in a new directory", []string{"-out", filepath.Join(dir, "f", "g"), "-scale", "2000", "-set", "sites=1"}, exitOK, ""},
	}
	for _, c := range cases {
		code, _, errOut := runWG(t, c.args...)
		if code != c.code || !strings.Contains(errOut, c.msg) {
			t.Errorf("%s: code %d, stderr %q", c.name, code, errOut)
		}
	}
}

func TestWorldgenParamsList(t *testing.T) {
	code, out, _ := runWG(t, "params")
	if code != exitOK || !strings.Contains(out, "cross_site_ratio") || !strings.Contains(out, "late_event_ratio") {
		t.Fatalf("%d %q", code, out)
	}
}

func TestDirStoreStaysInside(t *testing.T) {
	st := dirStore{t.TempDir()}
	for _, name := range []string{"../x", "/abs", "a/../../x"} {
		if _, err := st.Create(name); err == nil {
			t.Errorf("%q was created", name)
		}
	}
	w, err := st.Create("a/b/c.txt")
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	if _, err := os.Stat(filepath.Join(st.root, "a/b/c.txt")); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(st.root, "file")
	_ = os.WriteFile(blocker, nil, 0o600)
	if _, err := st.Create("file/x"); err == nil {
		t.Fatal("a file in the way")
	}
	nop, _ := discard{}.Create("x")
	if _, err := nop.Write([]byte("x")); err != nil || nop.Close() != nil {
		t.Fatal("discard")
	}
	if peakRSSKB() <= 0 {
		t.Log("peak RSS unknown on this system")
	}
}
