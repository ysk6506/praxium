package ingest

import (
	"slices"
	"strings"
	"testing"
)

// [REQ-910]
// What the helpers say of a text of a request and of a dataset that has a parent: the names of its variables, whether it has any, and
// the paths of the values it reads.
func TestHxeTheHelpersSayWhatATextAsksForAndWhatAChildReads(t *testing.T) {
	if names, ok := ParentVars("a-{parent.x}-{parent.y}-{{z}}"); !ok || !slices.Equal(names, []string{"x", "y"}) {
		t.Errorf("names %v %v", names, ok)
	}
	if names, ok := ParentVars("{oops"); ok || names != nil {
		t.Errorf("a text that cannot be read: %v %v", names, ok)
	}
	if !HasParentVar("{parent.x}") || HasParentVar("plain") || HasParentVar("{oops") {
		t.Errorf("HasParentVar")
	}
	d := Dataset{Parent: &Parent{Values: map[string]string{"id": "id", "host": "^.name"}, Batch: &Batch{Match: "id"}}}
	if !d.IsChild() || (Dataset{}).IsChild() {
		t.Errorf("IsChild")
	}
	paths, err := d.ParentValuePaths(Dataset{})
	if err != nil || len(paths) != 2 {
		t.Errorf("paths %v %v", paths, err)
	}
	if _, err := d.MatchPath(); err != nil {
		t.Errorf("the match: %v", err)
	}
	d.Parent.Values["bad"] = "a["
	if _, err := d.ParentValuePaths(Dataset{}); err == nil {
		t.Errorf("a path that is no path")
	}
}

// [REQ-910]
// A parent block that has no dataset, no values, or a name that cannot be one, and a value of a body with other text around it, are
// definition errors of the place they are in.
func TestHxeAParentBlockThatSaysNothingAndABodyThatMixesAreDefinitionErrors(t *testing.T) {
	block := func(dataset, values string) string {
		return `"parent":{"dataset":"` + dataset + `","values":` + values + `},`
	}
	for _, c := range []struct{ name, parent, path string }{
		{"no dataset", block("", `{"id":"id"}`), "/s/{parent.id}"},
		{"no values", block("ds_ids", `{}`), "/s"},
		{"a name", block("ds_ids", `{"Id":"id"}`), "/s/{parent.Id}"},
		{"an empty path", block("ds_ids", `{"id":" "}`), "/s/{parent.id}"},
	} {
		if codes := hxeCodes(t, adpKid(t, adpHelper, c.parent, c.path)); !strings.Contains(strings.Join(codes, ";"), "source.parent_invalid") {
			t.Errorf("%s: %v", c.name, codes)
		}
	}
	doc := adpKid(t, adpHelper, `"post_reads":true,`+block("ds_ids", `{"id":"id"}`), "/s")
	doc = adpSwap(t, doc, `"method":"GET","path":"/s"`, `"method":"POST","path":"/s","body":{"k":"a-{parent.id}"}`)
	if codes := hxeCodes(t, doc); !strings.Contains(strings.Join(codes, ";"), "source.parent_template_place") {
		t.Errorf("a body that mixes: %v", codes)
	}
}
