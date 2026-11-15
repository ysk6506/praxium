package kgread_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/kgread"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/query"
)

// world is a service over a scratch store with one production KG: 2 sites with 2 racks each and 3 servers in a rack.
type world struct {
	svc  *kgread.Service
	fx   *graph.Fixture
	logs *bytes.Buffer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	fx, err := graph.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	clock := kernel.NewManualClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC))
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
	store := graph.NewScratchStore()
	kg, err := graph.NewTestKG(ctx, store, fx.Reg, ids, clock, graph.KGSpec{Name: "main-kg", Role: graph.RoleProduction})
	if err != nil {
		t.Fatal(err)
	}
	applier, err := graph.NewApplier(store, fx.Reg, graph.NewTestKeyProvider("h"), clock, ids, graph.Options{Semantic: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := applier.Acquire(ctx, kg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applier.Apply(ctx, kg.ID, lease, fx.World(2, 2, 3)); err != nil {
		t.Fatal(err)
	}
	engine, err := query.New(store, fx.Reg, clock)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	svc := &kgread.Service{Registry: fx.Reg, Queries: engine, KGs: store, Log: slog.New(slog.NewJSONHandler(logs, nil))}
	return &world{svc: svc, fx: fx, logs: logs}
}

func (w *world) req(kg string) kgread.Request {
	return kgread.Request{Subject: access.Subject{ID: "ada", Roles: []access.Role{access.RoleViewer}}, Trace: "trace-7", KG: kg}
}

func (w *world) rackID() string { return w.fx.RackID(w.fx.SiteID("S00"), "R00").String() }

func body(s string) *strings.Reader { return strings.NewReader(s) }

func TestAskFind(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	ans, p := w.svc.AskFind(ctx, w.req("main"), body(`{"object_type":"Rack","limit":2,"shape":"items"}`))
	if p != nil {
		t.Fatalf("problem %+v", p)
	}
	if len(ans.Data.Items) != 2 {
		t.Fatalf("items: %d", len(ans.Data.Items))
	}
	asOf, _ := ans.Meta["as_of"].(map[string]any)
	if asOf["kg"] == nil || asOf["kg_id"] != nil || asOf["volatile_as_of"] == nil {
		t.Fatalf("the meta is not in the shape of the HTTP answer: %v", ans.Meta)
	}
	if hidden, _ := ans.Meta["hidden"].(map[string]any); hidden["has_hidden"] != false {
		t.Fatalf("hidden: %v", ans.Meta["hidden"])
	}
	_, p = w.svc.AskFind(ctx, w.req("main"), body(`{"object_type":"Rack","attributes":[{"name":"no_such_attribute","op":"equals","values":["x"]}]}`))
	if p == nil || p.Code != "invalid_input.filter_field_unknown" {
		t.Fatalf("an unknown attribute: %+v", p)
	}
	if _, p = w.svc.AskFind(ctx, w.req("main"), body(`{"object_type":"NoSuchType"}`)); p == nil {
		t.Fatal("an unknown type must be refused")
	}
}

func TestAskGetAndNotFound(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	ans, p := w.svc.AskGet(ctx, w.req("main-kg"), body(`{"object_id":"`+w.rackID()+`"}`))
	if p != nil || ans.Data.Object.ObjectID != w.rackID() {
		t.Fatalf("get: %+v %+v", ans.Data.Object, p)
	}
	gone := "obj_" + strings.Repeat("0", 26)
	_, p = w.svc.AskGet(ctx, w.req("main"), body(`{"object_id":"`+gone+`"}`))
	if p == nil || p.Code != "not_found.resource" || p.Params["resource"] != "object" || p.Params["id"] != gone {
		t.Fatalf("a missing object: %+v", p)
	}
	_, p = w.svc.AskGet(ctx, w.req("no-such-kg"), body(`{"object_id":"`+w.rackID()+`"}`))
	if p == nil || p.Code != "not_found.resource" || p.Params["resource"] != "kg" || p.Params["id"] != "no-such-kg" {
		t.Fatalf("a missing KG: %+v", p)
	}
	_, p = w.svc.AskGet(ctx, w.req("main"), body(`{"identifier":{"scheme":"asset_tag","value":"none"}}`))
	if p == nil {
		t.Fatal("an unknown identifier must be refused")
	}
	if _, p = w.svc.AskGet(ctx, w.req("main"), body(`{}`)); p == nil || p.Code != "invalid_input.field_required" ||
		len(p.Fields) != 1 || p.Fields[0].Pointer != "/object_id" {
		t.Fatalf("a question without an object: %+v", p)
	}
}

func TestGetNamesTheObjectOfThePath(t *testing.T) {
	w := newWorld(t)
	req := w.req("main")
	req.Object = "from-the-path"
	_, p := w.svc.Get(context.Background(), req, query.GetInput{Common: query.Common{KG: "main"}, ObjectID: "obj_" + strings.Repeat("0", 26)})
	if p == nil || p.Params["id"] != "from-the-path" {
		t.Fatalf("the object of the path is what the answer names: %+v", p)
	}
}

func TestAskNeighborsAndImpact(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	ans, p := w.svc.AskNeighbors(ctx, w.req("main"), body(`{"object_id":"`+w.rackID()+`","max_steps":1,"shape":"items"}`))
	if p != nil || len(ans.Data.Items) == 0 {
		t.Fatalf("neighbors: %+v %+v", ans.Data, p)
	}
	if _, p = w.svc.AskNeighbors(ctx, w.req("main"), body(`{}`)); p == nil || p.Code != "invalid_input.field_required" {
		t.Fatalf("neighbors without an object: %+v", p)
	}
	imp, p := w.svc.AskImpact(ctx, w.req("main"), body(`{"dead":["`+w.rackID()+`"],"time_budget":"PT5S"}`))
	if p != nil || len(imp.Data.Seeds) == 0 {
		t.Fatalf("impact: %+v %+v", imp.Data, p)
	}
}

func TestInputProblems(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	cases := []struct {
		name, text, pointer, code string
	}{
		{"unknown field", `{"object_type":"Rack","surprise":1}`, "/surprise", "invalid_input.field"},
		{"wrong type", `{"limit":"many"}`, "/limit", "invalid_input.field_format"},
		{"two values", `{} {}`, "", "invalid_input.field_format"},
		{"negative min_seq", `{"min_seq":-1}`, "/min_seq", "invalid_input.field_format"},
		{"bad wait_timeout", `{"wait_timeout":"5s"}`, "/wait_timeout", "invalid_input.field_format"},
		{"bad max_staleness", `{"max_staleness":"PT5"}`, "/max_staleness", "invalid_input.field_format"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, p := w.svc.AskFind(ctx, w.req("main"), body(tc.text))
			if p == nil || p.Code != "invalid_input.field" || len(p.Fields) != 1 || p.Fields[0].Pointer != tc.pointer || p.Fields[0].Code != tc.code {
				t.Fatalf("problem %+v", p)
			}
		})
	}
	if _, p := w.svc.AskFind(ctx, w.req("main"), body(`[1]`)); p == nil || p.Code != "invalid_input.field" || len(p.Fields) != 1 ||
		p.Fields[0].Code != "invalid_input.field_format" || p.Fields[0].Params["expected"] == nil {
		t.Fatalf("a body that is not an object: %+v", p)
	}
	if _, p := w.svc.AskImpact(ctx, w.req("main"), body(`{"time_budget":"soon"}`)); p == nil || p.Fields[0].Pointer != "/time_budget" {
		t.Fatalf("a time budget that is no duration: %+v", p)
	}
	if _, p := w.svc.AskFind(ctx, w.req("main"), body(``)); p != nil {
		t.Fatalf("an empty body is the empty object: %+v", p)
	}
}

type tooBigReader struct{}

func (tooBigReader) Read([]byte) (int, error) { return 0, &kgread.BodyTooLargeError{Limit: 1024} }

func TestBodyOverTheLimit(t *testing.T) {
	w := newWorld(t)
	_, p := w.svc.AskFind(context.Background(), w.req("main"), tooBigReader{})
	if p == nil || p.Code != "invalid_input.body_too_large" || p.Params["max_bytes"] != int64(1024) {
		t.Fatalf("problem %+v", p)
	}
	if !strings.Contains((&kgread.BodyTooLargeError{Limit: 5}).Error(), "5") {
		t.Fatal("the error must say the limit")
	}
}

func TestWithoutAStoreEveryReadIsUnavailable(t *testing.T) {
	svc := &kgread.Service{Log: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))}
	req := kgread.Request{KG: "main"}
	ctx := context.Background()
	for name, p := range map[string]*kgread.Problem{
		"ready":     svc.Ready(),
		"find":      second(svc.AskFind(ctx, req, body(`{}`))),
		"get":       second(svc.AskGet(ctx, req, body(`{}`))),
		"neighbors": second(svc.AskNeighbors(ctx, req, body(`{}`))),
		"impact":    second(svc.AskImpact(ctx, req, body(`{}`))),
	} {
		if p == nil || p.Code != "server.unavailable" || p.Params["reason"] != "no_store" {
			t.Fatalf("%s: %+v", name, p)
		}
	}
}

