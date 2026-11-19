package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-658]
// A path is read by one grammar: dots, positions, and a bracketed name for a key with a dot in it; the two notations kept
// for later are refused as such, and a path that is no path says why.
func TestAdpPathGrammar(t *testing.T) {
	good := map[string][]string{
		"a":                      {"a"},
		"a.b":                    {"a", "b"},
		"a.0.b":                  {"a", "0", "b"},
		`labels["app.name"]`:     {"labels", "app.name"},
		`labels["app.name"].x`:   {"labels", "app.name", "x"},
		`["a.b"]`:                {"a.b"},
		`a["q\"uote"]["back\\"]`: {"a", `q"uote`, `back\`},
	}
	for text, want := range good {
		got, err := ParsePath(text)
		if err != nil || !slices.Equal(got, Path(want)) {
			t.Errorf("%s: %v, %v", text, got, err)
		}
	}
	bad := []string{"", ".a", "a.", "a..b", "a.[", `a["b"`, `a["b"x`, `a[""]`, `a["b\n"]`, "a[0]", "a]b", `a"b`}
	for _, text := range bad {
		_, err := ParsePath(text)
		var pe *PathError
		if !errors.As(err, &pe) || pe.Unsupported || pe.Reason == "" {
			t.Errorf("%q should be refused as no path: %v", text, err)
		}
	}
	for text, notation := range map[string]string{"items[*].name": "[*]", "a[?kind=x]": "[?name=value]"} {
		_, err := ParsePath(text)
		var pe *PathError
		if !errors.As(err, &pe) || !pe.Unsupported || pe.Notation != notation {
			t.Errorf("%q should be refused as a notation that is not read yet: %v", text, err)
		}
	}
}

// [REQ-658]
// A path gives a scalar's text, a value, or an array, and a value that is no scalar, is null or is empty gives no text.
func TestAdpPathLookups(t *testing.T) {
	var doc any
	dec := json.NewDecoder(strings.NewReader(`{"items":[{"id":7,"on":true,"name":"","labels":{"app.name":"web"},"nested":{"x":1},"none":null}],"total":12345678901234567890}`))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	pick := func(p string) (string, bool) {
		path, err := ParsePath(p)
		if err != nil {
			t.Fatal(err)
		}
		return path.Pick(doc)
	}
	for p, want := range map[string]string{"items.0.id": "7", "items.0.on": "true", `items.0.labels["app.name"]`: "web", "total": "12345678901234567890"} {
		if got, ok := pick(p); !ok || got != want {
			t.Errorf("%s: %q %v, want %q", p, got, ok, want)
		}
	}
	for _, p := range []string{"items.0.name", "items.0.nested", "items.0.none", "items.1.id", "items.x", "missing", "total.x"} {
		if got, ok := pick(p); ok {
			t.Errorf("%s should give no text, gave %q", p, got)
		}
	}
	list, ok := Path{"items"}.Array(doc)
	if _, none := (Path{"total"}).Array(doc); !ok || len(list) != 1 || none {
		t.Errorf("array: %v %v %v", list, ok, none)
	}
	if rec, ok := (Path{}).Array([]any{1, 2}); !ok || len(rec) != 2 {
		t.Errorf("the empty path is the value itself: %v %v", rec, ok)
	}
}

const adpLines = `{"serial":"s1","rack":{"name":"r1"},"labels":{"app.name":"web"},"n":1}
{"serial":"s2","rack":{"name":"r2"},"labels":{"app.name":"db"},"n":2.50}
not json
{"serial":"","rack":{"name":"r3"}}
{"rack":{"name":"r4"}}
{"serial":"s5","rack":{"name":""},"labels":{}}
[1,2]
{"serial":"s6","a[0]":"literal"}
`

func adpDataset(parser string) Dataset {
	return Dataset{ID: "d", Parser: parser, NaturalKey: []string{"serial"}, Columns: []Column{
		{Name: "serial", Paths: []string{"serial"}}, {Name: "rack", Paths: []string{"rack.name", "rack.id"}}, {Name: "app", Paths: []string{`labels["app.name"]`}, Optional: true},
		{Name: "n", Paths: []string{"n"}, Optional: true}}}
}

// [REQ-658]
// A file of JSON lines and the builder read a record the same way: the same rows with the same cells, the same unreadable
// lines with the same reasons, and a path with a bracket in it is read by the grammar, one the grammar refuses staying
// the dotted path it was.
func TestAdpRowBuilderReadsARecordAsAJSONLineIs(t *testing.T) {
	ds := adpDataset(ParserJSONLines)
	want := ParseLimited(ds, []byte(adpLines), 0)
	literal := ParseLimited(Dataset{ID: "d", Parser: ParserJSONLines, NaturalKey: []string{"serial"}, Columns: []Column{{Name: "serial", Paths: []string{"serial"}}, {Name: "lit", Paths: []string{"a[0]"}, Optional: true}}}, []byte(adpLines), 0)
	b, err := NewRowBuilder(ds)
	if err != nil {
		t.Fatal(err)
	}
	var got Table
	for i, text := range strings.Split(strings.TrimSpace(adpLines), "\n") {
		var rec any
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		if dec.Decode(&rec) != nil {
			got.Unreadable = append(got.Unreadable, Unreadable{Line: i + 1, Reason: "invalid_json"})
			continue
		}
		one := b.Record(rec, i+1, "")
		got.Rows = append(got.Rows, one.Rows...)
		got.Unreadable = append(got.Unreadable, one.Unreadable...)
	}
	if len(want.Rows) != 4 || len(want.Unreadable) != 4 || !reflect.DeepEqual(got.Rows, want.Rows) {
		t.Fatalf("rows: %+v\nwant %+v", got.Rows, want.Rows)
	}
	if len(got.Unreadable) != len(want.Unreadable) {
		t.Fatalf("unreadable: %+v\nwant %+v", got.Unreadable, want.Unreadable)
	}
	for i, u := range want.Unreadable {
		if g := got.Unreadable[i]; g.Line != u.Line || g.Reason != u.Reason && !(u.Reason == "invalid_json" && g.Reason == ReasonNotAnObject) {
			t.Errorf("unreadable %d: %+v, want %+v", i, g, u)
		}
	}
	if r := want.Rows[0]; r.Fields["app"] != "web" || r.Fields["n"] != "1" || r.Key != "s1" {
		t.Errorf("the bracketed name was not read: %+v", r)
	}
	if r := literal.Rows[len(literal.Rows)-1]; r.Fields["lit"] != "literal" {
		t.Errorf("a path the grammar refuses stays the dotted path it was in a file: %+v", literal.Rows)
	}
}

// [REQ-658]
// The builder tells where a record is when a line number does not, never drops a record that gives no key, and refuses a
// path that is no path.
func TestAdpRowBuilderTellsWhereAndKeepsEveryRecord(t *testing.T) {
	ds := Dataset{ID: "d", NaturalKey: []string{"a", "b"}, IgnoreRowsWithoutKey: true, Columns: []Column{{Name: "a"}, {Name: "b"}}}
	b, err := NewRowBuilder(ds)
	if err != nil {
		t.Fatal(err)
	}
	ok := b.Record(map[string]any{"a": "x", "b": json.Number("2")}, 3, "page 1, record 3")
	if len(ok.Rows) != 1 || ok.Rows[0].Where != "page 1, record 3" || ok.Rows[0].Key != "x"+keySep+"2" || ok.Rows[0].Line != 3 {
		t.Errorf("row: %+v", ok.Rows)
	}
	half := b.Record(map[string]any{"a": "x"}, 4, "page 1, record 4")
	if len(half.Unreadable) != 1 || half.Unreadable[0].Reason != "missing_natural_key" || half.Unreadable[0].Column != "b" || half.Unreadable[0].Where != "page 1, record 4" {
		t.Errorf("half a key: %+v", half.Unreadable)
	}
	if none := b.Record(map[string]any{}, 5, ""); none.Skipped[SkipNoKey] != 1 || none.Received() != 0 {
		t.Errorf("no key at all is passed over when the dataset says so: %+v", none)
	}
	if odd := b.Record("text", 6, "page 1, record 6"); len(odd.Unreadable) != 1 || odd.Unreadable[0].Reason != ReasonNotAnObject {
		t.Errorf("a record that is no object: %+v", odd)
	}
	ds.Columns = append(ds.Columns, Column{Name: "c", Paths: []string{"x[*]"}})
	if _, err := NewRowBuilder(ds); err == nil || kernel.CodeOf(err) != kernel.CodeInvalidValue || !strings.Contains(err.Error(), "not read yet") {
		t.Errorf("a path that is no path: %v", err)
	}
}

// adpPagesOf collects the pages of a sequence, with what each page carries.
func adpPagesOf(t *testing.T, seq iter.Seq2[Page, error]) []Page {
	t.Helper()
	var out []Page
	for p, err := range seq {
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// adpRecords is the rows of n records as JSON lines, and the same as decoded records.
func adpRecords(n int) ([]byte, []any) {
	var buf bytes.Buffer
	recs := make([]any, n)
	for i := range n {
		fmt.Fprintf(&buf, `{"serial":"s%03d","v":"%s"}`+"\n", i, strings.Repeat("x", i%7))
		recs[i] = map[string]any{"serial": fmt.Sprintf("s%03d", i), "v": strings.Repeat("x", i%7)}
	}
	return buf.Bytes(), recs
}

// [REQ-658]
// The writer cuts the records into the pages the reader of a file cuts the same rows into, whatever the size of a page,
// and each page that is not the last carries the token of the position after its last record.
func TestAdpPageWriterCutsAsTheFileReaderDoes(t *testing.T) {
	ds := Dataset{ID: "d", Parser: ParserJSONLines, NaturalKey: []string{"serial"}, Columns: []Column{{Name: "serial", Paths: []string{"serial"}}, {Name: "v", Paths: []string{"v"}, Optional: true}}}
	data, recs := adpRecords(23)
	b, err := NewRowBuilder(ds)
	if err != nil {
		t.Fatal(err)
	}
	for _, opts := range []PageOptions{{MaxRows: 4}, {MaxRows: 1000}, {MaxRows: 100, MaxBytes: 9}, {MaxRows: 5, MaxDatasetRows: 11}} {
		want := adpPagesOf(t, StreamPages(context.Background(), bytes.NewReader(data), ds, opts))
		w := NewPageWriter(ds, opts)
		got := adpPagesOf(t, func(yield func(Page, error) bool) {
			for i, rec := range recs {
				if !w.Add(context.Background(), b.Record(rec, i+1, ""), []byte(fmt.Sprint(i+1)), yield) {
					return
				}
			}
			w.Finish(yield, Completeness{Complete: true})
		})
		if len(got) != len(want) {
			t.Fatalf("%+v: %d pages, want %d", opts, len(got), len(want))
		}
		read := 0
		for i := range want {
			g, wp := got[i], want[i]
			if g.No != wp.No || g.Last != wp.Last || len(g.Rows) != len(wp.Rows) || g.Bytes != wp.Bytes || g.End.Complete != wp.End.Complete {
				t.Errorf("%+v page %d: %+v, want %+v", opts, i, g, wp)
			}
			read += len(g.Rows)
			if !g.Last && string(g.Token) != fmt.Sprint(read) {
				t.Errorf("%+v page %d: the token %q is not the position after its last record (%d)", opts, i, g.Token, read)
			}
			if g.Last && g.Token != nil {
				t.Errorf("the last page has nothing to go on from: %q", g.Token)
			}
		}
	}
}

// [REQ-658]
// A caller that leaves the loop is told to stop reading, and a dataset over its limit ends with a last page that says it
// is not complete.
func TestAdpPageWriterStopsWhenTheCallerDoes(t *testing.T) {
	ds := Dataset{ID: "d", NaturalKey: []string{"k"}, Columns: []Column{{Name: "k"}}}
	b, _ := NewRowBuilder(ds)
	w := NewPageWriter(ds, PageOptions{MaxRows: 1})
	stopped := false
	for i := range 3 {
		if !w.Add(context.Background(), b.Record(map[string]any{"k": fmt.Sprint(i)}, i+1, ""), nil, func(Page, error) bool { return false }) {
			stopped = true
			break
		}
	}
	if !stopped {
		t.Error("the writer went on after the caller left the loop")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w = NewPageWriter(ds, PageOptions{MaxRows: 1})
	var seen error
	w.Add(ctx, b.Record(map[string]any{"k": "a"}, 1, ""), nil, func(Page, error) bool { return true })
	if w.Add(ctx, b.Record(map[string]any{"k": "b"}, 2, ""), nil, func(_ Page, err error) bool { seen = err; return true }) || !errors.Is(seen, context.Canceled) {
		t.Errorf("a canceled context ends the pages with its error: %v", seen)
	}
}

// [REQ-659]
// A source error has the class its kind has, a kind that is not on the list is the product's, and a check and a run tell
// the class the error says.
func TestAdpSourceErrorsHaveTheirClass(t *testing.T) {
	kinds := SourceKinds()
	want := map[CheckKind][]string{
		CheckConfig:    {SrcPolicyDenied, SrcNameNotFound, SrcTLSRejected, SrcAuthFailed, SrcRequestRejected, SrcResponseFormat, SrcSecretUnusable},
		CheckTransient: {SrcTimeout, SrcConnectionLost, SrcRateLimited, SrcSourceBusy},
		CheckProduct:   {SrcInternal},
	}
	n := 0
	for class, list := range want {
		for _, k := range list {
			n++
			e := NewSourceError(k, "words", nil)
			if kinds[k] != class || e.Class != class || ClassifyCheckError(e) != class || e.Temporary() != (class == CheckTransient) {
				t.Errorf("%s: class %s, want %s", k, e.Class, class)
			}
			wrapped := fmt.Errorf("while reading: %w", e)
			if se, ok := SourceErrorOf(wrapped); !ok || se != e || ClassifyCheckError(wrapped) != class {
				t.Errorf("%s: not found in a wrapped error", k)
			}
		}
	}
	if n != len(kinds) {
		t.Errorf("the kinds are %d and the test knows %d", len(kinds), n)
	}
	if e := NewSourceError("made up", "words", nil); e.Class != CheckProduct || e.Kind != SrcInternal {
		t.Errorf("a kind that is not on the list: %+v", e)
	}
	// A source error wins over what its inner error would be classed as: a deadline inside an auth failure is the source's word.
	if e := NewSourceError(SrcAuthFailed, "refused", context.DeadlineExceeded); ClassifyCheckError(e) != CheckConfig {
		t.Errorf("class of %v: %s", e, ClassifyCheckError(e))
	}
}

// [REQ-659]
// What a source error writes out is its message: neither its error underneath nor its detail is in Error, in the verbs
// of fmt, or in the result of a check, and the error is still there for errors.Is.
func TestAdpSourceErrorsWriteOnlyTheirMessage(t *testing.T) {
	const leak = "token-abc-123"
	inner := kernel.Errorf(kernel.CodeInvalidValue, "GET https://h.example.test/x?key=%s refused", leak)
	e := NewSourceError(SrcAuthFailed, "the source refused the credentials", inner).WithDetail("detail " + leak)
	if !errors.Is(e, inner) || e.Detail == "" {
		t.Fatal("the error underneath and the detail are kept")
	}
	for _, text := range []string{e.Error(), fmt.Sprintf("%v", e), fmt.Sprintf("%+v", e), fmt.Sprintf("%#v", e), fmt.Sprintf("%s", e), fmt.Sprint(e), fmt.Sprintf("%v", fmt.Errorf("x: %w", e))} {
		if strings.Contains(text, leak) || !strings.Contains(text, "the source refused the credentials") {
			t.Errorf("%q", text)
		}
	}
	// The words of a check are made by plainMessage, which would write the kernel error underneath a wrapped one.
	if got := plainMessage(e); got != "the source refused the credentials" || strings.Contains(plainMessage(fmt.Errorf("x: %w", e)), leak) {
		t.Errorf("the words of a check: %q", got)
	}
	plain := NewSourceError(SrcTimeout, "slow", nil)
	if plain.WithDetail("x") == plain || plain.Detail != "" {
		t.Error("WithDetail gives a copy")
	}
}

// [REQ-660]
// A field name is a secret's when it has one of the words, in any case; "key" alone is not one.
func TestAdpSensitiveNames(t *testing.T) {
	for _, n := range []string{"password", "DB_PASSWD", "client_secret", "access_token", "api_key", "X-ApiKey", "apikey", "Authorization", "credentials", "TOKEN"} {
		if !SensitiveName(n) {
			t.Errorf("%s should be a secret's", n)
		}
	}
	for _, n := range []string{"key", "keyboard", "name", "serial", "rack", "passenger"} {
		if SensitiveName(n) {
			t.Errorf("%s should not be", n)
		}
	}
}

// [REQ-660]
// A sample is masked at any depth without changing what it was made from, and says whether it masked anything.
func TestAdpMaskSample(t *testing.T) {
	in := map[string]any{"name": "web", "db_password": "pw-not-a-secret", "nested": map[string]any{"Token": "t", "ok": 1, "list": []any{map[string]any{"apikey": "k", "n": "v"}}}, "tags": []any{"a"}}
	out, masked := MaskSample(in)
	b, _ := json.Marshal(out)
	if !masked || strings.Contains(string(b), "pw-not-a-secret") || strings.Contains(string(b), `"t"`) || strings.Contains(string(b), `"k"`) || !strings.Contains(string(b), `"web"`) || !strings.Contains(string(b), MaskedValue) {
		t.Errorf("%s", b)
	}
	if in["db_password"] != "pw-not-a-secret" || in["nested"].(map[string]any)["Token"] != "t" {
		t.Error("the sample it was given was changed")
	}
	if _, again := MaskSample(map[string]any{"a": []any{"b"}}); again {
		t.Error("nothing to mask")
	}
	p, was := MaskPage(Page{Rows: []Row{{Key: "k", Fields: map[string]string{"serial": "s1", "api_token": "tt", "empty_secret": ""}}}})
	if !was || p.Rows[0].Fields["api_token"] != MaskedValue || p.Rows[0].Fields["serial"] != "s1" || p.Rows[0].Fields["empty_secret"] != "" {
		t.Errorf("page: %+v", p.Rows)
	}
}

// [REQ-660]
// A text has the values of the secrets taken out, the longer first, and an address the server gave loses its query's
// values and its user, and the headers show only the three that are shown.
func TestAdpMaskTextHeadersAndAddresses(t *testing.T) {
	got := MaskText("token abc-def-ghi and abc and also ghi", "abc", "abc-def-ghi", "", "ghi")
	if got != "token *** and *** and also ***" {
		t.Errorf("%q", got)
	}
	if MaskText("nothing here") != "nothing here" {
		t.Error("no secrets")
	}
	if got := MaskServerURL("https://u:p@h.example.test:8443/next?page=2&sig=abc%20def#frag"); got != "https://h.example.test:8443/next?page=***&sig=***" {
		t.Errorf("%s", got)
	}
	if got := MaskServerURL("http://h.example.test/p"); got != "http://h.example.test/p" {
		t.Errorf("%s", got)
	}
	if got := MaskServerURL("://bad"); strings.Contains(got, "bad") {
		t.Errorf("%s", got)
	}
	hs := MaskHeaders(map[string][]string{"Set-Cookie": {"s=1"}, "content-type": {"application/json"}, "Retry-After": {"7"}, "X-Request-Id": {"r"}, "Authorization": {"Bearer z"}})
	var names []string
	for _, h := range hs {
		names = append(names, h.Name)
		shown := h.Value != ""
		if shown == h.Masked || (shown && !slices.Contains([]string{"content-type", "Retry-After"}, h.Name)) {
			t.Errorf("%+v", h)
		}
	}
	if !slices.Equal(names, []string{"Authorization", "content-type", "Retry-After", "Set-Cookie", "X-Request-Id"}) {
		t.Errorf("%v", names)
	}
}

// adpWatcher is an Observer and a Previewer that keeps what it is told.
type adpWatcher struct{ events []Event }

func (w *adpWatcher) Observe(e Event) { w.events = append(w.events, e) }
func (w *adpWatcher) Preview(context.Context, string, Dataset, PreviewOptions) (Preview, error) {
	return Preview{}, nil
}

// [REQ-660]
// An event has the fields a log may hold and no field for a query, a header, a body, a statement, a key value or a secret;
// adding one is a change this test names, and a preview asks for no more than the most.
func TestAdpEventsHaveOnlyWhatALogMayHold(t *testing.T) {
	var names []string
	typ := reflect.TypeOf(Event{})
	for i := range typ.NumField() {
		names = append(names, typ.Field(i).Name)
	}
	want := []string{"Kind", "Source", "Dataset", "Host", "Port", "Method", "Path", "Status", "Attempt", "ElapsedMillis", "Bytes", "Page", "Rows", "Class", "ErrorKind", "Reason"}
	if !slices.Equal(names, want) {
		t.Errorf("the fields of an event are %v", names)
	}
	w := &adpWatcher{}
	var o Observer = ObserverFunc(w.Observe)
	var previewer Previewer = w
	o.Observe(Event{Kind: EventRetry, Attempt: 2})
	if _, err := previewer.Preview(context.Background(), "s", Dataset{}, PreviewOptions{}); err != nil {
		t.Error(err)
	}
	if len(w.events) != 1 || w.events[0].Attempt != 2 {
		t.Errorf("%+v", w.events)
	}
	if o := (PreviewOptions{}).WithDefaults(); o.MaxPages != DefaultPreviewPages || o.MaxRows != DefaultPreviewRows {
		t.Errorf("%+v", o)
	}
	if o := (PreviewOptions{MaxPages: 3, MaxRows: 5000}).WithDefaults(); o.MaxPages != 3 || o.MaxRows != MaxPreviewRows {
		t.Errorf("%+v", o)
	}
	r := CheckResult{Steps: []CheckStep{{Name: "connect", Kind: CheckOK, Message: "ok"}}}
	if len(r.Steps) != 1 {
		t.Error("a check tells item by item")
	}
}
