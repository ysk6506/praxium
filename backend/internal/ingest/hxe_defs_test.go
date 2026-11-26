package ingest

import (
	"context"
	"iter"
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The tests of the definition of a dataset that asks with the values of another dataset (ADR-0111). Every name here begins with hxe so
// that it never meets a name of the older tests of this folder; the values are made up.

// adpHelper is a dataset that is only read for its values.
const adpHelper = `{"id":"ds_ids","collect":false,"request":{"path":"/ids"},"records_path":"items"},`

// adpKid is the http definition of the cases with the datasets put before ds_servers (helper) and the parent block and the path of ds_servers
// changed.
func adpKid(t *testing.T, helper, parent, path string) string {
	t.Helper()
	return adpSwap(t, adpHTTP, `"datasets":[{"id":"ds_servers","request":{"method":"GET","path":"/servers"`,
		`"datasets":[`+helper+`{"id":"ds_servers",`+parent+`"request":{"method":"GET","path":"`+path+`"`)
}

func hxeCodes(t *testing.T, doc string) []string {
	t.Helper()
	_, err := ParseAdapterSource(strings.NewReader(doc))
	de, ok := DefinitionErrorsOf(err)
	if !ok {
		if err == nil {
			return nil
		}
		t.Fatalf("not a definition error: %v", err)
	}
	var out []string
	for _, e := range de.List {
		out = append(out, e.Code+" "+e.Pointer)
	}
	return out
}

// [REQ-910]
// The variable of a parent is read from the left: {{ and }} are the braces themselves, {parent.<name>} is a variable and any other brace
// is an error. A text with no variable is read as UnescapeBraces reads it.
func TestHxeTheGrammarOfTheVariablesOfAParent(t *testing.T) {
	cases := []struct {
		in   string
		want []ParentPiece
		ok   bool
	}{
		{"/vm/{parent.id}", []ParentPiece{{Text: "/vm/"}, {Var: "id"}}, true},
		{"{{parent.id}}", []ParentPiece{{Text: "{parent.id}"}}, true},
		{"{{{parent.id}}}", []ParentPiece{{Text: "{"}, {Var: "id"}, {Text: "}"}}, true},
		{"a{{b}}c", []ParentPiece{{Text: "a{b}c"}}, true},
		{"{parent.a}{parent.b}", []ParentPiece{{Var: "a"}, {Var: "b"}}, true},
		{"{parent.}", nil, false},
		{"{parent.A}", nil, false},
		{"{other.id}", nil, false},
		{"{id}", nil, false},
		{"}", nil, false},
		{"{parent.id", nil, false},
	}
	for _, c := range cases {
		got, err := ParseParentText(c.in)
		if (err == nil) != c.ok || (c.ok && !slices.Equal(got, c.want)) {
			t.Errorf("%q: %v %v, want %v (ok %v)", c.in, got, err, c.want, c.ok)
		}
	}
	for _, s := range []string{"plain", "a{{b}}c", "{{}}", "x}}y{{z"} {
		pieces, err := ParseParentText(s)
		if err != nil || len(pieces) != 1 || pieces[0].Text != UnescapeBraces(s) {
			t.Errorf("%q is read as %v %v, and UnescapeBraces gives %q", s, pieces, err, UnescapeBraces(s))
		}
	}
}

// [REQ-910]
// A variable of the parent is read where the definition says it may be (a path, a value of the query, a whole value of the body)
// and nowhere else; the defaults of the block are filled; a parent block that does not hold is refused.
func TestHxeThePlacesOfTheVariablesOfAParent(t *testing.T) {
	block := `"parent":{"dataset":"ds_ids","values":{"id":"id"}},`
	ok := adpSwap(t, adpKid(t, adpHelper, block, "/servers/{parent.id}/x"), `"query":{"status":"active"}`, `"query":{"status":"active","vm":"{parent.id}-1"}`)
	src, err := ParseAdapterSource(strings.NewReader(ok))
	if err != nil {
		t.Fatal(err)
	}
	p := src.Datasets[1].Parent
	if p.MaxRequests != DefaultChildRequests || p.MaxSeconds != DefaultChildSeconds || p.Batch != nil || !src.Datasets[0].Helper() {
		t.Errorf("the block: %+v", p)
	}
	if src.Datasets[0].Deletion != DeletionNone {
		t.Errorf("a helper makes no rows and closes none: %q", src.Datasets[0].Deletion)
	}
	for name, c := range map[string]struct{ doc, code string }{
		"no parent block":      {adpSwap(t, adpHTTP, `"path":"/servers"`, `"path":"/servers/{parent.id}"`), "source.text.braces_not_allowed /datasets/0/request/path"},
		"a query name":         {adpSwap(t, adpKid(t, adpHelper, block, "/servers/{parent.id}"), `"status":"active"`, `"{parent.id}":"active"`), "source.text.braces_not_allowed /datasets/1/request/query/{parent.id}"},
		"a text of a body":     {adpKid(t, adpHelper, block, "/servers/{parent.id}"), ""},
		"the variable of ^":    {adpSwap(t, adpKid(t, adpHelper, block, "/servers/{parent.id}"), `"status":"active"`, `"status":"{parent.id"`), "source.text.braces_not_allowed /datasets/1/request/query/status"},
		"on_not_found of 1":    {adpSwap(t, adpHTTP, `"id":"ds_servers",`, `"id":"ds_servers","on_not_found":"skip",`), "source.parent_invalid /datasets/0/on_not_found"},
		"on_not_found other":   {adpSwap(t, adpKid(t, adpHelper, block, "/servers/{parent.id}"), `"id":"ds_servers",`, `"id":"ds_servers","on_not_found":"retry",`), "source.parent_invalid /datasets/1/on_not_found"},
		"max_requests big":     {adpKid(t, adpHelper, `"parent":{"dataset":"ds_ids","values":{"id":"id"},"max_requests":60000},`, "/servers/{parent.id}"), "source.parent_invalid /datasets/1/parent/max_requests"},
		"batch with two":       {adpKid(t, adpHelper, `"parent":{"dataset":"ds_ids","values":{"id":"id","b":"b"},"batch":{"match":"id"}},`, "/servers/{parent.id}{parent.b}"), "source.batch_invalid /datasets/1/parent/batch/size"},
		"batch with no match":  {adpKid(t, adpHelper, `"parent":{"dataset":"ds_ids","values":{"id":"id"},"batch":{}},`, "/servers/{parent.id}"), "source.batch_invalid /datasets/1/parent/batch/match"},
		"batch separator":      {adpKid(t, adpHelper, `"parent":{"dataset":"ds_ids","values":{"id":"id"},"batch":{"separator":"ab","match":"id"}},`, "/servers/{parent.id}"), "source.batch_invalid /datasets/1/parent/batch/separator"},
		"a helper with parent": {adpKid(t, `{"id":"ds_ids","collect":false,"parent":{"dataset":"ds_servers","values":{"id":"id"}},"request":{"path":"/ids/{parent.id}"}},`, block, "/servers/{parent.id}"), "source.parent_invalid /datasets/0/parent/dataset"},
		"a path of a value":    {adpKid(t, adpHelper, `"parent":{"dataset":"ds_ids","values":{"id":"items[*]"}},`, "/servers/{parent.id}"), "source.path.unsupported /datasets/1/parent/values/id"},
		"mutual parents":       {adpKid(t, `{"id":"ds_ids","parent":{"dataset":"ds_servers","values":{"id":"serial"}},"request":{"path":"/ids/{parent.id}"},"natural_key":["a"],"columns":[{"name":"a"}]},`, block, "/servers/{parent.id}"), "source.parent_cycle /datasets/0/parent/dataset"},
	} {
		got := hxeCodes(t, c.doc)
		switch {
		case c.code == "" && len(got) != 0:
			t.Errorf("%s: %v", name, got)
		case c.code != "" && !slices.Contains(got, c.code):
			t.Errorf("%s: %v, want %s", name, got, c.code)
		}
	}
	if got := hxeCodes(t, adpSwap(t, adpHTTP, `"datasets":[{"id":"ds_servers",`, `"datasets":[{"id":"ds_servers","collect":false,`)); !slices.Contains(got, "source.collect_helper_mapped /datasets/0/columns") {
		t.Errorf("a dataset that is a helper and has columns: %v", got)
	}
	if got := hxeCodes(t, adpSwap(t, adpFile, `"datasets"`, `"datasets"`)); len(got) != 0 {
		t.Errorf("a file source: %v", got)
	}
	if _, err := ParseSource(strings.NewReader(adpSwap(t, adpFile, `"id":"d",`, `"id":"d","collect":false,`))); err == nil {
		t.Error("a file dataset with collect")
	}
}

// [REQ-918]
// A dataset that is a child, or a helper, is a change of the connection when it is made, and so are the block and on_not_found; a
// limit of the block that is lowered is a change of the mapping and one that is raised is not.
func TestHxeTheChangeOfAParentBlockIsAChangeOfTheConnectionUnlessItLowersALimit(t *testing.T) {
	block := func(extra string) string {
		return `"parent":{"dataset":"ds_ids","values":{"id":"id"}` + extra + `},`
	}
	parse := func(doc string) Source {
		s, err := ParseAdapterSource(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	base := parse(adpKid(t, adpHelper, block(`,"max_requests":1000`), "/servers/{parent.id}"))
	for name, c := range map[string]struct {
		doc  string
		want ChangeKind
	}{
		"lowered": {adpKid(t, adpHelper, block(`,"max_requests":500,"max_seconds":60`), "/servers/{parent.id}"), ChangeMapping},
		"same":    {adpKid(t, adpHelper, block(`,"max_requests":1000`), "/servers/{parent.id}"), ChangeMapping},
		"raised":  {adpKid(t, adpHelper, block(`,"max_requests":2000`), "/servers/{parent.id}"), ChangeConnection},
		"a path":  {adpKid(t, adpHelper, block(`,"max_requests":1000`), "/servers/{parent.id}/more"), ChangeMapping},
		"skip":    {adpSwap(t, adpKid(t, adpHelper, block(`,"max_requests":1000`), "/servers/{parent.id}"), `"id":"ds_servers",`, `"id":"ds_servers","on_not_found":"skip",`), ChangeConnection},
		"a value": {adpKid(t, adpHelper, `"parent":{"dataset":"ds_ids","values":{"id":"uuid"},"max_requests":1000},`, "/servers/{parent.id}"), ChangeConnection},
		"a batch": {adpKid(t, adpHelper, `"parent":{"dataset":"ds_ids","values":{"id":"id"},"max_requests":1000,"batch":{"match":"id"}},`, "/servers/{parent.id}"), ChangeConnection},
	} {
		if name == "a batch" { // a batch in the path is no definition; the change is what is asked
			c.doc = adpSwap(t, adpKid(t, adpHelper, `"parent":{"dataset":"ds_ids","values":{"id":"id"},"max_requests":1000,"batch":{"match":"id"}},`, "/servers"), `"query":{"status":"active"}`, `"query":{"status":"{parent.id}"}`)
		}
		got := ClassifyChange(base, parse(c.doc))
		if got.Kind != c.want {
			t.Errorf("%s: %s %v", name, got.Kind, got.Reasons)
		}
	}
	if got := ClassifyChange(parse(adpHTTP), base); got.Kind != ChangeConnection {
		t.Errorf("a source that gets a child: %s %v", got.Kind, got.Reasons)
	}
	raised := parse(adpKid(t, adpHelper, block(`,"max_requests":2000`), "/servers/{parent.id}"))
	if got := ClassifyChange(base, raised); !slices.Contains(got.Reasons, "datasets.ds_servers.parent_limits") {
		t.Errorf("the reasons of a raise: %v", got.Reasons)
	}
}

// hxeRecorder is a reader that says what it was asked for and gives a dataset that is not complete.
type hxeRecorder struct{ asked *[]string }

func (r hxeRecorder) ReadPages(_ context.Context, _ string, ds Dataset, _ PageOptions) iter.Seq2[Page, error] {
	return func(yield func(Page, error) bool) {
		*r.asked = append(*r.asked, ds.ID)
		if ds.ID != "ds_racks" {
			yield(Page{}, kernel.Errorf(kernel.CodeNotFound, "not given"))
			return
		}
		yield(Page{Dataset: ds.ID, Last: true, End: Completeness{Reason: "a limit of the child was reached"}}, nil)
	}
}

func (r hxeRecorder) Check(ctx context.Context, source string, ds Dataset) CheckResult {
	return CheckFirstPage(ctx, r, source, ds)
}

// [REQ-919]
// The receiving does not read a dataset that is only read for its values, and a child that is not complete is a dataset left out
// whole: nothing of it is used and nothing of it closed.
func TestHxeTheReceivingSkipsAHelperAndAChildThatIsNotCompleteIsLeftOut(t *testing.T) {
	plan := sxScalePlan(t, sourceJSON, mappingJSON)
	off := false
	plan.Source.Datasets = append(plan.Source.Datasets, Dataset{ID: "ds_helper", Collect: &off})
	var asked []string
	rep, err := (Intake{Reader: hxeRecorder{asked: &asked}, Sink: &sxMemory{}, Plan: plan}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(asked, "ds_helper") || len(rep.Datasets) != 1 {
		t.Errorf("asked %v, reported %+v", asked, rep.Datasets)
	}
	if got := rep.Datasets[0]; got.Dataset != "ds_racks" || got.Coverage() != CoverageIncomplete {
		t.Errorf("the child: %+v %s", got, got.Coverage())
	}
}
