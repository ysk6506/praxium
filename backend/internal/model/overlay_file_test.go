package model_test

import (
	"bytes"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

const sampleFile = `{ "format": "praxium-org-model", "version": 1,
  "overlays": [ { "type": "Server", "properties": [
    {"name": "org.management_department", "value_type": "string", "display_names": {"ko": "관리 부서"}},
    {"name": "org.introduced_year", "value_type": "integer"},
    {"name": "org.contract_no", "value_type": "vt.string", "sensitivity": "restricted"} ] },
    { "trait": "Powered", "properties": [ {"name": "org.owner_team", "value_type": "string", "change_class": "static"} ] } ] }`

func counter(from int) func() int {
	next := from - 1
	return func() int { next++; return next }
}

// [REQ-342] [REQ-349]
func TestOrgModelFileIsReadAndFilled(t *testing.T) {
	// The file names targets and value types by name; ids and numbers are given by a function that is called in a fixed order, and the result composes.
	items, err := model.ParseOrgModelFile([]byte(sampleFile), baseline())
	if err != nil {
		t.Fatalf("ParseOrgModelFile() = %v", err)
	}
	if len(items) != 2 || items[0].Target.ID != "ot.server" || items[1].Target.ID != "tr.powered" || items[1].Target.Kind != model.TargetTrait {
		t.Fatalf("overlays = %+v", items)
	}
	if items[0].Properties[0].ChangeClass != model.ChangeSlow || items[0].Properties[0].ValueType != vtID("vt.string") || !items[0].Properties[0].ID.IsZero() {
		t.Errorf("first property = %+v", items[0].Properties[0])
	}
	if errs := model.CheckOverlay(items[0]); !hasCode(errs, model.CodeRequiredField) || !hasCode(errs, model.CodeOverlayNumber) {
		t.Errorf("an overlay without ids and numbers passes: %v", errs)
	}
	filled := model.FillOverlays(baseline(), nil, items, counter(100001))
	wantIDs := []string{"pr.org.server.management_department", "pr.org.server.introduced_year", "pr.org.server.contract_no", "pr.org.powered.owner_team"}
	var gotIDs []string
	var gotNos []int
	for _, o := range filled {
		for _, p := range o.Properties {
			gotIDs, gotNos = append(gotIDs, p.ID.String()), append(gotNos, p.AttrNo)
		}
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) || !reflect.DeepEqual(gotNos, []int{100001, 100002, 100003, 100004}) {
		t.Errorf("filled ids %v and numbers %v", gotIDs, gotNos)
	}
	if items[0].Properties[0].AttrNo != 0 {
		t.Errorf("FillOverlays changed its input")
	}
	if _, errs := model.ComposeRegistry(baseline(), filled); len(errs) != 0 {
		t.Errorf("ComposeRegistry() = %v", errs)
	}
}

// [REQ-342] [REQ-349]
func TestFillKeepsWhatIsStoredAndNeverGivesATakenId(t *testing.T) {
	// A property that is stored keeps its id and number, found by its name or an old name; a new one never gets a taken id; a number is not issued for a stored one.
	stored := model.FillOverlays(baseline(), nil, mustParse(t, sampleFile)[:1], counter(100001))
	renamed := strings.Replace(sampleFile, "org.management_department", "org.dept", 1)
	wanted := mustParse(t, renamed)[:1]
	wanted[0].Properties[0].Aliases = nil
	again := model.FillOverlays(baseline(), stored, wanted, counter(100500))
	if again[0].Properties[0].ID.String() != "pr.org.server.dept" || again[0].Properties[0].AttrNo != 100500 {
		t.Errorf("a renamed property without an id is a new one: %+v", again[0].Properties[0])
	}
	same := mustParse(t, sampleFile)[:1]
	kept := model.FillOverlays(baseline(), stored, same, counter(100500))
	if !reflect.DeepEqual(kept, stored) {
		t.Errorf("the same file does not give the stored overlay back:\n%+v\n%+v", kept, stored)
	}
	withID := mustParse(t, renamed)[:1]
	withID[0].Properties[0].ID = prID("pr.org.server.management_department")
	withID[0].Properties[0].Aliases = []string{"org.management_department"}
	moved := model.FillOverlays(baseline(), stored, withID, counter(100500))
	if moved[0].Properties[0].AttrNo != 100001 || moved[0].Properties[0].Name != "org.dept" {
		t.Errorf("a rename by id keeps the number: %+v", moved[0].Properties[0])
	}
	if got := model.ClassifyOverlay(stored[0], moved[0]); model.HasBreaking(got) {
		t.Errorf("a rename that keeps the old name is breaking: %+v", got)
	}
	// An id the standard or the file already has is not given again.
	clash := serverOverlay(prop("x", "org.a", "vt.string", model.ChangeSlow), prop("y", "org.a", "vt.string", model.ChangeSlow))
	clash.Properties[0].ID, clash.Properties[1].ID = model.PropertyID{}, model.PropertyID{}
	filled := model.FillOverlays(baseline(), nil, []model.Overlay{clash}, counter(100001))
	if a, b := filled[0].Properties[0].ID.String(), filled[0].Properties[1].ID.String(); a != "pr.org.server.a" || b != "pr.org.server.a_2" {
		t.Errorf("ids = %s, %s", a, b)
	}
}

func mustParse(t *testing.T, text string) []model.Overlay {
	t.Helper()
	items, err := model.ParseOrgModelFile([]byte(text), baseline())
	if err != nil {
		t.Fatalf("ParseOrgModelFile() = %v", err)
	}
	return items
}

// [REQ-342]
func TestOrgModelFileErrors(t *testing.T) {
	// A file with an unknown field, format or name is refused with a code, and a field organisation properties do not take is refused by its reason.
	wrap := func(body string) string {
		return `{"format":"praxium-org-model","version":1,"overlays":[` + body + `]}`
	}
	tests := []struct {
		name string
		text string
		code kernel.Code
	}{
		{"not JSON", `[`, model.CodeDataInvalid},
		{"text after", sampleFile + ` 1`, model.CodeDataInvalid},
		{"another format", `{"format":"x","version":1,"overlays":[]}`, model.CodeDataInvalid},
		{"another version", `{"format":"praxium-org-model","version":2,"overlays":[]}`, model.CodeDataInvalid},
		{"unknown field in the file", `{"format":"praxium-org-model","version":1,"overlays":[],"extra":1}`, model.CodeDataInvalid},
		{"unknown field in an overlay", wrap(`{"type":"Server","extra":1,"properties":[]}`), model.CodeDataInvalid},
		{"unknown field in a property", wrap(`{"type":"Server","properties":[{"name":"org.a","value_type":"string","label":"x"}]}`), model.CodeDataInvalid},
		{"unknown type", wrap(`{"type":"Nothing","properties":[]}`), model.CodeOverlayTarget},
		{"unknown trait", wrap(`{"trait":"Nothing","properties":[]}`), model.CodeOverlayTarget},
		{"no target", wrap(`{"properties":[]}`), model.CodeOverlayTarget},
		{"both targets", wrap(`{"type":"Server","trait":"Powered","properties":[]}`), model.CodeOverlayTarget},
		{"unknown value type", wrap(`{"type":"Server","properties":[{"name":"org.a","value_type":"nothing"}]}`), model.CodeValueTypeUnknown},
		{"type twice", wrap(`{"type":"Server","properties":[]},{"type":"Server","properties":[]}`), model.CodeOverlayDuplicate},
		{"bad id", wrap(`{"type":"Server","properties":[{"id":"!","name":"org.a","value_type":"string"}]}`), model.CodeOverlayName},
		{"too big", strings.Repeat(" ", model.MaxOrgModelFileBytes+1), model.CodeOverlayLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := model.ParseOrgModelFile([]byte(tt.text), baseline())
			if err == nil || kernel.CodeOf(err) != tt.code {
				t.Errorf("ParseOrgModelFile() = %v, want the code %s", err, tt.code)
			}
		})
	}
	refused := wrap(`{"type":"Server","properties":[{"name":"org.a","value_type":"string","required":true,"scope_key":true,"history_kind":"volatile"}]}`)
	items := mustParse(t, refused)
	filled := model.FillOverlays(baseline(), nil, items, counter(100001))
	if errs := model.CheckOverlay(filled[0]); len(errs) != 3 || !hasCode(errs, model.CodeOverlayField) {
		t.Errorf("CheckOverlay() = %v, want three refusals", errs)
	}
}

