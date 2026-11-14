package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/devload"
	"github.com/ysk6506/praxium/backend/internal/memstore"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// demoRun is one `praxium demo` in this process.
type demoRun struct {
	t      *testing.T
	d      wiring
	out    func() string
	stderr func() string
	stop   context.CancelFunc
	done   chan int
	base   string
	token  string
}

// startDemo runs `praxium demo` on a free port with a small world and waits until it says it is ready.
func startDemo(t *testing.T, webBuilt bool, args ...string) *demoRun {
	t.Helper()
	d, listeners, stop := apiDeps(nil, nil)
	d.webBuilt = func() bool { return webBuilt }
	stderr, stderrText := logFile(t)
	stdout, stdoutText := logFile(t)
	r := &demoRun{t: t, d: d, out: stdoutText, stderr: stderrText, stop: stop, done: make(chan int, 1)}
	full := append([]string{"demo", "-addr", "127.0.0.1:0", "-scale", "500"}, args...)
	go func() { r.done <- execute(full, stdout, stderr, d) }()
	select {
	case ln := <-listeners:
		r.base = "http://" + ln.Addr().String()
	case code := <-r.done:
		t.Fatalf("the demo ended with %d: %s %s", code, r.out(), stderrText())
	case <-time.After(20 * time.Second):
		t.Fatalf("the demo did not listen: %s %s", r.out(), stderrText())
	}
	for i := 0; !strings.Contains(r.out(), "Ctrl+C"); i++ {
		if i > 2000 {
			t.Fatalf("the demo never said it was ready: %s / %s", r.out(), r.stderr())
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.token = tokenOf(r.out())
	t.Cleanup(func() { r.stop() })
	return r
}

// tokenOf finds the token the demo printed: in the address, or in the sentence of a build without the web UI.
func tokenOf(out string) string {
	for _, marker := range []string{"token=", "The token of this run is "} {
		if _, rest, ok := strings.Cut(out, marker); ok {
			return strings.FieldsFunc(rest, func(r rune) bool { return r == '&' || r == '\n' || r == ' ' })[0]
		}
	}
	return ""
}

func demoStop(r *demoRun) int {
	r.t.Helper()
	r.stop()
	select {
	case code := <-r.done:
		return code
	case <-time.After(20 * time.Second):
		r.t.Fatal("the demo did not stop")
	}
	return -1
}

func demoGet(r *demoRun, path, token string) (int, string) {
	r.t.Helper()
	req, err := http.NewRequest(http.MethodGet, r.base+path, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body)
}

func demoCLI(r *demoRun, args ...string) (int, string) {
	var out, errOut bytes.Buffer
	code := execute(append([]string{"--endpoint", r.base, "--token", r.token}, args...), &out, &errOut, r.d)
	return code, out.String() + errOut.String()
}

// [REQ-280]
func TestDemoFillsTheKGAndTheAPIAnswers(t *testing.T) {
	r := startDemo(t, true)
	if code, body := demoGet(r, "/v1/kgs", r.token); code != 200 || !strings.Contains(body, `"dev"`) {
		t.Fatalf("the demo KG must be listed for the demo token, got %d %s", code, body)
	}
	if code, _ := demoGet(r, "/v1/kgs", ""); code != 401 {
		t.Fatalf("a request without the token must be refused, got %d", code)
	}
	if code, out := demoCLI(r, "query", "find", "--type", "PhysicalServer", "--limit", "3"); code != 0 || !strings.Contains(out, "PhysicalServer") {
		t.Fatalf("the synthetic data center must hold servers, got %d %s", code, out)
	}
	if code, body := demoGet(r, "/", ""); code != 200 || !strings.Contains(body, "<html") {
		t.Fatalf("/ must be a page, got %d", code)
	}
	for _, want := range []string{"synthetic", "not connected to any real system", "not for production", "Ctrl+C", "gone when the demo stops"} {
		if !strings.Contains(r.out(), want) {
			t.Errorf("the start output must say %q: %s", want, r.out())
		}
	}
	if strings.Contains(r.stderr(), "not a loopback") {
		t.Errorf("a loopback demo must not warn: %s", r.stderr())
	}
	if code := demoStop(r); code != 0 {
		t.Fatalf("Ctrl+C must end the demo cleanly, got %d: %s", code, r.stderr())
	}
}

// [REQ-281]
func TestDemoTokenIsNewEveryRunAndOnlyInTheAddress(t *testing.T) {
	first := startDemo(t, true)
	if _, body := demoGet(first, "/", ""); strings.Contains(body, first.token) {
		t.Fatal("the page must not carry the token")
	}
	if code, _ := demoGet(first, "/v1/kgs", first.token); code != 200 {
		t.Fatal("the token must work")
	}
	out := first.out()
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, first.token) {
			if line != "" {
				t.Fatalf("the token must be printed once: %s", out)
			}
			line = l
		}
	}
	prefix, fragment, ok := strings.Cut(line, "#")
	if !ok || strings.Contains(prefix, first.token) || !strings.HasPrefix(prefix, first.base+"/") {
		t.Fatalf("the token must be in the fragment of the address and nowhere before it: %q", line)
	}
	if fragment != "/connect?token="+first.token+"&demo=1" || len(first.token) < 32 {
		t.Fatalf("unexpected fragment %q", fragment)
	}
	if strings.Contains(first.stderr(), first.token) {
		t.Fatalf("the log must not have the token: %s", first.stderr())
	}
	demoStop(first)
	second := startDemo(t, true)
	if second.token == first.token {
		t.Fatal("every run needs its own token")
	}
	if code, _ := demoGet(second, "/v1/kgs", first.token); code != 401 {
		t.Fatalf("the token of an earlier run must not work, got %d", code)
	}
}

