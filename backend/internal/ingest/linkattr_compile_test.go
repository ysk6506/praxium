package ingest

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-435]
// The mistakes in the attributes of a link definition are found when the mapping is compiled, once, with the definition and
// the attribute, and not row by row in a run.
func TestWrongLinkAttributesAreRefusedWhenTheMappingIsCompiled(t *testing.T) {
	reg := newRig(t, laSource, laMapping(laLink(laRackLink, ""))).runner.Reg
	src, err := ParseSource(strings.NewReader(laSource))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct{ attrs, want string }{
		"no such attribute":         {`{"attribute":"positon_u","column":"position_u"}`, `link type MOUNTED_IN has no attribute "positon_u"; its attributes are: face, height_u, position_u, side`},
		"an attribute of the end":   {`{"attribute":"name","column":"asset_id"}`, `link type MOUNTED_IN has no attribute "name"`},
		"a column and a value":      {`{"attribute":"face","column":"face","value":"front"}`, "a column and a fixed value; write one of the two"},
		"neither":                   {`{"attribute":"face"}`, `neither a "column" nor a "value"`},
		"an empty fixed value":      {`{"attribute":"face","value":""}`, "an empty fixed value"},
		"a fixed value of spaces":   {`{"attribute":"face","value":"  "}`, "an empty fixed value"},
		"the same attribute twice":  {laPos + "," + laPos, "attribute position_u is given twice"},
		"no such column":            {`{"attribute":"position_u","column":"posn"}`, `column "posn", which dataset ds_servers`},
		"a column of the To end":    {`{"attribute":"position_u","column":"rack_name"}`, `column "rack_name", which dataset ds_servers`},
		"a value not of the enum":   {`{"attribute":"face","value":"top"}`, `the fixed value "top" of attribute face does not fit`},
		"a value of the wrong kind": {`{"attribute":"position_u","value":"high"}`, `the fixed value "high" of attribute position_u does not fit`},
	}
	for name, c := range cases {
		m, err := ParseMapping(strings.NewReader(laMapping(laLink(laRackLink, c.attrs))))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		_, err = Compile(reg, src, m)
		if err == nil || kernel.CodeOf(err) != kernel.CodeInvalidValue || !strings.Contains(err.Error(), "link definition l_mounted") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want a refusal of link definition l_mounted with %q", name, err, c.want)
		}
	}
}

// [REQ-435]
// The column of a link attribute is read from the dataset of the From feed, and a fixed value that fits is accepted for
// every kind of value.
func TestLinkAttributesCompileAgainstTheDatasetOfTheFromFeedAndTakeFixedValuesOfEveryKind(t *testing.T) {
	reg := newRig(t, laSource, laMapping(laLink(laRackLink, ""))).runner.Reg
	src, _ := ParseSource(strings.NewReader(laSource))
	for _, attrs := range []string{
		`{"attribute":"position_u","value":"12"},{"attribute":"face","value":"rear"}`,
		laPos + "," + laFace,
	} {
		m, err := ParseMapping(strings.NewReader(laMapping(laLink(laRackLink, attrs))))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(reg, src, m); err != nil {
			t.Errorf("%s: %v", attrs, err)
		}
	}
	for name, c := range map[string]struct{ def, attrs string }{
		"a quantity": {laCoolLink, `{"attribute":"capacity_kw","value":"12.5"}`},
		"a number":   {laCoolLink, `{"attribute":"ride_through_s","value":"30"}`},
		"a boolean":  {laHostLink, `{"attribute":"ha_protected","value":"true"}`},
		"a text":     {laCoolLink, `{"attribute":"coolant","value":"water"}`},
	} {
		m, err := ParseMapping(strings.NewReader(laMapping(laLink(c.def, c.attrs))))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(reg, src, m); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	bad, _ := ParseMapping(strings.NewReader(laMapping(laLink(laHostLink, `{"attribute":"ha_protected","value":"maybe"}`))))
	if _, err := Compile(reg, src, bad); err == nil || !strings.Contains(err.Error(), "is not true or false") {
		t.Errorf("a fixed value that is no boolean: %v", err)
	}
}

// [REQ-435]
// The form of the attributes of a link definition is strict like the rest of the definition: the value is text, and a
// field the definition does not have (the "side" a later change may add) is refused with the fields it has.
func TestTheFormOfLinkAttributesIsStrict(t *testing.T) {
	for name, c := range map[string]struct{ attrs, want string }{
		"a number as the value": {`{"attribute":"position_u","value":12}`, "text in quotes"},
		"an unknown field":      {`{"attribute":"position_u","column":"position_u","side":"to"}`, `unknown field "side"`},
	} {
		_, err := ParseMapping(strings.NewReader(laMapping(laLink(laRackLink, c.attrs))))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}
