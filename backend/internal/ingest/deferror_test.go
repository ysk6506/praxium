package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The errors of a definition (ADR-0070): where, which code, the values of the sentence, the sentence; every
// unrelated one at once; the table of the codes.

// sub replaces the one place of text that has old, and refuses a test that names none or several.
func deSub(text, old, repl string) string {
	if strings.Count(text, old) != 1 {
		panic(fmt.Sprintf("the text has %d places with %q", strings.Count(text, old), old))
	}
	return strings.Replace(text, old, repl, 1)
}

const deSrcOK = `{"id":"s","adapter":"file","datasets":[{"id":"d","parser":"csv","natural_key":["a"],"columns":[{"name":"a"},{"name":"b"}]}]}`

// deCase is one input with the one error it must give: the code, and the place.
type deCase struct {
	name, doc, text, code, at string
	reg                       *model.Registry
	// unreadable is a reader that fails instead of the text.
	unreadable bool
}

func deSrcCase(name, text, code, at string) deCase {
	return deCase{name: name, doc: DocumentSource, text: text, code: code, at: at}
}

func deMapCase(name, text, code, at string) deCase {
	return deCase{name: name, doc: DocumentMapping, text: text, code: code, at: at}
}

var deMapBase = laMapping(laLink(laRackLink, ""))

func deWithRefinement(r string) string {
	return deSub(deMapBase, `{"feeds":[`, `{"refinements":[`+r+`],"feeds":[`)
}

func deWithLinkAttrs(attrs string) string { return laMapping(laLink(laRackLink, attrs)) }

func deSourceCases() []deCase {
	xlsx := func(sheet string) string {
		return deSub(deSrcOK, `"parser":"csv",`, `"parser":"xlsx","sheet":`+sheet+`,`)
	}
	return []deCase{
		{name: "unreadable", doc: DocumentSource, unreadable: true, code: "definition.unreadable", at: ""},
		deSrcCase("unknown field", deSub(deSrcOK, `"columns":`, `"colums":`), "definition.unknown_field", "/datasets/0/colums"),
		deSrcCase("wrong kind", deSub(deSrcOK, `"parser":"csv",`, `"parser":"csv","ignore_undeclared_columns":"yes",`), "definition.wrong_kind", "/datasets/0/ignore_undeclared_columns"),
		deSrcCase("not JSON", `{"id":`, "definition.malformed", ""),
		deSrcCase("two documents", deSrcOK+deSrcOK, "definition.multiple_documents", ""),
		deSrcCase("no id", deSub(deSrcOK, `"id":"s"`, `"id":""`), "source.id.missing", "/id"),
		deSrcCase("adapter", deSub(deSrcOK, `"adapter":"file"`, `"adapter":"ftp"`), "source.adapter.unsupported", "/adapter"),
		deSrcCase("limit", deSub(deSrcOK, `"adapter":"file",`, `"adapter":"file","limits":{"max_rows_per_sheet":-1},`), "source.limits.negative", "/limits/max_rows_per_sheet"),
		deSrcCase("no datasets", `{"id":"s","adapter":"file","datasets":[]}`, "source.datasets.empty", "/datasets"),
		deSrcCase("dataset without id", deSub(deSrcOK, `"id":"d"`, `"id":""`), "source.dataset.id_missing", "/datasets/0/id"),
		deSrcCase("dataset twice", deSub(deSrcOK, `"columns":[{"name":"a"},{"name":"b"}]}]`, `"columns":[{"name":"a"}]},{"id":"d","parser":"csv","natural_key":["a"],"columns":[{"name":"a"}]}]`), "source.dataset.id_duplicate", "/datasets/1/id"),
		deSrcCase("kind", deSub(deSrcOK, `"id":"d",`, `"id":"d","kind":"derived",`), "source.dataset.unsupported_mode", "/datasets/0/kind"),
		deSrcCase("parser", deSub(deSrcOK, `"parser":"csv"`, `"parser":"parquet"`), "source.dataset.unsupported_parser", "/datasets/0/parser"),
		deSrcCase("deletion", deSub(deSrcOK, `"parser":"csv",`, `"parser":"csv","deletion":"soft",`), "source.dataset.unsupported_deletion", "/datasets/0/deletion"),
		deSrcCase("no columns", deSub(deSrcOK, `"columns":[{"name":"a"},{"name":"b"}]`, `"columns":[]`), "source.dataset.columns_empty", "/datasets/0/columns"),
		deSrcCase("column twice", deSub(deSrcOK, `{"name":"b"}`, `{"name":"a"}`), "source.column.name_invalid", "/datasets/0/columns/1/name"),
		deSrcCase("sheet of a csv", deSub(deSrcOK, `"parser":"csv",`, `"parser":"csv","sheet":{},`), "source.dataset.sheet_not_for_parser", "/datasets/0/sheet"),
		deSrcCase("sheet name and index", xlsx(`{"name":"A","index":2}`), "source.sheet.name_and_index", "/datasets/0/sheet"),
		deSrcCase("sheet below minimum", xlsx(`{"index":-1}`), "source.sheet.below_minimum", "/datasets/0/sheet"),
		deSrcCase("merged cells", xlsx(`{"merged_cells":"all"}`), "source.sheet.merged_cells_invalid", "/datasets/0/sheet/merged_cells"),
		deSrcCase("range", xlsx(`{"range":"A5"}`), "source.sheet.range_invalid", "/datasets/0/sheet/range"),
		deSrcCase("range in header", xlsx(`{"range":"A1:C9"}`), "source.sheet.range_in_header", "/datasets/0/sheet/range"),
		deSrcCase("delimiter of json lines", deSub(deSrcOK, `"parser":"csv",`, `"parser":"json_lines","delimiter":";",`), "source.dataset.delimiter_not_for_parser", "/datasets/0/delimiter"),
		deSrcCase("delimiter", deSub(deSrcOK, `"parser":"csv",`, `"parser":"csv","delimiter":"#",`), "source.dataset.delimiter_invalid", "/datasets/0/delimiter"),
		deSrcCase("empty natural key", deSub(deSrcOK, `"natural_key":["a"]`, `"natural_key":[]`), "source.natural_key.empty", "/datasets/0/natural_key"),
		deSrcCase("natural key column", deSub(deSrcOK, `"natural_key":["a"]`, `"natural_key":["z"]`), "source.natural_key.unknown_column", "/datasets/0/natural_key/0"),
	}
}

