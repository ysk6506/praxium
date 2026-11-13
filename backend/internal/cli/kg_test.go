package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/cli"
)

// seen is one request the fake KG server got.
type seen struct{ method, path, query, body string }

type kgFake struct {
	*httptest.Server
	got []seen
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// newKGFake answers the KG routes with the answers the contract shows as its examples.
func newKGFake(t *testing.T) *kgFake {
	t.Helper()
	f := &kgFake{}
	pages := map[string]string{"/v1/kgs": "kgs.json", "/v1/kgs/main": "kg.json"}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.got = append(f.got, seen{r.Method, r.URL.Path, r.URL.RawQuery, string(body)})
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/kgs/nokg"):
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(404)
			fmt.Fprint(w, `{"code":"not_found.resource","status":404,"title":"Not found","trace_id":"tr-1"}`)
		case strings.HasPrefix(r.URL.Path, "/v1/kgs/odd"):
			fmt.Fprint(w, `{"data":[1],"meta":{}}`)
		case strings.HasPrefix(r.URL.Path, "/v1/kgs/sum") && strings.HasSuffix(r.URL.Path, "/questions/find"):
			fmt.Fprint(w, `{"data":{"counts_by_type":[{"object_type":"Rack","count":4}]},"meta":{}}`)
		case strings.HasPrefix(r.URL.Path, "/v1/kgs/sum") && strings.HasSuffix(r.URL.Path, "/questions/neighbors"):
			fmt.Fprint(w, `{"data":{"groups":[]},"meta":{}}`)
		case strings.HasPrefix(r.URL.Path, "/v1/kgs/sum"):
			fmt.Fprint(w, `{"data":{"object":{"object_id":"obj_1"}},"meta":{}}`)
		case pages[r.URL.Path] != "":
			fmt.Fprint(w, readFixture(t, pages[r.URL.Path]))
		case strings.HasSuffix(r.URL.Path, "/questions/find") || r.URL.Path == "/v1/kgs/main/objects":
			if strings.Contains(string(body), `"cursor"`) || r.URL.Query().Get("cursor") != "" {
				fmt.Fprint(w, `{"data":{"counts_by_type":[{"object_type":"Rack","count":4}],"items":[{"object_id":"obj_3","object_type":"Rack","display_name":"Rack R02","matched_on":[]}]},"meta":{"result_complete":{"total":4,"next_cursor":null}}}`)
				return
			}
			fmt.Fprint(w, readFixture(t, "find.json"))
		case strings.HasSuffix(r.URL.Path, "/links") || strings.HasSuffix(r.URL.Path, "/questions/neighbors"):
			fmt.Fprint(w, readFixture(t, "links.json"))
		case strings.Contains(r.URL.Path, "/objects/") || strings.HasSuffix(r.URL.Path, "/questions/get"):
			fmt.Fprint(w, readFixture(t, "get.json"))
		default:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(404)
			fmt.Fprint(w, `{"code":"not_found.resource","status":404,"title":"Not found","trace_id":"tr-1"}`)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *kgFake) last() seen { return f.got[len(f.got)-1] }

// [REQ-257]
func TestKGCommandsPrintWhatTheServerAnswered(t *testing.T) {
	f := newKGFake(t)
	for _, tc := range []struct {
		golden string
		args   []string
	}{
		{"kg_list.txt", []string{"kg", "list"}},
		{"kg_get.txt", []string{"kg", "get"}},
		{"object_get.txt", []string{"object", "get", "--include", "attributes,link_counts,evidence", "obj_0e54625b65d5501a4bdb9af739ac1907"}},
		{"object_links.txt", []string{"object", "links", "--direction", "out", "obj_0e54625b65d5501a4bdb9af739ac1907"}},
		{"query_find.txt", []string{"query", "find", "--type", "Rack"}},
		{"query_get.txt", []string{"query", "get", "--include", "attributes", "obj_0e54625b65d5501a4bdb9af739ac1907"}},
		{"query_neighbors.txt", []string{"query", "neighbors", "--steps", "2", "obj_0e54625b65d5501a4bdb9af739ac1907"}},
		{"query_find.json", []string{"--json", "query", "find", "--type", "Rack"}},
		{"object_get.json", []string{"--json", "object", "get", "obj_0e54625b65d5501a4bdb9af739ac1907"}},
	} {
		t.Run(tc.golden, func(t *testing.T) {
			code, out, errOut := run(t, &fakeServer{Server: f.Server}, tc.args...)
			if code != 0 {
				t.Fatalf("code %d: %s", code, errOut)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "golden", tc.golden))
			if err != nil {
				t.Fatal(err)
			}
			if !sameOutput(tc.golden, out, string(want)) {
				t.Fatalf("output differs from testdata/golden/%s:\n%s", tc.golden, out)
			}
		})
	}
}