// [REQ-280]
func TestDemoKeepsTheKGInADataFolder(t *testing.T) {
	dir := t.TempDir()
	first := startDemo(t, true, "-data-dir", dir)
	_, before := demoCLI(first, "query", "find", "--type", "PhysicalServer", "--limit", "100")
	if !strings.Contains(first.out(), "Loaded ") || !strings.Contains(first.stderr(), "dev truth loaded") {
		t.Fatalf("the first start must load: %s", first.out())
	}
	if code := demoStop(first); code != 0 {
		t.Fatalf("exit %d: %s", code, first.stderr())
	}
	second := startDemo(t, true, "-data-dir", dir)
	_, after := demoCLI(second, "query", "find", "--type", "PhysicalServer", "--limit", "100")
	if before != after || !strings.Contains(before, "PhysicalServer") {
		t.Fatalf("the KG must be the same after a restart:\n%s\n%s", before, after)
	}
	if !strings.Contains(second.out(), "nothing was loaded again") || strings.Contains(second.stderr(), "dev truth loaded") {
		t.Fatalf("the second start must not load again: %s %s", second.out(), second.stderr())
	}
	if strings.Contains(second.out(), "Loaded ") {
		t.Fatalf("the second start must not say it loaded: %s", second.out())
	}
}

// [REQ-280]
func TestDemoSaysWhenThePortIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	d, _, _ := apiDeps(nil, nil)
	var out bytes.Buffer
	stderr, stderrText := logFile(t)
	code := execute([]string{"demo", "-addr", taken.Addr().String(), "-scale", "500"}, &out, stderr, d)
	if code != exitFailure {
		t.Fatalf("a taken port must fail, got %d", code)
	}
	for _, want := range []string{"already in use", "-addr"} {
		if !strings.Contains(stderrText(), want) {
			t.Errorf("the error must say %q: %s", want, stderrText())
		}
	}
	if strings.Contains(out.String(), "Synthesizing") {
		t.Errorf("the port must be checked before the world is made: %s", out.String())
	}
}

// [REQ-280]
func TestDemoWithoutTheWebUIExplainsAndKeepsTheAPI(t *testing.T) {
	r := startDemo(t, false)
	out := r.out()
	for _, want := range []string{"does not include the web UI", "scripts/build-with-web.sh", "release executable", "--endpoint " + r.base, "--token " + r.token} {
		if !strings.Contains(out, want) {
			t.Errorf("the guidance must say %q: %s", want, out)
		}
	}
	if strings.Contains(out, "#/connect") {
		t.Errorf("there is no page to open: %s", out)
	}
	if code, _ := demoGet(r, "/v1/kgs", r.token); code != 200 {
		t.Fatalf("the API must still work, got %d", code)
	}
	if code, out := demoCLI(r, "kg", "list"); code != 0 || !strings.Contains(out, "dev") {
		t.Fatalf("the command line must work with the printed token, got %d %s", code, out)
	}
}

