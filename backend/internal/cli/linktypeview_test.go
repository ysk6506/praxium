package cli_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const mountedIn = `{"data":{"id":"lt.mounted_in","name":"MOUNTED_IN","bindings":[
 {"id":"b1","from":{"kind":"type","ref":"ot.physical_server"},"to":{"kind":"type","ref":"ot.rack"},"cardinality":"N:1","param_constraints":{}},
 {"id":"b2","from":{"kind":"trait","ref":"tr.mountable"},"to":{"kind":"type","ref":"ot.rack"},"cardinality":"N:1","param_constraints":{"mode":["front"]}}]},"meta":{}}`

// [REQ-388]
func TestTypesGetOfALinkTypeNamesTheTypesAtBothEnds(t *testing.T) {
	f := &fakeServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/link-types/MOUNTED_IN":
			fmt.Fprint(w, mountedIn)
		case "/v1/object-types":
			fmt.Fprint(w, `{"data":[{"id":"ot.rack","name":"Rack"},{"id":"ot.physical_server","name":"PhysicalServer"}],"meta":{}}`)
		case "/v1/traits":
			fmt.Fprint(w, `{"data":[{"id":"tr.mountable","name":"Mountable"}],"meta":{}}`)
		default:
			fmt.Fprint(w, `{"data":{},"meta":{}}`)
		}
	}))
	t.Cleanup(f.Close)
	code, out, errOut := run(t, f, "types", "get", "--kind", "link", "MOUNTED_IN")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errOut)
	}
	for _, want := range []string{"PhysicalServer -> Rack  cardinality N:1", "any object with the trait Mountable -> Rack", "only with mode=front"} {
		if !strings.Contains(out, want) {
			t.Errorf("the table lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "{...}") || strings.Contains(out, "(from=") {
		t.Errorf("no raw bindings:\n%s", out)
	}
	if _, jsonOut, _ := run(t, f, "--json", "types", "get", "--kind", "link", "MOUNTED_IN"); !strings.Contains(jsonOut, `"ref": "ot.physical_server"`) {
		t.Errorf("--json is the server's answer:\n%s", jsonOut)
	}
}
