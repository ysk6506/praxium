package cli_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/cli"
)

// answerProblem runs a command against a server that always answers one problem and returns what the command said.
func answerProblem(t *testing.T, status int, body string, args ...string) (int, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	var out, errOut bytes.Buffer
	code := cli.Run(append([]string{"--endpoint", srv.URL, "--token", "t"}, args...), fixedEnv(nil, nil), &out, &errOut, nil)
	return code, errOut.String()
}

// [REQ-290]
func TestAnErrorLineSaysWhatTheServerSaidWasWrong(t *testing.T) {
	code, text := answerProblem(t, 422, `{"code":"invalid_input.field","status":422,"trace_id":"tr-1","params":{"detail":"invalid_value: no object type \"NoSuchType\"","message":"no object type \"NoSuchType\"","field":"object_type"}}`, "query", "find", "--type", "NoSuchType")
	want := `praxium: invalid_input.field: field "object_type" (--type): no object type "NoSuchType" (HTTP 422, trace tr-1)`
	if code != cli.ExitInvalid || strings.TrimSpace(text) != want {
		t.Fatalf("%d %q", code, text)
	}
	_, text = answerProblem(t, 404, `{"code":"not_found.resource","status":404,"trace_id":"tr-2","params":{"resource":"object","id":"obj_x"}}`, "query", "get", "obj_x")
	if !strings.Contains(text, `no object "obj_x"`) || !strings.Contains(text, "cannot see it") {
		t.Errorf("a missing object is named: %q", text)
	}
	_, text = answerProblem(t, 404, `{"code":"not_found.resource","status":404,"trace_id":"tr-3","params":{"resource":"kg","id":"nokg"}}`, "kg", "get", "nokg")
	if !strings.Contains(text, `no KG "nokg"`) {
		t.Errorf("a missing KG is named: %q", text)
	}
}

// [REQ-290]
func TestAnErrorLineFallsBackToWhatAnOlderServerGaveAndToFieldErrors(t *testing.T) {
	_, text := answerProblem(t, 422, `{"code":"invalid_input.field","status":422,"trace_id":"tr-4","params":{"detail":"invalid_value: no trait \"x\""}}`, "kg", "list")
	if !strings.Contains(text, `invalid_input.field: no trait "x" (HTTP 422`) {
		t.Errorf("params.detail without its code: %q", text)
	}
	_, text = answerProblem(t, 422, `{"code":"invalid_input.field","status":422,"trace_id":"tr-5","errors":[{"pointer":"/min_seq","code":"invalid_input.field_format","params":{"expected":"a change number"}}]}`, "kg", "list")
	if !strings.Contains(text, `field "/min_seq" (invalid_input.field_format, expected=a change number)`) {
		t.Errorf("field errors: %q", text)
	}
	_, text = answerProblem(t, 401, `{"code":"auth.token_invalid","status":401,"trace_id":"tr-6"}`, "kg", "list")
	if strings.TrimSpace(text) != "praxium: auth.token_invalid (HTTP 401, trace tr-6)" {
		t.Errorf("a problem with nothing more to say stays as it was: %q", text)
	}
}

// [REQ-289]
func TestGlobalFlagsAfterTheCommandAndTheHelpOfAClientCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"kg_id":"kg1","name":"main"}],"meta":{}}`))
	}))
	defer srv.Close()
	var out, errOut bytes.Buffer
	if code := cli.Run([]string{"kg", "list", "--json", "--endpoint", srv.URL, "--token=tok"}, fixedEnv(nil, nil), &out, &errOut, nil); code != 0 || !strings.Contains(out.String(), `"kg_id": "kg1"`) {
		t.Fatalf("%d %s %s", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := cli.Run([]string{"query", "find", "--help"}, fixedEnv(nil, nil), &out, &errOut, nil); code != 0 || !strings.HasPrefix(out.String(), "usage: praxium query find") || !strings.Contains(out.String(), "global flags") {
		t.Fatalf("%d %s", code, out.String())
	}
	errOut.Reset()
	if code := cli.Run([]string{"query", "find", "--bogus"}, fixedEnv(nil, nil), &out, &errOut, nil); code != cli.ExitUsage ||
		!strings.Contains(errOut.String(), "unknown flag -bogus") || !strings.Contains(errOut.String(), "usage: praxium query find") || strings.Count(errOut.String(), "\n") > 4 {
		t.Fatalf("%d %q", code, errOut.String())
	}
}
