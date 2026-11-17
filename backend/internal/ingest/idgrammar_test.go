package ingest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// idgPlace is one place an id can be in, with the document text that holds the id, and where the error is.
type idgPlace struct {
	name, doc, code, at string
	// text is the document with the id given.
	text func(id string) string
}

func idgPlaces() []idgPlace {
	return []idgPlace{
		{"source", DocumentSource, "source.id.invalid", "/id", func(id string) string { return deSub(deSrcOK, `"id":"s"`, `"id":"`+id+`"`) }},
		{"dataset", DocumentSource, "source.dataset.id_invalid", "/datasets/0/id", func(id string) string { return deSub(deSrcOK, `"id":"d"`, `"id":"`+id+`"`) }},
		{"mapping", DocumentMapping, "mapping.id.invalid", "/id", func(id string) string { return deSub(deMapBase, `{"feeds":[`, `{"id":"`+id+`","feeds":[`) }},
		{"refinement", DocumentMapping, "mapping.refinement.id_invalid", "/refinements/0/id", func(id string) string {
			return deWithRefinement(`{"id":"` + id + `","input":"ds_racks","ops":[]}`)
		}},
		{"feed", DocumentMapping, "mapping.feed.id_invalid", "/feeds/5/id", func(id string) string { return deSub(deMapBase, `{"id":"f_cdu",`, `{"id":"`+id+`",`) }},
		{"link definition", DocumentMapping, "mapping.link.id_invalid", "/links/0/id", func(id string) string { return deSub(deMapBase, `"id":"l_mounted"`, `"id":"`+id+`"`) }},
	}
}

func (p idgPlace) idgErrors(t *testing.T, id string) []DefinitionError {
	t.Helper()
	c := deCase{name: p.name, doc: p.doc, text: p.text(id)}
	var err error
	if p.doc == DocumentSource {
		_, err = ParseSource(strings.NewReader(c.text))
	} else {
		err = c.deCompile(t)
	}
	if err == nil {
		return nil
	}
	de, ok := DefinitionErrorsOf(err)
	if !ok {
		t.Fatalf("%s %q: %v", p.name, id, err)
	}
	return de.List
}

// [REQ-402]
// The grammar of an id: a lower-case letter, then lower-case letters, digits, "_" and "-", up to 64 characters.
func TestIdgGrammarBoundaries(t *testing.T) {
	cases := []struct {
		id string
		ok bool
	}{
		{"a", true}, {"z9", true}, {"a-b_c0", true}, {"ds_racks", true},
		{strings.Repeat("a", 64), true}, {strings.Repeat("a", 65), false},
		{"", false}, {"1a", false}, {"_a", false}, {"-a", false}, {"A", false}, {"aB", false},
		{"a.b", false}, {"a b", false}, {" a", false}, {"a ", false}, {"a/b", false}, {"가나", false}, {"a가", false}, {"a\n", false}, {"a~", false},
	}
	for _, c := range cases {
		if got := ValidID(c.id); got != c.ok {
			t.Errorf("%q: ValidID %v, want %v (%s)", c.id, got, c.ok, IDProblem(c.id))
		}
		if (IDProblem(c.id) == "") != c.ok {
			t.Errorf("%q: IDProblem %q does not agree", c.id, IDProblem(c.id))
		}
	}
}

// [REQ-402]
// In every place an id is, a good id passes and a wrong one is an error at the place of the id, with the code of the place,
// and a sentence that says how to fix it.
func TestIdgEveryPlaceChecksTheGrammarAtItsPointer(t *testing.T) {
	bad := []string{"Big", "a.b", "a b", "한글", "a/b", "1x", "_x", strings.Repeat("a", 65)}
	for _, p := range idgPlaces() {
		for _, id := range []string{"a", "x-1_y", strings.Repeat("b", 64)} {
			if list := p.idgErrors(t, id); len(list) != 0 {
				t.Errorf("%s %q: %+v", p.name, id, list)
			}
		}
		for _, id := range bad {
			list := p.idgErrors(t, id)
			if len(list) != 1 || list[0].Code != p.code || list[0].Pointer != p.at || list[0].Document != p.doc {
				t.Errorf("%s %q: %+v", p.name, id, list)
				continue
			}
			e := list[0]
			if e.KernelCode() != kernel.CodeInvalidValue || e.Params["pattern"] != IDPattern || e.Params["max_length"] != IDMaxLength {
				t.Errorf("%s %q: %+v", p.name, id, e)
			}
			for _, want := range []string{"lower-case letter", "64 characters", IDPattern} {
				if !strings.Contains(e.Message, want) {
					t.Errorf("%s %q: the sentence %q lacks %q", p.name, id, e.Message, want)
				}
			}
		}
	}
}

