package ingest

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// The tests of the records_path that opens an array in each record, of the response format and of what a file of JSON lines does
// with them (ADR-0109). Every helper here begins with hxd so that it never meets a name of the older tests of this folder.

func hxdCodes(t *testing.T, doc string) []string {
	t.Helper()
	_, err := ParseAdapterSource(strings.NewReader(doc))
	if err == nil {
		return nil
	}
	de, ok := DefinitionErrorsOf(err)
	if !ok {
		t.Fatalf("not a definition error: %v", err)
	}
	var out []string
	for _, e := range de.List {
		out = append(out, e.Code+" "+e.Pointer)
	}
	return out
}

func hxdHTTP(dataset string) string {
	return `{"version":1,"id":"src_x","adapter":"http","connection":{"base_url":"https://x.example.test","auth":{"kind":"none"}},"datasets":[` + dataset + `]}`
}

func hxdFile(dataset string) string {
	return `{"id":"s","adapter":"file","datasets":[` + dataset + `]}`
}

const hxdOpened = `"request":{"path":"/a"},"records_path":"result[*].ifs","natural_key":["host","id"],"columns":[{"name":"host","paths":["^.hostid"]},{"name":"id"}]`

// [REQ-903]
// The records_path with [*] is read once, with the way to the records that have an array before it and the way to the array after,
// and the forms that are not read are refused with the reason.
func TestHxdRecordsPathWithAStarIsReadOnceAndTheOtherFormsAreRefused(t *testing.T) {
	for _, c := range []struct {
		text  string
		outer Path
		inner Path
		star  bool
	}{
		{"", nil, nil, false},
		{"items", Path{"items"}, nil, false},
		{"result[*].ifs", Path{"result"}, Path{"ifs"}, true},
		{"[*].ifs", nil, Path{"ifs"}, true},
		{`a.b[*]["x.y"].z`, Path{"a", "b"}, Path{"x.y", "z"}, true},
		{`a["[*]"].b`, Path{"a", "[*]", "b"}, nil, false},
	} {
		got, err := ParseRecordsPath(c.text)
		if err != nil || got.Expand != c.star || !slices.Equal(got.Outer, c.outer) || !slices.Equal(got.Inner, c.inner) {
			t.Errorf("%q: %+v, %v", c.text, got, err)
		}
	}
	for _, text := range []string{"a[*].b[*].c", "a[*]", "a[*]b", "a.[*].b", "a[*]..b", "a[?k=v].b", "a["} {
		if _, err := ParseRecordsPath(text); err == nil {
			t.Errorf("%q is read, and it is not a path", text)
		}
	}
	var pe *PathError
	if _, err := ParseRecordsPath("a[?k=v].b"); !errors.As(err, &pe) || !pe.Unsupported {
		t.Errorf("[?...] is still a notation that is not read: %v", err)
	}
}

// [REQ-903]
// A path that begins with ^ reads the record the element is in, once and as the first step; a key that is named ^ is written
// ["^"]. Where the records have no array opened, the text is read as it always was.
func TestHxdACaretIsTheOuterRecordOnlyAsTheFirstStepOfAPathThatOpensAnArray(t *testing.T) {
	for _, c := range []struct {
		text string
		up   bool
		path Path
	}{
		{"^.hostid", true, Path{"hostid"}},
		{`^["a.b"].c`, true, Path{"a.b", "c"}},
		{"hostid", false, Path{"hostid"}},
		{`["^"].x`, false, Path{"^", "x"}},
		{"a.^b", false, Path{"a", "^b"}},
	} {
		got, err := ParseColumnPath(c.text, true)
		if err != nil || got.Up != c.up || !slices.Equal(got.Path, c.path) {
			t.Errorf("%q: %+v, %v", c.text, got, err)
		}
	}
	for _, text := range []string{"^", "^.^.x", "a.^.b", "^.a.^", "a[*].b", "^."} {
		if _, err := ParseColumnPath(text, true); err == nil {
			t.Errorf("%q is read, and it is not a path of a column", text)
		}
	}
	if got, err := ParseColumnPath("^.hostid", false); err != nil || got.Up || !slices.Equal(got.Path, Path{"^", "hostid"}) {
		t.Errorf("without [*] the ^ is a key, as it was: %+v, %v", got, err)
	}
}

