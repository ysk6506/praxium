package query

import (
	"strconv"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// powered makes the power state of a server differ from the world's so that it is seen at a known time.
func (r *rig) powered(serial string) {
	r.t.Helper()
	r.apply(graph.Batch{Volatile: []graph.VolatileWrite{r.fx.Power(r.fx.ServerID(serial), "off", seenAt)}})
}

func (r *rig) get(in GetInput, f ReadFilter) (Answer[GetData], error) {
	if in.KG == "" {
		in.Common = r.common()
	}
	return r.eng.Get(r.ctx, in, f)
}

func attrByName(attrs []AttributeValue, name string) (AttributeValue, bool) {
	for _, a := range attrs {
		if a.Name == name {
			return a, true
		}
	}
	return AttributeValue{}, false
}

// [REQ-251]
func TestGetReturnsWhatTheIncludeListAsks(t *testing.T) {
	r := newRig(t, false)
	r.world()
	scheme, _ := r.reg.IdentifierSchemeByName("redfish_system_id")
	id := r.fx.ServerID("SN-000102")
	r.apply(graph.Batch{Identifiers: []graph.IdentifierWrite{{Object: id, Scheme: scheme.ID, Value: "sys-7", Scope: "bmc"}},
		Volatile: []graph.VolatileWrite{r.fx.Power(id, "off", seenAt)}})
	a, err := r.get(GetInput{ObjectID: id.String(), Include: []string{IncludeAttributes, IncludeIdentifiers, IncludeLinkCounts, IncludeEvidence, IncludeProvenance}}, AllowAll())
	if err != nil {
		t.Fatal(err)
	}
	d := a.Data
	if d.Object.ObjectType != "PhysicalServer" || d.Object.DisplayName != "Server SN-000102" || d.Valid.ToChangeSeq != nil {
		t.Fatalf("object %+v valid %+v", d.Object, d.Valid)
	}
	m, ok := attrByName(d.Attributes, "model")
	if !ok || m.Value != "M1" || m.Grade != "fact" || m.Basis != "configured" || m.SeenAt != seenAt || m.Stale {
		t.Fatalf("model attribute %+v", m)
	}
	p, ok := attrByName(d.Attributes, "power_state")
	if !ok || p.Value != "off" || !p.Volatile || a.Meta.VolatileAsOf != "current" {
		t.Fatalf("power attribute %+v, volatile_as_of %s", p, a.Meta.VolatileAsOf)
	}
	if len(d.Identifiers) != 1 || d.Identifiers[0].Scheme != "redfish_system_id" || d.Identifiers[0].Value != "sys-7" {
		t.Fatalf("identifiers %+v", d.Identifiers)
	}
	if d.LinkCounts == nil || d.LinkCounts.Out != 1 || d.LinkCounts.In != 0 || d.LinkCounts.Groups[0].LinkType != "MOUNTED_IN" {
		t.Fatalf("link counts %+v", d.LinkCounts)
	}
	if d.Evidence == nil || d.Evidence.WorstFreshness == nil || d.Evidence.WorstFreshness.OldestAgeSeconds != 7200 || d.Evidence.Counts.Fact != 2 {
		t.Fatalf("evidence %+v", d.Evidence)
	}
	if len(d.ProvenanceRefs) != 0 {
		t.Fatalf("provenance %v", d.ProvenanceRefs)
	}
	byIdent, err := r.get(GetInput{Identifier: &IdentifierFilter{Scheme: "redfish_system_id", Value: "sys-7"}}, AllowAll())
	if err != nil || byIdent.Data.Object.ObjectID != id.String() {
		t.Fatalf("by identifier: %v %+v", err, byIdent.Data.Object)
	}
	site, err := r.get(GetInput{ObjectID: r.fx.SiteID("S00").String(), Include: []string{IncludeProvenance, IncludeLinkCounts}}, AllowAll())
	if err != nil || len(site.Data.ProvenanceRefs) != 1 || site.Data.ProvenanceRefs[0] != "fixture/site/S00" || site.Data.LinkCounts.In != 2 {
		t.Fatalf("site: %v %+v", err, site.Data)
	}
}

// [REQ-251]
func TestGetRefusesBadInputs(t *testing.T) {
	r := newRig(t, false)
	r.world()
	id := r.fx.ServerID("SN-000102").String()
	scheme, _ := r.reg.IdentifierSchemeByName("redfish_system_id")
	for _, sn := range []string{"SN-000100", "SN-000101"} {
		r.apply(graph.Batch{Identifiers: []graph.IdentifierWrite{{Object: r.fx.ServerID(sn), Scheme: scheme.ID, Value: "dup", Scope: sn}}})
	}
	tests := []struct {
		name string
		in   GetInput
		code kernel.Code
	}{
		{"no input", GetInput{}, CodeInput},
		{"both inputs", GetInput{ObjectID: id, Identifier: &IdentifierFilter{Scheme: "redfish_system_id", Value: "x"}}, CodeInput},
		{"unknown object", GetInput{ObjectID: "obj_00000000000000000000000000000000"}, CodeObjectNotFound},
		{"not an id", GetInput{ObjectID: "banana"}, CodeObjectNotFound},
		{"unknown include", GetInput{ObjectID: id, Include: []string{"history"}}, CodeIncludeUnknown},
		{"unknown scheme", GetInput{Identifier: &IdentifierFilter{Scheme: "nope", Value: "x"}}, CodeInput},
		{"identifier held by nobody", GetInput{Identifier: &IdentifierFilter{Scheme: "redfish_system_id", Value: "nobody"}}, CodeObjectNotFound},
		{"identifier held twice", GetInput{Identifier: &IdentifierFilter{Scheme: "redfish_system_id", Value: "dup"}}, CodeInput},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := r.get(tc.in, AllowAll()); codeOf(err) != tc.code {
				t.Fatalf("code %q (%v), want %q", codeOf(err), err, tc.code)
			}
		})
	}
}

