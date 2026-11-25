package httpsource

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// [REQ-910]
// A dataset that asks with the values of another is read in two steps: the parent to its end first (no request of the child is sent
// while a body of the parent is open), then one request for each value without repeats. The events and the exchanges say the path
// as the definition writes it, never the value.
func TestHxeAListThenADetailForEachIdIsReadInTwoSteps(t *testing.T) {
	g := hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", "")), hxeVM("a", "b", "a", "c"))
	pages, err := g.read("ds_vm")
	if err != nil {
		t.Fatal(err)
	}
	if got := hxeIDsOf(pages); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("rows %v", got)
	}
	if end := hxeEnd(t, pages); !end.Complete {
		t.Errorf("the end: %+v", end)
	}
	if got := g.paths(); !slices.Equal(got, []string{"/v1/vm", "/v1/vm/a", "/v1/vm/b", "/v1/vm/c"}) {
		t.Errorf("requests %v", got)
	}
	if slices.Max(g.srv.openAt) != 0 {
		t.Errorf("a request was sent while a body was open: %v", g.srv.openAt)
	}
	for _, e := range g.events {
		if e.Kind == ingest.EventRequest && strings.Contains(e.Path, "/vm/") && e.Path != "/v1/vm/{parent.id}" {
			t.Errorf("the event says a path with a value in it: %q", e.Path)
		}
	}
	for _, p := range pages {
		if len(p.Token) != 0 {
			t.Errorf("a child gives no token: %q", p.Token)
		}
	}
}

// [REQ-910]
// A value is escaped once, as one step of the path: the bytes the source receives are those of the table of the review (X3).
func TestHxeAValueInThePathIsEscapedOnce(t *testing.T) {
	ids := []string{"a b", "a%b", "a;b", "a,b", "a?b#c", "a+b=c", "a:b@c"}
	g := hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", "")), hxeVM(ids...))
	pages, err := g.read("ds_vm")
	if err != nil {
		t.Fatal(err)
	}
	if got := hxeIDsOf(pages); !slices.Equal(got, ids) {
		t.Errorf("the source took the values for others: %v", got)
	}
	want := []string{"/v1/vm", "/v1/vm/a%20b", "/v1/vm/a%25b", "/v1/vm/a%3Bb", "/v1/vm/a%2Cb", "/v1/vm/a%3Fb%23c", "/v1/vm/a+b=c", "/v1/vm/a:b@c"}
	if got := g.paths(); !slices.Equal(got, want) {
		t.Errorf("the bytes of the paths:\n got %v\nwant %v", got, want)
	}
	u := &unit{names: []string{"id"}, rows: [][]pval{{{text: "ä"}}}}
	if p, raw := u.path("/v1", "/vm/{parent.id}"); p != "/v1/vm/ä" || raw != "/v1/vm/%C3%A4" {
		t.Errorf("a letter that is not ASCII: %q %q", p, raw)
	}
}

// [REQ-910]
// A value that could be read as another place, or as more than one value, is not sent: no request is made for it, and the dataset is
// read as what was seen. The value is in no event.
func TestHxeAValueThatCouldBeAnotherPlaceIsNotSent(t *testing.T) {
	bad := []string{"..", ".", "a/b", "%2f", "%2F", "a%2eb", "a\\b", "x\u0001y", "．．", strings.Repeat("a", 257), "ä", "x\ty"}
	for _, b := range bad {
		ids := []string{b}
		for i := 0; i < 39; i++ {
			ids = append(ids, fmt.Sprintf("ok%02d", i))
		}
		g := hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", "")), hxeVM(ids...))
		pages, err := g.read("ds_vm")
		if err != nil {
			t.Fatalf("%q: %v", b, err)
		}
		end := hxeEnd(t, pages)
		if end.Complete || !end.SeenOnly || !strings.Contains(end.Reason, "passed over") {
			t.Errorf("%q: the end %+v", b, end)
		}
		if len(g.srv.seen) != 40 || len(hxeIDsOf(pages)) != 39 {
			t.Errorf("%q: %d requests and %d rows", b, len(g.srv.seen), len(hxeIDsOf(pages)))
		}
		skipped := 0
		for _, e := range g.events {
			if e.Kind == ingest.EventSkipped {
				skipped += e.Rows
			}
			if strings.Contains(e.Reason, b) && len(b) > 3 {
				t.Errorf("%q: an event says the value", b)
			}
		}
		if skipped != 1 {
			t.Errorf("%q: the event says %d values were not used", b, skipped)
		}
	}
}

