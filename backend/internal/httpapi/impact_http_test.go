package httpapi_test

import "testing"

// A site failure through the body of POST /queries:impact: the contract describes the answer and the
// racks and servers of the site go down.
//
// [REQ-306]
func TestImpactOverHTTPAnswersASiteFailureAndAnUpstreamQuestion(t *testing.T) {
	w := newKGHarness(t)
	site, server := w.firstOf("Site"), w.firstOf("PhysicalServer")
	validator := contractValidator(t)
	down := w.post("/v1/kgs/main/queries:impact", `{"site_failure":["`+site+`"],"mode":"facts_only","shape":"items","limit":100}`)
	if down.status != 200 {
		t.Fatalf("status %d: %s", down.status, down.body)
	}
	if err := validator.ValidateResponse("POST", "/v1/kgs/{kg}/queries:impact", down.status, down.header.Get("Content-Type"), down.headers, down.body); err != nil {
		t.Fatal(err)
	}
	data := down.json(t)["data"].(map[string]any)
	section := data["facts_only"].(map[string]any)
	counts := section["counts_by_result"].(map[string]any)
	if data["direction"] != "downstream" || counts["down"] == nil || data["with_assumptions"] != nil || len(section["items"].([]any)) == 0 {
		t.Fatalf("answer: %s", down.body)
	}
	if down.header.Get("ETag") != "" {
		t.Error("an answer to a POST carries no ETag")
	}
	up := w.post("/v1/kgs/main/queries:impact", `{"direction":"upstream","target_object_id":"`+server+`","shape":"summary"}`)
	if up.status != 200 || up.json(t)["data"].(map[string]any)["upstream"] == nil {
		t.Fatalf("upstream: %d %s", up.status, up.body)
	}
	if err := validator.ValidateResponse("POST", "/v1/kgs/{kg}/queries:impact", up.status, up.header.Get("Content-Type"), up.headers, up.body); err != nil {
		t.Fatal(err)
	}
}

// What the query refuses becomes a problem with a code of the catalogue and the field it is about.
//
// [REQ-306]
func TestImpactOverHTTPRefusesWhatItCannotAnswerWithAProblem(t *testing.T) {
	w := newKGHarness(t)
	site := w.firstOf("Site")
	// the same id with its last digit changed names no object
	missing := site[:len(site)-1] + "0"
	if site[len(site)-1] == '0' {
		missing = site[:len(site)-1] + "1"
	}
	cases := []struct {
		name, body string
		status     int
		code       string
	}{
		{"no seed", `{}`, 422, "invalid_input.field"},
		{"unknown field", `{"dead":["` + site + `"],"colour":"red"}`, 422, "invalid_input.field"},
		{"bad mode", `{"dead":["` + site + `"],"mode":"maybe"}`, 422, "invalid_input.field"},
		{"negative hops", `{"dead":["` + site + `"],"max_hops":-1}`, 422, "invalid_input.field"},
		{"bad budget", `{"dead":["` + site + `"],"time_budget":"soon"}`, 422, "invalid_input.field"},
		{"upstream without target", `{"direction":"upstream"}`, 422, "invalid_input.field"},
		{"a seed that is not there", `{"dead":["` + missing + `"]}`, 404, "not_found.resource"},
		{"a scope", `{"dead_scope_id":"scope_1","dead":["` + site + `"]}`, 400, "invalid_input.param_invalid"},
		{"a link that is not there", `{"dead_links":["lnk_00000000000000000000000000000000"]}`, 422, "invalid_input.field"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := w.post("/v1/kgs/main/queries:impact", c.body)
			if r.status != c.status || r.code(t) != c.code {
				t.Fatalf("status %d code %s, want %d %s: %s", r.status, r.code(t), c.status, c.code, r.body)
			}
		})
	}
}
