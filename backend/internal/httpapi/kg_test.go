package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/httpapi"
)

// firstOf reads the id of the first item that a find returns.
func (w *kgWorld) firstOf(typeName string) string {
	w.t.Helper()
	r := w.get("/v1/kgs/main/objects?filter[object_type][equals]=" + typeName + "&limit=1")
	items, _ := r.json(w.t)["data"].(map[string]any)["items"].([]any)
	if r.status != 200 || len(items) != 1 {
		w.t.Fatalf("no %s: %d %s", typeName, r.status, r.body)
	}
	return items[0].(map[string]any)["object_id"].(string)
}

// [REQ-255]
func TestKGAnswersFitTheContract(t *testing.T) {
	w := newKGHarness(t)
	validator := contractValidator(t)
	server, rack := w.firstOf("PhysicalServer"), w.firstOf("Rack")
	cases := []struct {
		target, template string
		status           int
	}{
		{"/v1/kgs", "/v1/kgs", 200},
		{"/v1/kgs?filter[role][equals]=production", "/v1/kgs", 200},
		{"/v1/kgs/main", "/v1/kgs/{kg}", 200},
		{"/v1/kgs/nothing", "/v1/kgs/{kg}", 404},
		{"/v1/kgs/main/objects?filter[object_type][equals]=Rack&limit=2", "/v1/kgs/{kg}/objects", 200},
		{"/v1/kgs/main/objects?filter[object_type][equals]=Rack&shape=summary", "/v1/kgs/{kg}/objects", 200},
		{"/v1/kgs/main/objects?filter[hidden][equals]=true", "/v1/kgs/{kg}/objects", 400},
		{"/v1/kgs/main/objects/" + server + "?include=attributes,identifiers,link_counts,evidence", "/v1/kgs/{kg}/objects/{object_id}", 200},
		{"/v1/kgs/main/objects/" + server, "/v1/kgs/{kg}/objects/{object_id}", 200},
		{"/v1/kgs/main/objects/obj_00000000000000000000000000000000", "/v1/kgs/{kg}/objects/{object_id}", 404},
		{"/v1/kgs/main/objects/" + rack + "/links?max_steps=2", "/v1/kgs/{kg}/objects/{object_id}/links", 200},
		{"/v1/kgs/main/objects/" + rack + "/links?shape=summary&filter[direction][equals]=in", "/v1/kgs/{kg}/objects/{object_id}/links", 200},
	}
	for _, c := range cases {
		t.Run(c.target, func(t *testing.T) {
			r := w.get(c.target)
			if r.status != c.status {
				t.Fatalf("status = %d, want %d: %s", r.status, c.status, r.body)
			}
			if err := validator.ValidateResponse("GET", c.template, r.status, r.header.Get("Content-Type"), r.headers, r.body); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// [REQ-255]
func TestKGListAndOne(t *testing.T) {
	w := newKGHarness(t)
	list := w.get("/v1/kgs").json(t)
	items := list["data"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["alias"] != "main" {
		t.Fatalf("list = %v", list)
	}
	byID := items[0].(map[string]any)["kg_id"].(string)
	for _, ref := range []string{"main", "main-kg", byID} {
		r := w.get("/v1/kgs/" + ref)
		if r.status != 200 || r.json(t)["data"].(map[string]any)["kg_id"] != byID {
			t.Errorf("%s: %d %s", ref, r.status, r.body)
		}
	}
	if r := w.get("/v1/kgs?filter[role][equals]=candidate"); r.json(t)["meta"].(map[string]any)["result_complete"].(map[string]any)["total"] != float64(0) {
		t.Errorf("a candidate filter should find none: %s", r.body)
	}
	if r := w.get("/v1/kgs?filter[colour][equals]=red"); r.status != 400 {
		t.Errorf("unknown filter = %d", r.status)
	}
}

// [REQ-255]
func TestFindFiltersAndPages(t *testing.T) {
	w := newKGHarness(t)
	first := w.get("/v1/kgs/main/objects?filter[object_type][equals]=Rack&limit=3").json(t)
	rc := first["meta"].(map[string]any)["result_complete"].(map[string]any)
	if rc["total"] != float64(4) || rc["truncated"] != true || len(first["data"].(map[string]any)["items"].([]any)) != 3 {
		t.Fatalf("first page: %v", rc)
	}
	next := rc["next_cursor"].(string)
	second := w.get("/v1/kgs/main/objects?filter[object_type][equals]=Rack&limit=3&cursor=" + url.QueryEscape(next)).json(t)
	if n := len(second["data"].(map[string]any)["items"].([]any)); n != 1 {
		t.Errorf("second page holds %d items, want 1", n)
	}
	other := w.get("/v1/kgs/main/objects?filter[object_type][equals]=Site&limit=3&cursor=" + url.QueryEscape(next))
	if other.status != 400 || other.code(t) != "invalid_input.cursor_invalid" {
		t.Errorf("a cursor of another query: %d %s", other.status, other.body)
	}
	for target, want := range map[string]string{
		"/v1/kgs/main/objects?filter[object_type][in]=Rack":                        "invalid_input.filter_op_unknown",
		"/v1/kgs/main/objects?filter[scope_id][equals]=s1":                         "invalid_input.param_invalid",
		"/v1/kgs/main/objects?filter[identifier][equals]=nocolon":                  "invalid_input.filter_op_unknown",
		"/v1/kgs/main/objects?filter[object_type][equals]=Rack&limit=0":            "invalid_input.limit_invalid",
		"/v1/kgs/main/objects?filter[object_type][equals]=Rack&shape=wide":         "invalid_input.field",
		"/v1/kgs/main/objects?filter[object_type][equals]=Rack&include_subtypes=x": "invalid_input.param_invalid",
		"/v1/kgs/main/objects?filter[object_type][equals]=Rack&change_seq=abc":     "invalid_input.param_invalid",
		"/v1/kgs/main/objects?filter[object_type][equals]=Rack&wait_timeout=5":     "invalid_input.param_invalid",
		"/v1/kgs/main/objects?colour=red":                                          "invalid_input.param_unknown",
	} {
		r := w.get(target)
		if r.status < 400 || r.code(t) != want {
			t.Errorf("%s: %d %s, want %s", target, r.status, r.body, want)
		}
	}
	if r := w.get("/v1/kgs/main/objects?filter[object_type][equals]=Nothing"); r.status != 400 && r.status != 404 && r.status != 422 {
		t.Errorf("an unknown type answered %d", r.status)
	}
}

// [REQ-255]
func TestGetObjectAndItsLinks(t *testing.T) {
	w := newKGHarness(t)
	server, rack := w.firstOf("PhysicalServer"), w.firstOf("Rack")
	got := w.get("/v1/kgs/main/objects/" + server + "?include=attributes,link_counts").json(t)
	data := got["data"].(map[string]any)
	if data["object"].(map[string]any)["object_type"] != "PhysicalServer" || len(data["attributes"].([]any)) == 0 || data["link_counts"] == nil {
		t.Errorf("get = %v", data)
	}
	if _, ok := data["identifiers"]; ok {
		t.Error("identifiers were not asked for")
	}
	meta := got["meta"].(map[string]any)
	if meta["as_of"].(map[string]any)["kg"] == nil || meta["as_of"].(map[string]any)["volatile_as_of"] != "current" || meta["allowed_actions"] == nil {
		t.Errorf("meta = %v", meta)
	}
	if r := w.get("/v1/kgs/main/objects/" + server + "?include=nonsense"); r.status != 400 || r.code(t) != "invalid_input.include_unknown" {
		t.Errorf("bad include: %d %s", r.status, r.body)
	}
	if r := w.get("/v1/kgs/main/objects/obj_00000000000000000000000000000000"); r.status != 404 {
		t.Errorf("missing object = %d", r.status)
	}
	links := w.get("/v1/kgs/main/objects/" + rack + "/links?filter[direction][equals]=in&filter[link_type][equals]=MOUNTED_IN").json(t)
	if n := len(links["data"].(map[string]any)["items"].([]any)); n != 3 {
		t.Errorf("a rack has %d servers, want 3", n)
	}
	steps := w.get("/v1/kgs/main/objects/" + rack + "/links?max_steps=3&limit=1000").json(t)["data"].(map[string]any)["items"].([]any)
	if len(steps) <= 3 {
		t.Errorf("three steps should reach more than the servers: %d", len(steps))
	}
	if r := w.get("/v1/kgs/main/objects/" + rack + "/links?max_steps=9"); r.status < 400 {
		t.Errorf("too many steps = %d", r.status)
	}
	if r := w.get("/v1/kgs/main/objects/" + rack + "/links?max_steps=x"); r.status != 400 {
		t.Errorf("bad steps = %d", r.status)
	}
}

// [REQ-255]
func TestKGReadsKeepTimeAndConditions(t *testing.T) {
	w := newKGHarness(t)
	rack := w.firstOf("Rack")
	base := "/v1/kgs/main/objects/" + rack
	past := w.get(base + "?change_seq=1")
	if past.status != 200 || past.json(t)["meta"].(map[string]any)["as_of"].(map[string]any)["volatile_as_of"] != "omitted" {
		t.Errorf("a read at a change number omits volatile values: %d %s", past.status, past.body)
	}
	if r := w.get(base + "?change_seq=99"); r.status < 400 {
		t.Errorf("a change number ahead of the head = %d", r.status)
	}
	if r := w.get(base + "?min_seq=99&wait_timeout=PT0S"); r.status < 400 {
		t.Errorf("min_seq that never comes = %d", r.status)
	}
	if r := w.get(base + "?min_seq=1&wait_timeout=PT1S&max_staleness=P1D&view=as_designed"); r.status != 200 {
		t.Errorf("min_seq, view and max_staleness: %d %s", r.status, r.body)
	}
	if r := w.get(base + "?kg_version_id=kv_00000000000000000000000000000000"); r.status < 400 {
		t.Errorf("an unknown version = %d", r.status)
	}
	first := w.get(base)
	etag := first.header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	if r := w.do(http.MethodGet, base, true, "If-None-Match", etag); r.status != 304 {
		t.Errorf("a conditional read = %d, want 304", r.status)
	}
}

// [REQ-255]
func TestKGRoutesWantReadAndAStore(t *testing.T) {
	w := newKGHarness(t, access.RoleIngester)
	if r := w.get("/v1/kgs"); r.status != 403 {
		t.Errorf("an ingester reading a KG = %d, want 403", r.status)
	}
	if r := w.do(http.MethodGet, "/v1/kgs", false); r.status != 401 {
		t.Errorf("without a token = %d, want 401", r.status)
	}
	bare := newHarness(t, access.RoleViewer)
	for _, target := range []string{"/v1/kgs", "/v1/kgs/main", "/v1/kgs/main/objects", "/v1/kgs/main/objects/obj_00000000000000000000000000000000"} {
		r := bare.get(target)
		if r.status != 503 || r.code(t) != "server.unavailable" || !strings.Contains(string(r.body), "no_store") {
			t.Errorf("%s without a store: %d %s", target, r.status, r.body)
		}
	}
}

// post sends a JSON body with the token.
func (w *kgWorld) post(target, body string, headers ...string) reply {
	w.t.Helper()
	return w.send(sample{method: http.MethodPost, target: target, body: body}, true, headers...)
}

// [REQ-256]
func TestQuestionsAnswerLikeTheirShortForm(t *testing.T) {
	w := newKGWorldForPost(t)
	validator := contractValidator(t)
	rack := w.firstOf("Rack")
	pairs := []struct{ get, post, body, template string }{
		{"/v1/kgs/main/objects?filter[object_type][equals]=Rack&limit=3", "/v1/kgs/main/queries:find",
			`{"object_type":"Rack","limit":3,"shape":"items"}`, "/v1/kgs/{kg}/queries:find"},
		{"/v1/kgs/main/objects/" + rack + "?include=attributes,link_counts", "/v1/kgs/main/queries:get",
			`{"object_id":"` + rack + `","include":["attributes","link_counts"],"shape":"items"}`, "/v1/kgs/{kg}/queries:get"},
		{"/v1/kgs/main/objects/" + rack + "/links?max_steps=2&filter[direction][equals]=in", "/v1/kgs/main/queries:neighbors",
			`{"object_id":"` + rack + `","max_steps":2,"direction":"in","shape":"items"}`, "/v1/kgs/{kg}/queries:neighbors"},
	}
	for _, p := range pairs {
		t.Run(p.post, func(t *testing.T) {
			g, q := w.get(p.get), w.post(p.post, p.body)
			if g.status != 200 || q.status != 200 {
				t.Fatalf("statuses %d and %d: %s", g.status, q.status, q.body)
			}
			if err := validator.ValidateResponse("POST", p.template, q.status, q.header.Get("Content-Type"), q.headers, q.body); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(g.json(t)["data"], q.json(t)["data"]) {
				t.Errorf("the GET and the POST form disagree:\n%s\n%s", g.body, q.body)
			}
			if q.header.Get("ETag") != "" {
				t.Error("an answer to a POST carries no ETag")
			}
		})
	}
	byID := w.post("/v1/kgs/main/queries:get", `{"identifier":{"scheme":"redfish_system_id","value":"nobody"}}`)
	if byID.status != 404 {
		t.Errorf("an identifier nobody holds = %d %s", byID.status, byID.body)
	}
	if r := w.post("/v1/kgs/main/queries:find", ``); r.status != 200 {
		t.Errorf("find with an empty body asks for everything: %d %s", r.status, r.body)
	}
	attr := w.post("/v1/kgs/main/queries:find", `{"object_type":"PhysicalServer","attributes":[{"name":"model","op":"in","values":["M1","M9"]}],"identifier":{"scheme":"redfish_system_id","value":"x"},"limit":1}`)
	if attr.status != 404 && attr.status != 200 && attr.status != 400 {
		t.Errorf("attributes and an identifier in the body: %d %s", attr.status, attr.body)
	}
}

func newKGWorldForPost(t *testing.T) *kgWorld { return newKGHarness(t) }

// [REQ-256]
func TestQuestionBodiesAreRefusedWithAProblem(t *testing.T) {
	w := newKGHarness(t)
	rack := w.firstOf("Rack")
	huge := `{"text":"` + strings.Repeat("x", 2<<20) + `"}`
	cases := []struct {
		name, target, body string
		status             int
		code               string
	}{
		{"unknown field", "find", `{"colour":"red"}`, 422, "invalid_input.field"},
		{"wrong type", "find", `{"limit":"many"}`, 422, "invalid_input.field"},
		{"not an object", "find", `[1]`, 422, "invalid_input.field"},
		{"two values", "find", `{} {}`, 422, "invalid_input.field"},
		{"broken JSON", "get", `{"object_id":`, 422, "invalid_input.field"},
		{"get without an object", "get", `{}`, 422, "invalid_input.field_required"},
		{"neighbors without an object", "neighbors", `{"max_steps":1}`, 422, "invalid_input.field_required"},
		{"bad duration", "neighbors", `{"object_id":"` + rack + `","wait_timeout":"5"}`, 422, "invalid_input.field"},
		{"bad staleness", "get", `{"object_id":"` + rack + `","max_staleness":"soon"}`, 422, "invalid_input.field"},
		{"negative change number", "get", `{"object_id":"` + rack + `","min_seq":-1}`, 422, "invalid_input.field"},
		{"cursor of nothing", "find", `{"cursor":"nonsense"}`, 400, "invalid_input.cursor_invalid"},
		{"hidden attribute filter", "find", `{"attributes":[{"name":"hidden","op":"equals","values":["x"]}]}`, 400, ""},
		{"too many steps", "neighbors", `{"object_id":"` + rack + `","max_steps":9}`, 422, ""},
		{"too large", "find", huge, 413, "invalid_input.body_too_large"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := w.post("/v1/kgs/main/queries:"+c.target, c.body)
			if r.status != c.status && !(c.code == "" && r.status >= 400) {
				t.Fatalf("status = %d, want %d: %s", r.status, c.status, r.body)
			}
			if c.code != "" && r.code(t) != c.code {
				t.Fatalf("code = %s, want %s", r.code(t), c.code)
			}
		})
	}
	text := w.send(sample{method: http.MethodPost, target: "/v1/kgs/main/queries:find", body: "x=1"}, true, "Content-Type", "text/plain")
	if text.status != 415 {
		t.Errorf("a body that is not JSON = %d", text.status)
	}
	if r := w.do(http.MethodGet, "/v1/kgs/main/queries:find", true); r.status != 405 {
		t.Errorf("GET on a question = %d, want 405", r.status)
	}
	bare := newHarness(t, access.RoleViewer)
	if r := bare.send(sample{method: http.MethodPost, target: "/v1/kgs/main/queries:find", body: `{}`}, true); r.status != 503 {
		t.Errorf("a question without a store = %d", r.status)
	}
	ingester := newKGHarness(t, access.RoleIngester)
	if r := ingester.post("/v1/kgs/main/queries:find", `{}`); r.status != 403 {
		t.Errorf("an ingester asking = %d", r.status)
	}
}

// brokenKGs is a store whose catalogue fails: what a database that is down looks like.
type brokenKGs struct {
	*graph.ScratchStore
	list, get, head bool
}

func (b brokenKGs) ListKGs(ctx context.Context) ([]graph.KG, error) {
	if b.list {
		return nil, errors.New("the store is down")
	}
	return b.ScratchStore.ListKGs(ctx)
}

func (b brokenKGs) GetKG(ctx context.Context, ref string) (graph.KG, error) {
	if b.get {
		return graph.KG{}, errors.New("the store is down")
	}
	return b.ScratchStore.GetKG(ctx, ref)
}

func (b brokenKGs) Head(ctx context.Context, kg graph.KGID) (graph.Head, error) {
	if b.head {
		return graph.Head{}, errors.New("the store is down")
	}
	return b.ScratchStore.Head(ctx, kg)
}

// [REQ-255]
func TestAStoreThatFailsIsAServerProblem(t *testing.T) {
	for name, broken := range map[string]brokenKGs{"list": {list: true}, "get": {get: true}, "head": {head: true}} {
		t.Run(name, func(t *testing.T) {
			w := buildKGWorld(t, kgOptions{wrap: func(s *graph.ScratchStore) httpapi.KGSource {
				broken.ScratchStore = s
				return broken
			}})
			for _, target := range []string{"/v1/kgs", "/v1/kgs/main"} {
				r := w.get(target)
				if r.status == 200 {
					continue
				}
				if r.status != 500 || r.code(t) != "server.internal" || strings.Contains(string(r.body), "the store is down") {
					t.Errorf("%s: %d %s", target, r.status, r.body)
				}
			}
		})
	}
}

// [REQ-255]
func TestRefusalsOfTheQueriesKeepTheirCatalogueCodes(t *testing.T) {
	w := buildKGWorld(t, kgOptions{restricted: true})
	rack := w.firstOf("Rack")
	base := "/v1/kgs/main/objects?filter[object_type][equals]=PhysicalServer"
	for target, want := range map[string]string{
		base + "&filter[model][equals]=M1":                                  "permission.hidden_attribute",
		base + "&filter[power_state][equals]=on":                            "invalid_input.filter_field_unknown",
		base + "&filter[nothing][equals]=on":                                "invalid_input.filter_field_unknown",
		base + "&filter[serial_number][regex]=on":                           "invalid_input.filter_op_unknown",
		base + "&filter[serial_number][prefix]=SN-0&limit=1":                "",
		base + "&filter[serial_number][in]=SN-000000,SN-9":                  "",
		base + "&filter[serial_number][range]=SN-0..SN-9":                   "",
		base + "&filter[serial_number][range]=nothing":                      "invalid_input.param_invalid",
		base + "&filter[identifier][equals]=redfish_system_id:x":            "",
		base + "&filter[text][equals]=Server":                               "",
		base + "&filter[trait][equals]=nothing":                             "",
		"/v1/kgs/main/objects/" + rack + "?include=attributes":              "",
		"/v1/kgs/main/objects/" + rack + "?include=nothing":                 "invalid_input.include_unknown",
		"/v1/kgs/main/objects/" + rack + "?view=wide":                       "invalid_input.field",
		"/v1/kgs/main/objects/" + rack + "?min_seq=1&wait_timeout=PT1S":     "",
		"/v1/kgs/main/objects/" + rack + "?min_seq=1&wait_timeout=PT1M":     "",
		"/v1/kgs/main/objects/" + rack + "?min_seq=1&wait_timeout=PT0.5S":   "",
		"/v1/kgs/main/objects/" + rack + "?min_seq=1&wait_timeout=P1DT1H":   "",
		"/v1/kgs/main/objects/" + rack + "?min_seq=1&wait_timeout=P":        "invalid_input.param_invalid",
		"/v1/kgs/main/objects/" + rack + "?min_seq=1&wait_timeout=PT5":      "invalid_input.param_invalid",
		"/v1/kgs/main/objects/" + rack + "?min_seq=1&wait_timeout=P5S":      "invalid_input.param_invalid",
		"/v1/kgs/main/objects/" + rack + "?min_seq=1&wait_timeout=PT1.1.1S": "invalid_input.param_invalid",
		"/v1/kgs/main/objects/" + rack + "?min_seq=x":                       "invalid_input.param_invalid",
		"/v1/kgs/main/objects/" + rack + "?change_seq=-1":                   "invalid_input.param_invalid",
	} {
		r := w.get(target)
		switch {
		case want == "" && r.status >= 500:
			t.Errorf("%s: %d %s", target, r.status, r.body)
		case want != "" && (r.status < 400 || r.code(t) != want):
			t.Errorf("%s: %d %s, want %s", target, r.status, r.body, want)
		}
	}
	scope := w.post("/v1/kgs/main/queries:find", `{"scope_id":"s1"}`)
	if scope.status != 400 || scope.code(t) != "invalid_input.param_invalid" {
		t.Errorf("a scope: %d %s", scope.status, scope.body)
	}
	gone := w.post("/v1/kgs/main/queries:find", `{"kg_version_id":"kv_00000000000000000000000000000000"}`)
	if gone.status != 404 {
		t.Errorf("an unknown version: %d %s", gone.status, gone.body)
	}
	old := w.get("/v1/kgs/main/objects/" + rack + "?change_seq=0")
	if old.status < 400 {
		t.Errorf("a scratch store cannot read an older state: %d", old.status)
	}
}