// [REQ-911]
// A 404 for a value is, by default, a dataset that is not complete. With on_not_found skip it is read as what was seen, and when the
// values passed over (404 and values that are not sent together) are more than 5% and more than three it is not complete again.
func TestHxeA404IsNotCompleteUnlessSkippedAndTooManySkippedIsNotComplete(t *testing.T) {
	ids := func(n int) []string {
		var out []string
		for i := 0; i < n; i++ {
			out = append(out, fmt.Sprintf("v%02d", i))
		}
		return out
	}
	missing := func(gone ...string) func(n int, r hsRequest) hxeReply {
		return func(n int, r hsRequest) hxeReply {
			for _, id := range gone {
				if r.hsPath() == "/v1/vm/"+id {
					return hxeReply{status: 404, body: "{}"}
				}
			}
			return hxeVM(ids(40)...)(n, r)
		}
	}
	for _, c := range []struct {
		name       string
		extra      string
		gone       []string
		complete   bool
		seenOnly   bool
		rows       int
		reasonHave string
	}{
		{"default", "", []string{"v05"}, false, false, 5, "child_not_found"},
		{"skip, one of forty", `"on_not_found":"skip",`, []string{"v05"}, false, true, 39, "passed over"},
		{"skip, four of forty", `"on_not_found":"skip",`, []string{"v05", "v06", "v07", "v08"}, false, false, 0, "more than 5%"},
		{"no 404", `"on_not_found":"skip",`, nil, true, false, 40, ""},
	} {
		g := hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", c.extra)), missing(c.gone...))
		pages, err := g.read("ds_vm")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		end := hxeEnd(t, pages)
		if end.Complete != c.complete || end.SeenOnly != c.seenOnly || !strings.Contains(end.Reason, c.reasonHave) {
			t.Errorf("%s: the end %+v", c.name, end)
		}
		if c.name != "default" && len(hxeIDsOf(pages)) != c.rows && c.rows > 0 {
			t.Errorf("%s: %d rows, want %d", c.name, len(hxeIDsOf(pages)), c.rows)
		}
	}
	// Three of twenty is 15% and not more than three: the dataset is read as what was seen.
	g := hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", `"on_not_found":"skip",`)), func(n int, r hsRequest) hxeReply {
		if strings.HasSuffix(r.hsPath(), "v01") || strings.HasSuffix(r.hsPath(), "v02") || strings.HasSuffix(r.hsPath(), "v03") {
			return hxeReply{status: 404, body: "{}"}
		}
		return hxeVM(ids(20)...)(n, r)
	})
	pages, err := g.read("ds_vm")
	if end := hxeEnd(t, pages); err != nil || !end.SeenOnly {
		t.Errorf("three of twenty: %+v %v", end, err)
	}
	// Two values that are not sent and two that are gone are four together.
	g = hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", `"on_not_found":"skip",`)), func(n int, r hsRequest) hxeReply {
		if strings.HasSuffix(r.hsPath(), "v03") || strings.HasSuffix(r.hsPath(), "v04") {
			return hxeReply{status: 404, body: "{}"}
		}
		if r.hsPath() == "/v1/vm" {
			return hxeOK(hxeList(append(ids(38), "..", "a/b")...))
		}
		return hxeVM(ids(40)...)(n, r)
	})
	pages, _ = g.read("ds_vm")
	if end := hxeEnd(t, pages); end.Complete || end.SeenOnly || !strings.Contains(end.Reason, "4 of 40") {
		t.Errorf("the two kinds together: %+v", end)
	}
}

