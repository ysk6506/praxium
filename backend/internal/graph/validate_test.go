package graph

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

func TestObjectValidationAgainstTheRegistry(t *testing.T) {
	fx := mustFixture(t)
	v := newValidator(fx.Reg)
	with := func(edit func(*ObjectUpsert)) ObjectUpsert {
		u := fx.Server("SN-1", "M1")
		edit(&u)
		return u
	}
	attr := func(no int, a Attr) func(*ObjectUpsert) {
		return func(u *ObjectUpsert) { u.Attrs = map[int]Attr{no: a} }
	}
	cases := []struct {
		name string
		u    ObjectUpsert
		code kernel.Code
	}{
		{"sound", fx.Server("SN-1", "M1"), ""},
		{"unknown type", with(func(u *ObjectUpsert) { u.Type = fx.linkTypeAsObjectType() }), CodeBatchInvalid},
		{"no name", with(func(u *ObjectUpsert) { u.Name = "" }), CodeBatchInvalid},
		{"no key", with(func(u *ObjectUpsert) { u.Key = nil }), CodeBatchInvalid},
		{"key of another type", with(func(u *ObjectUpsert) { u.Key = fx.Site("S", "").Key }), CodeBatchInvalid},
		{"key part without a value", with(func(u *ObjectUpsert) { u.Key[1].Value = "" }), CodeBatchInvalid},
		{"volatile attribute in a row", with(attr(AttrServerPower, fact("on"))), CodeVolatileInRow},
		{"attribute of another type", with(attr(AttrSiteAddress, fact("x"))), CodeBatchInvalid},
		{"attribute without a grade", with(attr(AttrServerModel, Attr{Value: "x", Basis: model.BasisObserved})), CodeBatchInvalid},
		{"nil value with a fact grade", with(attr(AttrServerModel, fact(nil))), CodeBatchInvalid},
		{"unknown value with a nil value", with(attr(AttrServerModel, Attr{Grade: model.GradeUnknown, Basis: model.BasisObserved})), ""},
		{"integer where text is wanted", with(attr(AttrServerModel, fact(int64(3)))), CodeBatchInvalid},
		{"float", with(attr(AttrServerModel, fact(1.5))), CodeBatchInvalid},
		{"text that a value type rejects", with(attr(202, fact("not a host name!"))), CodeBatchInvalid},
		{"enum outside its values", with(attr(204, fact("round"))), CodeBatchInvalid},
		{"time that is no time", with(attr(13, fact("yesterday"))), CodeBatchInvalid},
		{"time", with(attr(13, fact("2030-01-01T00:00:00Z"))), ""},
		{"integer", with(attr(205, fact(int64(2)))), ""},
		{"integer as text", with(attr(205, fact("2"))), CodeBatchInvalid},
		{"quantity as a map", with(attr(207, fact(map[string]any{"value": "64", "unit": "gib"}))), ""},
		{"quantity as a decimal text", with(attr(207, fact("64.5"))), ""},
		{"number that is no number", with(attr(207, fact("many"))), CodeBatchInvalid},
		{"list", with(attr(30, fact([]any{"a.example.test"}))), ""},
		{"list of floats", with(attr(30, fact([]any{1.5}))), CodeBatchInvalid},
		{"map", with(attr(5, fact(map[string]any{"k": "v"}))), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := v.object(c.u)
			if c.code == "" && err != nil {
				t.Errorf("a sound upsert was refused: %v", err)
			}
			if c.code != "" {
				wantCode(t, c.name, err, c.code)
			}
		})
	}
}

func (f *Fixture) linkTypeAsObjectType() model.ObjectTypeID {
	id, _ := kernel.ParseID[model.ObjectTypeKind]("ot.nothing")
	return id
}

