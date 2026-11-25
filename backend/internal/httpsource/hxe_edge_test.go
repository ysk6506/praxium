package httpsource

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// The edges of the datasets that ask with the values of another dataset (ADR-0111): the values that are not texts, the pages of the
// parent that go wrong, and what a value of the parent is when it is in a place the definition does not expect.

// hxeParent is a helper dataset with the pieces of its definition given.
func hxeParent(extra string) string {
	return `{"id":"ds_ids","collect":false,` + extra + `"request":{"path":"/vm"},"records_path":"items"}`
}

// hxeAnswer is what a source gives for the list at /v1/vm and what is made of the path of a detail for the rest.
func hxeAnswer(list string) func(n int, r hsRequest) hxeReply {
	return func(_ int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			return hxeOK(list)
		}
		return hxeOK(hxeDetailBody(strings.TrimPrefix(r.hsPath(), "/v1/vm/")))
	}
}

// [REQ-910]
// What a unit says of a name it has not, of a text that is no text of a request, and of the values in a body.
func TestHxeAUnitSaysWhatItHasAndAnswersForWhatItHasNot(t *testing.T) {
	u := &unit{names: []string{"id"}, rows: [][]pval{{{text: "5", num: true}}}}
	if u.text("no") != "" || u.value("no") != "" {
		t.Errorf("a name the unit has not: %q %v", u.text("no"), u.value("no"))
	}
	if v, ok := u.value("id").(json.Number); !ok || v != "5" {
		t.Errorf("a number is a number: %#v", u.value("id"))
	}
	if got := u.render("a-{parent.id}-{{x}}"); got != "a-5-{x}" {
		t.Errorf("render %q", got)
	}
	if got := u.render("{oops"); got != "{oops" {
		t.Errorf("a text that cannot be read is kept as it is: %q", got)
	}
	if p, raw := u.path("/v1", "/vm/{oops"); p != "/v1/vm/{oops" || raw != "" {
		t.Errorf("path %q %q", p, raw)
	}
	if got := parentLabel("/vm/{oops"); got != "/vm/{oops" {
		t.Errorf("label %q", got)
	}
	in := []any{"{parent.id}", map[string]any{"k": "{parent.id}"}, "x"}
	out, _ := u.inBody(in).([]any)
	if len(out) != 3 || out[0] != json.Number("5") || out[2] != "x" {
		t.Errorf("a body with a list: %#v", out)
	}
}

// [REQ-910]
// An integer of the parent is sent as the number it is; a number that is not an integer, a boolean, an empty text and a record that has
// no value are not values (the dataset is not complete when they are more than 5% and more than three of what the parent gave).
func TestHxeAnIntegerOfTheParentIsANumberAndNothingElseThatIsNotATextIsAValue(t *testing.T) {
	list := `{"items":[{"id":5},{"id":1.5},{"id":true},{"id":""},{"id":7},{"other":1},{"id":{"a":1}}]}`
	g := hxeNew(t, hxeDoc("", hxeIDs, hxeBatch("")), func(n int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			return hxeOK(list)
		}
		return hxeOK(`{"items":[{"id":5,"name":"a"},{"id":7,"name":"b"}]}`)
	})
	pages, err := g.read("ds_vm")
	if err != nil {
		t.Fatal(err)
	}
	if got := g.srv.seen[1].hsBody; got != `{"ids":[5,7]}` {
		t.Errorf("the body: %s", got)
	}
	if end := hxeEnd(t, pages); end.Complete || !strings.Contains(end.Reason, "5 of 7") {
		t.Errorf("the end: %+v", end)
	}
	g = hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", "")), hxeAnswer(`{"items":[{"id":5},{"id":7}]}`))
	if _, err := g.read("ds_vm"); err != nil || !slices.Equal(g.paths(), []string{"/v1/vm", "/v1/vm/5", "/v1/vm/7"}) {
		t.Errorf("an integer in a path: %v %v", err, g.paths())
	}
}