func deMappingCases() []deCase {
	op := func(o string) string {
		return deWithRefinement(`{"id":"r1","input":"ds_racks","ops":[` + o + `]}`)
	}
	return []deCase{
		deMapCase("unknown field", deSub(deMapBase, `{"feeds":[`, `{"feed":[],"feeds":[`), "definition.unknown_field", "/feed"),
		deMapCase("wrong kind", deSub(deMapBase, `{"id":"f_rack",`, `{"id":"f_rack","approximate":"yes",`), "definition.wrong_kind", "/feeds/0/approximate"),
		deMapCase("refinement input", deWithRefinement(`{"id":"r1","input":"nope","ops":[]}`), "mapping.refinement.unknown_input", "/refinements/0/input"),
		deMapCase("refinement without id", deWithRefinement(`{"id":"","input":"ds_racks","ops":[]}`), "mapping.refinement.id_missing", "/refinements/0/id"),
		deMapCase("refinement id used", deWithRefinement(`{"id":"ds_racks","input":"ds_racks","ops":[]}`), "mapping.refinement.id_in_use", "/refinements/0/id"),
		deMapCase("operation column", op(`{"op":"filter","column":"zz","not_empty":true}`), "mapping.refinement.op.unknown_column", "/refinements/0/ops/0/column"),
		deMapCase("filter without a test", op(`{"op":"filter","column":"asset_id"}`), "mapping.refinement.op.filter_without_test", "/refinements/0/ops/0/op"),
		deMapCase("fill without a value", op(`{"op":"fill_empty","column":"asset_id"}`), "mapping.refinement.op.fill_without_value", "/refinements/0/ops/0/value"),
		deMapCase("map without a map", op(`{"op":"replace","column":"asset_id","function":"map"}`), "mapping.refinement.op.map_empty", "/refinements/0/ops/0/map"),
		deMapCase("function", op(`{"op":"replace","column":"asset_id","function":"reverse"}`), "mapping.refinement.op.function_unsupported", "/refinements/0/ops/0/function"),
		deMapCase("operation", op(`{"op":"sort","column":"asset_id"}`), "mapping.refinement.op.unsupported", "/refinements/0/ops/0/op"),
		deMapCase("no feeds", `{"feeds":[]}`, "mapping.feeds.empty", "/feeds"),
		deMapCase("feed twice", deSub(deMapBase, `{"id":"f_vm_host",`, `{"id":"f_rack",`), "mapping.feed.id_duplicate", "/feeds/4/id"),
		deMapCase("feed dataset", deSub(deMapBase, `"dataset":"ds_hosts"`, `"dataset":"ds_hostz"`), "mapping.feed.unknown_dataset", "/feeds/3/dataset"),
		deMapCase("object type", deSub(deMapBase, `"object_type":"HypervisorHost"`, `"object_type":"HypervisorHst"`), "mapping.feed.unknown_object_type", "/feeds/3/object_type"),
		deMapCase("basis", deSub(deMapBase, `"creates_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"host_name"}]`, `"creates_objects":true,"basis":"guess",
  "attributes":[{"attribute":"name","column":"host_name"}]`), "mapping.feed.invalid_basis", "/feeds/3/basis"),
		deMapCase("retires without creates", deSub(deMapBase, `"object_type":"HypervisorHost","creates_objects":true`, `"object_type":"HypervisorHost","retires_objects":true`), "mapping.feed.retires_without_creates", "/feeds/3/retires_objects"),
		deMapCase("attribute", deSub(deMapBase, `{"attribute":"name","column":"host_name"}`, `{"attribute":"nme","column":"host_name"}`), "mapping.attribute.unknown", "/feeds/3/attributes/0/attribute"),
		deMapCase("attribute column", deSub(deMapBase, `{"attribute":"name","column":"host_name"}`, `{"attribute":"name","column":"host_nam"}`), "mapping.attribute.unknown_column", "/feeds/3/attributes/0/column"),
		deMapCase("identity empty", deSub(deMapBase, `"identity":[{"scheme":"host_id","column":"host_id","priority":1}]`, `"identity":[]`), "mapping.identity.empty", "/feeds/3/identity"),
		deMapCase("identity form", deSub(deMapBase, `{"scheme":"host_id","column":"host_id","priority":1}`, `{"priority":1}`), "mapping.identity.scheme_or_components", "/feeds/3/identity/0"),
		deMapCase("identity scheme", deSub(deMapBase, `"scheme":"host_id"`, `"scheme":"host_idd"`), "mapping.identity.unknown_scheme", "/feeds/3/identity/0/scheme"),
		deMapCase("identity column", deSub(deMapBase, `"scheme":"host_id","column":"host_id"`, `"scheme":"host_id","column":"hostid"`), "mapping.identity.unknown_column", "/feeds/3/identity/0/column"),
		deMapCase("scheme is no key", deSub(deMapBase, `"scheme":"host_id","column":"host_id","priority":1`, `"scheme":"vm_instance_uuid","column":"host_id","priority":1`), "mapping.identity.scheme_not_a_key", "/feeds/3/identity/0/scheme"),
		deMapCase("identity property", deSub(deMapBase, `{"property":"asset_tag","column":"asset_id"}]`, `{"property":"asset_tg","column":"asset_id"}]`), "mapping.identity.unknown_property", "/feeds/0/identity/0/components/0/property"),
		deMapCase("identity component column", deSub(deMapBase, `"components":[{"property":"asset_tag","column":"asset_id"}]`, `"components":[{"property":"asset_tag","column":"asset_idx"}]`), "mapping.identity.component_unknown_column", "/feeds/0/identity/0/components/0/column"),
		deMapCase("identity without such key", deSub(deMapBase, `{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}`, `{"property":"serial_number","column":"serial"},{"property":"manufacturer","column":"manufacturer"}`), "mapping.identity.no_such_key", "/feeds/1/identity/0/components"),
		deMapCase("identifier scheme", deSub(deMapBase, `"identity":[{"scheme":"host_id","column":"host_id","priority":1}]}`, `"identity":[{"scheme":"host_id","column":"host_id","priority":1}],"identifiers":[{"scheme":"nope","column":"host_id"}]}`), "mapping.identifier.unknown_scheme", "/feeds/3/identifiers/0/scheme"),
		deMapCase("identifier column", deSub(deMapBase, `"identity":[{"scheme":"host_id","column":"host_id","priority":1}]}`, `"identity":[{"scheme":"host_id","column":"host_id","priority":1}],"identifiers":[{"scheme":"host_id","column":"nope"}]}`), "mapping.identifier.unknown_column", "/feeds/3/identifiers/0/column"),
		deMapCase("link type", deSub(deMapBase, `"link_type":"MOUNTED_IN"`, `"link_type":"MOUNTED_NI"`), "mapping.link.unknown_link_type", "/links/0/link_type"),
		deMapCase("condition", deSub(deMapBase, `"condition":"same_key"`, `"condition":"contains"`), "mapping.link.condition_unsupported", "/links/0/condition"),
		deMapCase("link feed", deSub(deMapBase, `"from":{"feed":"f_server"`, `"from":{"feed":"f_serverz"`), "mapping.link.unknown_feed", "/links/0/from/feed"),
		deMapCase("binding", deSub(deMapBase, `"link_type":"MOUNTED_IN"`, `"link_type":"COOLED_BY"`), "mapping.link.binding_not_allowed", "/links/0/link_type"),
		deMapCase("key columns", deSub(deMapBase, `"from":{"feed":"f_server","columns":["rack_asset_id"]}`, `"from":{"feed":"f_server","columns":[]}`), "mapping.link.key_columns_mismatch", "/links/0/from/columns"),
		deMapCase("key column", deSub(deMapBase, `"columns":["rack_asset_id"]`, `"columns":["rack_asset_idd"]`), "mapping.link.unknown_column", "/links/0/from/columns/0"),
		deMapCase("link basis", deSub(deMapBase, `"condition":"same_key","basis":"declared"`, `"condition":"same_key","basis":"x"`), "mapping.link.invalid_basis", "/links/0/basis"),
		deMapCase("key type", deSub(deMapBase, `"condition":"same_key","basis":"declared"`, `"condition":"same_key","basis":"declared","key_type":"nope"`), "mapping.link.unknown_key_type", "/links/0/key_type"),
		deMapCase("link attribute", deWithLinkAttrs(`{"attribute":"positon_u","column":"position_u"}`), "mapping.link.attribute.unknown", "/links/0/attributes/0/attribute"),
		deMapCase("link attribute twice", deWithLinkAttrs(laPos+","+laPos), "mapping.link.attribute.duplicate", "/links/0/attributes/1/attribute"),
		deMapCase("link attribute both", deWithLinkAttrs(`{"attribute":"face","column":"face","value":"front"}`), "mapping.link.attribute.column_and_value", "/links/0/attributes/0"),
		deMapCase("link attribute neither", deWithLinkAttrs(`{"attribute":"face"}`), "mapping.link.attribute.no_column_or_value", "/links/0/attributes/0"),
		deMapCase("link attribute empty value", deWithLinkAttrs(`{"attribute":"face","value":""}`), "mapping.link.attribute.empty_value", "/links/0/attributes/0/value"),
		deMapCase("link attribute column", deWithLinkAttrs(`{"attribute":"position_u","column":"posn"}`), "mapping.link.attribute.unknown_column", "/links/0/attributes/0/column"),
		deMapCase("link attribute value", deWithLinkAttrs(`{"attribute":"face","value":"top"}`), "mapping.link.attribute.value_mismatch", "/links/0/attributes/0/value"),
	}
}

