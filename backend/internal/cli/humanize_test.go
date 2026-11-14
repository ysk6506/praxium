package cli

import (
	"strings"
	"testing"
)

// [REQ-328]
func TestNestedValuesAreWrittenForAPersonInTheTableForm(t *testing.T) {
	if got := mapText(map[string]any{"ko": "랙", "en": "Rack"}); got != "en=Rack ko=랙" {
		t.Errorf("%q", got)
	}
	if mapText(nil) != "-" || mapText(map[string]any{"a": []any{1.0}}) != "{...}" || mapText(map[string]any{"a": map[string]any{}}) != "{...}" {
		t.Error("an empty object is a dash and a nested one is left as {...}")
	}
	if got := scalar([]any{map[string]any{"a": 1.0}, "x"}); got != "(a=1),x" {
		t.Errorf("%q", got)
	}
	if scalar(7) != "7" || scalar(map[string]any{"k": true}) != "k=true" {
		t.Error("an integer and an object of plain values")
	}
	set := map[string]any{"strength": "strong", "components": []any{
		map[string]any{"kind": "set", "ref": "lt.member_of"}, map[string]any{"kind": "context", "ref": "lt.located_in"}, map[string]any{"kind": "property", "ref": "pr.rack.asset_tag"}}}
	if got := keyText(set); got != "the set of the objects a member_of link reaches + link located_in + property asset_tag (not usable in file ingestion)" {
		t.Errorf("%q", got)
	}
	if got := keyText(map[string]any{"scheme": "is.bmc_mac"}); got != "scheme bmc_mac" {
		t.Errorf("%q", got)
	}
	if got := propertyText(map[string]any{"name": "width_mm", "value_type": "vt.quantity", "unit": "mm"}); got != "width_mm (quantity, unit mm)" {
		t.Errorf("%q", got)
	}
	if got := propertyText(map[string]any{"name": "total_u", "value_type": "vt.integer", "required": true}); got != "total_u (integer, required)" {
		t.Errorf("%q", got)
	}
	var out strings.Builder
	if err := printHealth(&out, map[string]any{"live": 200, "ready": 503}); err != nil || !strings.Contains(out.String(), "ready    not ok (503)") || !strings.Contains(out.String(), "started  not ok (0)") {
		t.Errorf("%v %q", err, out.String())
	}
	lines := fieldLines("identity_keys", []any{set, map[string]any{"strength": "weak", "scheme": "is.wwn"}})
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "2. weak: scheme wwn") {
		t.Errorf("%v", lines)
	}
	if got := fieldLines("needs", []any{map[string]any{"a": "b"}}); len(got) != 1 || got[0] != "(a=b)" {
		t.Errorf("another list of objects stays on one line: %v", got)
	}
	if got := fieldLines("properties", []any{"plain"}); len(got) != 1 || got[0] != "plain" {
		t.Errorf("%v", got)
	}
	if got := fieldLines("x", nil); len(got) != 1 || got[0] != "-" {
		t.Errorf("%v", got)
	}
	if healthText(200) != "ok (200)" || healthText(503) != "not ok (503)" || shortRef(nil) != "" || shortRef("plain") != "plain" {
		t.Error("health states and short references")
	}
	if !strings.Contains(unknownCommandMessage("ingest"), "does not ask a server") || unknownCommandMessage("zzz") != `unknown command "zzz"` {
		t.Error("the message for a command that does not ask a server")
	}
}