// [REQ-912]
// A value that is taken from the outer record (^.) is read, and what the parent has no array for is told as what was seen.
func TestHxeAValueOfTheOuterRecordIsReadAndARecordWithNoArrayIsToldAsWhatWasSeen(t *testing.T) {
	parent := `{"id":"ds_ids","collect":false,"request":{"path":"/vm"},"records_path":"hosts[*].disks"}`
	child := `{"id":"ds_vm","parent":{"dataset":"ds_ids","values":{"id":"^.name"}},"request":{"path":"/vm/{parent.id}"},"records_path":"items","natural_key":["id"],"columns":[{"name":"id"}]}`
	for _, c := range []struct{ name, list, want string }{
		{"some", `{"hosts":[{"name":"h1","disks":[{"x":1},{"x":2}]},{"name":"h2","disks":[{"x":1}]},{"name":"h3"}]}`, "1 of its records had no array"},
		{"all", `{"hosts":[{"name":"h1"},{"name":"h2"}]}`, "none of its records had an array"},
	} {
		g := hxeNew(t, hxeDoc("", parent, child), hxeAnswer(c.list))
		pages, err := g.read("ds_vm")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		end := hxeEnd(t, pages)
		if !end.SeenOnly || !strings.Contains(end.Reason, c.want) {
			t.Errorf("%s: the end %+v", c.name, end)
		}
		if c.name == "some" && !slices.Equal(g.paths(), []string{"/v1/vm", "/v1/vm/h1", "/v1/vm/h2"}) {
			t.Errorf("%s: the requests %v", c.name, g.paths())
		}
	}
}

// [REQ-912]
// A parent whose lines say nothing is read as what was seen, and so is the child.
func TestHxeAParentThatHasNoValueAtAllMakesTheChildWhatWasSeen(t *testing.T) {
	parent := `{"id":"ds_ids","collect":false,"response_format":"json_lines","request":{"path":"/vm"}}`
	g := hxeNew(t, hxeDoc("", parent, hxeDetail("", "")), hxeAnswer(""))
	pages, err := g.read("ds_vm")
	if err != nil {
		t.Fatal(err)
	}
	if end := hxeEnd(t, pages); !end.SeenOnly || !strings.Contains(end.Reason, "no value at all") {
		t.Errorf("the end %+v", end)
	}
}

// [REQ-912]
// A parent that cannot be read to its end gives no request of the child, whichever way it goes wrong: more pages than max_pages, the
// same answer for every page, a limit of the dataset, a body that breaks, an answer that is an error, and a value for the next page
// that is too long.
func TestHxeAParentThatCannotBeReadToItsEndGivesNoRequestOfTheChild(t *testing.T) {
	paged := `"pagination":{"kind":"page","page_param":"p","size_param":"s","size":2,"first_page":1,"max_pages":%d},`
	cursor := `"pagination":{"kind":"cursor","cursor_param":"c","cursor_in":"query","next_cursor_path":"next"},`
	two := `{"items":[{"id":"a"},{"id":"b"}]}`
	long := `{"items":[{"id":"a"}],"next":"` + strings.Repeat("x", 9000) + `"}`
	byPage := func(n int, _ hsRequest) hxeReply {
		return hxeOK(fmt.Sprintf(`{"items":[{"id":"a%d"},{"id":"b%d"}]}`, n, n))
	}
	for _, c := range []struct {
		name   string
		parent string
		handle func(n int, r hsRequest) hxeReply
		mut    func(*Config)
		want   string
	}{
		{"max_pages", fmt.Sprintf(paged, 1), byPage, nil, "more than 1 pages"},
		{"the same answer", fmt.Sprintf(paged, 5), func(int, hsRequest) hxeReply { return hxeOK(two) }, nil, "same answer twice"},
		{"a limit", fmt.Sprintf(paged, 9), byPage, func(c *Config) { c.Ceilings.RunRequests = 3 }, "requests"},
		{"a body that breaks", "", func(int, hsRequest) hxeReply { return hxeOK(`{"items":[{"id":"a"}`) }, nil, "ERR"},
		{"an error", "", func(int, hsRequest) hxeReply { return hxeReply{status: 400, body: "no"} }, nil, "ERR"},
		{"a value for the next page that is too long", cursor, func(int, hsRequest) hxeReply { return hxeOK(long) }, nil, "ERR"},
	} {
		g := hxeNew(t, hxeDoc("", hxeParent(c.parent), hxeDetail("", "")), c.handle, c.mut)
		pages, err := g.read("ds_vm")
		for _, p := range g.paths() {
			if strings.Contains(p, "/vm/") {
				t.Errorf("%s: a request of the child: %s", c.name, p)
			}
		}
		switch {
		case c.want == "ERR" && err == nil:
			t.Errorf("%s: no error, the end %+v", c.name, hxeEndOf(pages))
		case c.want != "ERR" && (err != nil || hxeEndOf(pages).Complete || !strings.Contains(hxeEndOf(pages).Reason, c.want)):
			t.Errorf("%s: %v, the end %+v", c.name, err, hxeEndOf(pages))
		}
	}
}

