package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/devload"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/memstore"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// demoRun is one server of `praxium local` in this process: how to reach it and how to stop it. It, tokenOf, demoGet,
// demoStop and demoCLI keep the names they had while `praxium demo` existed (ADR-0079).
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

// tokenOf finds the token the server printed: in the address, or in the sentence of a build without the web UI.
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
		r.t.Fatal("the server did not stop")
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

// localRun is one `praxium local` in this process.
type localRun struct {
	demoRun
}

// startLocal runs `praxium local` on a free port and waits until it says it is ready. More flags go after -data-dir.
func startLocal(t *testing.T, webBuilt bool, dir string, flags ...string) *localRun {
	t.Helper()
	d, listeners, stop := apiDeps(nil, nil)
	d.webBuilt = func() bool { return webBuilt }
	stderr, stderrText := logFile(t)
	stdout, stdoutText := logFile(t)
	r := &localRun{demoRun{t: t, d: d, out: stdoutText, stderr: stderrText, stop: stop, done: make(chan int, 1)}}
	go func() {
		r.done <- execute(append([]string{"local", "-addr", "127.0.0.1:0", "-data-dir", dir}, flags...), stdout, stderr, d)
	}()
	select {
	case ln := <-listeners:
		r.base = "http://" + ln.Addr().String()
	case code := <-r.done:
		t.Fatalf("local ended with %d: %s %s", code, stdoutText(), stderrText())
	case <-time.After(20 * time.Second):
		t.Fatalf("local did not listen: %s", stderrText())
	}
	for i := 0; !strings.Contains(stdoutText(), "Ctrl+C"); i++ {
		if i > 2000 {
			t.Fatalf("local never said it was ready: %s / %s", stdoutText(), stderrText())
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.token = tokenOf(stdoutText())
	t.Cleanup(func() { r.stop() })
	return r
}

// keepSample builds a data folder with the sample files through the command, as a person would.
func keepSample(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	if code, out, errText := keepRun(t, d, dir, keepRacks); code != exitOK {
		t.Fatalf("setup: %d %s%s", code, out, errText)
	}
	return dir
}

// [REQ-294]
func TestLocalServesTheKGOfAFolderWithoutMakingUpData(t *testing.T) {
	dir := keepSample(t)
	r := startLocal(t, true, dir)
	code, out := demoCLI(&r.demoRun, "query", "find", "--type", "Rack")
	if code != 0 || !strings.Contains(out, "rack-a") || !strings.Contains(out, "rack-b") {
		t.Fatalf("the objects the ingest run made must be served: %d %s", code, out)
	}
	if _, body := demoGet(&r.demoRun, "/v1/kgs/main/objects?filter[object_type][equals]=PhysicalServer", r.token); strings.Contains(body, "srv-") || !strings.Contains(body, "SV-2001") {
		t.Fatalf("nothing but the ingested data may be in the KG: %s", body)
	}
	if status, body := demoGet(&r.demoRun, "/", ""); status != 200 || !strings.Contains(body, "<html") {
		t.Fatalf("/ must be a page, got %d", status)
	}
	if status, _ := demoGet(&r.demoRun, "/v1/kgs", ""); status != 401 {
		t.Fatalf("a request without the token must be refused, got %d", status)
	}
	for _, want := range []string{"local mode", "one person", "not for production", "#/connect?token=" + r.token + "&mode=local", "Ctrl+C"} {
		if !strings.Contains(r.out(), want) {
			t.Errorf("the start output must say %q: %s", want, r.out())
		}
	}
	if strings.Contains(r.stderr(), "not a loopback") {
		t.Errorf("a loopback server must not warn: %s", r.stderr())
	}
	if code := demoStop(&r.demoRun); code != 0 || !strings.Contains(r.out(), "praxium local stopped") {
		t.Fatalf("Ctrl+C must end local cleanly, got %d: %s", code, r.stderr())
	}
}

// [REQ-294]
func TestLocalSaysWhatToDoWhenThereIsNothingToServe(t *testing.T) {
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	empty := t.TempDir()
	for name, c := range map[string]struct{ dir, want string }{
		"a folder that is not there": {filepath.Join(t.TempDir(), "none"), "does not exist"},
		"a folder with no KG":        {empty, "holds no KG"},
	} {
		var out, errOut bytes.Buffer
		code := execute([]string{"local", "-data-dir", c.dir, "-addr", "127.0.0.1:0"}, &out, &errOut, d)
		if code != exitFailure || !strings.Contains(errOut.String(), c.want) || !strings.Contains(errOut.String(), "praxium ingest run -data-dir") {
			t.Errorf("%s: %d %s", name, code, errOut.String())
		}
	}
}

// [REQ-294]
func TestLocalChecksThePortBeforeAnythingAndRefusesBadArguments(t *testing.T) {
	dir := keepSample(t)
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	var errOut bytes.Buffer
	if code := execute([]string{"local", "-data-dir", dir, "-addr", taken.Addr().String()}, &bytes.Buffer{}, &errOut, d); code != exitFailure ||
		!strings.Contains(errOut.String(), "already in use") || !strings.Contains(errOut.String(), "-addr") {
		t.Fatalf("a taken port: %d %s", code, errOut.String())
	}
	for _, args := range [][]string{{"local"}, {"local", "-data-dir", dir, "extra"}, {"local", "-data-dir", dir, "-addr", "nonsense"}, {"local", "-nope"}} {
		errOut.Reset()
		if code := execute(args, &bytes.Buffer{}, &errOut, d); code != exitUsage || !strings.Contains(errOut.String(), "usage: praxium local") || strings.Count(errOut.String(), "\n") > 4 {
			t.Errorf("%v: %d %s", args, code, errOut.String())
		}
	}
}

// [REQ-294]
func TestLocalExplainsABuildWithoutTheWebUI(t *testing.T) {
	dir := keepSample(t)
	r := startLocal(t, false, dir)
	for _, want := range []string{"does not include the web UI", "--endpoint " + r.base, "--token " + r.token} {
		if !strings.Contains(r.out(), want) {
			t.Errorf("the guidance must say %q: %s", want, r.out())
		}
	}
	if strings.Contains(r.out(), "#/connect") {
		t.Errorf("there is no page to open: %s", r.out())
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, r.base+"/v1/kgs", nil)
	req.Header.Set("Authorization", "Bearer "+r.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("the API must still work: %v", err)
	}
	_ = res.Body.Close()
	if code := demoStop(&r.demoRun); code != 0 {
		t.Fatalf("exit %d", code)
	}
}

// [REQ-294]
func TestLocalWarnsForANonLoopbackAddress(t *testing.T) {
	dir := keepSample(t)
	d, listeners, stop := apiDeps(nil, nil)
	d.webBuilt = func() bool { return true }
	stderr, stderrText := logFile(t)
	stdout, stdoutText := logFile(t)
	done := make(chan int, 1)
	go func() { done <- execute([]string{"local", "-data-dir", dir, "-addr", "0.0.0.0:0"}, stdout, stderr, d) }()
	select {
	case <-listeners:
	case <-time.After(20 * time.Second):
		t.Fatalf("no listener: %s", stderrText())
	}
	for i := 0; !strings.Contains(stdoutText(), "Ctrl+C") && i < 2000; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	<-done
	if !strings.Contains(stderrText(), "not a loopback address") || !strings.Contains(stderrText(), "weak protection") || strings.Contains(stdoutText(), "0.0.0.0") {
		t.Fatalf("a non-loopback local server must warn and print an address a browser can use: %s / %s", stderrText(), stdoutText())
	}
}

// [REQ-294]
func TestLocalTokenIsNewEveryRunAndOnlyInTheAddress(t *testing.T) {
	dir := keepSample(t)
	first := startLocal(t, true, dir)
	if _, body := demoGet(&first.demoRun, "/", ""); strings.Contains(body, first.token) {
		t.Fatal("the page must not carry the token")
	}
	if code, _ := demoGet(&first.demoRun, "/v1/kgs", first.token); code != 200 {
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
	if fragment != "/connect?token="+first.token+"&mode=local" || len(first.token) < 32 {
		t.Fatalf("unexpected fragment %q", fragment)
	}
	if strings.Contains(first.stderr(), first.token) {
		t.Fatalf("the log must not have the token: %s", first.stderr())
	}
	if code := demoStop(&first.demoRun); code != 0 {
		t.Fatalf("exit %d", code)
	}
	second := startLocal(t, true, dir)
	if second.token == first.token {
		t.Fatal("every run needs its own token")
	}
	if code, _ := demoGet(&second.demoRun, "/v1/kgs", first.token); code != 401 {
		t.Fatalf("the token of an earlier run must not work, got %d", code)
	}
}

// [REQ-294]
func TestLocalChecksTheFormOfTheAddressBeforeAnythingElse(t *testing.T) {
	code, out, errText := helpRun(t, "local", "-addr", "nonsense")
	if code != exitUsage || out != "" || !strings.Contains(errText, "host:port") || strings.Contains(errText, "loopback") {
		t.Fatalf("a malformed address is the first thing said: %d %s", code, errText)
	}
}

// [REQ-294]
func TestLocalRefusesAFolderWhoseFirstLoadDidNotFinish(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	d := wiring{deps: fixedDeps()}
	store := memstore.New()
	if _, err := devload.NewKG(context.Background(), store, reg, d.ids, d.clock, devKGName); err != nil {
		t.Fatal(err)
	}
	err = requireKG(context.Background(), store, "half-made")
	if err == nil || !strings.Contains(err.Error(), "never completed") || !strings.Contains(err.Error(), "half-made") {
		t.Fatalf("an unfinished KG must stop the start and name the folder, got %v", err)
	}
	if err := requireKG(context.Background(), memstore.New(), "empty"); kernel.CodeOf(err) != kernel.CodeNotFound || !strings.Contains(err.Error(), "holds no KG") {
		t.Fatalf("a store with no KG must say there is none: %v", err)
	}
}

// [REQ-294]
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

// [REQ-294]
func TestConnectURLNeedsAnAddressWithAPort(t *testing.T) {
	if got := connectURL(&net.UnixAddr{Name: "nonsense", Net: "unix"}, "t"); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := connectURL(&net.TCPAddr{IP: net.IPv6unspecified, Port: 9}, "t"); got != "http://127.0.0.1:9/#/connect?token=t&mode=local" {
		t.Fatalf("got %q", got)
	}
}
