package cli_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/cli"
)

func fixedEnv(vars map[string]string, files map[string]string) cli.Env {
	return cli.Env{
		Lookup: func(name string) (string, bool) { v, ok := vars[name]; return v, ok },
		ReadFile: func(path string) ([]byte, error) {
			if c, ok := files[path]; ok {
				return []byte(c), nil
			}
			return nil, errors.New("no such file")
		},
	}
}

// [REQ-245]
func TestSettingsFollowFlagThenEnvironmentThenFileThenDefault(t *testing.T) {
	files := map[string]string{"/cfg.json": `{"endpoint": "http://file.example", "token": "file-token"}`, "/bad.json": `[`}
	tests := []struct {
		name      string
		flagEP    string
		flagTok   string
		cfg       string
		vars      map[string]string
		wantEP    string
		wantToken string
		wantErr   bool
	}{
		{"defaults", "", "", "", nil, cli.DefaultEndpoint, "", false},
		{"file", "", "", "/cfg.json", nil, "http://file.example", "file-token", false},
		{"file named by the environment", "", "", "", map[string]string{cli.EnvConfig: "/cfg.json"}, "http://file.example", "file-token", false},
		{"environment beats file", "", "", "/cfg.json", map[string]string{cli.EnvEndpoint: "http://env.example/", cli.EnvToken: "env-token"}, "http://env.example", "env-token", false},
		{"flag beats environment", "http://flag.example", "flag-token", "/cfg.json", map[string]string{cli.EnvEndpoint: "http://env.example", cli.EnvToken: "env-token"}, "http://flag.example", "flag-token", false},
		{"missing file", "", "", "/nowhere.json", nil, "", "", true},
		{"broken file", "", "", "/bad.json", nil, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cli.Resolve(tt.flagEP, tt.flagTok, tt.cfg, fixedEnv(tt.vars, files))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v", err)
			}
			if err == nil && (got.Endpoint != tt.wantEP || got.Token != tt.wantToken) {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

// [REQ-245]
func TestExitCodesFollowTheProblemGroup(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{nil, cli.ExitOK},
		{&cli.ProblemError{Code: "auth.token_missing"}, cli.ExitAuth},
		{&cli.ProblemError{Code: "permission.role_missing"}, cli.ExitPermission},
		{&cli.ProblemError{Code: "not_found.resource"}, cli.ExitNotFound},
		{&cli.ProblemError{Code: "invalid_input.field"}, cli.ExitInvalid},
		{&cli.ProblemError{Code: "conflict.generation_changed"}, cli.ExitConflict},
		{&cli.ProblemError{Code: "rate_limited.requests"}, cli.ExitRate},
		{&cli.ProblemError{Code: "blocked.proposal"}, cli.ExitBlocked},
		{&cli.ProblemError{Code: "stale.since_too_old"}, cli.ExitStale},
		{&cli.ProblemError{Code: "server.internal"}, cli.ExitServer},
		{&cli.ProblemError{Code: "mystery"}, cli.ExitGeneral},
		{errors.New("other"), cli.ExitGeneral},
		{fmt.Errorf("wrapped: %w", &cli.ProblemError{Code: "not_found.resource"}), cli.ExitNotFound},
	}
	seen := map[int]bool{}
	for _, tt := range tests {
		if got := cli.ExitCode(tt.err); got != tt.want {
			t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
		}
		seen[tt.want] = true
	}
	if len(seen) < 11 {
		t.Errorf("only %d distinct codes are tested", len(seen))
	}
}

type fakeServer struct {
	*httptest.Server
	requests []*http.Request
}

func newFake(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/health/live" || r.URL.Path == "/v1/health/ready":
			fmt.Fprint(w, `{"data":{},"meta":{"trace_id":"t"}}`)
		case r.URL.Path == "/v1/health/started":
			w.WriteHeader(503)
			fmt.Fprint(w, `{"code":"server.unavailable","status":503,"trace_id":"t"}`)
		case r.URL.Path == "/v1/object-types" && r.URL.Query().Get("cursor") == "":
			fmt.Fprint(w, `{"data":[{"id":"ot.a","type_no":1,"name":"A","layer":"facility","namespace":"std","status":"active"}],"meta":{"result_complete":{"next_cursor":"c2"}}}`)
		case r.URL.Path == "/v1/object-types":
			fmt.Fprint(w, `{"data":[{"id":"ot.b","type_no":2,"name":"B","layer":"compute","namespace":"std","status":"active"}],"meta":{"result_complete":{"next_cursor":null}}}`)
		case r.URL.Path == "/v1/object-types/ot.a":
			fmt.Fprint(w, `{"data":{"id":"ot.a","name":"A","traits":["tr.x","tr.y"],"parent":null},"meta":{}}`)
		case r.URL.Path == "/v1/catalog/channels":
			fmt.Fprint(w, `{"data":[{"channel":"service","states":["up","down"]}],"meta":{}}`)
		case r.URL.Path == "/v1/catalog":
			fmt.Fprint(w, `{"data":[],"meta":{}}`)
		case r.URL.Path == "/v1/roles" && r.Header.Get("Authorization") == "":
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(401)
			fmt.Fprint(w, `{"code":"auth.token_missing","status":401,"title":"Unauthorized","trace_id":"tr-9"}`)
		case r.URL.Path == "/v1/object-types/ot.none":
			w.WriteHeader(404)
			fmt.Fprint(w, `not json at all`)
		default:
			fmt.Fprint(w, `{"data":[],"meta":{}}`)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func run(t *testing.T, f *fakeServer, args ...string) (code int, out, errOut string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	all := append([]string{"--endpoint", f.URL, "--token", "test-token-not-real-0001"}, args...)
	code = cli.Run(all, fixedEnv(nil, nil), &stdout, &stderr, nil)
	return code, stdout.String(), stderr.String()
}

// [REQ-245]
func TestTypesListPrintsATableOrJSONAndFollowsCursors(t *testing.T) {
	f := newFake(t)
	code, out, _ := run(t, f, "types", "list")
	if code != 0 || !strings.Contains(out, "ID") || !strings.Contains(out, "ot.a") || strings.Contains(out, "ot.b") {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	code, out, _ = run(t, f, "types", "list", "--all")
	if code != 0 || !strings.Contains(out, "ot.a") || !strings.Contains(out, "ot.b") {
		t.Fatalf("--all: code %d, out:\n%s", code, out)
	}
	code, out, _ = run(t, f, "--json", "types", "list", "--all", "--limit", "1", "--filter", "layer.equals=facility", "--filter", "name.prefix=A")
	if code != 0 || !strings.HasPrefix(strings.TrimSpace(out), "[") || !strings.Contains(out, `"ot.b"`) {
		t.Fatalf("--json: code %d, out:\n%s", code, out)
	}
	last := f.requests[len(f.requests)-2]
	if last.URL.Query().Get("filter[layer][equals]") != "facility" || last.URL.Query().Get("filter[name][prefix]") != "A" || last.URL.Query().Get("limit") != "1" {
		t.Fatalf("the query was %s", last.URL.RawQuery)
	}
	if got := last.Header.Get("Authorization"); got != "Bearer test-token-not-real-0001" {
		t.Fatalf("Authorization = %q", got)
	}
}

// [REQ-245]
func TestTypesGetCatalogAndHealth(t *testing.T) {
	f := newFake(t)
	code, out, _ := run(t, f, "types", "get", "ot.a", "--include", "ancestors")
	if code != 0 || !strings.Contains(out, "tr.x,tr.y") || !strings.Contains(out, "-") {
		t.Fatalf("get: code %d, out:\n%s", code, out)
	}
	if got := f.requests[len(f.requests)-1].URL.Query().Get("include"); got != "ancestors" {
		t.Fatalf("include = %q", got)
	}
	code, out, _ = run(t, f, "--json", "types", "get", "ot.a")
	if code != 0 || !strings.Contains(out, `"tr.x"`) {
		t.Fatalf("get --json: code %d, out:\n%s", code, out)
	}
	code, out, _ = run(t, f, "catalog", "channels")
	if code != 0 || !strings.Contains(out, "service") || !strings.Contains(out, "up,down") {
		t.Fatalf("catalog: code %d, out:\n%s", code, out)
	}
	if code, out, _ = run(t, f, "catalog"); code != 0 || !strings.Contains(out, "(none)") {
		t.Fatalf("catalog index: code %d, out:\n%s", code, out)
	}
	code, out, errOut := run(t, f, "health")
	if code != cli.ExitServer || !strings.Contains(out, "started") || !strings.Contains(errOut, "server.unavailable") {
		t.Fatalf("health: code %d, out:\n%s\nerr:\n%s", code, out, errOut)
	}
	code, out, _ = run(t, f, "--json", "health")
	if code != cli.ExitServer || !strings.Contains(out, `"live": 200`) {
		t.Fatalf("health --json: code %d, out:\n%s", code, out)
	}
}

// [REQ-245]
func TestProblemsBecomeExitCodesAndOneErrorLine(t *testing.T) {
	f := newFake(t)
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"--endpoint", f.URL, "catalog", "roles"}, fixedEnv(nil, nil), &stdout, &stderr, nil)
	if code != cli.ExitAuth || stdout.Len() != 0 || !strings.Contains(stderr.String(), "auth.token_missing") || !strings.Contains(stderr.String(), "tr-9") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	code, _, errOut := run(t, f, "types", "get", "ot.none")
	if code != cli.ExitServer || !strings.Contains(errOut, "server.internal") {
		t.Fatalf("a body that is not a problem: code %d, stderr %q", code, errOut)
	}
}

// [REQ-245]
func TestTransportAndUsageErrorsHaveTheirOwnCodes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	env := fixedEnv(nil, nil)
	if code := cli.Run([]string{"--endpoint", "http://127.0.0.1:1", "catalog"}, env, &stdout, &stderr, nil); code != cli.ExitTransport {
		t.Fatalf("an unreachable server: code %d, stderr %q", code, stderr.String())
	}
	usage := [][]string{
		{}, {"nothing"}, {"types"}, {"types", "list", "--kind", "pet"}, {"types", "list", "--filter", "nodots"},
		{"types", "get"}, {"types", "frob"}, {"catalog", "nope"}, {"catalog", "a", "b"}, {"--bogus", "health"},
	}
	for _, args := range usage {
		stderr.Reset()
		code := cli.Run(append([]string{"--endpoint", "http://127.0.0.1:1"}, args...), env, &stdout, &stderr, nil)
		if code != cli.ExitUsage || !strings.Contains(stderr.String(), "usage") && !strings.Contains(stderr.String(), "client commands") {
			t.Errorf("%v: code %d, stderr %q", args, code, stderr.String())
		}
	}
	if code := cli.Run([]string{"--config", "/nowhere.json", "catalog"}, env, &stdout, &stderr, nil); code != cli.ExitGeneral {
		t.Fatalf("a config file that cannot be read: code %d", code)
	}
	for _, word := range []string{"health", "types", "catalog"} {
		if !cli.IsClientCommand(word) {
			t.Errorf("%s is a client command", word)
		}
	}
	if cli.IsClientCommand("api") || cli.IsClientCommand("") {
		t.Fatal("api is not a client command")
	}
}

// [REQ-245]
func TestClientFlagsAreRecognisedBeforeTheCommand(t *testing.T) {
	for word, want := range map[string]bool{
		"--endpoint": true, "-token": true, "--config=/x": true, "--json": true,
		"--version": false, "-version": false, "health": false, "": false, "--": false,
	} {
		if got := cli.IsClientFlag(word); got != want {
			t.Errorf("IsClientFlag(%q) = %v", word, got)
		}
	}
}