// deExtraCases are the cases that need a model other than the standard one.
func deExtraCases(t *testing.T) []deCase {
	t.Helper()
	org := orgRegistry(t, deptProp+","+oldYear+`}`)
	withAttr := func(attr string) string {
		return deSub(deMapBase, `{"attribute":"serial_number","column":"serial"}`, `{"attribute":"serial_number","column":"serial"},{"attribute":"`+attr+`","column":"power_state"}`)
	}
	dep := deprecatedStandardAttribute(t, "form_factor", "boot_mode")
	c1 := deMapCase("deprecated attribute", withAttr("form_factor"), "mapping.attribute.deprecated", "/feeds/1/attributes/1/attribute")
	c1.reg = dep
	c2 := deMapCase("organization property in a key", deSub(deMapBase, `{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}`, `{"property":"org.department","column":"manufacturer"},{"property":"serial_number","column":"serial"}`), "mapping.identity.org_property", "/feeds/1/identity/0/components/0/property")
	c2.reg = org
	c3 := deMapCase("attribute of a kind a cell does not give", deSub(deMapBase, `{"attribute":"name","column":"rack_name"}`, `{"attribute":"tags","column":"rack_name"}`), "mapping.attribute.unsupported_kind", "/feeds/0/attributes/0/attribute")
	c4 := deMapCase("link attribute of a kind a cell does not give", deWithLinkAttrs(laFace), "mapping.link.attribute.unsupported_kind", "/links/0/attributes/0/attribute")
	c4.reg = deLinkFaceAsList(t)
	c5 := deMapCase("deprecated link attribute", deWithLinkAttrs(laFace), "mapping.link.attribute.deprecated", "/links/0/attributes/0/attribute")
	c5.reg = deLinkFaceDeprecated(t)
	return []deCase{c1, c2, c3, c4, c5}
}