func (r *rig) headSeq() int64 {
	r.t.Helper()
	h, err := r.store.Head(r.ctx, r.kg.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	return h.ReadableThrough
}

// [REQ-251] [REQ-253]
func TestGetMasksSensitiveAttributesAndHidesObjects(t *testing.T) {
	r := newRig(t, true)
	r.world()
	r.powered("SN-000102")
	id := r.fx.ServerID("SN-000102").String()
	open := NewGrants(r.reg, false, Grant{Max: model.SensitivityOpen})
	a, err := r.get(GetInput{ObjectID: id}, open)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := attrByName(a.Data.Attributes, "model")
	if !m.Masked || m.Reason != "sensitivity_above_limit" || m.Value != nil {
		t.Fatalf("model should be masked: %+v", m)
	}
	if a.Meta.Evidence.Counts.Fact != 1 {
		t.Fatalf("a masked value counted as used: %+v", a.Meta.Evidence)
	}
	wide := NewGrants(r.reg, false, Grant{Max: model.SensitivityOpen}, Grant{Types: []Selector{{Kind: "type", Ref: "PhysicalServer"}}, Max: model.SensitivityRestricted})
	a, _ = r.get(GetInput{ObjectID: id}, wide)
	if m, _ := attrByName(a.Data.Attributes, "model"); m.Masked || m.Value != "M1" {
		t.Fatalf("the highest matching cap wins: %+v", m)
	}
	servers := NewGrants(r.reg, false, Grant{Types: []Selector{{Kind: "type", Ref: "Rack"}}, Max: model.SensitivityOpen})
	if _, err := r.get(GetInput{ObjectID: id}, servers); codeOf(err) != CodeObjectNotFound {
		t.Fatalf("a hidden object must look like a missing one: %v", err)
	}
	counts, err := r.get(GetInput{ObjectID: r.fx.RackID(r.fx.SiteID("S00"), "R00").String(), Include: []string{IncludeLinkCounts}}, NewGrants(r.reg, true, Grant{Types: []Selector{{Kind: "type", Ref: "Rack"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if counts.Data.LinkCounts.In != 0 || counts.Data.LinkCounts.Out != 0 || *counts.Meta.Hidden.ReachedCount != 4 {
		t.Fatalf("link counts must leave the hidden ends out: %+v hidden %+v", counts.Data.LinkCounts, counts.Meta.Hidden)
	}
}

// [REQ-251]
func TestGetHonoursViewAndMaxStaleness(t *testing.T) {
	r := newRig(t, false)
	r.world()
	r.powered("SN-000102")
	id := r.fx.ServerID("SN-000102")
	up := r.fx.Server("SN-000102", "")
	up.Attrs = map[int]graph.Attr{graph.AttrServerModel: {Value: "M3", Grade: model.GradeAssumed, Basis: model.BasisObserved, SourceTime: seenAt, SourceRef: "probe"}}
	r.apply(graph.Batch{Objects: []graph.ObjectUpsert{up}})
	observed, _ := r.get(GetInput{ObjectID: id.String()}, AllowAll())
	if m, ok := attrByName(observed.Data.Attributes, "model"); !ok || m.Value != "M3" {
		t.Fatalf("as_observed: %+v", m)
	}
	if observed.Meta.Evidence.ObservationAgeSeconds == nil || *observed.Meta.Evidence.ObservationAgeSeconds != 7200 {
		t.Fatalf("observation age %+v", observed.Meta.Evidence)
	}
	designed, _ := r.get(GetInput{Common: Common{KG: "main", View: ViewAsDesigned}, ObjectID: id.String()}, AllowAll())
	if _, ok := attrByName(designed.Data.Attributes, "model"); ok {
		t.Fatal("as_designed gave an observed value")
	}
	stale, _ := r.get(GetInput{Common: Common{KG: "main", MaxStaleness: time.Hour}, ObjectID: id.String()}, AllowAll())
	if m, _ := attrByName(stale.Data.Attributes, "model"); !m.Stale || m.Value != nil {
		t.Fatalf("max_staleness: %+v", m)
	}
	fresh, _ := r.get(GetInput{Common: Common{KG: "main", MaxStaleness: 3 * time.Hour}, ObjectID: id.String()}, AllowAll())
	if m, _ := attrByName(fresh.Data.Attributes, "model"); m.Stale || m.Value != "M3" {
		t.Fatalf("within max_staleness: %+v", m)
	}
}

// [REQ-252]
func TestGetEnvelopeGolden(t *testing.T) {
	r := newRig(t, false)
	r.world()
	r.powered("SN-000102")
	id := r.fx.ServerID("SN-000102")
	a, err := r.get(GetInput{ObjectID: id.String(), Include: []string{IncludeEvidence}}, AllowAll())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"as_of":{"kg_id":"` + r.kg.ID.String() + `","generation":1,"change_seq":` + strconv.FormatInt(r.headSeq(), 10) + `,"sealed_version":null},` +
		`"volatile_as_of":"current","served_by":{"role":"api_sql"},` +
		`"freshness":{"worst_source_id":"src-inventory","oldest_age_seconds":7200,"state":"no_expected_period"},` +
		`"evidence":{"min_grade":"fact","counts":{"fact":2,"assumed":0,"unknown":0},"kinds":["configured","observed"],"observation_age_seconds":7200},` +
		`"result_complete":{"total":1,"returned":1,"truncated":false,"reason":null,"next_cursor":null},` +
		`"hidden":{"hidden_passed_count":0,"hidden_reached_count":0,"hidden_passed":false,"hidden_reached":false},` +
		`"model_limits":[],"warnings":[]}`
	if got := js(t, a.Meta); got != want {
		t.Fatalf("envelope\n got %s\nwant %s", got, want)
	}
}