// [REQ-280]
func TestDemoWarnsForANonLoopbackAddress(t *testing.T) {
	d, listeners, stop := apiDeps(nil, nil)
	d.webBuilt = func() bool { return true }
	stderr, stderrText := logFile(t)
	stdout, stdoutText := logFile(t)
	done := make(chan int, 1)
	go func() { done <- execute([]string{"demo", "-addr", "0.0.0.0:0", "-scale", "500"}, stdout, stderr, d) }()
	var ln net.Listener
	select {
	case ln = <-listeners:
	case <-time.After(20 * time.Second):
		t.Fatalf("no listener: %s", stderrText())
	}
	for i := 0; !strings.Contains(stdoutText(), "Ctrl+C") && i < 2000; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	<-done
	if !strings.Contains(stderrText(), "not a loopback address") || !strings.Contains(stderrText(), "weak protection") {
		t.Fatalf("a non-loopback demo must warn: %s", stderrText())
	}
	if !strings.Contains(stdoutText(), "http://127.0.0.1:") || strings.Contains(stdoutText(), "0.0.0.0") {
		t.Fatalf("the address to open must be one a browser can use: %s (%s)", stdoutText(), ln.Addr())
	}
}

// [REQ-280]
func TestDemoRefusesBadArguments(t *testing.T) {
	d, _, _ := apiDeps(nil, nil)
	for _, args := range [][]string{{"demo", "-nope"}, {"demo", "extra"}, {"demo", "-scale", "0"}} {
		var out, errOut bytes.Buffer
		if code := execute(args, &out, &errOut, d); code != exitUsage || !strings.Contains(errOut.String(), "usage: praxium demo") {
			t.Errorf("%v: want usage error, got %d %s", args, code, errOut.String())
		}
	}
}

// [REQ-280]
func TestIsLoopbackAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8080": true, "localhost:8080": true, "[::1]:8080": true,
		"0.0.0.0:8080": false, ":8080": false, "192.0.2.10:8080": false, "[::]:8080": false, "nonsense": false,
	} {
		if got := isLoopbackAddr(addr); got != want {
			t.Errorf("%s: got %v want %v", addr, got, want)
		}
	}
}

// [REQ-280]
func TestDemoStopsBeforeServingWhenTheWorldCannotBeMade(t *testing.T) {
	d, _, _ := apiDeps(nil, nil)
	var out bytes.Buffer
	stderr, stderrText := logFile(t)
	if code := execute([]string{"demo", "-addr", "127.0.0.1:0", "-scale", "100"}, &out, stderr, d); code != exitFailure || !strings.Contains(stderrText(), "scale") {
		t.Fatalf("a scale the generator refuses must fail the start, got %d %s", code, stderrText())
	}
}

// [REQ-280]
func TestDemoRefusesAFolderWhoseFirstLoadDidNotFinish(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	d := wiring{deps: fixedDeps()}
	store := memstore.New()
	if _, err := devload.NewKG(context.Background(), store, reg, d.ids, d.clock, devKGName); err != nil {
		t.Fatal(err)
	}
	_, err = demoLoad(context.Background(), store, reg, demoOptions{dataDir: "demo-data", scale: 500, seed: 1}, d, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "did not finish") || !strings.Contains(err.Error(), "demo-data") {
		t.Fatalf("an unfinished KG must stop the start and name the folder, got %v", err)
	}
}

// [REQ-280]
func TestDemoURLNeedsAnAddressWithAPort(t *testing.T) {
	if got := demoURL(&net.UnixAddr{Name: "nonsense", Net: "unix"}, "t"); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := demoURL(&net.TCPAddr{IP: net.IPv6unspecified, Port: 9}, "t"); got != "http://127.0.0.1:9/#/connect?token=t&demo=1" {
		t.Fatalf("got %q", got)
	}
}