// deLinkFaceDeprecated is the standard model with the face of MOUNTED_IN deprecated in favour of position_u.
func deLinkFaceDeprecated(t *testing.T) *model.Registry {
	t.Helper()
	base, err := model.StandardDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	defs := base.Clone()
	for i := range defs.LinkTypes {
		props := defs.LinkTypes[i].Properties
		if defs.LinkTypes[i].Name != "MOUNTED_IN" {
			continue
		}
		var next model.PropertyID
		for _, p := range props {
			if p.Name == "position_u" {
				next = p.ID
			}
		}
		for j := range props {
			if props[j].Name == "face" {
				props[j].Status, props[j].SupersededBy = model.StatusDeprecated, next
			}
		}
	}
	reg, errs := model.ComposeRegistry(defs, nil)
	if len(errs) > 0 {
		t.Fatalf("the test model: %v", errs)
	}
	return reg
}

// deLinkFaceAsList is the standard model with the face of MOUNTED_IN made a list, which a cell cannot give.
func deLinkFaceAsList(t *testing.T) *model.Registry {
	t.Helper()
	base, err := model.StandardDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	defs := base.Clone()
	var list model.ValueTypeID
	std, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	for _, vt := range std.ValueTypes() {
		if vt.Base == model.BaseList {
			list = vt.ID
		}
	}
	for i := range defs.LinkTypes {
		if defs.LinkTypes[i].Name != "MOUNTED_IN" {
			continue
		}
		for j := range defs.LinkTypes[i].Properties {
			if defs.LinkTypes[i].Properties[j].Name == "face" {
				defs.LinkTypes[i].Properties[j].ValueType, defs.LinkTypes[i].Properties[j].EnumValues = list, nil
			}
		}
	}
	reg, errs := model.ComposeRegistry(defs, nil)
	if len(errs) > 0 || list.IsZero() {
		t.Fatalf("the test model: %v", errs)
	}
	return reg
}