// [REQ-912]
// A read that is cancelled while the parent is read, and while the pages of a child are read, ends with the error of the context.
func TestHxeAReadThatIsCancelledEndsWithTheErrorOfTheContext(t *testing.T) {
	paged := `"pagination":{"kind":"page","page_param":"p","size_param":"s","size":2,"first_page":1},`
	for _, at := range []int{1, 3} {
		ctx, cancel := context.WithCancel(context.Background())
		child := hxeDetail("", `"pagination":{"kind":"page","page_param":"p","size_param":"s","size":1,"first_page":1},`)
		g := hxeNew(t, hxeDoc("", hxeParent(paged), child), func(n int, r hsRequest) hxeReply {
			if n == at {
				cancel()
			}
			if r.hsPath() == "/v1/vm" {
				return hxeOK(fmt.Sprintf(`{"items":[{"id":"a%d"},{"id":"b%d"}]}`, n, n))
			}
			return hxeOK(hxeDetailBody("a"))
		})
		var err error
		for _, e := range g.reader.ReadPages(ctx, "src_hosts", g.dataset("ds_vm"), ingest.PageOptions{}) {
			if e != nil {
				err = e
				break
			}
		}
		cancel()
		if err == nil || ctx.Err() == nil {
			t.Errorf("cancelled at request %d: %v", at, err)
		}
	}
}

// [REQ-914]
// A child whose pages are more than max_pages is not complete.
func TestHxeAChildThatHasMorePagesThanMaxPagesIsNotComplete(t *testing.T) {
	child := hxeDetail("", `"pagination":{"kind":"page","page_param":"p","size_param":"s","size":1,"first_page":1,"max_pages":1},`)
	g := hxeNew(t, hxeDoc("", hxeIDs, child), hxeVM("a", "b"))
	pages, err := g.read("ds_vm")
	if end := hxeEndOf(pages); err != nil || end.Complete || !strings.Contains(end.Reason, "max_pages") {
		t.Errorf("%v, the end %+v", err, end)
	}
}

