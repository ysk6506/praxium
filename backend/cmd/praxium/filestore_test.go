package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// serveFile starts a development server on the file store of dir and returns its client commands, a function that
// stops it (and waits until the store has taken its last snapshot) and the text it logged.
func serveFile(t *testing.T, dir string, extra ...string) (client func(args ...string) (int, string, string), stop func(), logged func() string) {
	t.Helper()
	d, listeners, cancel := apiDeps(map[string]string{"/dev-token": fakeToken}, nil)
	files := d.env.ReadFile
	d.env.ReadFile = func(path string) ([]byte, error) {
		if data, err := files(path); err == nil {
			return data, nil
		}
		return os.ReadFile(path)
	}
	d.open = systemOpen
	errFile, errText := logFile(t)
	done := make(chan int, 1)
	args := append([]string{"api", "serve", "-addr", "127.0.0.1:0", "-mode", "development", "-dev-token-file", "/dev-token",
		"-store", "file", "-store-data-dir", dir}, extra...)
	go func() { done <- execute(args, &bytes.Buffer{}, errFile, d) }()
	var ln net.Listener
	select {
	case ln = <-listeners:
	case code := <-done:
		t.Fatalf("the server exited with %d: %s", code, errText())
	case <-time.After(30 * time.Second):
		t.Fatalf("the server did not listen: %s", errText())
	}
	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the server did not stop")
		}
	}
	t.Cleanup(stop)
	endpoint := "http://" + ln.Addr().String()
	client = func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := execute(append([]string{"--endpoint", endpoint, "--token", fakeToken}, args...), &out, &errOut, d)
		return code, out.String(), errOut.String()
	}
	for attempt := 0; attempt < 1000; attempt++ {
		if code, _, _ := client("health"); code == 0 {
			return client, stop, errText
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the server never became healthy")
	return nil, nil, nil
}

// A development server on the file store serves the KG that -dev-truth loaded, and a server started later on the
// same folder, without the truth file, still has it. A second server on a folder that is open is refused.
// [REQ-260] [REQ-271]
func TestADevelopmentServerOnTheFileStoreKeepsTheKGAcrossRestarts(t *testing.T) {
	truth := writeWorld(t)
	dir := filepath.Join(t.TempDir(), "data")
	client, stop, logged := serveFile(t, dir, "-dev-truth", truth)
	code, out, errOut := client("kg", "list")
	if code != 0 || !strings.Contains(out, "dev") || !strings.Contains(out, "main") || !strings.Contains(out, "serving") {
		t.Fatalf("kg list: %d\n%s%s", code, out, errOut)
	}
	code, first, errOut := client("query", "find", "--type", "Rack", "--limit", "5")
	if code != 0 || !strings.Contains(first, "rack-") {
		t.Fatalf("find: %d\n%s%s", code, first, errOut)
	}
	if !strings.Contains(logged(), `"store":"file"`) {
		t.Fatalf("the log must name the store: %s", logged())
	}

	// The folder is open: a second process is refused, and says why.
	d, _, cancel := apiDeps(map[string]string{"/dev-token": fakeToken}, nil)
	defer cancel()
	var stderr bytes.Buffer
	code = execute([]string{"api", "serve", "-addr", "127.0.0.1:0", "-mode", "development", "-dev-token-file", "/dev-token",
		"-store", "file", "-store-data-dir", dir}, &bytes.Buffer{}, &stderr, d)
	if code != exitFailure || !strings.Contains(stderr.String(), "in use by another process") {
		t.Fatalf("a second server on an open folder: %d %q", code, stderr.String())
	}
	stop()

	again, stopAgain, _ := serveFile(t, dir)
	if code, out, errOut = again("kg", "get", "main"); code != 0 || !strings.Contains(out, "serving") {
		t.Fatalf("after a restart: %d\n%s%s", code, out, errOut)
	}
	code, second, errOut := again("query", "find", "--type", "Rack", "--limit", "5")
	if code != 0 || second != first {
		t.Fatalf("after a restart the racks differ: %d\n%s\nwant\n%s%s", code, second, first, errOut)
	}
	stopAgain()

	// A third start after a stop that took the last snapshot reads it.
	third, _, _ := serveFile(t, dir)
	if code, out, errOut = third("kg", "get", "main"); code != 0 || !strings.Contains(out, "serving") {
		t.Fatalf("after a second restart: %d\n%s%s", code, out, errOut)
	}
}

// The flags of the file store are checked.
// [REQ-271]
func TestTheFileStoreFlagsAreChecked(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"a file store without a folder", []string{"-store", "file"}, "-store-data-dir"},
		{"a folder without the file store", []string{"-store-data-dir", dir}, "-store file"},
		{"a folder with postgres", []string{"-store", "postgres", "-store-data-dir", dir}, "-store file"},
		{"a DSN file with the file store", []string{"-store", "file", "-store-data-dir", dir, "-store-dsn-file", "/x"}, "-store postgres"},
		{"an unknown store", []string{"-store", "sqlite"}, "memory, postgres or file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, listeners, stop := apiDeps(map[string]string{"/dev-token": fakeToken}, nil)
			defer stop()
			var stderr bytes.Buffer
			args := append([]string{"api", "serve", "-addr", "127.0.0.1:0", "-mode", "development", "-dev-token-file", "/dev-token"}, tt.args...)
			code := execute(args, &bytes.Buffer{}, &stderr, d)
			if code != exitFailure || !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("code %d, stderr %q, want it to mention %q", code, stderr.String(), tt.want)
			}
			select {
			case <-listeners:
				t.Fatal("the server listened")
			default:
			}
		})
	}
}

// The default store is still the memory store: nothing is written to disk without -store file.
// [REQ-271]
func TestTheDefaultStoreStaysInMemory(t *testing.T) {
	o, err := parseServe(nil)
	if err != nil || o.store != storeMemory || o.storeDataDir != "" {
		t.Fatalf("defaults %+v %v", o, err)
	}
}