// deRun gives the errors the case's input gets.
func (c deCase) deRun(t *testing.T) []DefinitionError {
	t.Helper()
	var err error
	if c.unreadable {
		_, err = ParseSource(iotest.ErrReader(errors.New("disk gone")))
	} else if c.doc == DocumentSource {
		_, err = ParseSource(strings.NewReader(c.text))
	} else {
		err = c.deCompile(t)
	}
	de, ok := DefinitionErrorsOf(err)
	if !ok {
		t.Fatalf("%s: no definition errors, got %v", c.name, err)
	}
	return de.List
}

func (c deCase) deCompile(t *testing.T) error {
	reg := c.reg
	if reg == nil {
		var err error
		if reg, err = model.LoadStandard(); err != nil {
			t.Fatal(err)
		}
	}
	src, err := ParseSource(strings.NewReader(laSource))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseMapping(strings.NewReader(c.text))
	if err != nil {
		return err
	}
	_, err = Compile(reg, src, m)
	return err
}

var deCodeName = regexp.MustCompile(`^(source|mapping|definition)(\.[a-z]+(_[a-z]+)*)+$`)

// [REQ-400]
// Every code of the table is given by a case, in the place it names; the codes a check gives are in the table, with the
// kernel code and the keys of the params the table says, and an error is JSON.
func TestEveryCodeOfTheTableIsGivenByACaseAtItsPlace(t *testing.T) {
	cases := append(append(deSourceCases(), deMappingCases()...), deExtraCases(t)...)
	table := map[string]ErrorCode{}
	for _, ec := range ErrorCodes() {
		if _, dup := table[ec.Code]; dup {
			t.Errorf("the code %s is in the table twice", ec.Code)
		}
		if !deCodeName.MatchString(ec.Code) || ec.Meaning == "" || !strings.HasSuffix(ec.Meaning, ".") {
			t.Errorf("the row of %s is no stable name with a one-line meaning: %+v", ec.Code, ec)
		}
		table[ec.Code] = ec
	}
	given := map[string]bool{}
	for _, c := range cases {
		hit := false
		for _, e := range c.deRun(t) {
			row, known := table[e.Code]
			if !known {
				t.Errorf("%s: the code %s is not in the table", c.name, e.Code)
				continue
			}
			given[e.Code] = true
			if e.Code == c.code && e.Pointer == c.at {
				hit = true
			}
			deCheckRow(t, c.name, e, row)
		}
		if !hit {
			t.Errorf("%s: no error with code %s at %q: %+v", c.name, c.code, c.at, c.deRun(t))
		}
	}
	for code := range table {
		if !given[code] {
			t.Errorf("no case gives the code %s", code)
		}
	}
}

