package mcpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// [REQ-515]
func TestParseStrictReadsWhatTheStandardDecoderReads(t *testing.T) {
	good := []string{
		`{}`, `[]`, `"a"`, `0`, `-0.5e+3`, `true`, `false`, `null`,
		`{"a":[1,2,{"b":null}],"c":"d\u00e9\n\ud83d\ude00"}`, ` { "a" : 1 } `,
	}
	for _, in := range good {
		got, err := parseStrict([]byte(in))
		if err != nil {
			t.Errorf("parseStrict(%q) refused a valid value: %v", in, err)
			continue
		}
		want, err := decodeStandard(in)
		if err != nil {
			t.Fatalf("the standard decoder refused %q: %v", in, err)
		}
		if jsonOf(t, got) != jsonOf(t, want) {
			t.Errorf("parseStrict(%q) = %s; the standard decoder reads %s", in, jsonOf(t, got), jsonOf(t, want))
		}
	}
}

func decodeStandard(in string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(in))
	dec.UseNumber()
	var v any
	err := dec.Decode(&v)
	return v, err
}

// [REQ-515]
func TestParseStrictRefusesWhatIsNotJSON(t *testing.T) {
	bad := []string{
		``, ` `, `{`, `}`, `[1,]`, `{"a":1,}`, `{"a" 1}`, `{a:1}`, `'a'`, `01`, `1.`, `.5`, `+1`, `1e`, `tru`, `nul`, `truex`,
		`"a`, "\"a\x01\"", `"\x"`, `"\u12"`, `"\ud800"`, `"\udc00"`, `"\ud800\u0041"`, `{"a":1} {"b":2}`, `[1 2]`, "\"\xff\"", `{"a":1}x`,
	}
	for _, in := range bad {
		if _, err := parseStrict([]byte(in)); !errors.Is(err, errSyntax) {
			t.Errorf("parseStrict(%q) error = %v; want a syntax error", in, err)
		}
	}
}

// [REQ-515]
func TestParseStrictNamesTheOtherRefusals(t *testing.T) {
	cases := map[string]error{
		`{"a":1,"a":2}`:                     errDuplicate,
		`{"a":1,"\u0061":2}`:                errDuplicate,
		`{"x":{"name":"a","name":"b"}}`:     errDuplicate,
		`{"a\u0000":1}`:                     errNulKey,
		`{"x":{"method\u0000":1}}`:          errNulKey,
		strings.Repeat("[", maxDepth+1):     errTooDeep,
		strings.Repeat(`{"a":`, maxDepth+1): errTooDeep,
	}
	for in, want := range cases {
		if _, err := parseStrict([]byte(in)); !errors.Is(err, want) {
			t.Errorf("parseStrict(%.40q) error = %v; want %v", in, err, want)
		}
	}
	deepest := strings.Repeat("[", maxDepth) + strings.Repeat("]", maxDepth)
	if _, err := parseStrict([]byte(deepest)); err != nil {
		t.Errorf("a value nested %d deep must be read: %v", maxDepth, err)
	}
}

// [REQ-515]
func TestParseStrictKeepsKeysLetterForLetter(t *testing.T) {
	got, err := parseStrict([]byte(`{"Method":1,"method":2}`))
	if err != nil {
		t.Fatalf("keys that differ in case are different keys: %v", err)
	}
	obj, _ := got.(map[string]any)
	if len(obj) != 2 || obj["method"] != json.Number("2") || obj["Method"] != json.Number("1") {
		t.Errorf("got %v; want both keys kept apart", obj)
	}
}

// [REQ-515] Security test 1: a member that is a known name in other letters is refused, not read as the known one.
func TestSecurity01KeyLetterCase(t *testing.T) {
	rg := newRig(t)
	good := modernCall(t, tokViewer, "tools/list", nil)
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"Method":"tools/list","params":{"_meta":{}}}`,
		`{"JSONRPC":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","ID":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","Params":{"_meta":{}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","METHOD":"tools/call"}`,
	} {
		r := rg.do(good.withBody(body))
		if r.status != http.StatusBadRequest || r.errCode(t) != codeInvalidRequest {
			t.Errorf("body %s: status %d, body %s; want 400 and -32600", body, r.status, r.body)
		}
	}
	callBody := message(t, 1, "tools/call", map[string]any{"_meta": meta(), "name": "kg_whoami", "Arguments": map[string]any{}})
	if r := rg.do(modernCall(t, tokViewer, "tools/call", map[string]any{"name": "kg_whoami"}).withBody(callBody)); r.status != http.StatusOK {
		t.Errorf("a stray member in params is not a refusal of the transport: %d %s", r.status, r.body)
	}
}

