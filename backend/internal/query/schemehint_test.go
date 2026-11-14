package query

import (
	"strings"
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
