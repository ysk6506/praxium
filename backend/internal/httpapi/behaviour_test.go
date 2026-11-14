package httpapi_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/contract"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

func ids(t *testing.T, r reply) []string {
	t.Helper()
	var out []string
	for _, item := range r.json(t)["data"].([]any) {
		out = append(out, item.(map[string]any)["id"].(string))
	}
	return out
}

func resultComplete(t *testing.T, r reply) map[string]any {
	t.Helper()
	return r.json(t)["meta"].(map[string]any)["result_complete"].(map[string]any)
}

// [REQ-148]
func TestCursorPagingWalksEveryItemOnceInOrder(t *testing.T) {
	h := newHarness(t)
	reg, _ := model.LoadStandard()
	want := len(reg.ObjectTypes())
	for _, query := range []string{"", "&sort=-name", "&filter[layer][in]=facility,compute"} {
		t.Run("query"+query, func(t *testing.T) {
			var all []string
			target := "/v1/object-types?limit=17" + query
			for pages := 0; ; pages++ {
				if pages > 100 {
					t.Fatal("the walk does not end")
				}
				r := h.get(target)
				if r.status != 200 {
					t.Fatalf("status %d: %s", r.status, r.body)
				}
				all = append(all, ids(t, r)...)
				next, _ := resultComplete(t, r)["next_cursor"].(string)
				if next == "" {
					break
				}
				target = "/v1/object-types?limit=17" + query + "&cursor=" + url.QueryEscape(next)
			}
			seen := map[string]bool{}
			for _, id := range all {
				if seen[id] {
					t.Fatalf("%s appears twice", id)
				}
				seen[id] = true
			}
			if query == "" && len(all) != want {
				t.Fatalf("walked %d items, the registry has %d", len(all), want)
			}
			total := resultComplete(t, h.get("/v1/object-types?limit=1"+query))["total"].(float64)
			if int(total) != len(all) {
				t.Fatalf("total %v, walked %d", total, len(all))
			}
		})
	}
}

// [REQ-148]
func TestCursorIsRefusedWhenItDoesNotBelong(t *testing.T) {
	h := newHarness(t)
	first := h.get("/v1/object-types?limit=2")
	cursor := url.QueryEscape(resultComplete(t, first)["next_cursor"].(string))
	tests := []struct {
		name   string
		target string
		status int
		code   string
	}{
		{"garbage", "/v1/object-types?cursor=zzz", 400, "invalid_input.cursor_invalid"},
		{"another filter", "/v1/object-types?limit=2&filter[layer][equals]=facility&cursor=" + cursor, 400, "invalid_input.cursor_invalid"},
		{"another model", "/v1/object-types?limit=2&cursor=" + staleCursor(t, h), 409, "conflict.generation_changed"},
		{"the same query", "/v1/object-types?limit=2&cursor=" + cursor, 200, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := h.get(tt.target)
			if r.status != tt.status || (tt.code != "" && r.code(t) != tt.code) {
				t.Fatalf("got %d %s, want %d %s", r.status, r.body, tt.status, tt.code)
			}
		})
	}
}

// [REQ-148]
func TestListQueryGrammarIsClosed(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		query string
		code  string
	}{
		{"limit=1001", "invalid_input.limit_invalid"},
		{"limit=abc", "invalid_input.limit_invalid"},
		{"offset=5", "invalid_input.param_unknown"},
		{"page=2", "invalid_input.param_unknown"},
		{"filter[colour][equals]=red", "invalid_input.filter_field_unknown"},
		{"filter[layer][regex]=x", "invalid_input.filter_op_unknown"},
		{"filter[name][in]=a", "invalid_input.filter_op_unknown"},
		{"filter[layer]=x", "invalid_input.param_unknown"},
		{"sort=colour", "invalid_input.sort_field_unknown"},
		{"fields=colour", "invalid_input.fields_unknown"},
		{"filter[type_no][range]=1..3&filter[type_no][range]=2..9", ""},
		{"filter[type_no][range]=..3", ""},
		{"sort=-type_no,name&fields=id,name", ""},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			r := h.get("/v1/object-types?" + tt.query)
			if tt.code == "" {
				if r.status != 200 {
					t.Fatalf("status %d: %s", r.status, r.body)
				}
				return
			}
			if r.code(t) != tt.code || r.status != 400 {
				t.Fatalf("got %d %s, want 400 %s", r.status, r.body, tt.code)
			}
		})
	}
}

