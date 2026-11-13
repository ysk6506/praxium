package httpapi_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/contract"
	"github.com/ysk6506/praxium/backend/internal/httpapi"
)

// [REQ-143]
func TestServedRoutesAreExactlyTheContractsOperations(t *testing.T) {
	doc, err := contract.ParseDocument(contract.OpenAPI())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, op := range doc.Operations() {
		want[op.Method+" "+op.Path] = strings.Join(op.Roles, ",")
	}
	got := map[string]string{}
	for _, r := range httpapi.Routes() {
		got[r.Method+" "+r.Path] = strings.Join(r.Roles, ",")
	}
	for key, roles := range want {
		if g, ok := got[key]; !ok {
			t.Errorf("the contract has %s and the server does not", key)
		} else if g != roles {
			t.Errorf("%s: the server needs roles %q, the contract says %q", key, g, roles)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("the server has %s and the contract does not", key)
		}
	}
	planned, err := contract.PlannedRoutes()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range planned {
		if _, ok := got[p.Method+" "+p.Path]; ok {
			t.Errorf("%s %s is served but still listed as planned", p.Method, p.Path)
		}
	}
}

// exchange is one request and the contract operation that must describe the answer.
type exchange struct {
	name     string
	target   string
	template string
	status   int
	token    bool
	headers  []string
}

func exchanges() []exchange {
	return []exchange{
		{"live", "/v1/health/live", "/v1/health/live", 200, false, nil},
		{"ready", "/v1/health/ready", "/v1/health/ready", 200, false, nil},
		{"started", "/v1/health/started", "/v1/health/started", 200, false, nil},
		{"contract", "/v1/contract", "/v1/contract", 200, true, nil},
		{"problem", "/v1/problems/not_found.resource", "/v1/problems/{code}", 200, false, nil},
		{"problem unknown", "/v1/problems/nothing.here", "/v1/problems/{code}", 404, false, nil},
		{"catalog", "/v1/catalog", "/v1/catalog", 200, true, nil},
		{"meanings", "/v1/catalog/link-meanings", "/v1/catalog/link-meanings", 200, true, nil},
		{"channels", "/v1/catalog/channels", "/v1/catalog/channels", 200, true, nil},
		{"error codes", "/v1/catalog/error-codes?filter[group][equals]=auth", "/v1/catalog/error-codes", 200, true, nil},
		{"reason codes", "/v1/catalog/reason-codes", "/v1/catalog/reason-codes", 200, true, nil},
		{"roles", "/v1/roles", "/v1/roles", 200, true, nil},
		{"value types", "/v1/value-types", "/v1/value-types", 200, true, nil},
		{"layers", "/v1/layers", "/v1/layers", 200, true, nil},
		{"object types", "/v1/object-types?limit=3&sort=-type_no", "/v1/object-types", 200, true, nil},
		{"object type", "/v1/object-types/ot.site?include=effective_properties,ancestors,subtypes,bindings,effective_needs", "/v1/object-types/{type_id}", 200, true, nil},
		{"link types", "/v1/link-types?limit=3", "/v1/link-types", 200, true, nil},
		{"link type", "/v1/link-types/lt.located_in", "/v1/link-types/{link_type_id}", 200, true, nil},
		{"bindings", "/v1/link-types/lt.located_in/bindings", "/v1/link-types/{link_type_id}/bindings", 200, true, nil},
		{"action types", "/v1/action-types?limit=3", "/v1/action-types", 200, true, nil},
		{"action type", "/v1/action-types/ac.acknowledge_alarm", "/v1/action-types/{action_type_id}", 200, true, nil},
		{"traits", "/v1/traits?limit=3", "/v1/traits", 200, true, nil},
		{"trait", "/v1/traits/tr.common", "/v1/traits/{trait_id}", 200, true, nil},
		{"schemes", "/v1/identifier-schemes?limit=3", "/v1/identifier-schemes", 200, true, nil},
		{"scheme", "/v1/identifier-schemes/is.array_system_id", "/v1/identifier-schemes/{scheme_id}", 200, true, nil},
		{"no token", "/v1/roles", "/v1/roles", 401, false, nil},
		{"bad token", "/v1/roles", "/v1/roles", 401, false, []string{"Authorization", "Bearer wrong-token-value-0000"}},
		{"missing type", "/v1/object-types/ot.nothing", "/v1/object-types/{type_id}", 404, true, nil},
		{"missing link type", "/v1/link-types/lt.nothing/bindings", "/v1/link-types/{link_type_id}/bindings", 404, true, nil},
		{"bad limit", "/v1/object-types?limit=0", "/v1/object-types", 400, true, nil},
		{"unknown parameter", "/v1/layers?colour=red", "/v1/layers", 400, true, nil},
		{"unknown filter field", "/v1/object-types?filter[colour][equals]=red", "/v1/object-types", 400, true, nil},
		{"unacceptable", "/v1/layers", "/v1/layers", 406, true, []string{"Accept", "text/html"}},
		{"stale cursor", "", "/v1/object-types", 409, true, nil},
	}
}