func deCheckRow(t *testing.T, name string, e DefinitionError, row ErrorCode) {
	t.Helper()
	if e.KernelCode() != row.Kernel || e.Message == "" || (row.Document != "both" && row.Document != e.Document) {
		t.Errorf("%s: %s: kernel %s, document %s, message %q against the row %+v", name, e.Code, e.KernelCode(), e.Document, e.Message, row)
	}
	for k := range e.Params {
		if !slices.Contains(row.Params, k) {
			t.Errorf("%s: %s: the param %q is not in the row of the table (%v)", name, e.Code, k, row.Params)
		}
	}
	if _, err := json.Marshal(e); err != nil {
		t.Errorf("%s: %s is not JSON: %v", name, e.Code, err)
	}
}

// [REQ-400]
// A definition that cannot be read gives one error with its code, and a reader that fails is told as such.
func TestAnUnreadableDefinitionGivesItsCode(t *testing.T) {
	_, err := ParseMapping(iotest.ErrReader(errors.New("disk gone")))
	de, ok := DefinitionErrorsOf(err)
	if !ok || len(de.List) != 1 || de.List[0].Code != "definition.unreadable" || de.List[0].Document != DocumentMapping || !strings.Contains(err.Error(), "cannot read the mapping definition: disk gone") {
		t.Fatalf("%v", err)
	}
}

func deCompileText(t *testing.T, mapping string) *DefinitionErrors {
	t.Helper()
	c := deCase{doc: DocumentMapping, text: mapping}
	de, ok := DefinitionErrorsOf(c.deCompile(t))
	if !ok {
		t.Fatal("the mapping was accepted")
	}
	return de
}

// [REQ-400]
// Mistakes that have nothing to do with each other are all told at once, in the order of the document, and the same on
// every run; one that comes of another is not told.
func TestUnrelatedMistakesAreAllToldAndConsequencesAreNot(t *testing.T) {
	text := deSub(deMapBase, `"object_type":"HypervisorHost"`, `"object_type":"HypervisorHst"`)
	text = deSub(text, `{"attribute":"total_u","column":"total_u"}`, `{"attribute":"total_u","column":"total"}`)
	text = deSub(text, `"dataset":"ds_cdus"`, `"dataset":"ds_nothing"`)
	text = deSub(text, `"condition":"same_key"`, `"condition":"contains"`)
	want := []string{"mapping.attribute.unknown_column", "mapping.feed.unknown_object_type", "mapping.feed.unknown_dataset", "mapping.link.condition_unsupported"}
	for run := 0; run < 5; run++ {
		var got []string
		for _, e := range deCompileText(t, text).List {
			got = append(got, e.Code)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("run %d: %v, want %v", run, got, want)
		}
	}
	// the feed whose dataset is not found says that and nothing about the columns it reads from it, and the one whose
	// type is not found says that and nothing about its attributes or keys.
	if n := len(deCompileText(t, text).List); n != len(want) {
		t.Fatalf("%d errors", n)
	}
}