// [REQ-903]
// The grammar of a path is not changed by this: [*] is still a notation that is not read in a column, and ^ is a name, in the
// grammar the column paths of every adapter and the old paths of a file of JSON lines are read by.
func TestHxdTheGrammarOfAPathIsLeftAsItWas(t *testing.T) {
	var pe *PathError
	if _, err := ParsePath("a[*].b"); !errors.As(err, &pe) || !pe.Unsupported || pe.Notation != "[*]" {
		t.Errorf("[*] in ParsePath: %v", err)
	}
	if p, err := ParsePath("^.hostid"); err != nil || !slices.Equal(p, Path{"^", "hostid"}) {
		t.Errorf("^ in ParsePath: %v, %v", p, err)
	}
	d := Dataset{ID: "d", Parser: ParserJSONLines, NaturalKey: []string{"a"}, Columns: []Column{{Name: "a", Paths: []string{"x[*].y"}}, {Name: "b", Paths: []string{"^.q"}}}}
	tab := Parse(d, []byte(`{"x[*]":{"y":"1"},"^":{"q":"2"}}`+"\n"))
	if len(tab.Rows) != 1 || tab.Rows[0].Fields["a"] != "1" || tab.Rows[0].Fields["b"] != "2" {
		t.Errorf("the old reading of a file of JSON lines: %+v", tab)
	}
}

// [REQ-900]
// The response format is json or json_lines, and a response of lines has no pages.
func TestHxdTheResponseFormatIsOneOfTwoAndLinesHaveNoPages(t *testing.T) {
	base := `"id":"d","request":{"path":"/a"},"natural_key":["a"],"columns":[{"name":"a"}]`
	for _, c := range []struct {
		name, ds string
		codes    []string
	}{
		{"json", `{` + base + `,"response_format":"json"}`, nil},
		{"json_lines", `{` + base + `,"response_format":"json_lines"}`, nil},
		{"json_lines with a records_path", `{` + base + `,"response_format":"json_lines","records_path":"result"}`, nil},
		{"none of the two", `{` + base + `,"response_format":"xml"}`, []string{"source.response_format_invalid /datasets/0/response_format"}},
		{"lines with pages", `{` + base + `,"response_format":"json_lines","pagination":{"kind":"cursor","cursor_param":"c","cursor_in":"query","next_cursor_path":"n"}}`, []string{"source.response_format_pagination /datasets/0/pagination/kind"}},
		{"lines with none", `{` + base + `,"response_format":"json_lines","pagination":{"kind":"none"}}`, nil},
	} {
		if got := hxdCodes(t, hxdHTTP(c.ds)); !slices.Equal(got, c.codes) {
			t.Errorf("%s: %v, want %v", c.name, got, c.codes)
		}
	}
	pg := `{"id":"d","table":"t","response_format":"json_lines","natural_key":["a"],"columns":[{"name":"a"}]}`
	doc := `{"version":1,"id":"s","adapter":"postgres","connection":{"host":"h.example.test","database":"d","credential":{"secret_id":"k"}},"datasets":[` + pg + `]}`
	if got := hxdCodes(t, doc); !slices.Equal(got, []string{"source.dataset.field_not_for_adapter /datasets/0/response_format"}) {
		t.Errorf("a dataset of a database: %v", got)
	}
}