// [REQ-148]
func TestEveryAnswerFitsTheContract(t *testing.T) {
	h := newHarness(t, access.RoleViewer)
	validator := contractValidator(t)
	doc, _ := contract.ParseDocument(contract.OpenAPI())
	exercised := map[string]bool{}
	for _, ex := range exchanges() {
		t.Run(ex.name, func(t *testing.T) {
			target := ex.target
			if ex.name == "stale cursor" {
				target = "/v1/object-types?limit=1&cursor=" + staleCursor(t, h)
			}
			r := h.do(http.MethodGet, target, ex.token, ex.headers...)
			if r.status != ex.status {
				t.Fatalf("status = %d, want %d: %s", r.status, ex.status, r.body)
			}
			ctype := r.header.Get("Content-Type")
			if err := validator.ValidateResponse("GET", ex.template, r.status, ctype, r.headers, r.body); err != nil {
				t.Fatal(err)
			}
			if r.header.Get("X-Trace-Id") == "" {
				t.Fatal("no X-Trace-Id")
			}
			exercised["GET "+ex.template] = true
		})
	}
	for _, op := range doc.Operations() {
		if !exercised[op.Method+" "+op.Path] {
			t.Errorf("no test exchange for %s %s", op.Method, op.Path)
		}
	}
}

// staleCursor makes a cursor the server of another model would have issued: the
// same query, a different generation.
func staleCursor(t *testing.T, h *harness) string {
	t.Helper()
	other := newHarnessWith(t, differentRegistry(t), access.RoleViewer)
	r := other.get("/v1/object-types?limit=1")
	cursor, _ := r.json(t)["meta"].(map[string]any)["result_complete"].(map[string]any)["next_cursor"].(string)
	if cursor == "" {
		t.Fatal("no cursor from the other server")
	}
	return cursor
}

// [REQ-148]
func TestProblemsKeepTheirShapeAndCatalogue(t *testing.T) {
	h := newHarness(t, access.RoleViewer)
	known := map[string]bool{}
	for _, p := range contract.ProblemCodes() {
		known[p.Code] = true
	}
	for _, ex := range exchanges() {
		if ex.status < 400 || ex.name == "stale cursor" {
			continue
		}
		r := h.do(http.MethodGet, ex.target, ex.token, ex.headers...)
		body := r.json(t)
		code, _ := body["code"].(string)
		if !known[code] {
			t.Errorf("%s: code %q is outside the catalogue", ex.name, code)
		}
		if r.header.Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: media type %q", ex.name, r.header.Get("Content-Type"))
		}
		if body["trace_id"] != r.header.Get("X-Trace-Id") {
			t.Errorf("%s: trace_id %v differs from the header %q", ex.name, body["trace_id"], r.header.Get("X-Trace-Id"))
		}
		if body["type"] != "/v1/problems/"+code {
			t.Errorf("%s: type = %v", ex.name, body["type"])
		}
	}
	if r := h.do(http.MethodGet, "/v1/roles", false); r.header.Get("WWW-Authenticate") != "Bearer" {
		t.Errorf("a 401 must carry WWW-Authenticate: Bearer, has %q", r.header.Get("WWW-Authenticate"))
	}
}

// [REQ-148]
func TestEveryOperationAsksForTheRoleItDeclares(t *testing.T) {
	doc, _ := contract.ParseDocument(contract.OpenAPI())
	ingester := newHarness(t, access.RoleIngester)
	viewer := newHarness(t, access.RoleViewer)
	for _, op := range doc.Operations() {
		path := sampleURL(op.Path)
		anonymous := viewer.do(http.MethodGet, path, false)
		asIngester := ingester.do(http.MethodGet, path, true)
		asViewer := viewer.do(http.MethodGet, path, true)
		switch {
		case slices.Contains(op.Roles, "anonymous"):
			if anonymous.status == 401 || anonymous.status == 403 {
				t.Errorf("%s needs no login but answered %d", op.Path, anonymous.status)
			}
		case slices.Contains(op.Roles, "read"):
			if anonymous.status != 401 || asIngester.status != 403 || asViewer.status == 401 || asViewer.status == 403 {
				t.Errorf("%s: anonymous %d, ingester %d, viewer %d", op.Path, anonymous.status, asIngester.status, asViewer.status)
			}
		default:
			if anonymous.status != 401 || asIngester.status != 200 {
				t.Errorf("%s: anonymous %d, ingester %d", op.Path, anonymous.status, asIngester.status)
			}
		}
	}
}

// sampleURL fills the path parameters of a template with ids of the standard model.
func sampleURL(template string) string {
	r := strings.NewReplacer("{type_id}", "ot.site", "{link_type_id}", "lt.located_in", "{action_type_id}", "ac.acknowledge_alarm",
		"{trait_id}", "tr.common", "{scheme_id}", "is.array_system_id", "{code}", "auth.token_missing")
	return r.Replace(template)
}