// sameOutput compares what a command printed with its golden file: a text byte for byte, JSON as a value (the
// golden files of JSON are laid out by the formatter of the repository, not by the command).
func sameOutput(name, got, want string) bool {
	if !strings.HasSuffix(name, ".json") {
		return got == want
	}
	var a, b any
	return json.Unmarshal([]byte(got), &a) == nil && json.Unmarshal([]byte(want), &b) == nil && reflect.DeepEqual(a, b)
}

// [REQ-257]
func TestKGCommandsAskTheRoutesTheyName(t *testing.T) {
	f := newKGFake(t)
	fake := &fakeServer{Server: f.Server}
	id := "obj_0e54625b65d5501a4bdb9af739ac1907"
	for _, tc := range []struct {
		args              []string
		method, path      string
		queryHas, bodyHas []string
	}{
		{[]string{"kg", "list", "--role", "production", "--state", "serving", "--limit", "5"}, "GET", "/v1/kgs",
			[]string{"filter%5Brole%5D%5Bequals%5D=production", "filter%5Bstate%5D%5Bequals%5D=serving", "limit=5"}, nil},
		{[]string{"object", "get", "--kg", "main", "--at-seq", "1", "--view", "as_designed", "--min-seq", "1", "--wait", "PT1S", id}, "GET", "/v1/kgs/main/objects/" + id,
			[]string{"change_seq=1", "view=as_designed", "min_seq=1", "wait_timeout=PT1S"}, nil},
		{[]string{"object", "get", "--version", "kv_1", id}, "GET", "/v1/kgs/main/objects/" + id, []string{"kg_version_id=kv_1"}, nil},
		{[]string{"object", "links", "--link-type", "MOUNTED_IN", "--link-type", "LOCATED_IN", "--steps", "3", "--limit", "9", id}, "GET", "/v1/kgs/main/objects/" + id + "/links",
			[]string{"filter%5Blink_type%5D%5Bin%5D=MOUNTED_IN%2CLOCATED_IN", "max_steps=3", "limit=9"}, nil},
		{[]string{"query", "find", "--no-subtypes", "--trait", "tr.x", "--text", "R0", "--identifier", "redfish_system_id:a:b",
			"--attr", "model.in=M1,M2", "--attr", "name.range=A..B", "--attr", "name.prefix=R", "--at-seq", "0", "--limit", "3"}, "POST", "/v1/kgs/main/questions/find", nil,
			[]string{`"include_subtypes":false`, `"trait":"tr.x"`, `"text":"R0"`, `"scheme":"redfish_system_id"`, `"values":["M1","M2"]`,
				`"values":["A","B"]`, `"op":"prefix"`, `"change_seq":0`, `"limit":3`, `"shape":"items"`}},
		{[]string{"query", "find", "--summary"}, "POST", "/v1/kgs/main/questions/find", nil, []string{`"shape":"summary"`}},
		{[]string{"query", "get", "--identifier", "redfish_system_id:x", "--include", "attributes,identifiers"}, "POST", "/v1/kgs/main/questions/get", nil,
			[]string{`"identifier":{"scheme":"redfish_system_id","value":"x"}`, `"include":["attributes","identifiers"]`}},
		{[]string{"query", "neighbors", "--direction", "in", "--meaning", "contained_in", "--peer-type", "Rack", "--link-type", "MOUNTED_IN", id}, "POST", "/v1/kgs/main/questions/neighbors", nil,
			[]string{`"direction":"in"`, `"meanings":["contained_in"]`, `"peer_types":["Rack"]`, `"link_types":["MOUNTED_IN"]`, `"object_id":"` + id + `"`}},
	} {
		if code, _, errOut := run(t, fake, tc.args...); code != 0 {
			t.Fatalf("%v: code %d: %s", tc.args, code, errOut)
		}
		got := f.last()
		if got.method != tc.method || got.path != tc.path {
			t.Errorf("%v asked %s %s", tc.args, got.method, got.path)
		}
		for _, part := range tc.queryHas {
			if !strings.Contains(got.query, part) {
				t.Errorf("%v: the query %q lacks %q", tc.args, got.query, part)
			}
		}
		for _, part := range tc.bodyHas {
			if !strings.Contains(got.body, part) {
				t.Errorf("%v: the body %q lacks %q", tc.args, got.body, part)
			}
		}
	}
}