func TestKeyShapes(t *testing.T) {
	fx := mustFixture(t)
	v := newValidator(fx.Reg)
	site := fx.SiteID("S1")
	rack := fx.Rack(site, "R1")
	bad := map[string][]KeyPart{
		"context without an object":   {{Kind: PartContext, Ref: "lt.located_in"}, rack.Key[1]},
		"context with two objects":    {{Kind: PartContext, Ref: "lt.located_in", Objects: []ObjectID{site, site}}, rack.Key[1]},
		"property with an object":     {rack.Key[0], {Kind: PartProperty, Ref: "pr.common.name", Value: "R", Objects: []ObjectID{site}}},
		"set without members":         {{Kind: PartSet, Ref: "lt.located_in"}, rack.Key[1]},
		"unknown kind":                {{Kind: "other", Ref: "x", Value: "v"}},
		"wrong order":                 {rack.Key[1], rack.Key[0]},
		"scheme part for a plain key": {{Kind: PartScheme, Ref: "is.bmc_mac", Value: "x"}},
	}
	for name, key := range bad {
		u := rack
		u.Key = key
		if _, _, err := v.object(u); err == nil {
			t.Errorf("%s: a key of the wrong shape was accepted", name)
		}
	}
	if _, _, err := v.object(rack); err != nil {
		t.Errorf("a sound rack was refused: %v", err)
	}
	// The scheme-only keys: the identifier of the scheme is the whole key.
	if !keyMatches(model.IdentityKey{Scheme: schemeID(t, "is.bmc_mac")}, []KeyPart{{Kind: PartScheme, Ref: "is.bmc_mac", Value: "x"}}) {
		t.Errorf("a scheme-only key does not match")
	}
	if keyMatches(model.IdentityKey{Scheme: schemeID(t, "is.bmc_mac")}, []KeyPart{{Kind: PartScheme, Ref: "is.wwpn", Value: "x"}}) {
		t.Errorf("a key of another scheme matched")
	}
}

func schemeID(t *testing.T, raw string) model.IdentifierSchemeID {
	t.Helper()
	id, err := kernel.ParseID[model.IdentifierSchemeKind](raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestLinkAndVolatileValidation(t *testing.T) {
	fx := mustFixture(t)
	v := newValidator(fx.Reg)
	site, rack := fx.SiteID("S1"), fx.RackID(fx.SiteID("S1"), "R1")
	server := fx.ServerID("SN-1")
	if _, _, err := v.link(fx.MountedIn(server, rack, 3)); err != nil {
		t.Errorf("a sound link was refused: %v", err)
	}
	noGrade := fx.LocatedIn(rack, site)
	noGrade.Grade = ""
	badAttr := fx.MountedIn(server, rack, 3)
	badAttr.Attrs = map[int]Attr{AttrMountPosition: fact("three")}
	foreign := fx.MountedIn(server, rack, 3)
	foreign.Attrs = map[int]Attr{AttrServerModel: fact("x")}
	unknown := fx.LocatedIn(rack, site)
	unknown.Type, _ = kernel.ParseID[model.LinkTypeKind]("lt.nothing")
	for name, u := range map[string]LinkUpsert{"no grade": noGrade, "wrong attribute value": badAttr, "attribute of a type": foreign, "unknown link type": unknown} {
		if _, _, err := v.link(u); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	typ, _ := fx.Reg.ObjectType(fx.typeID("PhysicalServer"))
	good := fx.Power(server, "on", "2030-01-01T00:00:00Z")
	if err := v.volatile(typ, good); err != nil {
		t.Errorf("a sound volatile write was refused: %v", err)
	}
	for name, w := range map[string]VolatileWrite{
		"versioned attribute": {Object: server, AttrNo: AttrServerModel, Value: "x", Grade: model.GradeFact, Basis: model.BasisObserved},
		"unknown attribute":   {Object: server, AttrNo: 99999, Value: "x", Grade: model.GradeFact, Basis: model.BasisObserved},
		"no grade":            {Object: server, AttrNo: AttrServerPower, Value: "on"},
		"enum outside":        {Object: server, AttrNo: AttrServerPower, Value: "sideways", Grade: model.GradeFact, Basis: model.BasisObserved},
	} {
		if err := v.volatile(typ, w); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
