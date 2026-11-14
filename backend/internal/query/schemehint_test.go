package query

import (
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"testing"
)

// [REQ-321]
func TestAnIdentifierSchemeThatDoesNotExistIsAnsweredWithTheSchemesThatDo(t *testing.T) {
	r := newRig(t, false)
	_, err := r.get(GetInput{Identifier: &IdentifierFilter{Scheme: "asset_tag", Value: "A-1"}, Common: r.common()}, AllowAll())
	if err == nil || FieldOf(err) != "identifier.scheme" || !strings.Contains(err.Error(), `no identifier scheme "asset_tag"; the schemes are:`) || !strings.Contains(err.Error(), "bmc_mac") {
		t.Fatalf("get: %v", err)
	}
	_, err = r.find(FindInput{Common: r.common(), Identifier: &IdentifierFilter{Scheme: "asset_tag", Value: "A-1"}}, AllowAll())
	if err == nil || !strings.Contains(err.Error(), "the schemes are:") || !strings.Contains(err.Error(), "redfish_system_id") {
		t.Fatalf("find: %v", err)
	}
}

// [REQ-322]
func TestAnIdentifierIsLookedUpTidiedTheWayItWasStored(t *testing.T) {
	r := newRig(t, false)
	r.world()
	id := r.fx.ServerID("SN-000102")
	scheme, _ := r.reg.IdentifierSchemeByName("bmc_mac")
	r.apply(graph.Batch{Identifiers: []graph.IdentifierWrite{{Object: id, Scheme: scheme.ID, Value: "020000004002"}}})
	for _, typed := range []string{"02:00:00:00:40:02", "02-00-00-00-40-02", "020000004002"} {
		a, err := r.get(GetInput{Identifier: &IdentifierFilter{Scheme: "bmc_mac", Value: typed}, Common: r.common()}, AllowAll())
		if err != nil || a.Data.Object.ObjectID != id.String() {
			t.Fatalf("%s: %v", typed, err)
		}
		found := r.mustFind(FindInput{Identifier: &IdentifierFilter{Scheme: "bmc_mac", Value: typed}})
		if len(found.Data.Items) != 1 {
			t.Fatalf("find by %s: %+v", typed, found.Data.Items)
		}
	}
	if _, err := r.get(GetInput{Identifier: &IdentifierFilter{Scheme: "bmc_mac", Value: "not a mac"}, Common: r.common()}, AllowAll()); err == nil {
		t.Fatal("a value that is no MAC finds nothing")
	}
}