// [REQ-912]
// A parent that has more values than the dataset may ask for, a parent that is not complete and a parent that answers 404 are
// datasets that are not complete, and no request of the child is sent.
func TestHxeAParentThatCannotBeUsedSendsNoRequestOfTheChild(t *testing.T) {
	for _, c := range []struct {
		name, doc string
		handle    func(n int, r hsRequest) hxeReply
		reason    string
	}{
		{"more values than max_requests", hxeDoc("", hxeIDs, hxeDetail(`,"max_requests":2`, "")), hxeVM("a", "b", "c"), "more distinct values"},
		{"the parent answers 404", hxeDoc("", hxeIDs, hxeDetail("", "")), func(int, hsRequest) hxeReply { return hxeReply{status: 404, body: "{}"} }, "parent answered 404"},
		{"the parent ends short of its total", hxeDoc("", `{"id":"ds_ids","collect":false,"request":{"path":"/vm"},"records_path":"items","pagination":{"kind":"page","page_param":"p","size_param":"n","size":5,"first_page":1,"total_path":"total"}}`, hxeDetail("", "")),
			func(_ int, r hsRequest) hxeReply {
				if r.hsQuery().Get("p") == "1" {
					return hxeOK(`{"items":[{"id":"a"}],"total":9}`)
				}
				return hxeOK(`{"items":[],"total":9}`)
			}, "not read to its end"},
	} {
		g := hxeNew(t, c.doc, c.handle)
		pages, err := g.read("ds_vm")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		end := hxeEnd(t, pages)
		if end.Complete || end.SeenOnly || !strings.Contains(end.Reason, c.reason) {
			t.Errorf("%s: the end %+v", c.name, end)
		}
		for _, p := range g.paths() {
			if strings.HasPrefix(p, "/v1/vm/") {
				t.Errorf("%s: a request of the child: %s", c.name, p)
			}
		}
	}
}

// [REQ-913]
// A batch asks for many values in one request. The answer says by match which values it had records for; one that it had none for
// is a value that was not seen, and the dataset is read as what was seen. A value that has the separator in it is not sent, and a
// batch is cut so that the address and the body stay under their limits.
func TestHxeABatchIsCutAndWhatTheAnswerLeavesOutIsNotSeen(t *testing.T) {
	g := hxeNew(t, hxeDoc("", hxeIDs, hxeBatch("")), func(n int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			return hxeOK(hxeList("a", "b", "c", "d", "e"))
		}
		left := strings.NewReplacer(`{"ids":[`, "", `]}`, "", `"`, "").Replace(r.hsBody)
		var items []string
		for _, id := range strings.Split(left, ",") {
			if id != "b" {
				items = append(items, fmt.Sprintf(`{"id":%q,"name":"n"}`, id))
			}
		}
		return hxeOK(`{"items":[` + strings.Join(items, ",") + `]}`)
	})
	pages, err := g.read("ds_vm")
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, r := range g.srv.seen[1:] {
		bodies = append(bodies, r.hsBody)
	}
	if want := []string{`{"ids":["a","b"]}`, `{"ids":["c","d"]}`, `{"ids":["e"]}`}; !slices.Equal(bodies, want) {
		t.Errorf("bodies %v", bodies)
	}
	end := hxeEnd(t, pages)
	if end.Complete || !end.SeenOnly || !strings.Contains(end.Reason, "1 values of a batch had no record") {
		t.Errorf("the end %+v", end)
	}
	if got := hxeIDsOf(pages); !slices.Equal(got, []string{"a", "c", "d", "e"}) {
		t.Errorf("rows %v", got)
	}
	// A 404 for a batch is never passed over.
	g = hxeNew(t, hxeDoc("", hxeIDs, strings.Replace(hxeBatch(""), `"post_reads"`, `"on_not_found":"skip","post_reads"`, 1)), func(n int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			return hxeOK(hxeList("a", "b"))
		}
		return hxeReply{status: 404, body: "{}"}
	})
	pages, _ = g.read("ds_vm")
	if end := hxeEnd(t, pages); end.Complete || end.SeenOnly || !strings.Contains(end.Reason, "batch") {
		t.Errorf("404 for a batch: %+v", end)
	}
}