// [REQ-229]
func TestFiltersSelectWhatTheModelHolds(t *testing.T) {
	h := newHarness(t)
	reg, _ := model.LoadStandard()
	count := func(f func(model.ObjectType) bool) int {
		n := 0
		for _, ot := range reg.ObjectTypes() {
			if f(ot) {
				n++
			}
		}
		return n
	}
	total := func(target string) int {
		r := h.get(target)
		if r.status != 200 {
			t.Fatalf("%s: %d %s", target, r.status, r.body)
		}
		return int(resultComplete(t, r)["total"].(float64))
	}
	site, _ := kernel.ParseID[model.ObjectTypeKind]("ot.site")
	tests := []struct {
		name   string
		target string
		want   int
	}{
		{"layer", "/v1/object-types?filter[layer][equals]=facility", count(func(o model.ObjectType) bool { return o.Layer == model.LayerFacility })},
		{"status in", "/v1/object-types?filter[status][in]=active,experimental", count(func(o model.ObjectType) bool {
			return o.Status == model.StatusActive || o.Status == model.StatusExperimental
		})},
		{"name prefix", "/v1/object-types?filter[name][prefix]=S", count(func(o model.ObjectType) bool { return strings.HasPrefix(o.Name, "S") })},
		{"type_no range", "/v1/object-types?filter[type_no][range]=1..10", 10},
		{"subtype_of is reflexive", "/v1/object-types?filter[subtype_of][equals]=ot.site", 1 + len(reg.Subtypes(site))},
		{"two filters are and", "/v1/object-types?filter[layer][equals]=facility&filter[namespace][equals]=org", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := total(tt.target); got != tt.want {
				t.Fatalf("total = %d, want %d", got, tt.want)
			}
		})
	}
	traits := reg.Traits()[0]
	if got, want := total("/v1/object-types?filter[trait][equals]="+traits.ID.String()), len(reg.TypesWithTrait(traits.ID)); got != want {
		t.Fatalf("trait filter: %d, want %d", got, want)
	}
	if got := total("/v1/link-types?filter[from_type][equals]=ot.site"); got == 0 {
		t.Fatal("no link type has a binding from a site")
	}
	r := h.get("/v1/object-types?limit=2&fields=id,name")
	for _, item := range r.json(t)["data"].([]any) {
		if len(item.(map[string]any)) != 2 {
			t.Fatalf("fields kept %v", item)
		}
	}
}

// [REQ-148]
func TestConditionalReadsAnswerNotModified(t *testing.T) {
	h := newHarness(t)
	for _, target := range []string{"/v1/object-types?limit=5", "/v1/object-types/ot.site", "/v1/roles", "/v1/contract"} {
		t.Run(target, func(t *testing.T) {
			first := h.get(target)
			etag := first.header.Get("ETag")
			if first.status != 200 || etag == "" || !strings.HasPrefix(etag, `"`) {
				t.Fatalf("status %d etag %q", first.status, etag)
			}
			for _, header := range []string{etag, "W/" + etag, `"other", ` + etag, "*"} {
				r := h.do(http.MethodGet, target, true, "If-None-Match", header)
				if r.status != 304 || len(r.body) != 0 || r.header.Get("ETag") != etag {
					t.Fatalf("If-None-Match %s: %d %q", header, r.status, r.body)
				}
			}
			if r := h.do(http.MethodGet, target, true, "If-None-Match", `"other"`); r.status != 200 {
				t.Fatalf("another tag answered %d", r.status)
			}
		})
	}
	a := h.get("/v1/object-types?limit=5").header.Get("ETag")
	b := h.get("/v1/object-types?limit=6").header.Get("ETag")
	if a == b {
		t.Fatal("two different queries share a tag")
	}
	other := newHarnessWith(t, differentRegistry(t))
	if c := other.get("/v1/object-types?limit=5").header.Get("ETag"); c == a {
		t.Fatal("a different model keeps the tag")
	}
}

// [REQ-241]
func TestOldNamesAnswerAsAliases(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	defs := reg.Definitions()
	for i := range defs.ObjectTypes {
		if defs.ObjectTypes[i].ID.String() == "ot.site" {
			defs.ObjectTypes[i].Aliases = []string{"OldSiteName"}
		}
	}
	for i := range defs.LinkTypes {
		if defs.LinkTypes[i].ID.String() == "lt.located_in" {
			defs.LinkTypes[i].Aliases = []string{"OLD_LOCATED_IN"}
		}
	}
	aliased, err := model.NewRegistry(defs)
	if err != nil {
		t.Fatal(err)
	}
	h := newHarnessWith(t, aliased)
	for _, tt := range []struct{ old, canonical string }{
		{"/v1/object-types/OldSiteName", "/v1/object-types/ot.site"},
		{"/v1/object-types/Site", "/v1/object-types/ot.site"},
		{"/v1/link-types/OLD_LOCATED_IN", "/v1/link-types/lt.located_in"},
	} {
		r := h.get(tt.old)
		if r.status != 200 || r.header.Get("Content-Location") != tt.canonical {
			t.Fatalf("%s: %d, Content-Location %q", tt.old, r.status, r.header.Get("Content-Location"))
		}
		if id := r.json(t)["data"].(map[string]any)["id"]; !strings.HasSuffix(tt.canonical, id.(string)) {
			t.Fatalf("%s answered %v", tt.old, id)
		}
	}
	if r := h.get("/v1/object-types/ot.site"); r.header.Get("Content-Location") != "" {
		t.Fatal("the canonical address needs no Content-Location")
	}
	if r := h.get("/v1/object-types/OldSiteName?include=ancestors"); r.status != 200 {
		t.Fatalf("an alias with include: %d", r.status)
	}
}

