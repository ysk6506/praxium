package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// serveAndGet starts `api serve` with the flags, asks / over HTTP and stops the server.
func serveAndGet(t *testing.T, extra ...string) (int, string) {
	t.Helper()
	d, listeners, stop := apiDeps(map[string]string{"/dev-token": fakeToken}, nil)
	stderr, stderrText := logFile(t)
	done := make(chan int, 1)
	args := append([]string{"api", "serve", "-addr", "127.0.0.1:0", "-mode", "development", "-dev-token-file", "/dev-token"}, extra...)
	go func() { done <- execute(args, &bytes.Buffer{}, stderr, d) }()
	var status int
	var text string
	select {
	case ln := <-listeners:
		for attempt := 0; attempt < 1000; attempt++ {
			res, err := http.Get("http://" + ln.Addr().String() + "/")
			if err == nil {
				data, _ := io.ReadAll(res.Body)
				_ = res.Body.Close()
				status, text = res.StatusCode, string(data)
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the server did not listen: %s", stderrText())
	}
	stop()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
	if status == 0 {
		t.Fatalf("the server never answered: %s", stderrText())
	}
	if !strings.Contains(stderrText(), `"web_ui"`) {
		t.Fatalf("the startup log must say what is served at /: %s", stderrText())
	}
	return status, text
}

// [REQ-275]
func TestApiServeServesTheEmbeddedWebUIAtTheRoot(t *testing.T) {
	status, text := serveAndGet(t)
	if status != 200 || !strings.Contains(text, "<html") {
		t.Fatalf("/ must be the web UI page (or the placeholder of a build without it), got %d %q", status, text)
	}
}

// [REQ-275]
func TestApiServeCanLeaveTheWebUIOut(t *testing.T) {
	status, text := serveAndGet(t, "-web=false")
	if status != 404 || !strings.Contains(text, "not_found.resource") {
		t.Fatalf("with -web=false / is an unknown route, got %d %q", status, text)
	}
}