// [REQ-913]
// A batch in the query is joined with the separator, a value with the separator in it is not sent, and the address of a request is
// never longer than the limit: the batch is cut before.
func TestHxeABatchInTheQueryIsJoinedAndKeptUnderTheLimitOfTheAddress(t *testing.T) {
	query := `{"id":"ds_vm","parent":{"dataset":"ds_ids","values":{"id":"id"},"batch":{"size":100,"separator":"|","match":"id"}},` +
		`"request":{"path":"/vm/query","query":{"ids":"{parent.id}"}},"records_path":"items","natural_key":["id"],"columns":[{"name":"id"}]}`
	var ids []string
	for i := 0; i < 30; i++ {
		ids = append(ids, fmt.Sprintf("v%02d", i))
	}
	ids = append(ids, "x|y")
	g := hxeNew(t, hxeDoc("", hxeIDs, query), func(n int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			return hxeOK(hxeList(ids...))
		}
		var items []string
		for _, id := range strings.Split(r.hsQuery().Get("ids"), "|") {
			items = append(items, fmt.Sprintf(`{"id":%q}`, id))
		}
		return hxeOK(`{"items":[` + strings.Join(items, ",") + `]}`)
	}, func(c *Config) { c.Ceilings.URLBytes = 640 })
	pages, err := g.read("ds_vm")
	if err != nil {
		t.Fatal(err)
	}
	if got := hxeIDsOf(pages); len(got) != 30 {
		t.Errorf("%d rows: %v", len(got), got)
	}
	if len(g.srv.seen) < 4 {
		t.Errorf("the batch was not cut: %d requests", len(g.srv.seen))
	}
	for _, r := range g.srv.seen {
		if len(r.hsURL) > 640 {
			t.Errorf("an address of %d bytes", len(r.hsURL))
		}
	}
	if end := hxeEnd(t, pages); end.Complete || !end.SeenOnly || !strings.Contains(end.Reason, "passed over") {
		t.Errorf("the value with the separator in it: %+v", end)
	}
}

// [REQ-914]
// Every request counts against the limits of the dataset and of the run: the parent's, the token's, the pages and the sends again.
func TestHxeTheLimitsCountEveryRequestOnTheClockOfTheReader(t *testing.T) {
	five := []string{"a", "b", "c", "d", "e"}
	read := func(name, doc string, each int, ids []string, mut ...func(*Config)) (*hxeRig, ingest.Completeness) {
		g := hxeNew(t, doc, hxeVM(ids...), mut...)
		g.srv.each = hxeSeconds(each)
		pages, err := g.read("ds_vm")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return g, hxeEnd(t, pages)
	}
	g, end := read("requests of the dataset", hxeDoc("", hxeIDs, hxeDetail(`,"max_requests":3`, "")), 0, five[:3])
	if end.Complete || !strings.Contains(end.Reason, "parent.max_requests") || len(g.srv.seen) != 3 {
		t.Errorf("max_requests: %+v after %d requests", end, len(g.srv.seen))
	}
	g, end = read("time of the dataset", hxeDoc("", hxeIDs, hxeDetail(`,"max_seconds":10`, "")), 4, five)
	if end.Complete || !strings.Contains(end.Reason, "parent.max_seconds") || len(g.srv.seen) != 3 {
		t.Errorf("max_seconds: %+v after %d requests", end, len(g.srv.seen))
	}
	g, end = read("time of the run", hxeDoc("", hxeIDs, hxeDetail("", "")), 4, five, func(c *Config) { c.Ceilings.RunTime = 10 * 1e9 })
	if end.Complete || !strings.Contains(end.Reason, "the run has taken the longest") || len(g.srv.seen) != 3 {
		t.Errorf("the time of the run: %+v after %d requests", end, len(g.srv.seen))
	}
	g, end = read("requests of the run", hxeDoc("", hxeIDs, hxeDetail("", "")), 0, five, func(c *Config) { c.Ceilings.RunRequests = 4 })
	if end.Complete || !strings.Contains(end.Reason, "the most requests") || len(g.srv.seen) != 4 {
		t.Errorf("the requests of the run: %+v after %d requests", end, len(g.srv.seen))
	}
	g, end = read("a ceiling under the definition", hxeDoc("", hxeIDs, hxeDetail(`,"max_requests":100`, "")), 0, five[:2], func(c *Config) { c.Ceilings.ChildRequests = 2 })
	if end.Complete || len(g.srv.seen) != 2 {
		t.Errorf("a ceiling of the server: %+v after %d requests", end, len(g.srv.seen))
	}
	// A second read with the same reader goes on from what the first spent.
	g = hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", "")), hxeVM("a", "b", "c"), func(c *Config) { c.Ceilings.RunRequests = 6 })
	for i, wantComplete := range []bool{true, false} {
		pages, err := g.read("ds_vm")
		if err != nil || hxeEnd(t, pages).Complete != wantComplete {
			t.Errorf("read %d: %+v %v", i, pages, err)
		}
	}
	if len(g.srv.seen) != 6 {
		t.Errorf("two reads of four requests under a limit of six: %d", len(g.srv.seen))
	}
}

