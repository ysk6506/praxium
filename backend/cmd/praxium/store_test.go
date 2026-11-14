package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// dsnFile writes a DSN into a file of its own, as an operator would hand it over.
func dsnFile(t *testing.T, dsn string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dsn")
	if err := os.WriteFile(path, []byte(dsn+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// servePG starts a development server on the PostgreSQL store of dsn and returns its client commands, a function
// that stops it and the text it logged.
func servePG(t *testing.T, dsn string, extra ...string) (client func(args ...string) (int, string, string), stop func(), logged func() string) {
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
		"-store", "postgres", "-store-dsn-file", dsnFile(t, dsn)}, extra...)
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

// [REQ-260] [REQ-263]
func TestADevelopmentServerOnPostgresServesTheKgAndKeepsItAcrossRestarts(t *testing.T) {
	truth := writeWorld(t)
	dsn := pgstore.NewTestDatabase(t)
	client, stop, logged := servePG(t, dsn, "-dev-truth", truth)
	code, out, errOut := client("kg", "list")
	if code != 0 || !strings.Contains(out, "dev") || !strings.Contains(out, "main") || !strings.Contains(out, "serving") {
		t.Fatalf("kg list: %d\n%s%s", code, out, errOut)
	}
	code, out, errOut = client("query", "find", "--type", "Rack", "--limit", "5")
	if code != 0 || !strings.Contains(out, "rack-") {
		t.Fatalf("find: %d\n%s%s", code, out, errOut)
	}
	if !strings.Contains(logged(), `"store":"postgres"`) || strings.Contains(logged(), dsn) {
		t.Fatalf("the log must name the store and never the DSN: %s", logged())
	}
	stop()

	// A second server on the same database, with no truth file, still has the KG: the data is the database's.
	again, _, _ := servePG(t, dsn)
	if code, out, errOut = again("kg", "get", "main"); code != 0 || !strings.Contains(out, "serving") {
		t.Fatalf("after a restart: %d\n%s%s", code, out, errOut)
	}
}

// [REQ-260] [REQ-263]
func TestTheStoreFlagsAreCheckedAndTheDSNNeverShows(t *testing.T) {
	good := dsnFile(t, "host=/nonexistent-socket-dir user=u password=hunter2 dbname=x sslmode=disable connect_timeout=2")
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"an unknown store", []string{"-store", "sqlite"}, "unknown -store"},
		{"postgres without a file", []string{"-store", "postgres"}, "-store-dsn-file"},
		{"a file without postgres", []string{"-store-dsn-file", good}, "-store postgres"},
		{"a file that is not there", []string{"-store", "postgres", "-store-dsn-file", filepath.Join(t.TempDir(), "none")}, "DSN file"},
		{"a server that cannot be reached", []string{"-store", "postgres", "-store-dsn-file", good}, "cannot reach"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, listeners, stop := apiDeps(map[string]string{"/dev-token": fakeToken}, nil)
			defer stop()
			var stderr bytes.Buffer
			args := append([]string{"api", "serve", "-addr", "127.0.0.1:0", "-mode", "development", "-dev-token-file", "/dev-token"}, tt.args...)
			d.env.ReadFile = func(path string) ([]byte, error) {
				if path == "/dev-token" {
					return []byte(fakeToken), nil
				}
				return os.ReadFile(path)
			}
			code := execute(args, &bytes.Buffer{}, &stderr, d)
			if code != exitFailure || !strings.Contains(stderr.String(), tt.want) || strings.Contains(stderr.String(), "hunter2") {
				t.Fatalf("code %d, stderr %q, want it to mention %q and not the password", code, stderr.String(), tt.want)
			}
			select {
			case <-listeners:
				t.Fatal("the server listened")
			default:
			}
		})
	}
}

// [REQ-263]
func TestOpenStoreClosesWhatItOpened(t *testing.T) {
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	d.env.ReadFile = os.ReadFile
	st, closeStore, err := openStore(context.Background(), serveOptions{store: storePostgres, storeDSNFile: dsnFile(t, pgstore.NewTestDatabase(t))}, d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListKGs(context.Background()); err != nil {
		t.Fatal(err)
	}
	closeStore()
	if _, err := st.ListKGs(context.Background()); err == nil {
		t.Fatal("a closed store still answered")
	}
}
