package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/cli"
)

type admSeen struct {
	method, path, query, key string
	body                     map[string]any
}

type admFake struct {
	*httptest.Server
	got []admSeen
}

// admNewFake answers the management routes with fixed bodies and keeps what it was asked.
func admNewFake(t *testing.T) *admFake {
	t.Helper()
	f := &admFake{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		seen := admSeen{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, key: r.Header.Get("Idempotency-Key")}
		_ = json.Unmarshal(raw, &seen.body)
		f.got = append(f.got, seen)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tokens/current":
			fmt.Fprint(w, `{"data":{"subject":{"id":"ada","kind":"person","display_name":"Ada","roles":["viewer"]},"token":{"id":"0123456789abcdef","state":"active","expires_at":"2030-04-02T00:00:00Z","last_used_at":null,"description":"d"}},"meta":{}}`)
		case r.Method == "POST" && r.URL.Path == "/v1/tokens":
			w.WriteHeader(201)
			fmt.Fprint(w, `{"data":{"id":"0123456789abcdef","owner":"ada","token":"pxm_0123456789abcdef_not-a-real-secret"},"meta":{}}`)
		case r.URL.Path == "/v1/users/nobody:disable":
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(404)
			fmt.Fprint(w, `{"code":"not_found.resource","status":404,"title":"Not found","trace_id":"tr-1"}`)
		case r.Method == "POST":
			w.WriteHeader(200)
			fmt.Fprint(w, `{"data":{"id":"ada","state":"disabled"},"meta":{}}`)
		default:
			fmt.Fprint(w, `{"data":[{"id":"ada","kind":"person","roles":["viewer"],"state":"active","created_at":"2030-01-02T03:04:05Z","display_name":"Ada"}],"meta":{"result_complete":{"next_cursor":null}}}`)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *admFake) run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	all := append([]string{"--endpoint", f.URL, "--token", "adm-test-token-not-real"}, args...)
	code := cli.Run(all, fixedEnv(nil, nil), &stdout, &stderr, nil)
	return code, stdout.String(), stderr.String()
}