// [REQ-515] Security test 2: a NUL in a key.
func TestSecurity02NulInKey(t *testing.T) {
	rg := newRig(t)
	good := modernCall(t, tokViewer, "tools/list", nil)
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"method\u0000":"tools/list","method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{},"x\u0000":1}}`,
	} {
		r := rg.do(good.withBody(body))
		if r.status != http.StatusBadRequest || r.errCode(t) != codeInvalidRequest {
			t.Errorf("body %s: status %d %s; want 400 and -32600", body, r.status, r.body)
		}
	}
}

// [REQ-515] Security test 3: the same key twice, at the top and inside params.
func TestSecurity03DuplicateKey(t *testing.T) {
	rg := newRig(t)
	good := modernCall(t, tokViewer, "tools/call", map[string]any{"name": "kg_whoami"})
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","method":"tools/call"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kg_whoami","name":"other","_meta":{}}}`,
		`{"jsonrpc":"2.0","id":1,"id":2,"method":"tools/call"}`,
	} {
		r := rg.do(good.withBody(body))
		if r.status != http.StatusBadRequest || r.errCode(t) != codeInvalidRequest {
			t.Errorf("body %s: status %d %s; want 400 and -32600", body, r.status, r.body)
		}
	}
}

// [REQ-515] Security test 13: a big body, a deeply nested one, a long header: refused, and the server stands.
func TestSecurity13BigDeepAndLong(t *testing.T) {
	rg := newRig(t, func(c *Config) { c.MaxBodyBytes = 2048 })
	good := modernCall(t, tokViewer, "tools/list", nil)
	big := good.withBody(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"pad":"` + strings.Repeat("a", 4000) + `"}}`)
	if r := rg.do(big); r.status != http.StatusRequestEntityTooLarge || r.problemCode(t) != "invalid_input.body_too_large" {
		t.Errorf("a big body: %d %s; want 413 body_too_large", r.status, r.body)
	}
	deep := good.withBody(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":` + strings.Repeat("[", 500) + strings.Repeat("]", 500) + `}`)
	if r := rg.do(deep); r.status != http.StatusBadRequest {
		t.Errorf("a deep body: %d %s; want 400", r.status, r.body)
	}
	long := good.withHeader("X-Padding", strings.Repeat("h", 100000))
	if r := rg.do(long); r.status >= 500 {
		t.Errorf("a long header must not break the handler: %d", r.status)
	}
	if r := rg.do(good); r.status != http.StatusOK {
		t.Errorf("the handler must answer a good request after those: %d %s", r.status, r.body)
	}
}

// [REQ-515] Security test 14: a batch, and any body that is not one message.
func TestSecurity14Batch(t *testing.T) {
	rg := newRig(t)
	good := modernCall(t, tokViewer, "tools/list", nil)
	for _, body := range []string{
		`[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`, `[]`, `"tools/list"`, `1`, `null`,
	} {
		r := rg.do(good.withBody(body))
		if r.status != http.StatusBadRequest || r.errCode(t) != codeInvalidRequest {
			t.Errorf("body %s: %d %s; want 400 and -32600", body, r.status, r.body)
		}
	}
}

// FuzzParseStrict holds three promises for any input: it does not panic; what it accepts the standard decoder accepts
// too and reads the same when no key repeats; and what it accepts it accepts again written out in the standard way.
// Only the seed corpus runs in the usual checks.
func FuzzParseStrict(f *testing.F) {
	seeds := []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kg_whoami","arguments":{}}}`,
		`{"a":1,"a":2}`, `{"A":1,"a":2}`, `{"a\u0000":1}`, `[1,[2,[3]]]`, `"\ud83d\ude00"`, `"\ud800"`, `-1.5E+10`, `{"a":`, ``, `nul`,
		"\"\xff\"", `{"":{"":{}}}`, `[` + strings.Repeat("[", 40),
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := parseStrict(data)
		if err == nil {
			checkAccepted(t, data, got)
		}
	})
}

// checkAccepted holds what must be true of a value the parser accepted.
func checkAccepted(t *testing.T, data []byte, got any) {
	t.Helper()
	if !json.Valid(data) {
		t.Fatalf("parseStrict accepted what encoding/json refuses: %q", data)
	}
	again, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("an accepted value must be written out: %v", err)
	}
	back, err := parseStrict(again)
	if err != nil || jsonOf(t, back) != string(again) {
		t.Fatalf("an accepted value must read back the same: %q -> %q (%v)", data, again, err)
	}
}