// [REQ-402]
// A long id is cut in the sentence and in the params, and the sentence tells how long it was.
func TestIdgALongIdIsCutInTheError(t *testing.T) {
	long := strings.Repeat("a", 300)
	e := idgPlaces()[0].idgErrors(t, long)[0]
	if got, _ := e.Params["id"].(string); len([]rune(got)) > idClipAt || len(e.Message) > 400 || !strings.Contains(e.Message, "300 characters") {
		t.Fatalf("%d %s", len(e.Message), e.Message)
	}
}

// [REQ-402]
// An empty id keeps the code that says it is missing; the new code is for an id that is there and wrong. A feed and a
// link definition have a code for the missing one too. The mapping may have no id.
func TestIdgAnEmptyIdIsMissingAndNotInvalid(t *testing.T) {
	cases := []struct {
		place idgPlace
		code  string
	}{
		{idgPlaces()[0], "source.id.missing"},
		{idgPlaces()[1], "source.dataset.id_missing"},
		{idgPlaces()[3], "mapping.refinement.id_missing"},
		{idgPlaces()[4], "mapping.feed.id_missing"},
		{idgPlaces()[5], "mapping.link.id_missing"},
	}
	for _, c := range cases {
		list := c.place.idgErrors(t, "")
		if len(list) != 1 || list[0].Code != c.code || list[0].Pointer != c.place.at {
			t.Errorf("%s: %+v", c.place.name, list)
		}
	}
	if list := idgPlaces()[2].idgErrors(t, ""); len(list) != 0 {
		t.Errorf("a mapping with no id is as it was: %+v", list)
	}
	if _, err := idgCompile(t, laSource, deMapBase); err != nil {
		t.Errorf("a mapping with no id key: %v", err)
	}
}