// [REQ-257]
func TestKGCommandsFollowCursorsAndRefuseWhatTheyCannotAsk(t *testing.T) {
	f := newKGFake(t)
	fake := &fakeServer{Server: f.Server}
	code, out, _ := run(t, fake, "query", "find", "--type", "Rack", "--all")
	if code != 0 || !strings.Contains(out, "R02") || !strings.Contains(out, "R01") || strings.Contains(out, "--all reads") {
		t.Fatalf("--all: code %d\n%s", code, out)
	}
	code, out, _ = run(t, fake, "--json", "query", "find", "--type", "Rack", "--all")
	if code != 0 || strings.Count(out, `"object_id"`) != 3 {
		t.Fatalf("--all --json should merge the pages: code %d\n%s", code, out)
	}
	if code, out, _ = run(t, fake, "query", "find", "--type", "Rack", "--limit", "2"); code != 0 || !strings.Contains(out, "shown of") {
		t.Fatalf("a page that is not the last says so: code %d\n%s", code, out)
	}
	code, _, errOut := run(t, fake, "object", "get", "obj_none/x")
	if code != cli.ExitNotFound && code != 0 {
		t.Fatalf("a missing object: code %d %s", code, errOut)
	}
	var stdout, stderr bytes.Buffer
	for _, args := range [][]string{
		{"kg"}, {"kg", "frob"}, {"kg", "get", "a", "b"}, {"object"}, {"object", "frob", "x"}, {"object", "get"}, {"object", "links", "a", "b"},
		{"query"}, {"query", "frob"}, {"query", "get"}, {"query", "get", "a", "--identifier", "s:v"}, {"query", "get", "--identifier", "nocolon"},
		{"query", "neighbors"}, {"query", "find", "--identifier", "x"}, {"query", "find", "--attr", "nodot"}, {"query", "find", "--attr", "a.range=1"},
		{"query", "find", "--bogus"}, {"kg", "list", "--bogus"},
	} {
		stderr.Reset()
		code := cli.Run(append([]string{"--endpoint", f.URL}, args...), fixedEnv(nil, nil), &stdout, &stderr, nil)
		if code != cli.ExitUsage {
			t.Errorf("%v: code %d, stderr %q", args, code, stderr.String())
		}
	}
	for _, word := range []string{"kg", "object", "query"} {
		if !cli.IsClientCommand(word) {
			t.Errorf("%s is a client command", word)
		}
	}
}

// [REQ-257]
func TestKGCommandsReportProblemsAndOddAnswers(t *testing.T) {
	f := newKGFake(t)
	fake := &fakeServer{Server: f.Server}
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"kg", "get", "nokg"}, cli.ExitNotFound},
		{[]string{"object", "get", "--kg", "nokg", "obj_1"}, cli.ExitNotFound},
		{[]string{"object", "links", "--kg", "nokg", "obj_1"}, cli.ExitNotFound},
		{[]string{"query", "find", "--kg", "nokg"}, cli.ExitNotFound},
		{[]string{"query", "get", "--kg", "nokg", "obj_1"}, cli.ExitNotFound},
		{[]string{"query", "neighbors", "--kg", "nokg", "obj_1"}, cli.ExitNotFound},
		{[]string{"query", "find", "--kg", "odd"}, cli.ExitTransport},
		{[]string{"query", "get", "--kg", "odd", "obj_1"}, cli.ExitTransport},
		{[]string{"query", "neighbors", "--kg", "odd", "obj_1"}, cli.ExitTransport},
		{[]string{"object", "get", "--kg", "odd", "obj_1"}, cli.ExitTransport},
	} {
		if code, _, errOut := run(t, fake, tc.args...); code != tc.code {
			t.Errorf("%v: code %d, want %d: %s", tc.args, code, tc.code, errOut)
		}
	}
	code, out, _ := run(t, fake, "query", "find", "--kg", "sum", "--summary")
	if code != 0 || strings.Contains(out, "OBJECT_ID") || !strings.Contains(out, "Rack") {
		t.Errorf("a summary has no items: code %d\n%s", code, out)
	}
	if code, out, _ = run(t, fake, "query", "neighbors", "--kg", "sum", "obj_1"); code != 0 || strings.Contains(out, "STEP") {
		t.Errorf("a summary of neighbors: code %d\n%s", code, out)
	}
	if code, out, _ = run(t, fake, "query", "get", "--kg", "sum", "obj_1"); code != 0 || !strings.Contains(out, "obj_1") {
		t.Errorf("a get without parts: code %d\n%s", code, out)
	}
	var stdout, stderr bytes.Buffer
	for _, args := range [][]string{{"query", "find"}, {"query", "get", "obj_1"}, {"query", "neighbors", "obj_1"}} {
		if code := cli.Run(append([]string{"--endpoint", "http://127.0.0.1:1"}, args...), fixedEnv(nil, nil), &stdout, &stderr, nil); code != cli.ExitTransport {
			t.Errorf("%v with nobody listening: code %d", args, code)
		}
	}
}