// [REQ-914]
// A dataset that was not given the numbers the definition fills in (it is made by hand) is read with the defaults, a parent that is
// not a dataset of the source is an error of the product, and a dataset that cannot make rows is its error.
func TestHxeADatasetMadeByHandIsReadWithTheDefaultsAndABadOneIsAnError(t *testing.T) {
	g := hxeNew(t, hxeDoc("", hxeIDs, hxeBatch("")), hxeVM("a", "b", "c"))
	ds := g.dataset("ds_vm")
	p := *ds.Parent
	b := *p.Batch
	p.MaxRequests, p.MaxSeconds, b.Size, b.Separator, p.Batch = 0, 0, 0, "", &b
	ds.Parent = &p
	readOf := func(d ingest.Dataset) error {
		for _, e := range g.reader.ReadPages(context.Background(), "src_hosts", d, ingest.PageOptions{}) {
			if e != nil {
				return e
			}
		}
		return nil
	}
	if err := readOf(ds); err != nil {
		t.Errorf("defaults: %v", err)
	}
	orphan := g.dataset("ds_vm")
	q := *orphan.Parent
	q.Dataset = "ds_nope"
	orphan.Parent = &q
	if err := readOf(orphan); err == nil || !strings.Contains(err.Error(), "parent of the dataset") {
		t.Errorf("a parent that is not there: %v", err)
	}
	bad := g.dataset("ds_vm")
	bad.Columns = []ingest.Column{{Name: "id", Paths: []string{"a["}}}
	if err := readOf(bad); err == nil {
		t.Errorf("a dataset that has a column with a path that is no path")
	}
}

// [REQ-914]
// A preview makes at most ten requests of the child, even when the values do not fit ten of them in a request of the address.
func TestHxeAPreviewStopsAtTenRequestsWhateverTheSizeOfTheValues(t *testing.T) {
	query := `{"id":"ds_vm","parent":{"dataset":"ds_ids","values":{"id":"id"},"batch":{"size":1000,"match":"id"}},` +
		`"request":{"path":"/vm/query","query":{"ids":"{parent.id}"}},"records_path":"items","natural_key":["id"],"columns":[{"name":"id"}]}`
	var ids []string
	for i := 0; i < 200; i++ {
		ids = append(ids, fmt.Sprintf("%03d%s", i, strings.Repeat("v", 240)))
	}
	g := hxeNew(t, hxeDoc("", hxeIDs, query), func(n int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			return hxeOK(hxeList(ids...))
		}
		return hxeOK(`{"items":[]}`)
	})
	pv, err := g.reader.Preview(context.Background(), "src_hosts", g.dataset("ds_vm"), ingest.PreviewOptions{MaxRows: 100, MaxPages: 50})
	if err != nil || len(g.srv.seen) != 11 || !strings.Contains(pv.Note, "10 requests") {
		t.Errorf("%v, %d requests, %q", err, len(g.srv.seen), pv.Note)
	}
}

// [REQ-910]
// The detail that is an object may be broken, or too big, or read for its first record only, and then it is what any answer is.
func TestHxeADetailThatIsAnObjectMayBeBrokenOrTooBigOrReadForItsFirstRecord(t *testing.T) {
	d := `{"id":"ds_vm","parent":{"dataset":"ds_ids","values":{"id":"id"}},"request":{"path":"/vm/{parent.id}"},"natural_key":["id"],"columns":[{"name":"id"}]}`
	for _, c := range []struct{ name, body string }{
		{"broken in a name", `{"id":"a",`},
		{"broken in a value", `{"id":"a","x":`},
		{"not closed", `{"id":"a"`},
		{"too big", `{"id":"a","x":"` + strings.Repeat("z", recordMaxBytes+10) + `"}`},
	} {
		g := hxeNew(t, hxeDoc("", hxeIDs, d), func(n int, r hsRequest) hxeReply {
			if r.hsPath() == "/v1/vm" {
				return hxeOK(hxeList("a"))
			}
			return hxeOK(c.body)
		})
		if _, err := g.read("ds_vm"); err == nil {
			t.Errorf("%s: no error", c.name)
		}
	}
	g := hxeNew(t, hxeDoc("", hxeIDs, d), func(n int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			return hxeOK(hxeList("a", "b"))
		}
		return hxeOK(`{"id":"a"}`)
	})
	if res := g.reader.Check(context.Background(), "src_hosts", g.dataset("ds_vm")); res.Kind != ingest.CheckOK {
		t.Errorf("a check: %+v", res)
	}
}
