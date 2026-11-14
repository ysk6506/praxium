package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func helpRun(t *testing.T, args ...string) (code int, out, errText string) {
	t.Helper()
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	var stdout, stderr bytes.Buffer
	code = execute(args, &stdout, &stderr, d)
	return code, stdout.String(), stderr.String()
}

// [REQ-289]
func TestTopLevelHelpListsEveryCommandInGroups(t *testing.T) {
	var first string
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		code, out, errText := helpRun(t, args...)
		if code != exitOK || errText != "" {
			t.Fatalf("%v: %d %s", args, code, errText)
		}
		if first == "" {
			first = out
		}
		if out != first {
			t.Fatalf("%v must print the same help", args)
		}
	}
	if !strings.Contains(first[:200], "praxium demo") {
		t.Errorf("the fastest start must be at the top:\n%s", first[:200])
	}
	for _, want := range []string{"Get started", "Look at the data", "Bring in your data", "Run a server", "Tools",
		"  demo ", "  local ", "  kg ", "  types ", "  query ", "  object ", "  catalog ", "  health ",
		"  ingest run ", "  ingest inspect ", "  ingest init ", "  api serve ", "  worldgen ", "  contract ", "  version ",
		"--endpoint", "--token", "--json", "before or after the command", "Not implemented yet: the roles worker and collector"} {
		if !strings.Contains(first, want) {
			t.Errorf("the help must have %q", want)
		}
	}
	code, out, errText := helpRun(t)
	if code != exitUsage || out != "" || errText != first {
		t.Errorf("a bare praxium prints the help with the usage code: %d", code)
	}
}

// [REQ-289]
func TestEveryCommandAnswersHelpWithUsageFlagsAndAnExample(t *testing.T) {
	for _, path := range []string{"demo", "local", "ingest", "ingest run", "ingest inspect", "ingest init", "api", "api routes", "worldgen", "contract", "version", "dev",
		"kg", "kg list", "kg get", "types", "types list", "types get", "query", "query find", "query get", "query neighbors", "object", "object get", "object links", "catalog", "health"} {
		for _, flag := range []string{"-h", "--help"} {
			code, out, errText := helpRun(t, append(strings.Fields(path), flag)...)
			if code != exitOK || errText != "" || !strings.HasPrefix(out, "usage: praxium "+strings.Fields(path)[0]) {
				t.Errorf("%s %s: %d %q %q", path, flag, code, out, errText)
			}
			if strings.Contains(out, "help requested") {
				t.Errorf("%s: help is no error", path)
			}
		}
		if code, out, _ := helpRun(t, append([]string{"help"}, strings.Fields(path)...)...); code != exitOK || !strings.HasPrefix(out, "usage: praxium") {
			t.Errorf("help %s: %d", path, code)
		}
	}
	_, out, _ := helpRun(t, "demo", "--help")
	for _, want := range []string{"-addr", "127.0.0.1:8080", "-scale", "20000", "-data-dir", "Ctrl+C"} {
		if !strings.Contains(out, want) {
			t.Errorf("the help of demo lists its flags with the defaults, missing %q:\n%s", want, out)
		}
	}
	if _, out, _ = helpRun(t, "query", "find", "-h"); !strings.Contains(out, "--type") || !strings.Contains(out, "example") || !strings.Contains(out, "before or after the command") {
		t.Errorf("the help of query find:\n%s", out)
	}
	// api serve -h is printed, but its code stays the failure code a test locked before the help asks for.
	if code, out, _ := helpRun(t, "api", "serve", "-h"); code != exitFailure || !strings.Contains(out, "-store-data-dir") || !strings.Contains(out, "-mode") {
		t.Errorf("api serve -h: %d %s", code, out)
	}
}

