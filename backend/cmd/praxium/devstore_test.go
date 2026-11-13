package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/cli"
)

// writeWorld generates a small world into a directory and returns the path of its truth file.
func writeWorld(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	if code := execute([]string{"worldgen", "-seed", "4", "-scale", "500", "-out", dir}, &out, &errOut, wiring{deps: fixedDeps()}); code != exitOK {
		t.Fatalf("worldgen: %d %s", code, errOut.String())
	}
	return filepath.Join(dir, "truth.jsonl")
}

// serveDev starts a development server on a free port and returns a function that runs client commands against it.
func serveDev(t *testing.T, extra ...string) (client func(args ...string) (int, string, string), stderr func() string) {
	t.Helper()
	d, listeners, stop := apiDeps(map[string]string{"/dev-token": fakeToken}, nil)
	d.open = systemOpen
	errFile, errText := logFile(t)
	done := make(chan int, 1)
	args := append([]string{"api", "serve", "-addr", "127.0.0.1:0", "-mode", "development", "-dev-token-file", "/dev-token"}, extra...)
	go func() { done <- execute(args, &bytes.Buffer{}, errFile, d) }()
	var ln net.Listener
	select {
	case ln = <-listeners:
	case code := <-done:
		t.Fatalf("the server exited with %d: %s", code, errText())
	case <-time.After(10 * time.Second):
		t.Fatalf("the server did not listen: %s", errText())
	}
	t.Cleanup(func() {
		stop()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the server did not stop")
		}
	})
	endpoint := "http://" + ln.Addr().String()
	client = func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := execute(append([]string{"--endpoint", endpoint, "--token", fakeToken}, args...), &out, &errOut, d)
		return code, out.String(), errOut.String()
	}
	for attempt := 0; attempt < 1000; attempt++ {
		if code, _, _ := client("health"); code == 0 {
			return client, errText
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the server never became healthy")
	return nil, nil
}

// [REQ-260]
func TestADevelopmentServerWithATruthFileServesTheKgToTheClient(t *testing.T) {
	truth := writeWorld(t)
	client, stderr := serveDev(t, "-dev-truth", truth)
	code, out, errOut := client("kg", "list")
	if code != 0 || !strings.Contains(out, "dev") || !strings.Contains(out, "main") || !strings.Contains(out, "serving") {
		t.Fatalf("kg list: %d\n%s%s", code, out, errOut)
	}
	code, out, errOut = client("query", "find", "--type", "Rack", "--limit", "5")
	if code != 0 || !strings.Contains(out, "rack-") {
		t.Fatalf("find: %d\n%s%s", code, out, errOut)
	}
	var id string
	for _, field := range strings.Fields(out) {
		if strings.HasPrefix(field, "obj_") {
			id = field
			break
		}
	}
	if id == "" {
		t.Fatalf("find named no object:\n%s", out)
	}
	if code, out, errOut = client("object", "get", id); code != 0 || !strings.Contains(out, "rack-") {
		t.Fatalf("object get: %d\n%s%s", code, out, errOut)
	}
	if code, out, errOut = client("query", "neighbors", "--direction", "in", id); code != 0 || !strings.Contains(out, "MOUNTED_IN") {
		t.Fatalf("neighbors: %d\n%s%s", code, out, errOut)
	}
	if !strings.Contains(stderr(), `"msg":"praxium dev truth loaded"`) {
		t.Fatalf("the load is not logged: %s", stderr())
	}
}

// [REQ-260]
func TestADevelopmentServerWithoutATruthFileHasNoKgToRead(t *testing.T) {
	client, _ := serveDev(t)
	code, out, errOut := client("kg", "list")
	if code != 0 || strings.Contains(out, "serving") {
		t.Fatalf("kg list: %d\n%s%s", code, out, errOut)
	}
	if code, _, errOut = client("kg", "get", "main"); code != cli.ExitNotFound {
		t.Fatalf("kg get main: %d %s", code, errOut)
	}
}

// [REQ-260]
func TestADevelopmentServerRefusesATruthFileItCannotLoad(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(bad, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, truth := range []string{bad, filepath.Join(dir, "missing.jsonl")} {
		d, listeners, stop := apiDeps(map[string]string{"/dev-token": fakeToken}, nil)
		d.open = systemOpen
		var stderr bytes.Buffer
		code := execute([]string{"api", "serve", "-mode", "development", "-dev-token-file", "/dev-token", "-dev-truth", truth}, &bytes.Buffer{}, &stderr, d)
		stop()
		if code != exitFailure || stderr.Len() == 0 {
			t.Fatalf("%s: code %d, stderr %q", truth, code, stderr.String())
		}
		select {
		case <-listeners:
			t.Fatalf("%s: the server listened", truth)
		default:
		}
	}
}

// [REQ-260]
func TestProductionNamesWhatItLacks(t *testing.T) {
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	var stderr bytes.Buffer
	if code := execute([]string{"api", "serve", "-mode", "production"}, &bytes.Buffer{}, &stderr, d); code != exitFailure ||
		!strings.Contains(stderr.String(), "authenticator") || !strings.Contains(stderr.String(), "store") ||
		!strings.Contains(stderr.String(), "TD-0010") || !strings.Contains(stderr.String(), "TD-0011") {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
}

// [REQ-258] [REQ-260]
func TestDevLoadTruthLoadsIntoAThrowawayStoreAndPrintsTheCounts(t *testing.T) {
	truth := writeWorld(t)
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	d.open = systemOpen
	var out, errOut bytes.Buffer
	if code := execute([]string{"dev", "load-truth", truth, "-batch", "100"}, &out, &errOut, d); code != exitOK {
		t.Fatalf("code %d: %s", code, errOut.String())
	}
	for _, want := range []string{"devload objects=", " links=", "not_ingestible_objects=0", "unkeyed=0", "dangling_links=0", "version=kv_", "graph_hash=", "elapsed_ms=", "peak_rss_kb="} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the report lacks %q:\n%s", want, out.String())
		}
	}
	// the opener falls back to the environment's reader
	d.open = nil
	d.env.ReadFile = os.ReadFile
	if code := execute([]string{"dev", "load-truth", truth}, &bytes.Buffer{}, &errOut, d); code != exitOK {
		t.Fatalf("with the environment's reader: %d: %s", code, errOut.String())
	}
}

// [REQ-260]
func TestDevLoadTruthRefusesWhatItCannotRun(t *testing.T) {
	truth := writeWorld(t)
	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	if err := os.WriteFile(bad, []byte(`{"kind":"header","format":"other","version":1}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		code int
	}{
		{"no subcommand", []string{"dev"}, exitUsage},
		{"an unknown subcommand", []string{"dev", "unload"}, exitUsage},
		{"no file", []string{"dev", "load-truth"}, exitUsage},
		{"a flag and no file", []string{"dev", "load-truth", "-batch", "5"}, exitUsage},
		{"two files", []string{"dev", "load-truth", truth, truth}, exitUsage},
		{"a bad batch size", []string{"dev", "load-truth", truth, "-batch", "0"}, exitUsage},
		{"a batch size that is no number", []string{"dev", "load-truth", truth, "-batch", "many"}, exitUsage},
		{"a missing file", []string{"dev", "load-truth", filepath.Join(t.TempDir(), "none")}, exitFailure},
		{"a file of another format", []string{"dev", "load-truth", bad}, exitFailure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, _, stop := apiDeps(nil, nil)
			defer stop()
			d.open = systemOpen
			var out, errOut bytes.Buffer
			if code := execute(tt.args, &out, &errOut, d); code != tt.code || errOut.Len() == 0 || out.Len() != 0 {
				t.Fatalf("code %d (want %d), stdout %q, stderr %q", code, tt.code, out.String(), errOut.String())
			}
		})
	}
}
