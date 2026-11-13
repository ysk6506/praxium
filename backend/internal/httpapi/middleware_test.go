package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

func newInternal(t *testing.T, maxBody int64) (*Server, *bytes.Buffer) {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	srv, err := New(Config{
		Registry: reg, Clock: kernel.NewManualClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)), IDs: kernel.NewSequentialIDs(),
		Logger: slog.New(slog.NewJSONHandler(logs, nil)), MaxBodyBytes: maxBody, Grace: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv, logs
}

func serve(srv *Server, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// [REQ-148]
func TestTraceIDComesFromTraceparentOrIsMade(t *testing.T) {
	srv, _ := newInternal(t, 0)
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"valid", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "4bf92f3577b34da6a3ce929d0e0e4736"},
		{"missing", "", "tr-000001"},
		{"wrong version", "01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "tr-000002"},
		{"upper case", "00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01", "tr-000003"},
		{"all zero", "00-00000000000000000000000000000000-0000000000000000-00", "tr-000004"},
		{"short", "00-abc-def-01", "tr-000005"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/health/live", nil)
			if tt.header != "" {
				req.Header.Set("Traceparent", tt.header)
			}
			rec := serve(srv, req)
			if got := rec.Header().Get("X-Trace-Id"); got != tt.want {
				t.Fatalf("X-Trace-Id = %q, want %q", got, tt.want)
			}
			var body struct {
				Meta struct {
					TraceID string `json:"trace_id"`
				} `json:"meta"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Meta.TraceID != tt.want {
				t.Fatalf("meta.trace_id = %q (%v)", body.Meta.TraceID, err)
			}
		})
	}
}

// [REQ-148]
func TestPanicInAHandlerBecomesAProblemAndIsLogged(t *testing.T) {
	srv, logs := newInternal(t, 0)
	srv.mux.HandleFunc("GET /v1/boom", func(http.ResponseWriter, *http.Request) { panic("secret detail") })
	rec := serve(srv, httptest.NewRequest(http.MethodGet, "/v1/boom", nil))
	if rec.Code != 500 || rec.Header().Get("Content-Type") != problemMediaType {
		t.Fatalf("status %d type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if strings.Contains(rec.Body.String(), "secret detail") {
		t.Fatal("the panic text reached the caller")
	}
	var p problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.Code != "server.internal" || p.TraceID == "" {
		t.Fatalf("problem = %+v (%v)", p, err)
	}
	if !strings.Contains(logs.String(), "secret detail") || !strings.Contains(logs.String(), p.TraceID) {
		t.Fatalf("the log lacks the panic or the trace id: %s", logs.String())
	}
}

// [REQ-148]
func TestAccessLogHasOneStructuredLinePerRequest(t *testing.T) {
	srv, logs := newInternal(t, 0)
	serve(srv, httptest.NewRequest(http.MethodGet, "/v1/health/live", nil))
	serve(srv, httptest.NewRequest(http.MethodGet, "/v1/nothing", nil))
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d lines: %s", len(lines), logs.String())
	}
	want := []struct {
		path   string
		status float64
	}{{"/v1/health/live", 200}, {"/v1/nothing", 404}}
	for i, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"trace_id", "method", "path", "status", "bytes", "duration_ms"} {
			if _, ok := entry[key]; !ok {
				t.Errorf("line %d lacks %s: %s", i, key, line)
			}
		}
		if entry["path"] != want[i].path || entry["status"] != want[i].status || entry["msg"] != "request" {
			t.Errorf("line %d = %s", i, line)
		}
	}
}

// [REQ-148]
func TestBodyLimitAndMediaType(t *testing.T) {
	srv, _ := newInternal(t, 16)
	tests := []struct {
		name   string
		method string
		body   string
		ctype  string
		status int
		code   string
	}{
		{"too large", http.MethodPost, strings.Repeat("x", 17), "application/json", 413, "invalid_input.body_too_large"},
		{"not JSON", http.MethodPost, "{}", "text/plain", 415, "invalid_input.unsupported_media_type"},
		{"JSON body on a GET-only path", http.MethodPost, "{}", "application/json", 405, "invalid_input.method_not_allowed"},
		{"merge patch is JSON", http.MethodPatch, "{}", "application/merge-patch+json", 405, "invalid_input.method_not_allowed"},
		{"a GET with no body", http.MethodGet, "", "", 200, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/v1/health/live", strings.NewReader(tt.body))
			if tt.ctype != "" {
				req.Header.Set("Content-Type", tt.ctype)
			}
			rec := serve(srv, req)
			if rec.Code != tt.status {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			var p problem
			_ = json.Unmarshal(rec.Body.Bytes(), &p)
			if p.Code != tt.code {
				t.Fatalf("code %q, want %q", p.Code, tt.code)
			}
		})
	}
}

// [REQ-148]
func TestAcceptHeaderNegotiation(t *testing.T) {
	tests := []struct {
		accept string
		want   bool
	}{
		{"", true}, {"*/*", true}, {"application/json", true}, {"application/*", true},
		{"application/problem+json", true}, {"text/html, application/json;q=0.5", true},
		{"text/html", false}, {"application/json;q=0", false}, {"application/xml, text/plain", false},
		{"APPLICATION/JSON", true}, {"application/json;q=abc", false},
	}
	for _, tt := range tests {
		if got := acceptsJSON(tt.accept); got != tt.want {
			t.Errorf("acceptsJSON(%q) = %v, want %v", tt.accept, got, tt.want)
		}
	}
}

// [REQ-148]
func TestEntityTagComparisonIsWeakAndListAware(t *testing.T) {
	tests := []struct {
		header string
		want   bool
	}{
		{`"a"`, true}, {`W/"a"`, true}, {`"b", "a"`, true}, {`*`, true}, {`"b"`, false}, {``, false}, {`a`, false},
	}
	for _, tt := range tests {
		if got := matchesETag(tt.header, `"a"`); got != tt.want {
			t.Errorf("matchesETag(%q) = %v", tt.header, got)
		}
	}
}

// [REQ-148]
func TestCursorRoundTripAndTampering(t *testing.T) {
	body := cursorBody{Gen: "g1", Query: "q1", Keys: []string{"7"}, ID: "ot.site"}
	text := encodeCursor(body)
	got, status := decodeCursor(text, "g1", "q1")
	if status != cursorOK || got.ID != "ot.site" || got.Keys[0] != "7" {
		t.Fatalf("round trip: %+v %v", got, status)
	}
	tests := []struct {
		name  string
		text  string
		gen   string
		query string
		want  cursorError
	}{
		{"another generation", text, "g2", "q1", cursorStaleGeneration},
		{"another query", text, "g1", "q2", cursorMalformed},
		{"not base64", "%%%", "g1", "q1", cursorMalformed},
		{"not JSON", "e3t7", "g1", "q1", cursorMalformed},
		{"no id", encodeCursor(cursorBody{Gen: "g1", Query: "q1"}), "g1", "q1", cursorMalformed},
		{"empty", "", "g1", "q1", cursorMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, status := decodeCursor(tt.text, tt.gen, tt.query); status != tt.want {
				t.Fatalf("status %v, want %v", status, tt.want)
			}
		})
	}
}

// [REQ-148]
func TestRangeFilterReadsOpenEnds(t *testing.T) {
	tests := []struct {
		text string
		v    int
		want bool
	}{
		{"1..5", 3, true}, {"1..5", 6, false}, {"1..5", 0, false}, {"..5", 0, true}, {"5..", 99, true},
		{"5..", 4, false}, {"..", 1, true}, {"abc..5", 1, false}, {"1..x", 1, false}, {"3", 3, false},
	}
	for _, tt := range tests {
		if got := inRange(tt.v, tt.text); got != tt.want {
			t.Errorf("inRange(%d, %q) = %v", tt.v, tt.text, got)
		}
	}
}

// [REQ-148]
func TestServeIsGracefulAndReadyFollowsLife(t *testing.T) {
	srv, _ := newInternal(t, 0)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()
	base := "http://" + ln.Addr().String()
	started := false
	var probeErr error
	for attempt := 0; attempt < 500 && !started; attempt++ {
		var resp *http.Response
		resp, probeErr = http.Get(base + "/v1/health/started")
		if probeErr == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			started = resp.StatusCode == 200
		}
		if !started {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !started {
		t.Fatalf("the server never started: %v", probeErr)
	}
	srv.draining.Store(true)
	resp, err := http.Get(base + "/v1/health/ready")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("ready while draining = %d, want 503", resp.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after the context ended")
	}
}

// [REQ-148]
func TestServerNeedsItsParts(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("a server without parts was built")
	}
}
