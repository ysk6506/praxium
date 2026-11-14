package ingest

import (
	"reflect"
	"strings"
	"testing"
)

// [REQ-392]
func TestNaturalKeyCandidatesAreTheColumnsFilledInEveryRowAndDifferentInEach(t *testing.T) {
	src, err := ParseSourceLoose(strings.NewReader(`{"id":"s","adapter":"file","datasets":[
 {"id":"ds_a","parser":"csv","delimiter":"semicolon","natural_key":["` + NaturalKeyPlaceholder + `"],"columns":[{"name":"asset"},{"name":"maker","paths":["Maker"]},{"name":"gap"}]},
 {"id":"ds_j","parser":"json_lines","natural_key":["x"],"columns":[{"name":"id"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !src.Datasets[0].HasKeyPlaceholder() || src.Datasets[1].HasKeyPlaceholder() {
		t.Fatal("only the first dataset has the placeholder")
	}
	got, err := NaturalKeyCandidates(src.Datasets[0], []byte("asset;Maker;gap\na1;m;x\na2;m;\na3;n;y\n"), nil)
	if err != nil || !reflect.DeepEqual(got, []string{"asset"}) {
		t.Fatalf("%v %v", got, err)
	}
	if note := KeyCandidatesNote("ds_a", got); !strings.Contains(note, "are: asset") {
		t.Errorf("%s", note)
	}
	if note := KeyCandidatesNote("ds_a", nil); !strings.Contains(note, "no single column") {
		t.Errorf("%s", note)
	}
	if jl, err := NaturalKeyCandidates(src.Datasets[1], []byte(`{"id":"1"}`+"\n"+`{"id":"2"}`+"\n"), nil); err != nil || !reflect.DeepEqual(jl, []string{"id"}) {
		t.Errorf("%v %v", jl, err)
	}
	if _, err := NaturalKeyCandidates(Dataset{Parser: ParserXLSX, Sheet: &SheetOptions{Index: 2}}, []byte("x"), nil); err == nil {
		t.Error("a workbook needs a reader")
	}
	if _, err := ParseSourceLoose(strings.NewReader(`{"nope":1}`)); err == nil {
		t.Error("an unknown field is refused")
	}
}

// [REQ-389]
func TestTheKindsOfValueAreNamedInPlainWords(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{{true, "true or false"}, {"", "text in quotes"}, {3, "a whole number"}, {2.5, "a number"}, {[]string{}, "a list"},
		{struct{}{}, "an object"}, {map[string]int{}, "an object"}, {&struct{}{}, "an object"}, {make(chan int), "another kind"}}
	for _, c := range cases {
		if got := kindWanted(reflect.TypeOf(c.v)); !strings.Contains(got, c.want) {
			t.Errorf("%T: %q", c.v, got)
		}
	}
	if kindWanted(nil) == "" {
		t.Error("no type")
	}
	for v, want := range map[string]string{"string": "text", "bool": "true or false", "array": "a list", "object": "an object", "number": "a number", "other": "other"} {
		if got := kindGiven(v); got != want {
			t.Errorf("%s: %q", v, got)
		}
	}
}

// [REQ-390]
func TestARowIsPlacedByItsSheetRangeOrItsLine(t *testing.T) {
	for r, want := range map[string]Row{"row a|b at S!A1:B1": {Key: "a" + keySep + "b", Where: "S!A1:B1"}, "row k on line 4": {Key: "k", Line: 4}, "row k": {Key: "k"}} {
		if got := rowPlace(want); got != r {
			t.Errorf("%q != %q", got, r)
		}
	}
}