// [REQ-914]
// A check makes one request of the child and a preview ten, and both read the parent only as far as they need its values; they say so.
func TestHxeACheckAndAPreviewReadTheFirstValuesOnly(t *testing.T) {
	var ids []string
	for i := 0; i < 15; i++ {
		ids = append(ids, fmt.Sprintf("v%02d", i))
	}
	g := hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", "")), hxeVM(ids...))
	res := g.reader.Check(context.Background(), "src_hosts", g.dataset("ds_vm"))
	if res.Kind != ingest.CheckOK || !strings.Contains(res.Message, "first value") || len(g.srv.seen) != 2 {
		t.Errorf("check: %+v after %d requests", res, len(g.srv.seen))
	}
	g = hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", "")), hxeVM(ids...))
	pv, err := g.reader.Preview(context.Background(), "src_hosts", g.dataset("ds_vm"), ingest.PreviewOptions{MaxRows: 100, MaxPages: 5})
	if err != nil || len(g.srv.seen) != 11 || !strings.Contains(pv.Note, "10 requests") {
		t.Fatalf("preview: %v, %d requests, %q", err, len(g.srv.seen), pv.Note)
	}
	for _, e := range pv.Exchanges {
		if strings.Contains(e.Path, "/vm/v") {
			t.Errorf("an exchange says a value: %q", e.Path)
		}
	}
}

// [REQ-915]
// A parent whose reading takes longer than the time limit of one request is read all the same: its pages are read one after the other
// and no request of the child is sent while a body of the parent is open (X5), so no body is read after the time its request had.
func TestHxeAParentThatTakesLongerThanTheTimeOfARequestIsStillRead(t *testing.T) {
	parent := `{"id":"ds_ids","collect":false,"request":{"path":"/vm"},"records_path":"items","pagination":{"kind":"page","page_param":"p","size_param":"n","size":2,"first_page":1}}`
	g := hxeNew(t, hxeDoc(`,"timeout_seconds":30`, parent, hxeDetail("", "")), func(n int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			switch r.hsQuery().Get("p") {
			case "1":
				return hxeOK(hxeList("a", "b"))
			case "2":
				return hxeOK(hxeList("c", "d"))
			}
			return hxeOK(hxeList())
		}
		return hxeOK(hxeDetailBody(strings.TrimPrefix(r.hsPath(), "/v1/vm/")))
	})
	g.srv.each = hxeSeconds(20)
	pages, err := g.read("ds_vm")
	if err != nil {
		t.Fatal(err)
	}
	if end := hxeEnd(t, pages); !end.Complete || len(hxeIDsOf(pages)) != 4 {
		t.Errorf("the end %+v, rows %v", end, hxeIDsOf(pages))
	}
	if slices.Max(g.srv.openAt) != 0 {
		t.Errorf("a request was sent while a body was open: %v", g.srv.openAt)
	}
}

