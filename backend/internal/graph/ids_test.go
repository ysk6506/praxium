package graph

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func TestIDsAreDerivedFromTheirParts(t *testing.T) {
	fx := mustFixture(t)
	site := fx.SiteID("S01")
	if !strings.HasPrefix(site.String(), PrefixObject) || len(site.String()) != len(PrefixObject)+32 {
		t.Fatalf("object id %q has the wrong form", site)
	}
	if again := fx.SiteID("S01"); again != site {
		t.Errorf("the same key gave two ids: %s and %s", site, again)
	}
	if fx.SiteID("S02") == site {
		t.Errorf("two keys gave one id")
	}
	if fx.RackID(site, "R1") == fx.RackID(fx.SiteID("S02"), "R1") {
		t.Errorf("the context object does not enter the id")
	}
	// A set names its members by their ids, in any order.
	a, b := fx.SiteID("A"), fx.SiteID("B")
	typ := fx.typeID("Rack")
	one, err := NewObjectID(typ, []KeyPart{{Kind: PartSet, Ref: "lt.x", Objects: []ObjectID{a, b}}})
	if err != nil {
		t.Fatal(err)
	}
	two, err := NewObjectID(typ, []KeyPart{{Kind: PartSet, Ref: "lt.x", Objects: []ObjectID{b, a}}})
	if err != nil {
		t.Fatal(err)
	}
	if one != two {
		t.Errorf("the order of a set changed the id")
	}
	other, err := NewObjectID(fx.typeID("Site"), []KeyPart{{Kind: PartSet, Ref: "lt.x", Objects: []ObjectID{a, b}}})
	if err != nil || other == one {
		t.Errorf("the type does not enter the id (%v)", err)
	}
}

func TestLinkAndVersionIDs(t *testing.T) {
	fx := mustFixture(t)
	site, rack := fx.SiteID("S1"), fx.RackID(fx.SiteID("S1"), "R1")
	lt := fx.linkID("LOCATED_IN")
	one, err := NewLinkID(lt, rack, site, "")
	if err != nil || !strings.HasPrefix(one.String(), PrefixLink) {
		t.Fatalf("link id %q (%v)", one, err)
	}
	if two, _ := NewLinkID(lt, rack, site, "second"); two == one {
		t.Errorf("the discriminator does not enter the link id")
	}
	if rev, _ := NewLinkID(lt, site, rack, ""); rev == one {
		t.Errorf("the direction does not enter the link id")
	}
	zero := kernel.Hash{}
	v1, err := NewVersionID(VersionID{}, zero, zero, 1)
	if err != nil || !strings.HasPrefix(v1.String(), PrefixVersion) {
		t.Fatalf("version id %q (%v)", v1, err)
	}
	for name, v := range map[string]func() (VersionID, error){
		"engine":      func() (VersionID, error) { return NewVersionID(VersionID{}, zero, zero, 2) },
		"input":       func() (VersionID, error) { return NewVersionID(VersionID{}, zero, kernel.Hash{1}, 1) },
		"definitions": func() (VersionID, error) { return NewVersionID(VersionID{}, kernel.Hash{1}, zero, 1) },
		"previous":    func() (VersionID, error) { return NewVersionID(v1, zero, zero, 1) },
	} {
		if got, err := v(); err != nil || got == v1 {
			t.Errorf("%s does not enter the version id (%v)", name, err)
		}
	}
}