func second[T any](_ T, p *kgread.Problem) *kgread.Problem { return p }

func TestRefuseMapsErrorsToCodes(t *testing.T) {
	w := newWorld(t)
	req := w.req("main")
	cases := []struct {
		err    error
		code   string
		params string
	}{
		{kernel.Errorf(query.CodeCursorInvalid, "bad cursor"), "invalid_input.cursor_invalid", ""},
		{kernel.Errorf(query.CodeLimitInvalid, "bad limit"), "invalid_input.limit_invalid", "max"},
		{kernel.Errorf(query.CodeFilterOp, "bad op"), "invalid_input.filter_op_unknown", "detail"},
		{kernel.Errorf(query.CodeHiddenFilter, "hidden"), "permission.hidden_attribute", "detail"},
		{kernel.Errorf(query.CodeScopeUnsupported, "no scopes"), "invalid_input.param_invalid", "param"},
		{kernel.Errorf(graph.CodeGenerationChanged, "moved"), "conflict.generation_changed", ""},
		{kernel.Errorf(graph.CodeWaitTimeout, "late"), "stale.min_seq_not_reached", "detail"},
		{kernel.Errorf(graph.CodeNotReadable, "not yet"), "stale.min_seq_not_reached", "detail"},
		{kernel.Errorf(kernel.CodeInvalidValue, "bad value"), "invalid_input.field", "message"},
		{kernel.Errorf(kernel.CodeUnsupported, "not yet"), "server.unavailable", "detail"},
	}
	for _, tc := range cases {
		p := w.svc.Refuse(req, tc.err)
		if p.Code != tc.code {
			t.Fatalf("%v: %+v", tc.err, p)
		}
		if _, ok := p.Params[tc.params]; tc.params != "" && !ok {
			t.Fatalf("%v: params %v", tc.err, p.Params)
		}
	}
	p := w.svc.Refuse(req, context.DeadlineExceeded)
	if p.Code != "server.internal" || !strings.Contains(w.logs.String(), `"trace_id":"trace-7"`) {
		t.Fatalf("an error of no code: %+v, log %s", p, w.logs.String())
	}
}