func idgCompile(t *testing.T, source, mapping string) (*Plan, error) {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	src, err := ParseSource(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseMapping(strings.NewReader(mapping))
	if err != nil {
		t.Fatal(err)
	}
	return Compile(reg, src, m)
}

// [REQ-402]
// The ids that are wrong in several places are all told at once, in the order of the documents.
func TestIdgWrongIdsInManyPlacesAreAllTold(t *testing.T) {
	src := deSub(deSub(deSrcOK, `"id":"s"`, `"id":"S.1"`), `"id":"d"`, `"id":"D 1"`)
	_, err := ParseSource(strings.NewReader(src))
	de, ok := DefinitionErrorsOf(err)
	if !ok || len(de.List) != 2 || de.List[0].Pointer != "/id" || de.List[1].Pointer != "/datasets/0/id" {
		t.Fatalf("%v", err)
	}
	text := deSub(deMapBase, `{"feeds":[`, `{"id":"M.1","refinements":[{"id":"R 1","input":"ds_racks","ops":[]}],"feeds":[`)
	text = deSub(text, `{"id":"f_cdu",`, `{"id":"F-ÜBER",`)
	text = deSub(text, `"id":"l_mounted"`, `"id":"L/1"`)
	var at []string
	for _, e := range deCompileText(t, text).List {
		at = append(at, e.Code+" "+e.Pointer)
	}
	want := "mapping.id.invalid /id|mapping.refinement.id_invalid /refinements/0/id|mapping.feed.id_invalid /feeds/5/id|mapping.link.id_invalid /links/0/id"
	if strings.Join(at, "|") != want {
		t.Fatalf("%v", at)
	}
}

// [REQ-402]
// A wrong id does not make the checks that follow it say more: the refinement, the dataset and the feed with it are still
// found by the name they have.
func TestIdgAWrongIdIsToldOnceAndNotAgainWhereItIsUsed(t *testing.T) {
	text := deSub(deMapBase, `{"feeds":[`, `{"refinements":[{"id":"R1","input":"ds_hosts","ops":[]}],"feeds":[`)
	text = deSub(text, `"dataset":"ds_hosts"`, `"dataset":"R1"`)
	list := deCompileText(t, text).List
	if len(list) != 1 || list[0].Code != "mapping.refinement.id_invalid" {
		t.Fatalf("%+v", list)
	}
}

// idgVersionCase is a document text and what its version gives.
type idgVersionCase struct {
	name, set string
	ok        bool
	code      string
}

// [REQ-402]
// The version of the two documents: none is 1, 1 is read, any other whole number is a version this build does not know,
// and what is no whole number is a wrong kind.
func TestIdgTheVersionOfADocument(t *testing.T) {
	cases := []idgVersionCase{
		{"none", "", true, ""}, {"one", `"version":1,`, true, ""},
		{"two", `"version":2,`, false, "definition.unsupported_version"}, {"zero", `"version":0,`, false, "definition.unsupported_version"},
		{"negative", `"version":-1,`, false, "definition.unsupported_version"},
		{"text", `"version":"1",`, false, "definition.wrong_kind"}, {"fraction", `"version":1.5,`, false, "definition.wrong_kind"},
		{"null", `"version":null,`, true, ""},
	}
	for _, c := range cases {
		for _, doc := range []string{DocumentSource, DocumentMapping} {
			var list []DefinitionError
			if doc == DocumentSource {
				_, err := ParseSource(strings.NewReader(deSub(deSrcOK, `{"id":"s"`, `{`+c.set+`"id":"s"`)))
				list = idgList(err)
			} else {
				list = idgList(deCase{doc: doc, text: deSub(deMapBase, `{"feeds":[`, `{`+c.set+`"feeds":[`)}.deCompile(t))
			}
			if c.ok != (len(list) == 0) || (!c.ok && (len(list) != 1 || list[0].Code != c.code || list[0].Pointer != "/version" || list[0].Document != doc)) {
				t.Errorf("%s %s: %+v", doc, c.name, list)
			}
			if c.code == "definition.unsupported_version" && len(list) == 1 {
				if list[0].KernelCode() != kernel.CodeUnsupported || !strings.Contains(list[0].Message, "reads version 1 only") {
					t.Errorf("%s %s: %+v", doc, c.name, list[0])
				}
			}
		}
	}
}

func idgList(err error) []DefinitionError {
	if err == nil {
		return nil
	}
	de, _ := DefinitionErrorsOf(err)
	return de.List
}

// [REQ-402]
// A version this build does not know is told with the other mistakes of the document, and the version of the mapping and
// its id are kept in the mapping.
func TestIdgTheVersionIsToldWithTheOtherMistakesAndTheMappingKeepsItsId(t *testing.T) {
	_, err := ParseSource(strings.NewReader(deSub(deSub(deSrcOK, `{"id":"s"`, `{"version":3,"id":"S"`), `"parser":"csv"`, `"parser":"parquet"`)))
	list := idgList(err)
	if len(list) != 3 || list[0].Code != "definition.unsupported_version" || list[1].Code != "source.id.invalid" || list[2].Code != "source.dataset.unsupported_parser" {
		t.Fatalf("%+v", list)
	}
	plan, err := idgCompile(t, laSource, deSub(deMapBase, `{"feeds":[`, `{"version":1,"id":"example-mapping","feeds":[`))
	if err != nil || plan.Mapping.ID != "example-mapping" || plan.Mapping.Version == nil || *plan.Mapping.Version != 1 {
		t.Fatalf("%v %+v", err, plan)
	}
}

// [REQ-402]
// A draft the tool makes has the version and ids that follow the grammar, even from a file name that is long, in capitals or
// not Latin, and a sheet name the same way; a name used twice gets a number and stays inside the length.
func TestIdgADraftHasTheVersionAndIdsOfTheGrammar(t *testing.T) {
	csvData := []byte("Asset No,Name\nA-1,x\n")
	fp, err := Inspect(csvData, InspectOptions{Format: FormatCSV}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"/x/My Ledger.CSV", "/x/" + strings.Repeat("Long Name ", 20) + ".csv", "/x/원장.csv", "/x/9 lives.csv", "/x/a.b.c.csv"} {
		d := DraftSource(fp, file, "")
		data, err := json.Marshal(d.Source)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(data), `{"version":1,`) || !ValidID(d.Source.ID) || !ValidID(d.Source.Datasets[0].ID) {
			t.Errorf("%s: %s", file, data)
		}
	}
	used := map[string]bool{}
	long := "ds_" + strings.Repeat("x", 80)
	for i := 0; i < 12; i++ {
		if id := uniqueDefID(long, used); !ValidID(id) {
			t.Fatalf("%q", id)
		}
	}
	if len(used) != 12 {
		t.Fatalf("%d different ids", len(used))
	}
}