// [REQ-289]
func TestAWrongCommandLinePrintsOneLineAboutItAndWhereTheHelpIs(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"demo", "-bogus"}, "unknown flag -bogus"},
		{[]string{"ingest", "run", "--bogus"}, "unknown flag -bogus"},
		{[]string{"query", "find", "--bogus"}, "unknown flag -bogus"},
		{[]string{"kg", "frobnicate"}, `kg has no "frobnicate"`},
		{[]string{"frobnicate"}, `unknown command "frobnicate"`},
		{[]string{"worldgen", "-nope"}, "unknown flag -nope"},
	}
	for _, c := range cases {
		code, out, errText := helpRun(t, c.args...)
		if code != exitUsage || out != "" || !strings.Contains(errText, c.want) || strings.Count(errText, "\n") > 4 {
			t.Errorf("%v: %d (%d lines) %q", c.args, code, strings.Count(errText, "\n"), errText)
		}
		if !strings.Contains(errText, "praxium --help") && !strings.Contains(errText, "--help") {
			t.Errorf("%v: it must say where the help is: %q", c.args, errText)
		}
	}
}

// [REQ-289]
func TestRolesThatDoNothingSayTheyAreNotImplemented(t *testing.T) {
	for _, role := range []string{"worker", "collector"} {
		code, out, errText := helpRun(t, role)
		if code != exitFailure || out != "" || !strings.Contains(errText, "not implemented yet") || strings.Contains(errText, "started") {
			t.Errorf("%s: %d %q %q", role, code, out, errText)
		}
	}
}

// [REQ-289]
func TestGlobalFlagsMayFollowTheCommand(t *testing.T) {
	r := startDemo(t, true)
	endpoint := []string{"--endpoint", r.base, "--token", r.token}
	_, before := demoCLI(r, "--json", "kg", "list")
	var out, errOut bytes.Buffer
	args := append([]string{"kg", "list", "--json"}, endpoint...)
	if code := execute(args, &out, &errOut, r.d); code != exitOK || out.String() != before {
		t.Fatalf("flags after the command must work like flags before it: %d %s %s", code, errOut.String(), out.String())
	}
	if code := execute([]string{"kg", "list", "--endpoint=" + r.base, "--token=" + r.token}, &bytes.Buffer{}, &errOut, r.d); code != exitOK {
		t.Fatalf("the --name=value form: %d %s", code, errOut.String())
	}
	errOut.Reset()
	if code := execute([]string{"kg", "list", "--token"}, &bytes.Buffer{}, &errOut, r.d); code != exitUsage || !strings.Contains(errOut.String(), "--token needs a value") {
		t.Fatalf("%d %s", code, errOut.String())
	}
}

// [REQ-290]
func TestApiServeWithoutAModeSaysWhatToRunInsteadWithoutDebtNumbers(t *testing.T) {
	_, _, errText := helpRun(t, "api", "serve")
	for _, want := range []string{"praxium demo", "praxium local", "-mode development"} {
		if !strings.Contains(errText, want) {
			t.Errorf("the refusal must say %q: %s", want, errText)
		}
	}
	if strings.Contains(errText, "TD-") || strings.Contains(errText, "invalid_value") || strings.Count(errText, "\n") > 3 {
		t.Errorf("a bare api serve is told in a few plain lines: %s", errText)
	}
}

// [REQ-290]
func TestDemoChecksTheFormOfTheAddressBeforeAnythingElse(t *testing.T) {
	code, out, errText := helpRun(t, "demo", "-addr", "nonsense")
	if code != exitUsage || out != "" || !strings.Contains(errText, "host:port") || strings.Contains(errText, "loopback") {
		t.Fatalf("a malformed address is the first thing said: %d %s", code, errText)
	}
}

// [REQ-294]
func TestAStartThatTakesLongSaysItIsStillWorking(t *testing.T) {
	var out bytes.Buffer
	err := reportWhile(&out, 5*time.Millisecond, "still loading", func() error {
		time.Sleep(40 * time.Millisecond)
		return nil
	})
	if err != nil || !strings.Contains(out.String(), "still loading (5ms)...") {
		t.Fatalf("%v %q", err, out.String())
	}
	var quiet bytes.Buffer
	if err := reportWhile(&quiet, time.Hour, "x", func() error { return nil }); err != nil || quiet.Len() != 0 {
		t.Fatalf("a fast start says nothing: %v %q", err, quiet.String())
	}
}