func TestPlainMessage(t *testing.T) {
	plain := kernel.Errorf(kernel.CodeInvalidValue, "not a number")
	if got := kgread.PlainMessage(plain); got != "not a number" {
		t.Fatalf("got %q", got)
	}
	if got := kgread.PlainMessage(context.Canceled); got != context.Canceled.Error() {
		t.Fatalf("got %q", got)
	}
	wrapped := &kernel.Error{Code: kernel.CodeInvalidValue, Message: "outer", Cause: context.Canceled}
	if got := kgread.PlainMessage(wrapped); got != "outer: "+context.Canceled.Error() {
		t.Fatalf("got %q", got)
	}
}

func TestReadFilterIsTheDefaultGrant(t *testing.T) {
	w := newWorld(t)
	if w.svc.ReadFilter(access.Subject{ID: "ada"}) == nil {
		t.Fatal("a read filter is always given")
	}
}

func TestOptIntAndDurations(t *testing.T) {
	if n, err := kgread.OptInt(""); n != nil || err != nil {
		t.Fatalf("empty: %v %v", n, err)
	}
	if n, err := kgread.OptInt("7"); err != nil || *n != 7 {
		t.Fatalf("7: %v %v", n, err)
	}
	for _, bad := range []string{"-1", "x"} {
		if _, err := kgread.OptInt(bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
	good := map[string]time.Duration{"P1D": 24 * time.Hour, "PT1H30M": 90 * time.Minute, "PT0.5S": 500 * time.Millisecond, "P1DT1S": 24*time.Hour + time.Second}
	for text, want := range good {
		if got, err := kgread.ParseDuration(text); err != nil || got != want {
			t.Fatalf("%s: %v %v", text, got, err)
		}
	}
	for _, bad := range []string{"", "P", "1D", "PT5", "P1H", "PT1D", "PTS", "P1Y"} {
		if _, err := kgread.ParseDuration(bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
}

func TestCatalogViews(t *testing.T) {
	w := newWorld(t)
	reg := w.fx.Reg
	ot, ok := reg.ObjectTypeByName("Rack")
	if !ok {
		t.Fatal("no Rack type")
	}
	sum := kgread.ObjectTypeSummary(ot)
	if sum["name"] != "Rack" || sum["id"] != ot.ID.String() || sum["parent"] == nil && ot.Parent.String() != "" {
		t.Fatalf("summary: %v", sum)
	}
	if p := kgread.PropertiesOf(ot.Properties); p == nil {
		t.Fatal("properties are never nil")
	}
	if kgread.Strs(nil) == nil || kgread.Opt("") != nil || *kgread.Opt("x") != "x" {
		t.Fatal("strs and opt")
	}
	var all []string
	for _, t := range reg.ObjectTypes() {
		all = append(all, t.ID.String())
	}
	for _, lt := range reg.LinkTypes() {
		if kgread.LinkTypeSummary(lt)["binding_count"] != len(lt.Bindings) {
			t.Fatalf("link type %s", lt.Name)
		}
		for _, b := range lt.Bindings {
			if len(kgread.Covered(reg, all, b.From)) == 0 && b.From.Kind != model.EndpointTrait {
				t.Fatalf("an end of %s covers nothing", lt.Name)
			}
			_ = kgread.BindingOf(b)
		}
	}
	for _, tr := range reg.Traits() {
		if kgread.TraitView(tr)["id"] != tr.ID.String() {
			t.Fatalf("trait %s", tr.Name)
		}
	}
	for _, s := range reg.IdentifierSchemes() {
		if kgread.SchemeView(s)["name"] != s.Name {
			t.Fatalf("scheme %s", s.Name)
		}
	}
	for _, a := range reg.ActionTypes() {
		if kgread.ActionTypeView(a)["id"] != a.ID.String() {
			t.Fatalf("action %s", a.Name)
		}
	}
	for _, o := range reg.ObjectTypes() {
		_ = kgread.KeysOf(o.IdentityKeys)
		_ = kgread.NeedsOf(o.Needs)
	}
	if kgread.ToMap(make(chan int)) == nil || len(kgread.ToMap(make(chan int))) != 0 {
		t.Fatal("a view that cannot be encoded is the empty map")
	}
	if got := kgread.ToAny([]kgread.TypeRef{{ID: "a", Name: "b"}}); got == nil {
		t.Fatal("toany")
	}
}

func TestAnObjectIsExpectedWhereABodyIsNotOne(t *testing.T) {
	w := newWorld(t)
	for _, text := range []string{`[1]`, `{"identifier":3}`, `{"attributes":[5]}`} {
		_, p := w.svc.AskFind(context.Background(), w.req("main"), body(text))
		if p == nil || len(p.Fields) != 1 || p.Fields[0].Params["expected"] != "object" {
			t.Fatalf("%s: %+v", text, p)
		}
		raw := fmt.Sprintf("%+v", p)
		if strings.Contains(raw, "kgread.") || strings.Contains(raw, "httpapi.") {
			t.Fatalf("a Go type name reached the answer: %s", raw)
		}
	}
	_, p := w.svc.AskFind(context.Background(), w.req("main"), body(`{"limit":"many"}`))
	if p == nil || p.Fields[0].Params["expected"] != "int" {
		t.Fatalf("the other fields keep their value: %+v", p)
	}
}
