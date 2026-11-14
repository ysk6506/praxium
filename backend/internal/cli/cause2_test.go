package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/cli"
)

// [REQ-290]
func TestCausesNameWhatWasNotFoundForEveryKindOfResource(t *testing.T) {
	cases := []struct{ body, want string }{
		{`{"code":"not_found.resource","status":404,"params":{"resource":"route","id":"/v1/x"}}`, `no route "/v1/x"`},
		{`{"code":"not_found.resource","status":404,"params":{"resource":"object_type"}}`, "no such object type"},
		{`{"code":"not_found.resource","status":404,"params":{"resource":"object_type","id":"Rack"}}`, `no object type "Rack"`},
		{`{"code":"not_found.resource","status":404,"params":{}}`, "not_found.resource"},
	}
	for _, c := range cases {
		_, text := answerProblem(t, 404, c.body, "types", "list")
		if !strings.Contains(text, c.want) {
			t.Errorf("%s: want %q in %q", c.body, c.want, text)
		}
	}
}

// [REQ-290]
func TestACauseListsEachRejectedFieldWithItsPlainParams(t *testing.T) {
	body := `{"code":"invalid_input.body","status":422,"errors":[{"pointer":"/limit","code":"out_of_range","params":{"max":100,"note":"x"}},{"pointer":"/q","code":"required"}]}`
	_, text := answerProblem(t, 422, body, "types", "list")
	for _, want := range []string{`field "/limit" (out_of_range, max=100, note=x)`, `field "/q" (required)`} {
		if !strings.Contains(text, want) {
			t.Errorf("want %q in %q", want, text)
		}
	}
}

// [REQ-289]
func TestGlobalFlagsMayStandAfterTheCommandInEveryForm(t *testing.T) {
	for _, args := range [][]string{
		{"health", "--json=true", "--token=t"},
		{"health", "--json=false", "--token", "t"},
		{"health", "--", "--endpoint"},
	} {
		var out, errOut bytes.Buffer
		cli.Run(append([]string{"--endpoint", "http://127.0.0.1:1"}, args...), fixedEnv(nil, nil), &out, &errOut, nil)
		if strings.Contains(errOut.String(), "unknown flag") {
			t.Errorf("%v: %q", args, errOut.String())
		}
	}
	var out, errOut bytes.Buffer
	if code := cli.Run([]string{"health", "--token"}, fixedEnv(nil, nil), &out, &errOut, nil); code == 0 || !strings.Contains(errOut.String(), "--token needs a value") {
		t.Errorf("%d %q", code, errOut.String())
	}
	errOut.Reset()
	if code := cli.Run([]string{"--nope", "health"}, fixedEnv(nil, nil), &out, &errOut, nil); code == 0 || !strings.Contains(errOut.String(), "unknown flag") {
		t.Errorf("%d %q", code, errOut.String())
	}
}

// [REQ-289]
func TestHelpAndUsageLineOfClientCommands(t *testing.T) {
	if _, ok := cli.Help([]string{"nosuchcommand"}); ok {
		t.Error("an unknown command has no help")
	}
	if got := cli.UsageLine([]string{"nosuch"}); !strings.HasPrefix(got, "usage: praxium") {
		t.Errorf("%q", got)
	}
	if got := cli.UsageLine([]string{"kg", "nosuch"}); !strings.Contains(got, "kg") {
		t.Errorf("the nearest known command answers: %q", got)
	}
}