// [REQ-915]
// The values of a parent of a hundred thousand records are held within the limit, and more values than the limit send no request of
// the child.
func TestHxeTheValuesOfABigParentAreHeldWithinTheLimit(t *testing.T) {
	const total = 100_000
	parent := `{"id":"ds_ids","collect":false,"request":{"path":"/vm"},"records_path":"items","pagination":{"kind":"offset","offset_param":"o","size_param":"n","size":1000}}`
	batch := strings.Replace(hxeBatch(""), `"size":2,`, `"size":1000,`, 1)
	handle := func(count int) func(n int, r hsRequest) hxeReply {
		return func(n int, r hsRequest) hxeReply {
			if r.hsPath() != "/v1/vm" {
				return hxeOK(`{"items":[]}`)
			}
			var off int
			_, _ = fmt.Sscan(r.hsQuery().Get("o"), &off)
			var b strings.Builder
			b.WriteString(`{"items":[`)
			for i := off; i < min(off+1000, count); i++ {
				if i > off {
					b.WriteByte(',')
				}
				fmt.Fprintf(&b, `{"id":"id%07d"}`, i)
			}
			b.WriteString(`]}`)
			return hxeOK(b.String())
		}
	}
	g := hxeNew(t, hxeDoc("", parent, strings.Replace(batch, `"batch":{`, `"max_requests":50000,"batch":{`, 0)), handle(total))
	if _, err := g.read("ds_vm"); err != nil {
		t.Fatal(err)
	}
	if peak := g.reader.bud.peak; peak != total || peak > g.reader.ceil.ParentValues {
		t.Errorf("the values held: %d (limit %d)", peak, g.reader.ceil.ParentValues)
	}
	if len(g.srv.seen) != 101+100 {
		t.Errorf("%d requests, want 101 pages of the parent (the last is empty) and 100 batches", len(g.srv.seen))
	}
	g = hxeNew(t, hxeDoc("", parent, batch), handle(120_000), func(c *Config) { c.Ceilings.ParentValues = 50_000 })
	pages, err := g.read("ds_vm")
	end := hxeEnd(t, pages)
	if err != nil || end.Complete || !strings.Contains(end.Reason, "more distinct values") || g.reader.bud.peak > 50_000 {
		t.Errorf("over the limit: %+v %v, held %d", end, err, g.reader.bud.peak)
	}
	for _, p := range g.paths() {
		if strings.HasSuffix(strings.SplitN(p, "?", 2)[0], "/vm/query") {
			t.Errorf("a request of the child: %s", p)
		}
	}
	if len(g.srv.seen) > 52 {
		t.Errorf("the parent was read on after the limit: %d requests", len(g.srv.seen))
	}
}

// [REQ-916]
// A child has pages of its own for each value; none of them has a token, and a token given to a read of it is not used.
func TestHxeThePagesOfAChildForEachValueHaveNoTokenAndAreReadFromTheStart(t *testing.T) {
	detail := strings.Replace(hxeDetail("", ""), `"records_path":"items",`, `"records_path":"items","pagination":{"kind":"page","page_param":"p","size_param":"n","size":2,"first_page":1},`, 1)
	g := hxeNew(t, hxeDoc("", hxeIDs, detail), func(n int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			return hxeOK(hxeList("a", "b"))
		}
		who := strings.TrimPrefix(r.hsPath(), "/v1/vm/")
		if r.hsQuery().Get("p") == "1" {
			return hxeOK(fmt.Sprintf(`{"items":[{"id":"%s1"},{"id":"%s2"}]}`, who, who))
		}
		return hxeOK(fmt.Sprintf(`{"items":[{"id":"%s3"}]}`, who))
	})
	var pages []ingest.Page
	for p, err := range g.reader.ReadPages(context.Background(), "src_hosts", g.dataset("ds_vm"), ingest.PageOptions{MaxRows: 1, Resume: []byte("a token of a read that went before")}) {
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, p)
	}
	if got := hxeIDsOf(pages); !slices.Equal(got, []string{"a1", "a2", "a3", "b1", "b2", "b3"}) {
		t.Errorf("rows %v", got)
	}
	for _, p := range pages {
		if len(p.Token) != 0 {
			t.Errorf("a page has a token: %q", p.Token)
		}
	}
	if end := hxeEnd(t, pages); !end.Complete {
		t.Errorf("the end %+v", end)
	}
}