// [REQ-313]
func TestWhoamiSaysWhoTheTokenIs(t *testing.T) {
	f := admNewFake(t)
	code, out, errOut := f.run("whoami")
	if code != cli.ExitOK || !strings.Contains(out, "ada") || !strings.Contains(out, "token_id") || errOut != "" {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	if code, out, _ := f.run("whoami", "--json"); code != cli.ExitOK || !strings.Contains(out, `"subject"`) {
		t.Fatalf("json: %d %q", code, out)
	}
	if code, _, errOut := f.run("whoami", "extra"); code != cli.ExitUsage || !strings.Contains(errOut, "usage: praxium whoami") {
		t.Fatalf("%d %q", code, errOut)
	}
}

// [REQ-313]
func TestAdminCommandsAskTheRoutesTheyNameWithAFreshIdempotencyKey(t *testing.T) {
	f := admNewFake(t)
	steps := []struct {
		args         []string
		method, path string
		body         string
	}{
		{[]string{"admin", "user", "create", "ada", "--role", "viewer,auditor", "--display-name", "Ada"}, "POST", "/v1/users", `{"description":"","display_name":"Ada","name":"ada","roles":["viewer","auditor"]}`},
		{[]string{"admin", "account", "create", "ingest-demo", "--role", "ingester"}, "POST", "/v1/system-accounts", `{"description":"","display_name":"","name":"ingest-demo","roles":["ingester"]}`},
		{[]string{"admin", "user", "disable", "ada"}, "POST", "/v1/users/ada:disable", `{}`},
		{[]string{"admin", "account", "disable", "ingest-demo"}, "POST", "/v1/system-accounts/ingest-demo:disable", `{}`},
		{[]string{"admin", "token", "revoke", "0123456789abcdef"}, "POST", "/v1/tokens/0123456789abcdef:revoke", `{}`},
		{[]string{"admin", "token", "create", "--owner", "ada", "--role", "viewer", "--expires-in-days", "30", "--description", "dash"}, "POST", "/v1/tokens", `{"description":"dash","expires_in_days":30,"owner":"ada","roles":["viewer"]}`},
	}
	keys := map[string]bool{}
	for _, s := range steps {
		if code, _, errOut := f.run(s.args...); code != cli.ExitOK {
			t.Fatalf("%v: %d %s", s.args, code, errOut)
		}
		got := f.got[len(f.got)-1]
		want := map[string]any{}
		_ = json.Unmarshal([]byte(s.body), &want)
		gotJSON, _ := json.Marshal(got.body)
		wantJSON, _ := json.Marshal(want)
		if got.method != s.method || got.path != s.path || string(gotJSON) != string(wantJSON) {
			t.Errorf("%v asked %s %s %s", s.args, got.method, got.path, gotJSON)
		}
		if len(got.key) < 8 || keys[got.key] {
			t.Errorf("%v: the Idempotency-Key %q is short or used twice", s.args, got.key)
		}
		keys[got.key] = true
	}
}

// [REQ-313]
func TestAdminListsFilterAndATokenIsShownOnceWithAWarning(t *testing.T) {
	f := admNewFake(t)
	code, out, _ := f.run("admin", "user", "list", "--state", "active", "--limit", "5")
	if code != cli.ExitOK || !strings.Contains(out, "ID") || !strings.Contains(out, "ada") {
		t.Fatalf("%d %q", code, out)
	}
	if got := f.got[len(f.got)-1]; got.path != "/v1/users" || !strings.Contains(got.query, "filter%5Bstate%5D%5Bequals%5D=active") || !strings.Contains(got.query, "limit=5") {
		t.Fatalf("%+v", got)
	}
	f.run("admin", "token", "list", "--owner", "ada", "--all")
	if got := f.got[len(f.got)-1]; got.path != "/v1/tokens" || !strings.Contains(got.query, "filter%5Bowner%5D%5Bequals%5D=ada") {
		t.Fatalf("%+v", got)
	}
	code, out, _ = f.run("admin", "token", "create", "--owner", "ada")
	if code != cli.ExitOK || !strings.Contains(out, "pxm_0123456789abcdef_") || !strings.Contains(out, "only time") {
		t.Fatalf("%d %q", code, out)
	}
	if _, out, _ = f.run("admin", "token", "create", "--owner", "ada", "--json"); strings.Contains(out, "only time") {
		t.Fatalf("a JSON answer must stay JSON: %q", out)
	}
}

// [REQ-313]
func TestAdminRefusesWhatItCannotAskAndReportsProblems(t *testing.T) {
	f := admNewFake(t)
	for _, args := range [][]string{
		{"admin"}, {"admin", "user"}, {"admin", "group", "list"}, {"admin", "user", "delete"}, {"admin", "user", "create"},
		{"admin", "user", "list", "extra"}, {"admin", "user", "disable"}, {"admin", "token", "create"}, {"admin", "token", "revoke"},
		{"admin", "token", "purge"}, {"admin", "token", "list", "--bogus"},
	} {
		code, _, errOut := f.run(args...)
		if code != cli.ExitUsage || !strings.Contains(errOut, "usage: praxium admin") {
			t.Errorf("%v: %d %q", args, code, errOut)
		}
	}
	if code, _, errOut := f.run("admin", "user", "disable", "nobody"); code != cli.ExitNotFound || !strings.Contains(errOut, "not_found") {
		t.Errorf("a problem: %d %q", code, errOut)
	}
	for _, path := range []string{"admin", "admin user", "admin account", "admin token", "whoami"} {
		code, out, _ := f.run(append(strings.Fields(path), "--help")...)
		if code != cli.ExitOK || !strings.HasPrefix(out, "usage: praxium ") {
			t.Errorf("%s --help: %d %q", path, code, out)
		}
	}
}

// [REQ-313]
func TestAdminCommandsReportAServerThatIsGoneOrSaysNonsense(t *testing.T) {
	nonsense := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/tokens/current" && r.Method == "GET" {
			fmt.Fprint(w, `{"data":[1],"meta":{}}`)
			return
		}
		fmt.Fprint(w, `{not json`)
	}))
	defer nonsense.Close()
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()
	for _, endpoint := range []string{nonsense.URL, goneURL} {
		for _, args := range [][]string{
			{"whoami"}, {"admin", "user", "create", "ada", "--role", "viewer"}, {"admin", "token", "revoke", "abc"}, {"admin", "token", "create", "--owner", "ada"},
		} {
			var stdout, stderr bytes.Buffer
			code := cli.Run(append([]string{"--endpoint", endpoint, "--token", "adm-test-token-not-real"}, args...), fixedEnv(nil, nil), &stdout, &stderr, nil)
			if code == cli.ExitOK || stderr.Len() == 0 {
				t.Errorf("%s %v: %d %q", endpoint, args, code, stderr.String())
			}
		}
	}
	f := admNewFake(t)
	for _, args := range [][]string{{"admin", "user", "create", "ada", "--bogus"}, {"admin", "token", "create", "--bogus"}} {
		if code, _, _ := f.run(args...); code != cli.ExitUsage {
			t.Errorf("%v: %d", args, code)
		}
	}
}
