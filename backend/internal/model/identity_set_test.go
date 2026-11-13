package model_test

import (
	"encoding/json"
	"testing"

	_ "embed"

	"github.com/ysk6506/praxium/backend/internal/model"
)

//go:embed testdata/numbering_v1.json
var numberingV1 []byte

func TestRulesOfIdentitySetsAndContextOnlyKeys(t *testing.T) {
	server := func(d *model.Definitions) *model.ObjectType { return objectType(d, "ot.server") }
	asset := func(d *model.Definitions) *model.ObjectType { return objectType(d, "ot.asset") }
	setOf := func(id string, incoming bool, size int) model.IdentityKey {
		return model.IdentityKey{Strength: model.IdentityStrong, Components: []model.KeyComponent{{Context: ltID(id), Set: true, Incoming: incoming, Size: size}}}
	}
	runRuleCases(t, []ruleCase{
		{"[REQ-104] set over a link type that does not exist", model.CodeIdentitySet, func(d *model.Definitions) {
			server(d).IdentityKeys = []model.IdentityKey{setOf("lt.nothing", false, 0)}
		}},
		{"[REQ-104] set over a link with no binding that leaves the type", model.CodeIdentitySet, func(d *model.Definitions) {
			asset(d).IdentityKeys = append(asset(d).IdentityKeys, setOf("lt.mounted_in", false, 0))
		}},
		{"[REQ-104] incoming set over a link with no binding that arrives at the type", model.CodeIdentitySet, func(d *model.Definitions) {
			server(d).IdentityKeys = []model.IdentityKey{setOf("lt.mounted_in", true, 0)}
		}},
		{"[REQ-104] set with a negative size", model.CodeIdentitySet, func(d *model.Definitions) {
			server(d).IdentityKeys = []model.IdentityKey{setOf("lt.mounted_in", false, -1)}
		}},
		{"[REQ-104] property component that says it is a set", model.CodeIdentitySet, func(d *model.Definitions) {
			asset(d).IdentityKeys[0].Components[0].Set = true
		}},
		{"[REQ-104] context component with a size but no set", model.CodeIdentitySet, func(d *model.Definitions) {
			asset(d).IdentityKeys[1].Components[0].Size = 2
		}},
		{"[REQ-104] context component that is incoming but not a set", model.CodeIdentitySet, func(d *model.Definitions) {
			asset(d).IdentityKeys[1].Components[0].Incoming = true
		}},
		{"[REQ-104] key of a context alone when the link is many to one", model.CodeIdentityKey, func(d *model.Definitions) {
			server(d).IdentityKeys = []model.IdentityKey{{Strength: model.IdentityStrong, Components: []model.KeyComponent{{Context: ltID("lt.mounted_in")}}}}
		}},
		{"[REQ-104] two keys that differ only in the size of a set", model.CodeIdentityKey, func(d *model.Definitions) {
			server(d).IdentityKeys = []model.IdentityKey{setOf("lt.mounted_in", false, 2), setOf("lt.mounted_in", false, 2)}
		}},
	})
}

func TestIdentitySetsAndContextOnlyKeysAreAccepted(t *testing.T) {
	// [REQ-104] A set stands alone as a key, and a context alone is a key when the link is one to one.
	d := baseline()
	srv := objectType(&d, "ot.server")
	srv.IdentityKeys = []model.IdentityKey{
		{Strength: model.IdentityStrong, Components: []model.KeyComponent{{Context: ltID("lt.mounted_in"), Set: true, Size: 1}}},
		{Strength: model.IdentityWeak, Components: []model.KeyComponent{{Context: ltID("lt.part_of")}}},
	}
	linkType(&d, "lt.part_of").Bindings[0].Cardinality = model.CardOneToOne
	if err := model.Validate(d); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestIdentitySetFieldsChangeTheHash(t *testing.T) {
	// [REQ-104] The set flag, its direction and its size are part of what a key means; a key without them hashes as before.
	hashWith := func(c model.KeyComponent) string {
		d := baseline()
		objectType(&d, "ot.server").IdentityKeys = []model.IdentityKey{{Strength: model.IdentityStrong, Components: []model.KeyComponent{c}}}
		r, err := model.NewRegistry(d)
		if err != nil {
			t.Fatalf("NewRegistry() = %v", err)
		}
		return r.Hash().String()
	}
	base := model.KeyComponent{Context: ltID("lt.mounted_in"), Set: true}
	seen := map[string]string{}
	for name, c := range map[string]model.KeyComponent{
		"set": base, "sized": {Context: base.Context, Set: true, Size: 2}, "other size": {Context: base.Context, Set: true, Size: 3},
	} {
		h := hashWith(c)
		if other, dup := seen[h]; dup {
			t.Errorf("%s and %s have the same model hash", name, other)
		}
		seen[h] = name
	}
}

func TestStandardNumbersAreStable(t *testing.T) {
	// [REQ-233] Every number and binding id the standard model had before the correction step is still there, unchanged.
	var want struct {
		TypeNo   map[string]int `json:"type_no"`
		LinkNo   map[string]int `json:"link_type_no"`
		AttrNo   map[string]int `json:"attr_no"`
		Bindings []string       `json:"binding_ids"`
	}
	if err := json.Unmarshal(numberingV1, &want); err != nil {
		t.Fatalf("numbering_v1.json: %v", err)
	}
	r := loadStd(t)
	got := r.Definitions()
	types, links, attrs, bindings := map[string]int{}, map[string]int{}, map[string]int{}, map[string]bool{}
	for _, tr := range got.Traits {
		for _, p := range tr.Properties {
			attrs[p.ID.String()] = p.AttrNo
		}
	}
	for _, ot := range got.ObjectTypes {
		types[ot.ID.String()] = ot.TypeNo
		for _, p := range ot.Properties {
			attrs[p.ID.String()] = p.AttrNo
		}
	}
	for _, lt := range got.LinkTypes {
		links[lt.ID.String()] = lt.LinkTypeNo
		for _, p := range lt.Properties {
			attrs[p.ID.String()] = p.AttrNo
		}
		for _, b := range lt.Bindings {
			bindings[b.ID.String()] = true
		}
	}
	for name, pair := range map[string][2]map[string]int{"type": {want.TypeNo, types}, "link type": {want.LinkNo, links}, "attribute": {want.AttrNo, attrs}} {
		for id, no := range pair[0] {
			if pair[1][id] != no {
				t.Errorf("%s %s has the number %d, want %d", name, id, pair[1][id], no)
			}
		}
	}
	for _, id := range want.Bindings {
		if !bindings[id] {
			t.Errorf("the binding %s is gone", id)
		}
	}
}
