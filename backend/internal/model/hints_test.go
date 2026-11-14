package model

import (
	"strings"
	"testing"
)

// [REQ-321]
func TestTheModelDescribesTheIdentityKeysAndSchemesOfATypeForMessages(t *testing.T) {
	reg, err := LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	rack, _ := reg.ObjectTypeByName("Rack")
	if got := strings.Join(reg.DescribeIdentityKeys(rack.ID), "; "); got != "link LOCATED_IN + property name (strong; not usable in file ingestion); property asset_tag (weak)" {
		t.Fatalf("%q", got)
	}
	server, _ := reg.ObjectTypeByName("PhysicalServer")
	if got := strings.Join(reg.SchemeKeyNames(server.ID), ","); got != "bmc_mac,redfish_system_id" {
		t.Fatalf("%q", got)
	}
	if got := strings.Join(reg.DescribeIdentityKeys(server.ID), "; "); !strings.Contains(got, "scheme bmc_mac (weak)") || !strings.Contains(got, "property manufacturer + property serial_number (strong)") {
		t.Fatalf("%q", got)
	}
	names := reg.IdentifierSchemeNames()
	if len(names) != len(reg.IdentifierSchemes()) || names[0] > names[1] {
		t.Fatalf("every scheme, in alphabetical order: %v", names)
	}
	if props := reg.PropertyNames(rack.ID); len(props) == 0 || !strings.Contains(strings.Join(props, ","), "asset_tag") {
		t.Fatalf("%v", props)
	}
	if reg.DescribeIdentityKeys(ObjectTypeID{}) != nil {
		t.Fatal("a type that does not exist has no keys")
	}
}

// [REQ-321]
func TestAKeyThatNamesWhatTheRegistryLacksIsStillDescribed(t *testing.T) {
	reg, err := LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	if got := reg.describeKey(IdentityKey{Scheme: IdentifierSchemeID{}}); !strings.HasPrefix(got, "scheme ") {
		t.Errorf("%q", got)
	}
	if got := reg.describeComponent(KeyComponent{Set: true}); !strings.HasPrefix(got, "the set of the objects a ") {
		t.Errorf("%q", got)
	}
	if got := reg.describeComponent(KeyComponent{Context: LinkTypeID{}}); !strings.HasPrefix(got, "link ") {
		t.Errorf("%q", got)
	}
	if got := reg.describeComponent(KeyComponent{Property: PropertyID{}}); !strings.HasPrefix(got, "link ") {
		t.Errorf("%q", got)
	}
	if reg.SchemeKeyNames(ObjectTypeID{}) != nil {
		t.Error("a type that does not exist has no scheme keys")
	}
}
