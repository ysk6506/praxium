package ingest

import (
	"slices"
	"strings"
	"testing"
)

// The tests of what an independent review of ADR-0109 found in the definitions and in the file of JSON lines. Every name begins
// with hxd.

// [REQ-905]
// What an outer record has at the place of the array is told apart: no key or null is no array, and an outer record that is no
// object, a step that goes through a value of another kind, or an end that is no array is a value of another kind.
func TestHxdWhatARecordHasAtTheArrayIsToldApart(t *testing.T) {
	rp, err := ParseRecordsPath("result[*].net.ifs")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		rec  any
		want ArrayState
	}{
		{"an array", map[string]any{"net": map[string]any{"ifs": []any{1}}}, ArrayPresent},
		{"an empty array", map[string]any{"net": map[string]any{"ifs": []any{}}}, ArrayPresent},
		{"no key", map[string]any{"a": 1}, ArrayMissing},
		{"a step that is null", map[string]any{"net": nil}, ArrayMissing},
		{"null at the end", map[string]any{"net": map[string]any{"ifs": nil}}, ArrayMissing},
		{"a step that is a text", map[string]any{"net": "denied"}, ArrayWrongKind},
		{"a step that is a number", map[string]any{"net": 7}, ArrayWrongKind},
		{"an end that is an object", map[string]any{"net": map[string]any{"ifs": map[string]any{}}}, ArrayWrongKind},
		{"a record that is a text", "denied", ArrayWrongKind},
		{"a record that is null", nil, ArrayWrongKind},
	} {
		if _, got := rp.Elements(c.rec); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// [REQ-907]
// A file of JSON lines that has a records_path is only as complete as the lines it could read: a line that is not JSON, one with a
// value of another kind, an element that is not an object or has no natural key makes the dataset not complete, whatever
// missing_array says; and no line with an array, with some that had none, or records and no row, is only what was seen.
func TestHxdAFileOfJSONLinesThatHadALineItCouldNotReadIsNotComplete(t *testing.T) {
	empty := strings.Replace(hxdFileDS, `"records_path"`, `"missing_array":"empty","records_path"`, 1)
	good := `{"hostid":"h1","ifs":[{"id":"i1"}]}`
	for _, c := range []struct {
		name, ds, body string
		seen           bool
		complete       bool
	}{
		{"a line that is not JSON", hxdFileDS, good + "\n" + `{"hostid":"h2","ifs":`, false, false},
		{"a value of another kind", hxdFileDS, good + "\n" + `{"hostid":"h2","ifs":"denied"}`, false, false},
		{"a value of another kind, with empty", empty, good + "\n" + `{"hostid":"h2","ifs":5}`, false, false},
		{"no line with an array, and one of another kind", empty, `{"hostid":"h2"}` + "\n" + `{"hostid":"h3","ifs":5}`, false, false},
		{"an element that is no object", hxdFileDS, `{"hostid":"h1","ifs":["eth0"]}`, false, false},
		{"an element with no natural key", hxdFileDS, good + "\n" + `{"hostid":"h2","ifs":[{"name":"x"}]}`, false, false},
		{"a line that is a number", hxdFileDS, `17`, false, false},
		{"no line with an array", empty, `{"hostid":"h2"}` + "\n" + `{"hostid":"h3","ifs":null}`, true, false},
		{"records and no row", strings.Replace(hxdFileDS, `"natural_key"`, `"ignore_rows_without_key":true,"natural_key"`, 1), `{"ifs":[{"name":"x"}]}`, true, false},
		{"all whole", hxdFileDS, good + "\n" + `{"hostid":"h2","ifs":[]}`, false, true},
	} {
		_, _, end := hxdLinesOf(t, c.ds, c.body, PageOptions{})
		if end.Complete != c.complete || end.SeenOnly != c.seen {
			t.Errorf("%s: %+v", c.name, end)
		}
	}
}

// [REQ-903]
// A path of a column that begins with ^ is the outer record only where records_path has [*]; without it the text would be read as a
// key that is named ^ and the column would be empty without a word, so it is a definition error that says how to write the key.
// A key column whose first path reads the outer record never reads the element, so it is not a field of the element.
func TestHxdACaretWhereNothingIsOpenedAndAKeyThatReadsTheOuterRecordFirstAreRefused(t *testing.T) {
	plain := hxdHTTP(`{"id":"d","request":{"path":"/a"},"records_path":"result","natural_key":["id"],"columns":[{"name":"id","paths":["^.hostid"]}]}`)
	if got := hxdCodes(t, plain); !slices.Contains(got, "source.path.invalid /datasets/0/columns/0/paths/0") {
		t.Errorf("^ with no [*]: %v", got)
	}
	named := hxdHTTP(`{"id":"d","request":{"path":"/a"},"records_path":"result","natural_key":["id"],"columns":[{"name":"id","paths":["[\"^\"].hostid"]}]}`)
	if got := hxdCodes(t, named); got != nil {
		t.Errorf("a key that is named ^: %v", got)
	}
	first := hxdHTTP(`{"id":"d",` + strings.Replace(hxdOpened, `{"name":"id"}`, `{"name":"id","paths":["^.hostid","id"]}`, 1) + `}`)
	if got := hxdCodes(t, first); !slices.Contains(got, "source.dataset.expand_invalid /datasets/0/natural_key") {
		t.Errorf("a key column that reads the outer record first: %v", got)
	}
}