// [REQ-229]
func TestAllowedActionsFollowTheRoleTable(t *testing.T) {
	tests := []struct {
		role    access.Role
		allowed bool
		reason  any
	}{
		{access.RoleViewer, false, "forbidden.role"},
		{access.RoleEditor, true, nil},
		{access.RoleAdmin, false, "forbidden.role"},
	}
	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			h := newHarness(t, tt.role, access.RoleViewer)
			r := h.get("/v1/object-types/ot.site")
			actions := r.json(t)["meta"].(map[string]any)["allowed_actions"].([]any)
			got := actions[0].(map[string]any)
			if got["action"] != "edit" || got["allowed"] != tt.allowed || got["reason_code"] != tt.reason {
				t.Fatalf("allowed_actions = %v", actions)
			}
		})
	}
	known := map[string]bool{}
	for _, rc := range contract.ReasonCodes() {
		known[rc.Code] = true
	}
	if !known["forbidden.role"] {
		t.Fatal("forbidden.role is not in the reason catalogue")
	}
}

// [REQ-229]
func TestCatalogsServeTheClosedLists(t *testing.T) {
	h := newHarness(t)
	count := func(target string) int {
		r := h.get(target)
		if r.status != 200 {
			t.Fatalf("%s: %d %s", target, r.status, r.body)
		}
		return int(resultComplete(t, r)["total"].(float64))
	}
	tests := map[string]int{
		"/v1/catalog/link-meanings": len(model.Meanings()), "/v1/catalog/channels": len(model.Channels()),
		"/v1/catalog/error-codes": len(contract.ProblemCodes()), "/v1/catalog/reason-codes": len(contract.ReasonCodes()),
		"/v1/roles": len(access.Roles()), "/v1/layers": len(model.Layers()), "/v1/catalog": 7,
	}
	for target, want := range tests {
		if got := count(target); got != want {
			t.Errorf("%s: %d items, want %d", target, got, want)
		}
	}
	if len(access.Roles()) != 8 {
		t.Fatalf("the role table has %d roles, want the eight of access-control.md", len(access.Roles()))
	}
	r := h.get("/v1/catalog/error-codes?filter[status][range]=500..599")
	for _, item := range r.json(t)["data"].([]any) {
		status := item.(map[string]any)["status"].(float64)
		if status < 500 {
			t.Fatalf("range filter let %v through", status)
		}
	}
}

// [REQ-148]
func TestObjectTypeDetailCarriesWhatWasAskedFor(t *testing.T) {
	h := newHarness(t)
	reg, _ := model.LoadStandard()
	id, _ := kernel.ParseID[model.ObjectTypeKind]("ot.site")
	plain := h.get("/v1/object-types/ot.site").json(t)["data"].(map[string]any)
	for _, extra := range []string{"effective_properties", "ancestors", "subtypes", "bindings", "effective_needs"} {
		if _, ok := plain[extra]; ok {
			t.Errorf("%s is there without include", extra)
		}
	}
	full := h.get("/v1/object-types/ot.site?include=effective_properties,subtypes,ancestors").json(t)["data"].(map[string]any)
	if got, want := len(full["effective_properties"].([]any)), len(reg.EffectiveProperties(id)); got != want {
		t.Errorf("effective_properties = %d, want %d", got, want)
	}
	if got, want := len(full["subtypes"].([]any)), len(reg.Subtypes(id)); got != want {
		t.Errorf("subtypes = %d, want %d", got, want)
	}
	r := h.get("/v1/object-types/ot.site?include=nothing")
	if r.status != 400 || r.code(t) != "invalid_input.include_unknown" {
		t.Fatalf("got %d %s", r.status, r.body)
	}
	if r := h.get("/v1/object-types/ot.site?limit=3"); r.status != 400 {
		t.Fatalf("a list parameter on a single resource: %d", r.status)
	}
	trait := h.get("/v1/traits/tr.common").json(t)["data"].(map[string]any)
	if len(trait["carriers"].([]any)) == 0 {
		t.Fatal("the common trait has no carrier")
	}
	if n := len(h.get("/v1/link-types/lt.located_in/bindings").json(t)["data"].([]any)); n != 22 && n == 0 {
		t.Fatalf("bindings = %d", n)
	}
}

// [REQ-148]
func TestMethodsAndPathsOutsideTheContractAreProblems(t *testing.T) {
	h := newHarness(t)
	r := h.do(http.MethodPost, "/v1/roles", true)
	if r.status != 405 || r.code(t) != "invalid_input.method_not_allowed" || r.header.Get("Allow") != "GET" {
		t.Fatalf("POST: %d %s Allow %q", r.status, r.body, r.header.Get("Allow"))
	}
	r = h.get("/v1/nothing-here")
	if r.status != 404 || r.code(t) != "not_found.resource" {
		t.Fatalf("unknown path: %d %s", r.status, r.body)
	}
	r = h.do(http.MethodGet, "/v1/layers", true, "Accept", "application/json;q=0.9, text/html")
	if r.status != 200 {
		t.Fatalf("an acceptable list was refused: %d", r.status)
	}
	if _, err := strconv.Atoi(r.header.Get("Content-Length")); err != nil {
		t.Fatalf("no Content-Length: %v", err)
	}
}
