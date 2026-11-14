package cli_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const rackType = `{"data":{"id":"ot.rack","name":"Rack","effective_properties":[
 {"id":"pr.rack.asset_tag","name":"asset_tag","value_type":"vt.string","required":true},
 {"id":"pr.rack.state","name":"state","value_type":"vt.enum","enum_values":["on","off"]},
 {"id":"pr.rack.width","name":"width_mm","value_type":"vt.quantity","unit":"mm"}],
 "identity_keys":[
  {"strength":"strong","components":[{"kind":"property","ref":"pr.rack.asset_tag"}]},
  {"strength":"weak","components":[{"kind":"context","ref":"lt.located_in"},{"kind":"property","ref":"pr.rack.unknown"}]}]},"meta":{}}`

func typeViewServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/object-types/Rack":
			fmt.Fprint(w, rackType)
		case "/v1/object-types/Broken":
			fmt.Fprint(w, `{"data":"not an object","meta":{}}`)
		case "/v1/link-types/MOUNTED_IN":
			fmt.Fprint(w, `{"data":{"id":"lt.mounted_in","name":"MOUNTED_IN"},"meta":{}}`)
		default:
			fmt.Fprint(w, `{"data":{},"meta":{}}`)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// [REQ-328]
func TestTypesGetShowsTheEffectivePropertiesAndKeysByName(t *testing.T) {
	f := typeViewServer(t)
	code, out, errOut := run(t, f, "types", "get", "Rack")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errOut)
	}
	for _, want := range []string{"asset_tag (string, required)", "state (enum, values on,off)", "width_mm (quantity, unit mm)", "property asset_tag", "link located_in", "(not usable in file ingestion)", "property unknown"} {
		if !strings.Contains(out, want) {
			t.Errorf("the table lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "effective_properties") {
		t.Errorf("the effective properties are shown as the properties:\n%s", out)
	}
	if got := f.requests[len(f.requests)-1].URL.Query().Get("include"); got != "effective_properties" {
		t.Errorf("the table asks for the effective properties, not %q", got)
	}
	if _, jsonOut, _ := run(t, f, "--json", "types", "get", "Rack"); !strings.Contains(jsonOut, `"effective_properties"`) {
		t.Errorf("--json is the server's answer:\n%s", jsonOut)
	}
	if code, out, _ = run(t, f, "types", "get", "MOUNTED_IN", "--kind", "link"); code != 0 || !strings.Contains(out, "MOUNTED_IN") {
		t.Errorf("a link type stays a plain table: %d\n%s", code, out)
	}
}

// [REQ-328]
func TestTypesGetNeedsOneNameAndReadsAnObject(t *testing.T) {
	f := typeViewServer(t)
	if code, _, errOut := run(t, f, "types", "get"); code == 0 || !strings.Contains(errOut, "needs one id or name") {
		t.Errorf("no name: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, f, "types", "get", "A", "B"); code == 0 || !strings.Contains(errOut, "one id or name") {
		t.Errorf("two names: %d %q", code, errOut)
	}
	if code, _, _ := run(t, f, "types", "get", "Broken"); code == 0 {
		t.Error("an answer that is not an object is an error")
	}
}

// [REQ-328]
func TestHealthInTheTableFormGivesOneLinePerProbe(t *testing.T) {
	f := typeViewServer(t)
	code, out, _ := run(t, f, "health")
	if code != 0 || !strings.Contains(out, "live     ok (200)") || !strings.Contains(out, "started  ok (200)") {
		t.Errorf("code %d:\n%s", code, out)
	}
}
