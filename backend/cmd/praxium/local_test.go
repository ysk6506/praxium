package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// localRun is one `praxium local` in this process.
type localRun struct {
	demoRun
}

// startLocal runs `praxium local` on a free port and waits until it says it is ready.
func startLocal(t *testing.T, webBuilt bool, dir string) *localRun {
	t.Helper()
	d, listeners, stop := apiDeps(nil, nil)
	d.webBuilt = func() bool { return webBuilt }
	stderr, stderrText := logFile(t)
	stdout, stdoutText := logFile(t)
	r := &localRun{demoRun{t: t, d: d, out: stdoutText, stderr: stderrText, stop: stop, done: make(chan int, 1)}}
	go func() {
		r.done <- execute([]string{"local", "-addr", "127.0.0.1:0", "-data-dir", dir}, stdout, stderr, d)
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
	for _, want := range []string{"local mode", "one person", "not for production", "#/connect?token=" + r.token + "&mode=local", "Ctrl+C"} {
		if !strings.Contains(r.out(), want) {
			t.Errorf("the start output must say %q: %s", want, r.out())
		}
	}
	if strings.Contains(r.out(), "demo=1") || strings.Contains(r.out(), "synthetic") {
		t.Errorf("local mode is not the demo: %s", r.out())
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