// [REQ-342]
func TestOverlayBodyRoundTrips(t *testing.T) {
	// The body has no namespace, reads back the same, and refuses what it does not know.
	filled := model.FillOverlays(baseline(), nil, mustParse(t, sampleFile), counter(100001))
	for _, o := range filled {
		body, err := o.Body()
		if err != nil {
			t.Fatalf("Body() = %v", err)
		}
		if bytes.Contains(body, []byte(`"namespace"`)) {
			t.Errorf("the body has a namespace: %s", body)
		}
		back, err := model.ParseOverlay(body)
		if err != nil {
			t.Fatalf("ParseOverlay() = %v", err)
		}
		want := o.Clone()
		sort.Slice(want.Properties, func(i, j int) bool { return want.Properties[i].ID.String() < want.Properties[j].ID.String() })
		if !reflect.DeepEqual(back, want) {
			t.Fatalf("ParseOverlay() = %+v; want %+v", back, want)
		}
		if again, _ := back.Body(); !bytes.Equal(again, body) {
			t.Errorf("the body is not stable")
		}
		if errs := model.CheckOverlay(back); len(errs) != 0 {
			t.Errorf("CheckOverlay() = %v", errs)
		}
	}
	for name, text := range map[string]string{
		"unknown field":   `{"schema_version":1,"kind":"type_overlay","target":{"kind":"object_type","id":"ot.server"},"properties":[],"x":1}`,
		"text after":      `{"schema_version":1} 1`,
		"another version": `{"schema_version":2,"kind":"type_overlay","target":{"kind":"object_type","id":"ot.server"},"properties":[]}`,
		"std namespace":   `{"schema_version":1,"kind":"type_overlay","target":{"kind":"object_type","id":"ot.server"},"properties":[{"id":"pr.org.a","name":"org.a","namespace":"std","value_type":"vt.string","change_class":"slow"}]}`,
		"too big":         strings.Repeat(" ", model.MaxOverlayBytes+1),
	} {
		o, err := model.ParseOverlay([]byte(text))
		if err == nil && len(model.CheckOverlay(o)) == 0 {
			t.Errorf("%s is accepted", name)
		}
	}
}