// [REQ-903]
// What goes with a records_path that opens an array is checked: [*] twice, with nothing after it, missing_array and max_elements
// that are not valid or have no [*], a natural key that reads a place in an array or the outer record only, and a path of a column
// with a ^ that is not the first step.
func TestHxdTheDefinitionOfADatasetThatOpensAnArrayIsChecked(t *testing.T) {
	with := func(from, to string) string {
		return hxdHTTP(`{"id":"d",` + strings.Replace(hxdOpened, from, to, 1) + `}`)
	}
	ok := with("", "")
	if got := hxdCodes(t, ok); got != nil {
		t.Fatalf("the base definition: %v", got)
	}
	for _, c := range []struct {
		name, doc, code string
	}{
		{"two [*]", with(`result[*].ifs`, `result[*].ifs[*].x`), "source.path.invalid /datasets/0/records_path"},
		{"nothing after [*]", with(`result[*].ifs`, `result[*]`), "source.path.invalid /datasets/0/records_path"},
		{"[?]", with(`result[*].ifs`, `result[*].ifs[?k=v]`), "source.path.unsupported /datasets/0/records_path"},
		{"missing_array that is none", with(`"natural_key"`, `"missing_array":"none","natural_key"`), "source.dataset.expand_invalid /datasets/0/missing_array"},
		{"max_elements out of range", with(`"natural_key"`, `"max_elements":2000000,"natural_key"`), "source.dataset.expand_invalid /datasets/0/max_elements"},
		{"max_elements below one", with(`"natural_key"`, `"max_elements":-1,"natural_key"`), "source.dataset.expand_invalid /datasets/0/max_elements"},
		{"missing_array with no [*]", with(`result[*].ifs`, `result.ifs","missing_array":"empty`), "source.dataset.expand_invalid /datasets/0/missing_array"},
		{"max_elements with no [*]", with(`"records_path":"result[*].ifs"`, `"max_elements":5`), "source.dataset.expand_invalid /datasets/0/max_elements"},
		{"a key that reads a place in an array", with(`{"name":"id"}`, `{"name":"id","paths":["ifs.0.id"]}`), "source.dataset.expand_invalid /datasets/0/natural_key/1"},
		{"a key of the outer record only", with(`"natural_key":["host","id"]`, `"natural_key":["host"]`), "source.dataset.expand_invalid /datasets/0/natural_key"},
		{"a ^ in the middle of a path", with(`{"name":"id"}`, `{"name":"id","paths":["a.^.b"]}`), "source.path.invalid /datasets/0/columns/1/paths/0"},
		{"two ^", with(`"^.hostid"`, `"^.^.hostid"`), "source.path.invalid /datasets/0/columns/0/paths/0"},
		{"[*] in a path of a column", with(`{"name":"id"}`, `{"name":"id","paths":["a[*].b"]}`), "source.path.unsupported /datasets/0/columns/1/paths/0"},
	} {
		if got := hxdCodes(t, c.doc); !slices.Contains(got, c.code) {
			t.Errorf("%s: %v, want %s", c.name, got, c.code)
		}
	}
	for _, good := range []string{with(`"natural_key"`, `"missing_array":"empty","max_elements":500,"natural_key"`), with(`"natural_key"`, `"missing_array":"seen_only","natural_key"`),
		with(`{"name":"id"}`, `{"name":"id","paths":["id","ifs_id"]}`)} {
		if got := hxdCodes(t, good); got != nil {
			t.Errorf("%v", got)
		}
	}
}

// [REQ-907]
// A file of JSON lines has a records_path in the flow of the adapters only, and only with the parser json_lines; the flow of the
// files of the command line refuses it as it always did.
func TestHxdAFileOfJSONLinesHasARecordsPathInTheFlowOfTheAdaptersOnly(t *testing.T) {
	ds := `{"id":"d","parser":"json_lines","records_path":"result[*].ifs","natural_key":["host","id"],"columns":[{"name":"host","paths":["^.hostid"]},{"name":"id"}]}`
	if got := hxdCodes(t, hxdFile(ds)); got != nil {
		t.Errorf("the flow of the adapters: %v", got)
	}
	_, err := ParseSource(strings.NewReader(hxdFile(ds)))
	if de, ok := DefinitionErrorsOf(err); !ok || de.List[0].Code != "source.dataset.field_not_for_adapter" {
		t.Errorf("the flow of the files of the command line: %v", err)
	}
	csv := strings.Replace(ds, `"parser":"json_lines"`, `"parser":"csv"`, 1)
	if got := hxdCodes(t, hxdFile(csv)); !slices.Contains(got, "source.dataset.field_not_for_adapter /datasets/0/records_path") {
		t.Errorf("a csv file: %v", got)
	}
	opts := strings.Replace(ds, `"records_path"`, `"missing_array":"empty","max_elements":9,"records_path"`, 1)
	if got := hxdCodes(t, hxdFile(opts)); got != nil {
		t.Errorf("the options: %v", got)
	}
	noPath := `{"id":"d","parser":"json_lines","missing_array":"empty","natural_key":["a"],"columns":[{"name":"a"}]}`
	if got := hxdCodes(t, hxdFile(noPath)); !slices.Contains(got, "source.dataset.expand_invalid /datasets/0/missing_array") {
		t.Errorf("an option with no [*]: %v", got)
	}
}

