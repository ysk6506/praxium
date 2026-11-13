package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/cli"
	"github.com/ysk6506/praxium/backend/internal/contract"
)

const fakeToken = "test-token-not-real-0001"

// logFile is a stderr two goroutines may share: a file is safe for that, and read back with text.
func logFile(t *testing.T) (*os.File, func() string) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, func() string {
		raw, _ := os.ReadFile(f.Name())
		return string(raw)
	}
}

func apiDeps(files map[string]string, vars map[string]string) (wiring, chan net.Listener, context.CancelFunc) {
	d := wiring{deps: fixedDeps()}
	d.env = cli.Env{
		Lookup: func(name string) (string, bool) { v, ok := vars[name]; return v, ok },
		ReadFile: func(path string) ([]byte, error) {
			if c, ok := files[path]; ok {
				return []byte(c), nil
			}
			return nil, errors.New("no such file")
		},
	}
	listeners := make(chan net.Listener, 1)
	d.listen = func(addr string) (net.Listener, error) {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			listeners <- ln
		}
		return ln, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.signals = func() (context.Context, context.CancelFunc) { return ctx, cancel }
	return d, listeners, cancel
}

// [REQ-245]
func TestApiServeAndTheClientTalkOverTheContract(t *testing.T) {
	d, listeners, stop := apiDeps(map[string]string{"/dev-token": fakeToken + "\n"}, nil)
	stderr, stderrText := logFile(t)
	done := make(chan int, 1)
	go func() {
		done <- execute([]string{"api", "serve", "-addr", "127.0.0.1:0", "-mode", "development", "-dev-token-file", "/dev-token", "-dev-roles", "viewer,editor"},
			&bytes.Buffer{}, stderr, d)
	}()
	var ln net.Listener
	select {
	case ln = <-listeners:
	case <-time.After(10 * time.Second):
		t.Fatalf("the server did not listen: %s", stderrText())
	}
	endpoint := "http://" + ln.Addr().String()
	client := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := execute(append([]string{"--endpoint", endpoint, "--token", fakeToken}, args...), &out, &errOut, d)
		return code, out.String(), errOut.String()
	}
	healthy := false
	for attempt := 0; attempt < 1000 && !healthy; attempt++ {
		code, _, _ := client("health")
		healthy = code == 0
		if !healthy {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !healthy {
		t.Fatal("the server never became healthy")
	}
	code, out, _ := client("--json", "types", "get", "ot.site")
	if code != 0 {
		t.Fatalf("types get: %d", code)
	}
	var one map[string]any
	if err := json.Unmarshal([]byte(out), &one); err != nil || one["id"] != "ot.site" {
		t.Fatalf("types get printed %q (%v)", out, err)
	}
	if code, out, _ = client("types", "list", "--filter", "layer.equals=facility", "--limit", "5"); code != 0 || !strings.Contains(out, "ot.site") {
		t.Fatalf("types list: %d\n%s", code, out)
	}
	if code, out, _ = client("catalog", "roles"); code != 0 || !strings.Contains(out, "model_steward") {
		t.Fatalf("catalog roles: %d\n%s", code, out)
	}
	if code, _, errOut := client("types", "get", "ot.nothing"); code != cli.ExitNotFound || !strings.Contains(errOut, "not_found.resource") {
		t.Fatalf("a missing type: %d %s", code, errOut)
	}
	var unauthorized bytes.Buffer
	if code := execute([]string{"--endpoint", endpoint, "catalog", "roles"}, &bytes.Buffer{}, &unauthorized, d); code != cli.ExitAuth {
		t.Fatalf("no token: %d", code)
	}
	stop()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("serve exited %d: %s", code, stderrText())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
	if !strings.Contains(stderrText(), `"msg":"praxium api listening"`) || !strings.Contains(stderrText(), `"dev_auth":true`) {
		t.Fatalf("the start log is missing: %s", stderrText())
	}
}

// [REQ-148]
func TestApiServeRefusesUnsafeOrBrokenConfiguration(t *testing.T) {
	files := map[string]string{"/dev-token": fakeToken, "/short": "short"}
	tests := []struct {
		name string
		args []string
		vars map[string]string
		want string
	}{
		{"a dev token in production", []string{"-dev-token-file", "/dev-token"}, nil, "production"},
		{"a dev token from the environment in production", nil, map[string]string{envDevToken: fakeToken}, "production"},
		{"a short token", []string{"-mode", "development", "-dev-token-file", "/short"}, nil, "at least"},
		{"a missing token file", []string{"-mode", "development", "-dev-token-file", "/nowhere"}, nil, "token file"},
		{"an unknown role", []string{"-mode", "development", "-dev-token-file", "/dev-token", "-dev-roles", "root"}, nil, "root"},
		{"an unknown flag", []string{"-colour"}, nil, "flag"},
		{"a stray argument", []string{"extra"}, nil, "no arguments"},
		{"an address that cannot be used", []string{"-addr", "256.256.256.256:99999"}, nil, "listen"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, _, stop := apiDeps(files, tt.vars)
			defer stop()
			var stderr bytes.Buffer
			code := execute(append([]string{"api", "serve"}, tt.args...), &bytes.Buffer{}, &stderr, d)
			if code != exitFailure || !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("code %d, stderr %q, want it to mention %q", code, stderr.String(), tt.want)
			}
		})
	}
}