// [REQ-342]
func TestOrgModelFileIsWrittenBack(t *testing.T) {
	// A file written from the overlays has every id and number, reads back to the same overlays, and is the same bytes each time.
	filled := model.FillOverlays(baseline(), nil, mustParse(t, sampleFile), counter(100001))
	out, err := model.FormatOrgModelFile(baseline(), filled)
	if err != nil {
		t.Fatalf("FormatOrgModelFile() = %v", err)
	}
	back, err := model.ParseOrgModelFile(out, baseline())
	if err != nil || !reflect.DeepEqual(back, filled) {
		t.Fatalf("the file does not read back: %v\n%s", err, out)
	}
	if again, _ := model.FormatOrgModelFile(baseline(), back); !bytes.Equal(again, out) {
		t.Errorf("the file is not stable")
	}
	for _, text := range []string{`"id": "pr.org.server.contract_no"`, `"attr_no": 100003`, `"type": "Server"`, `"trait": "Powered"`} {
		if !strings.Contains(string(out), text) {
			t.Errorf("the file lacks %s:\n%s", text, out)
		}
	}
	missing := model.NewOverlay(model.TargetObjectType, "ot.nothing", nil)
	if _, err := model.FormatOrgModelFile(baseline(), []model.Overlay{missing}); kernel.CodeOf(err) != model.CodeOverlayTarget {
		t.Errorf("FormatOrgModelFile() of a missing target = %v", err)
	}
}