// [REQ-908]
// The new fields are in the table of ClassifyChange: where the records are and how a response is read is the mapping, and a limit
// of the elements of a record that is raised is a change of the connection, as a lowered one is the mapping.
func TestHxdTheNewFieldsAreClassified(t *testing.T) {
	base := hxdHTTP(`{"id":"d",` + hxdOpened + `}`)
	swap := func(doc, from, to string) string { return strings.Replace(doc, from, to, 1) }
	for _, c := range []struct {
		name         string
		before, next string
		kind         ChangeKind
		reason       string
	}{
		{"missing_array", base, swap(base, `"natural_key"`, `"missing_array":"empty","natural_key"`), ChangeMapping, ""},
		{"the records_path", base, swap(base, `result[*].ifs`, `result[*].addresses`), ChangeMapping, ""},
		{"response_format", base, swap(swap(base, `"natural_key"`, `"response_format":"json_lines","natural_key"`), `"records_path":"result[*].ifs"`, `"records_path":"[*].ifs"`), ChangeMapping, ""},
		{"max_elements raised", swap(base, `"natural_key"`, `"max_elements":10,"natural_key"`), swap(base, `"natural_key"`, `"max_elements":20,"natural_key"`), ChangeConnection, "datasets.d.max_elements"},
		{"max_elements raised over the default", base, swap(base, `"natural_key"`, `"max_elements":500000,"natural_key"`), ChangeConnection, "datasets.d.max_elements"},
		{"max_elements lowered", base, swap(base, `"natural_key"`, `"max_elements":10,"natural_key"`), ChangeMapping, ""},
		{"max_elements given at its default", base, swap(base, `"natural_key"`, `"max_elements":100000,"natural_key"`), ChangeMapping, ""},
	} {
		b, err := ParseAdapterSource(strings.NewReader(c.before))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		a, err := ParseAdapterSource(strings.NewReader(c.next))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got := ClassifyChange(b, a)
		if got.Kind != c.kind || (c.reason != "" && !slices.Equal(got.Reasons, []string{c.reason})) {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}

// hxdLinesOf reads the pages of a file of JSON lines with a records_path, and gives the keys of the rows and how the dataset ended.
func hxdLinesOf(t *testing.T, ds string, body string, opts PageOptions) ([]string, []int, Completeness) {
	t.Helper()
	src, err := ParseAdapterSource(strings.NewReader(hxdFile(ds)))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	var sizes []int
	var end Completeness
	for p, err := range StreamPages(context.Background(), strings.NewReader(body), src.Datasets[0], opts) {
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range p.Rows {
			keys = append(keys, strings.ReplaceAll(r.Key, keySep, "/"))
		}
		sizes = append(sizes, len(p.Rows))
		if p.Last {
			end = p.End
		}
	}
	return keys, sizes, end
}

const hxdFileDS = `{"id":"d","parser":"json_lines","records_path":"[*].ifs","natural_key":["host","id"],"columns":[{"name":"host","paths":["^.hostid"]},{"name":"id"}]}`

// [REQ-907]
// A file of JSON lines whose records_path opens an array gives the elements as rows, a page is cut inside a line, a line with no
// array makes the dataset only what was seen unless the definition says that is none, and a line with more elements than the limit
// makes it not complete.
func TestHxdAFileOfJSONLinesThatOpensAnArray(t *testing.T) {
	body := `{"hostid":"h1","ifs":[{"id":"i1"},{"id":"i2"},{"id":"i3"}]}` + "\n" + `{"hostid":"h2","ifs":[]}` + "\n\n" + `{"hostid":"h3","ifs":[{"id":"i4"}]}`
	keys, sizes, end := hxdLinesOf(t, hxdFileDS, body, PageOptions{MaxRows: 2})
	if want := []string{"h1/i1", "h1/i2", "h1/i3", "h3/i4"}; !slices.Equal(keys, want) || !end.Complete || !slices.Equal(sizes, []int{2, 2}) {
		t.Errorf("keys %v, sizes %v, end %+v", keys, sizes, end)
	}
	missing := body + "\n" + `{"hostid":"h4"}` + "\n" + `{"hostid":"h5","ifs":null}`
	keys, _, end = hxdLinesOf(t, hxdFileDS, missing, PageOptions{})
	if len(keys) != 4 || end.Complete || !end.SeenOnly || !strings.HasPrefix(end.Reason, "2 lines") {
		t.Errorf("a line with no array: keys %v, end %+v", keys, end)
	}
	empty := strings.Replace(hxdFileDS, `"records_path"`, `"missing_array":"empty","records_path"`, 1)
	if keys, _, end = hxdLinesOf(t, empty, missing, PageOptions{}); len(keys) != 4 || !end.Complete || end.SeenOnly {
		t.Errorf("missing_array empty: keys %v, end %+v", keys, end)
	}
	limited := strings.Replace(hxdFileDS, `"records_path"`, `"max_elements":2,"records_path"`, 1)
	if keys, _, end = hxdLinesOf(t, limited, body, PageOptions{}); end.Complete || end.SeenOnly || !strings.Contains(end.Reason, "max_elements") {
		t.Errorf("more elements than the limit: keys %v, end %+v", keys, end)
	}
	notArray := `{"hostid":"h1","ifs":{"id":"i1"}}`
	if keys, _, end = hxdLinesOf(t, hxdFileDS, notArray, PageOptions{}); len(keys) != 0 || !end.Complete {
		t.Errorf("a value that is no array is an unreadable line: keys %v, end %+v", keys, end)
	}
}

// [REQ-907]
// A file of JSON lines whose records_path has no [*] gives the record at that place of each line, and one with no records_path is
// read as it always was.
func TestHxdAFileOfJSONLinesWithARecordsPathOfOneRecord(t *testing.T) {
	ds := `{"id":"d","parser":"json_lines","records_path":"result","natural_key":["id"],"columns":[{"name":"id"}]}`
	keys, _, end := hxdLinesOf(t, ds, `{"result":{"id":"a"}}`+"\n"+`{"nothing":1}`+"\n"+`{"result":{"id":"b"}}`, PageOptions{})
	if !slices.Equal(keys, []string{"a", "b"}) || !end.Complete {
		t.Errorf("keys %v, end %+v", keys, end)
	}
	old := `{"id":"d","parser":"json_lines","natural_key":["id"],"columns":[{"name":"id","paths":["a.b"]}]}`
	if keys, _, end = hxdLinesOf(t, old, `{"a":{"b":"x"}}`+"\n", PageOptions{}); !slices.Equal(keys, []string{"x"}) || !end.Complete {
		t.Errorf("no records_path: keys %v, end %+v", keys, end)
	}
}

// [REQ-907]
// The reading of a capture of the flow of the files of the command line does not read a records_path: a line is a record, so no
// record that has an array in it is read as nothing and left to be taken for a deletion.
func TestHxdTheOldReadingOfAFileIgnoresARecordsPath(t *testing.T) {
	d := Dataset{ID: "d", Parser: ParserJSONLines, RecordsPath: "result[*].ifs", NaturalKey: []string{"id"}, Columns: []Column{{Name: "id", Paths: []string{"id"}}}}
	tab := Parse(d, []byte(`{"hostid":"h1","ifs":[{"id":"i1"}]}`+"\n"))
	if len(tab.Rows) != 0 || len(tab.Unreadable) != 1 {
		t.Errorf("rows %+v, unreadable %+v", tab.Rows, tab.Unreadable)
	}
}
