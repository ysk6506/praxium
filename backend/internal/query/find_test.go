package query

import (
	"reflect"
	"sort"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

func boolp(b bool) *bool { return &b }

// [REQ-250]
func TestFindFiltersTableDriven(t *testing.T) {
	r := newRig(t, false)
	r.world()
	scheme, _ := r.reg.IdentifierSchemeByName("redfish_system_id")
	r.apply(graph.Batch{Identifiers: []graph.IdentifierWrite{{Object: r.fx.ServerID("SN-000102"), Scheme: scheme.ID, Value: "sys-7", Scope: "bmc"}}})
	tests := []struct {
		name string
		in   FindInput
		want []string
	}{
		{"by type", FindInput{ObjectType: "Site"}, []string{"Site S00", "Site S01"}},
		{"by alias-free name only", FindInput{ObjectType: "Rack", Attributes: []AttrFilter{{Name: "total_u", Op: OpEquals, Values: []string{"42"}}}}, []string{"Rack R00", "Rack R00", "Rack R01", "Rack R01"}},
		{"in", FindInput{ObjectType: "PhysicalServer", Attributes: []AttrFilter{{Name: "model", Op: OpIn, Values: []string{"M1", "M9"}}}, Text: "SN-000102"}, []string{"Server SN-000102"}},
		{"prefix", FindInput{ObjectType: "Site", Attributes: []AttrFilter{{Name: "address", Op: OpPrefix, Values: []string{"Fixture Road S01"}}}}, []string{"Site S01"}},
		{"range numeric", FindInput{ObjectType: "Rack", Attributes: []AttrFilter{{Name: "total_u", Op: OpRange, Values: []string{"40", "9"}}}}, []string{}},
		{"range open lower", FindInput{ObjectType: "Rack", Attributes: []AttrFilter{{Name: "total_u", Op: OpRange, Values: []string{"", "100"}}}}, []string{"Rack R00", "Rack R00", "Rack R01", "Rack R01"}},
		{"range text", FindInput{ObjectType: "Site", Attributes: []AttrFilter{{Name: "address", Op: OpRange, Values: []string{"Fixture Road S01", ""}}}}, []string{"Site S01"}},
		{"identifier", FindInput{Identifier: &IdentifierFilter{Scheme: "redfish_system_id", Value: "sys-7"}}, []string{"Server SN-000102"}},
		{"identifier unknown value", FindInput{Identifier: &IdentifierFilter{Scheme: "redfish_system_id", Value: "nope"}}, []string{}},
		{"text on name", FindInput{Text: "sn-0001"}, []string{"Server SN-000100", "Server SN-000101", "Server SN-000102"}},
		{"text on a string attribute", FindInput{ObjectType: "Site", Text: "road s00"}, []string{"Site S00"}},
		{"nothing matches", FindInput{ObjectType: "Site", Text: "zzz"}, []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.Common = r.common()
			got := names(r.mustFind(tc.in).Data.Items)
			sort.Strings(got)
			want := append([]string{}, tc.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

// [REQ-250]
func TestFindExpandsSubtypesAndCountsByType(t *testing.T) {
	r := newRig(t, false)
	var parent, child model.ObjectType
	for _, ty := range r.reg.ObjectTypes() {
		for _, sub := range r.reg.Subtypes(ty.ID) {
			if parent.Name == "" && keyFor(r.reg, ty, "x") != nil && keyFor(r.reg, sub, "x") != nil {
				parent, child = ty, sub
			}
		}
	}
	if parent.Name == "" {
		t.Fatal("the standard model has no type with a subtype that has a plain key")
	}
	put := func(ty model.ObjectType, name string) graph.ObjectUpsert {
		return graph.ObjectUpsert{Type: ty.ID, Name: name, Key: keyFor(r.reg, ty, name)}
	}
	r.apply(graph.Batch{Kind: graph.BatchSync, SourceID: "t", Objects: []graph.ObjectUpsert{put(parent, "the parent"), put(child, "the child")}})
	with := r.mustFind(FindInput{ObjectType: parent.Name})
	if got := names(with.Data.Items); !reflect.DeepEqual(got, sortedTwo(got, "the child", "the parent")) {
		t.Fatalf("with subtypes: %v", got)
	}
	if len(with.Data.CountsByType) != 2 || with.Meta.ResultComplete.Total != 2 {
		t.Fatalf("counts %+v total %d", with.Data.CountsByType, with.Meta.ResultComplete.Total)
	}
	without := r.mustFind(FindInput{ObjectType: parent.Name, IncludeSubtypes: boolp(false)})
	if got := names(without.Data.Items); !reflect.DeepEqual(got, []string{"the parent"}) {
		t.Fatalf("without subtypes: %v", got)
	}
	trait := ""
	for _, tr := range r.reg.Traits() {
		if len(r.reg.TypesWithTrait(tr.ID)) > 0 && r.reg.HasTrait(parent.ID, tr.ID) {
			trait = tr.Name
			break
		}
	}
	if trait != "" {
		got := r.mustFind(FindInput{Trait: trait, ObjectType: parent.Name})
		if got.Meta.ResultComplete.Total != 2 {
			t.Fatalf("trait %s found %d", trait, got.Meta.ResultComplete.Total)
		}
	}
}

func sortedTwo(got []string, a, b string) []string {
	out := append([]string{}, got...)
	sort.Strings(out)
	if len(out) == 2 && out[0] == a && out[1] == b {
		return out
	}
	return []string{a, b}
}

// [REQ-250]
func TestFindRefusesWhatIsNotServedYet(t *testing.T) {
	r := newRig(t, true)
	r.world()
	f := NewGrants(r.reg, false, Grant{Max: model.SensitivityOpen})
	tests := []struct {
		name string
		in   FindInput
		f    ReadFilter
		code kernel.Code
	}{
		{"scope id", FindInput{ScopeID: "sc-1"}, AllowAll(), CodeScopeUnsupported},
		{"volatile attribute", FindInput{ObjectType: "PhysicalServer", Attributes: []AttrFilter{{Name: "oper_state", Op: OpEquals, Values: []string{"up"}}}}, AllowAll(), CodeVolatileFilter},
		{"hidden attribute", FindInput{ObjectType: "PhysicalServer", Attributes: []AttrFilter{{Name: "model", Op: OpEquals, Values: []string{"M1"}}}}, f, CodeHiddenFilter},
		{"unknown field", FindInput{ObjectType: "Site", Attributes: []AttrFilter{{Name: "no_such", Op: OpEquals, Values: []string{"x"}}}}, AllowAll(), CodeFilterField},
		{"unknown operation", FindInput{ObjectType: "Site", Attributes: []AttrFilter{{Name: "address", Op: "like", Values: []string{"x"}}}}, AllowAll(), CodeFilterOp},
		{"equals with two values", FindInput{ObjectType: "Site", Attributes: []AttrFilter{{Name: "address", Op: OpEquals, Values: []string{"x", "y"}}}}, AllowAll(), CodeInput},
		{"in with none", FindInput{ObjectType: "Site", Attributes: []AttrFilter{{Name: "address", Op: OpIn}}}, AllowAll(), CodeInput},
		{"range with one bound", FindInput{ObjectType: "Site", Attributes: []AttrFilter{{Name: "address", Op: OpRange, Values: []string{"a"}}}}, AllowAll(), CodeInput},
		{"range with no bound", FindInput{ObjectType: "Site", Attributes: []AttrFilter{{Name: "address", Op: OpRange, Values: []string{"", ""}}}}, AllowAll(), CodeInput},
		{"unknown type", FindInput{ObjectType: "Nope"}, AllowAll(), CodeInput},
		{"unknown trait", FindInput{Trait: "Nope"}, AllowAll(), CodeInput},
		{"unknown scheme", FindInput{Identifier: &IdentifierFilter{Scheme: "nope", Value: "1"}}, AllowAll(), CodeInput},
		{"limit too high", FindInput{Common: Common{KG: "main", Limit: 5000}}, AllowAll(), CodeLimitInvalid},
		{"limit negative", FindInput{Common: Common{KG: "main", Limit: -1}}, AllowAll(), CodeLimitInvalid},
		{"view", FindInput{Common: Common{KG: "main", View: "as_planned"}}, AllowAll(), CodeInput},
		{"shape", FindInput{Common: Common{KG: "main", Shape: "tall"}}, AllowAll(), CodeInput},
		{"version and seq", FindInput{Common: Common{KG: "main", KGVersionID: "kv_00000000000000000000000000000000", ChangeSeq: new(int64)}}, AllowAll(), CodeInput},
		{"bad version", FindInput{Common: Common{KG: "main", KGVersionID: "x"}}, AllowAll(), graph.CodeKGNotFound},
		{"unknown version", FindInput{Common: Common{KG: "main", KGVersionID: "kv_00000000000000000000000000000000"}}, AllowAll(), graph.CodeKGNotFound},
		{"unknown kg", FindInput{Common: Common{KG: "kg_00000000000000000000000000000000"}}, AllowAll(), graph.CodeKGNotFound},
		{"bad cursor", FindInput{Common: Common{KG: "main", Cursor: "!!"}}, AllowAll(), CodeCursorInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.in.KG == "" {
				tc.in.Common = r.common()
			}
			_, err := r.find(tc.in, tc.f)
			if got := codeOf(err); got != tc.code {
				t.Fatalf("code %q (%v), want %q", got, err, tc.code)
			}
		})
	}
}

// [REQ-250]
func TestFindPagesRoundTripAndStayWhereTheyWereWhileTheKGMoves(t *testing.T) {
	r := newRig(t, false)
	r.world()
	all := r.mustFind(FindInput{Common: Common{KG: "main", Shape: ShapeItems, Limit: 1000}})
	if all.Meta.ResultComplete.Truncated || all.Meta.ResultComplete.Total != 18 {
		t.Fatalf("whole answer: %+v", all.Meta.ResultComplete)
	}
	ids := func(items []FindItem) []string {
		out := []string{}
		for _, it := range items {
			out = append(out, it.ObjectID)
		}
		return out
	}
	if !sort.StringsAreSorted(ids(all.Data.Items)) {
		t.Fatal("items are not in the order of the object id")
	}
	var got []string
	in := FindInput{Common: Common{KG: "main", Shape: ShapeItems, Limit: 5}}
	moved := false
	for pages := 0; ; pages++ {
		a, err := r.find(in, AllowAll())
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ids(a.Data.Items)...)
		if !moved { // the KG changes between the first and the second page
			moved = true
			r.apply(graph.Batch{Kind: graph.BatchSync, SourceID: "t", Objects: []graph.ObjectUpsert{r.fx.Site("S99", "later")}})
		}
		if a.Meta.ResultComplete.NextCursor == nil {
			if a.Meta.ResultComplete.Truncated {
				t.Fatal("truncated without a cursor")
			}
			break
		}
		if pages > 10 {
			t.Fatal("too many pages")
		}
		in.Cursor = *a.Meta.ResultComplete.NextCursor
	}
	if !reflect.DeepEqual(got, ids(all.Data.Items)) {
		t.Fatalf("pages do not add up to the whole answer:\n%v\n%v", got, ids(all.Data.Items))
	}
	// A cursor of another query is refused.
	in.Cursor = *func() *string {
		a, _ := r.find(FindInput{Common: Common{KG: "main", Shape: ShapeItems, Limit: 5}}, AllowAll())
		return a.Meta.ResultComplete.NextCursor
	}()
	in.ObjectType = "Site"
	if _, err := r.find(in, AllowAll()); codeOf(err) != CodeCursorInvalid {
		t.Fatalf("a cursor of another query: %v", err)
	}
}

// [REQ-250]
func TestFindSummaryHasCountsOnly(t *testing.T) {
	r := newRig(t, false)
	r.world()
	a, err := r.find(FindInput{Common: Common{KG: "main"}, ObjectType: "Rack"}, AllowAll())
	if err != nil {
		t.Fatal(err)
	}
	if a.Data.Items != nil || a.Meta.ResultComplete.Total != 4 || a.Meta.ResultComplete.Returned != 0 || len(a.Data.CountsByType) != 1 {
		t.Fatalf("summary: %+v %+v", a.Data, a.Meta.ResultComplete)
	}
	empty := r.mustFind(FindInput{ObjectType: "Site", Text: "zzz"})
	if empty.Data.Items == nil {
		t.Fatal("items shape with no match must give an empty list, not null")
	}
}

// [REQ-250]
func TestFindIsDeterministic(t *testing.T) {
	var last string
	for i := 0; i < 3; i++ {
		r := newRig(t, false)
		r.world()
		a := r.mustFind(FindInput{Text: "sn-"})
		got := js(t, a)
		if i > 0 && got != last {
			t.Fatalf("two runs differ:\n%s\n%s", got, last)
		}
		last = got
	}
}

// keyFor builds an identity key of a type from its first key made of properties only; nil if none.
func keyFor(reg *model.Registry, ty model.ObjectType, value string) []graph.KeyPart {
	for _, k := range reg.IdentityKeys(ty.ID) {
		var parts []graph.KeyPart
		for _, c := range k.Components {
			if c.Property.IsZero() {
				parts = nil
				break
			}
			parts = append(parts, graph.KeyPart{Kind: graph.PartProperty, Ref: c.Property.String(), Value: value})
		}
		if len(parts) > 0 {
			return parts
		}
	}
	return nil
}

// [REQ-250] [REQ-252]
func TestFindPagesAtASealedVersionKeepTheVersion(t *testing.T) {
	r := newRig(t, false)
	r.world()
	sealed, err := r.a.Seal(r.ctx, r.kg.ID, r.lease, graph.SealSync)
	if err != nil || !sealed.Created {
		t.Fatalf("seal: %v", err)
	}
	r.apply(graph.Batch{Kind: graph.BatchSync, SourceID: "t", Objects: []graph.ObjectUpsert{r.fx.Site("S77", "after the seal")}})
	in := FindInput{Common: Common{KG: "main", Shape: ShapeItems, Limit: 10, KGVersionID: sealed.Version.ID.String()}}
	first, err := r.find(in, AllowAll())
	if err != nil || first.Meta.ResultComplete.NextCursor == nil || first.Meta.VolatileAsOf != "omitted" {
		t.Fatalf("first page: %v %+v", err, first.Meta)
	}
	next, err := r.find(FindInput{Common: Common{KG: "main", Shape: ShapeItems, Limit: 10, Cursor: *first.Meta.ResultComplete.NextCursor, KGVersionID: in.KGVersionID}}, AllowAll())
	if err != nil || next.Meta.ResultComplete.Total != 18 || next.Meta.AsOf.SealedVersion == nil || len(next.Data.Items) != 8 {
		t.Fatalf("second page: %v total %d items %d", err, next.Meta.ResultComplete.Total, len(next.Data.Items))
	}
}
