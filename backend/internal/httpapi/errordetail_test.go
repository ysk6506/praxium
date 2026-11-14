package httpapi_test

import (
	"net/http"
	"testing"
)

// problemParams reads params of the problem a reply holds.
func problemParams(t *testing.T, r reply) map[string]any {
	t.Helper()
	params, _ := r.json(t)["params"].(map[string]any)
	return params
}

// [REQ-290]
func TestANotFoundAnswerSaysWhatWasNotFound(t *testing.T) {
	w := newKGHarness(t)
	for target, want := range map[string][2]string{
		"/v1/kgs/nokg":                       {"kg", "nokg"},
		"/v1/kgs/main/objects/obj-0000-nope": {"object", "obj-0000-nope"},
		"/v1/object-types/NoSuchType":        {"object_type", "NoSuchType"},
		"/v1/no/such/route":                  {"route", "/v1/no/such/route"},
	} {
		r := w.get(target)
		params := problemParams(t, r)
		if r.status != http.StatusNotFound || r.code(t) != "not_found.resource" || params["resource"] != want[0] || params["id"] != want[1] {
			t.Errorf("%s: %d %s, want resource %s id %s", target, r.status, r.body, want[0], want[1])
		}
	}
}

// [REQ-290]
func TestARefusedValueNamesTheFieldAndSaysWhy(t *testing.T) {
	w := newKGHarness(t)
	r := w.get("/v1/kgs/main/objects?filter[object_type][equals]=NoSuchType")
	params := problemParams(t, r)
	if r.status != http.StatusUnprocessableEntity || r.code(t) != "invalid_input.field" || params["field"] != "object_type" ||
		params["message"] != `no object type "NoSuchType"` || params["detail"] == nil {
		t.Fatalf("%d %s", r.status, r.body)
	}
	if r = w.get("/v1/kgs/main/objects?filter[object_type][equals]=Rack&shape=wide"); problemParams(t, r)["message"] == nil {
		t.Errorf("a refusal with no field still says why: %s", r.body)
	}
}

// [REQ-295]
func TestTheAPIAnswersCarryNoSniffAndNoReferrerHeaders(t *testing.T) {
	w := newKGHarness(t)
	for _, target := range []string{"/v1/kgs", "/v1/kgs/nokg", "/v1/health/live"} {
		r := w.get(target)
		if r.headers["x-content-type-options"] != "nosniff" || r.headers["referrer-policy"] != "no-referrer" {
			t.Errorf("%s: %v", target, r.headers)
		}
	}
}