// [REQ-917]
// The values come from the record with the values of the secrets taken out: a secret that the parent gives back is never in an address.
func TestHxeAValueThatIsASecretIsNeverPutInAnAddress(t *testing.T) {
	doc := `{"version":1,"id":"src_inventory","adapter":"http","connection":{"base_url":"https://api.example.test/v1","auth":{"kind":"token_request","credential":{"secret_id":"sec_pw"},` +
		`"request":{"path":"/login","format":"json","body":{"username":"{{credential.username}}","password":"{{credential.password}}"}},"token_path":"token"}},"datasets":[` + hxeIDs + `,` + hxeDetail("", "") + `]}`
	g := hsNew(t, doc, func(n int, r hsRequest) hsReply {
		switch p := r.hsPath(); {
		case p == "/v1/login":
			return hsJSON(`{"token":"` + hsMintedValue + `"}`)
		case p == "/v1/vm":
			return hsJSON(hxeList("a", hsPassValue, hsMintedValue))
		}
		return hsJSON(hxeDetailBody(strings.TrimPrefix(r.hsPath(), "/v1/vm/")))
	})
	var ds ingest.Dataset
	for _, d := range g.hsDef.Datasets {
		if d.ID == "ds_vm" {
			ds = d
		}
	}
	if _, err := g.hsReadDS(ds, ingest.PageOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, r := range g.hsSrv.hsSeen {
		if strings.Contains(r.hsURL, hsPassValue) || strings.Contains(r.hsURL, hsMintedValue) || strings.Contains(r.hsBody, hsMintedValue) {
			t.Errorf("a secret is in a request: %s", r.hsURL)
		}
	}
}

// [REQ-914]
// The request for a token counts with the others: with max_requests 3 the token, the list and one detail are what is sent.
func TestHxeTheRequestForATokenCountsAgainstTheLimits(t *testing.T) {
	doc := `{"version":1,"id":"src_inventory","adapter":"http","connection":{"base_url":"https://api.example.test/v1","auth":{"kind":"token_request","credential":{"secret_id":"sec_pw"},` +
		`"request":{"path":"/login","format":"json","body":{"username":"{{credential.username}}","password":"{{credential.password}}"}},"token_path":"token"}},"datasets":[` + hxeIDs + `,` + hxeDetail(`,"max_requests":3`, "") + `]}`
	g := hsNew(t, doc, func(n int, r hsRequest) hsReply {
		switch p := r.hsPath(); {
		case p == "/v1/login":
			return hsJSON(`{"token":"` + hsMintedValue + `"}`)
		case p == "/v1/vm":
			return hsJSON(hxeList("a", "b", "c"))
		}
		return hsJSON(hxeDetailBody(strings.TrimPrefix(r.hsPath(), "/v1/vm/")))
	})
	pages, err := g.hsReadDS(g.hsDef.Datasets[1], ingest.PageOptions{})
	if err != nil || len(g.hsSrv.hsSeen) != 3 || hxeEndOf(pages).Complete {
		t.Errorf("%d requests, end %+v, %v", len(g.hsSrv.hsSeen), hxeEndOf(pages), err)
	}
}

func hxeEndOf(pages []ingest.Page) ingest.Completeness {
	if len(pages) == 0 {
		return ingest.Completeness{}
	}
	return pages[len(pages)-1].End
}

// [REQ-910]
// The answer of the detail of one thing is an object, and a child reads it as one record; a list under records_path is read as
// before.
func TestHxeADetailThatIsAnObjectIsOneRecord(t *testing.T) {
	d := `{"id":"ds_vm","parent":{"dataset":"ds_ids","values":{"id":"id"}},"request":{"path":"/vm/{parent.id}"},"natural_key":["id"],"columns":[{"name":"id"},{"name":"host","paths":["placement.host"]}]}`
	g := hxeNew(t, hxeDoc("", hxeIDs, d), func(n int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			return hxeOK(hxeList("a", "b"))
		}
		who := strings.TrimPrefix(r.hsPath(), "/v1/vm/")
		return hxeOK(fmt.Sprintf(`{"id":%q,"placement":{"host":"h-%s"},"nics":[1,2]}`, who, who))
	})
	pages, err := g.read("ds_vm")
	if err != nil {
		t.Fatal(err)
	}
	var hosts []string
	for _, p := range pages {
		for _, r := range p.Rows {
			hosts = append(hosts, r.Fields["host"])
		}
	}
	if !slices.Equal(hosts, []string{"h-a", "h-b"}) || !hxeEnd(t, pages).Complete {
		t.Errorf("hosts %v, end %+v", hosts, hxeEnd(t, pages))
	}
}