// [REQ-400]
// A refinement whose input is refused makes no error in the feed that reads it.
func TestAFeedThatReadsARefusedRefinementIsNotRefusedAgain(t *testing.T) {
	text := deSub(deWithRefinement(`{"id":"r1","input":"nope","ops":[]}`), `"dataset":"ds_hosts"`, `"dataset":"r1"`)
	list := deCompileText(t, text).List
	if len(list) != 1 || list[0].Code != "mapping.refinement.unknown_input" {
		t.Fatalf("%+v", list)
	}
}

// [REQ-400]
// The list has a limit, and says how many it left out.
func TestTheListOfErrorsHasALimitAndSaysHowManyAreLeftOut(t *testing.T) {
	var cols []string
	for i := 0; i < MaxDefinitionErrors+7; i++ {
		cols = append(cols, `{"name":""}`)
	}
	_, err := ParseSource(strings.NewReader(`{"id":"s","adapter":"file","datasets":[{"id":"d","parser":"csv","natural_key":["a"],"columns":[` + strings.Join(cols, ",") + `]}]}`))
	de, ok := DefinitionErrorsOf(err)
	if !ok || len(de.List) != MaxDefinitionErrors || de.Omitted == 0 || !strings.Contains(err.Error(), "more problems are left out") {
		t.Fatalf("%v", err)
	}
}

// [REQ-400]
// One error reads as it always did, and a list of them gives the kernel code of its first; the list comes out of an
// error that wraps it.
func TestTheErrorKeepsTheKernelCodeAndTheSentenceOfTheFirst(t *testing.T) {
	_, err := ParseSource(strings.NewReader(deSub(deSrcOK, `"adapter":"file"`, `"adapter":"ftp"`)))
	if err == nil || err.Error() != `unsupported: source s: adapter "ftp" is not one this build has (only file)` || kernel.CodeOf(err) != kernel.CodeUnsupported {
		t.Fatalf("%v", err)
	}
	mixed := deSub(deSub(deSrcOK, `"adapter":"file"`, `"adapter":"ftp"`), `"natural_key":["a"]`, `"natural_key":["z"]`)
	_, err = ParseSource(strings.NewReader(mixed))
	de, _ := DefinitionErrorsOf(err)
	if len(de.List) != 2 || kernel.CodeOf(err) != kernel.CodeUnsupported || strings.Count(err.Error(), "\n") != 1 {
		t.Fatalf("the first is unsupported, so the code is: %v", err)
	}
	mixed = deSub(deSub(deSrcOK, `"id":"s"`, `"id":""`), `"parser":"csv"`, `"parser":"parquet"`)
	_, err = ParseSource(strings.NewReader(mixed))
	if kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("the first is invalid_value, so the code is: %v", err)
	}
	wrapped := fmt.Errorf("while reading: %w", err)
	if got, ok := DefinitionErrorsOf(wrapped); !ok || len(got.List) != 2 {
		t.Fatalf("%v", wrapped)
	}
}

// [REQ-400]
// The errors of the two documents are joined in the order given, within the limit.
func TestTheErrorsOfTwoDocumentsAreJoined(t *testing.T) {
	_, e1 := ParseSource(strings.NewReader(deSub(deSrcOK, `"id":"s"`, `"id":""`)))
	_, e2 := ParseMapping(strings.NewReader(`{"feed":[]}`))
	joined, _ := DefinitionErrorsOf(JoinDefinitionErrors(e1, nil, e2))
	if len(joined.List) != 2 || joined.List[0].Document != DocumentSource || joined.List[1].Document != DocumentMapping {
		t.Fatalf("%+v", joined)
	}
	if JoinDefinitionErrors(nil, nil) != nil {
		t.Fatal("nothing to join is nothing")
	}
}