// [REQ-148]
func TestApiServeWithoutATokenServesOnlyAnonymousRoutes(t *testing.T) {
	d, listeners, stop := apiDeps(nil, nil)
	stderr, _ := logFile(t)
	done := make(chan int, 1)
	go func() { done <- execute([]string{"api", "serve", "-addr", "127.0.0.1:0"}, &bytes.Buffer{}, stderr, d) }()
	ln := <-listeners
	endpoint := "http://" + ln.Addr().String()
	healthy := false
	for attempt := 0; attempt < 1000 && !healthy; attempt++ {
		healthy = execute([]string{"--endpoint", endpoint, "health"}, &bytes.Buffer{}, &bytes.Buffer{}, d) == 0
		if !healthy {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !healthy {
		t.Fatal("never healthy")
	}
	if code := execute([]string{"--endpoint", endpoint, "--token", fakeToken, "catalog", "roles"}, &bytes.Buffer{}, &bytes.Buffer{}, d); code != cli.ExitAuth {
		t.Fatalf("a token without an authenticator: %d", code)
	}
	stop()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d", code)
	}
}

// [REQ-143]
func TestApiRoutesListsWhatTheServerRegisters(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := execute([]string{"api", "routes", "--json"}, &stdout, &stderr, wiring{deps: fixedDeps()}); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	var got struct {
		Routes []struct {
			Method string   `json:"method"`
			Path   string   `json:"path"`
			Roles  []string `json:"roles"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	doc, err := contract.ParseDocument(contract.OpenAPI())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Routes) != len(doc.Operations()) || len(got.Routes) == 0 {
		t.Fatalf("%d routes, the contract has %d operations", len(got.Routes), len(doc.Operations()))
	}
	for _, args := range [][]string{{"api", "routes"}, {"api", "routes", "--yaml"}, {"api", "routes", "--json", "x"}} {
		stderr.Reset()
		if code := execute(args, &bytes.Buffer{}, &stderr, wiring{deps: fixedDeps()}); code != exitUsage || !strings.Contains(stderr.String(), "usage") {
			t.Errorf("%v: code %d", args, code)
		}
	}
}

// [REQ-143]
func TestContractCommandAssemblesAndChecks(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := execute([]string{"contract", "assemble"}, &stdout, &stderr, wiring{deps: fixedDeps()}); code != exitOK {
		t.Fatalf("assemble: %d %s", code, stderr.String())
	}
	if stdout.String() != string(contract.OpenAPI()) {
		t.Fatal("the assembled document is not the committed one")
	}
	if code := execute([]string{"contract", "check"}, &bytes.Buffer{}, &stderr, wiring{deps: fixedDeps()}); code != exitOK {
		t.Fatalf("check: %d %s", code, stderr.String())
	}
	for _, args := range [][]string{{"contract"}, {"contract", "frob"}, {"contract", "check", "x"}} {
		stderr.Reset()
		if code := execute(args, &bytes.Buffer{}, &stderr, wiring{deps: fixedDeps()}); code != exitUsage {
			t.Errorf("%v: code %d", args, code)
		}
	}
}

// [REQ-148]
func TestApiUsageStaysForUnknownSubcommands(t *testing.T) {
	var stderr bytes.Buffer
	if code := execute([]string{"api", "worker"}, &bytes.Buffer{}, &stderr, wiring{deps: fixedDeps()}); code != exitUsage || !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	if code := execute([]string{"api", "serve", "-h"}, &bytes.Buffer{}, &stderr, wiring{deps: fixedDeps()}); code != exitFailure {
		t.Fatalf("help flag: %d", code)
	}
}

// [REQ-245]
func TestSystemWiringIsTheRealThing(t *testing.T) {
	w := systemWiring()
	ln, err := w.listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()
	ctx, cancel := w.signals()
	cancel()
	if ctx.Err() == nil {
		t.Fatal("the stop context did not end")
	}
	if _, ok := w.env.Lookup("PRAXIUM_SURELY_UNSET_VARIABLE"); ok {
		t.Fatal("an unset variable was found")
	}
	if _, err := w.env.ReadFile("/surely/not/here"); err == nil {
		t.Fatal("a missing file was read")
	}
}

// [REQ-245]
func TestApiRoutesAndListenFailuresAreReported(t *testing.T) {
	var stderr bytes.Buffer
	closed, err := os.Create(filepath.Join(t.TempDir(), "closed"))
	if err != nil {
		t.Fatal(err)
	}
	_ = closed.Close()
	if code := execute([]string{"api", "routes", "--json"}, closed, &stderr, wiring{deps: fixedDeps()}); code != exitFailure {
		t.Fatalf("a broken stdout: %d", code)
	}
	d, _, stop := apiDeps(map[string]string{"/dev-token": fakeToken}, nil)
	defer stop()
	d.listen = func(string) (net.Listener, error) { return nil, os.ErrPermission }
	stderr.Reset()
	args := []string{"api", "serve", "-mode", "development", "-dev-token-file", "/dev-token", "-dev-roles", "viewer"}
	if code := execute(args, &bytes.Buffer{}, &stderr, d); code != exitFailure || !strings.Contains(stderr.String(), "cannot listen") {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
}
