package httpsource

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// The tests of the review of the child datasets (ADR-0111): what is not read is not complete, what is held and what is sent is held to
// what the limits say, and the edges of a value of the parent.

// hxeRootDetail is a child whose detail is one object, with the extra pieces of the dataset given.
func hxeRootDetail(extra string) string {
	return `{"id":"ds_vm","parent":{"dataset":"ds_ids","values":{"id":"id"}},` + extra +
		`"request":{"path":"/vm/{parent.id}"},"natural_key":["id"],"columns":[{"name":"id"}]}`
}

func hxeManyIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("v%02d", i)
	}
	return ids
}

// [REQ-912]
// A detail that is not what the definition reads (an error that comes with a 200, an object with no key) is a value that was not read:
// all of them is not complete, even when the definition passes over rows with no key, one of them is what was seen, and a parent with
// no value is complete as an empty answer is. An answer that is a list with one error in it is a value that was not read as well
// (seen, not complete), and so is a list that is empty for every value of more than three: no value had a record, which a path that is
// wrong or an account that may not read looks like. A list that is empty for some of the values, or for three values, is complete.
func TestHxeADetailThatHasNoKeyIsAValueThatWasNotRead(t *testing.T) {
	ids := hxeManyIDs(50)
	envelope := func(string) string { return `{"error":"forbidden","status":403}` }
	empty := func(string) string { return `{}` }
	dataDetail := `{"id":"ds_vm","parent":{"dataset":"ds_ids","values":{"id":"id"}},"request":{"path":"/vm/{parent.id}"},"records_path":"data",` +
		`"natural_key":["id"],"columns":[{"name":"id"}]}`
	for _, c := range []struct {
		name   string
		child  string
		ids    []string
		detail func(id string) string
		want   string // "complete", "seen" or "incomplete"
	}{
		{"every answer an error", hxeRootDetail(""), ids, envelope, "incomplete"},
		{"every answer empty", hxeRootDetail(""), ids, empty, "incomplete"},
		{"rows with no key passed over", hxeRootDetail(`"ignore_rows_without_key":true,`), ids, envelope, "incomplete"},
		{"one answer an error", hxeRootDetail(""), ids, func(id string) string {
			if id == "v07" {
				return envelope(id)
			}
			return `{"id":"` + id + `"}`
		}, "seen"},
		{"a list with an error element", hxeRootDetail(""), ids, func(string) string { return `[{"error":"x"}]` }, "seen"},
		{"an items list with an error element", hxeDetail("", ""), ids, func(string) string { return `{"items":[{"error":"x"}]}` }, "seen"},
		{"the same with rows with no key passed over", hxeDetail("", `"ignore_rows_without_key":true,`), ids, func(string) string {
			return `{"items":[{"error":"x"}]}`
		}, "seen"},
		{"a parent with no value", hxeRootDetail(""), nil, envelope, "complete"},
		{"an empty list for each value", hxeDetail("", ""), ids, func(string) string { return `{"items":[]}` }, "seen"},
		{"null for each value under data", dataDetail, ids, func(string) string { return `{"data":null}` }, "seen"},
		{"an empty top-level list for each value", hxeRootDetail(""), ids, func(string) string { return `[]` }, "seen"},
		{"three values, all empty", hxeDetail("", ""), hxeManyIDs(3), func(string) string { return `{"items":[]}` }, "complete"},
		{"one value with a row, the rest empty", hxeRootDetail(""), ids, func(id string) string {
			if id == "v07" {
				return `[{"id":"` + id + `"}]`
			}
			return `[]`
		}, "complete"},
	} {
		g := hxeNew(t, hxeDoc("", hxeIDs, c.child), func(_ int, r hsRequest) hxeReply {
			if r.hsPath() == "/v1/vm" {
				return hxeOK(hxeList(c.ids...))
			}
			return hxeOK(c.detail(strings.TrimPrefix(r.hsPath(), "/v1/vm/")))
		})
		pages, err := g.read("ds_vm")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		end := hxeEnd(t, pages)
		got := "incomplete"
		switch {
		case end.Complete:
			got = "complete"
		case end.SeenOnly:
			got = "seen"
		}
		if got != c.want {
			t.Errorf("%s: %s, the end %+v", c.name, got, end)
		}
	}
}

// [REQ-915]
// An object that has very many keys is cut when it is as big as a record may be, and the body is not read on to its end.
func TestHxeAnObjectOfManyKeysIsCutAtTheSizeOfARecordAndNotReadToItsEnd(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"id":"a"`)
	for i := 0; b.Len() < 12<<20; i++ {
		fmt.Fprintf(&b, `,"k%d":1`, i)
	}
	b.WriteString(`}`)
	g := hxeNew(t, hxeDoc("", hxeIDs, hxeRootDetail("")), func(_ int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			return hxeOK(hxeList("a"))
		}
		return hxeOK(b.String())
	})
	if _, err := g.read("ds_vm"); err == nil {
		t.Errorf("no error for an object of 12 MiB")
	}
	if g.srv.bytesRead > 4<<20 {
		t.Errorf("%d bytes of the body were read", g.srv.bytesRead)
	}
}

// [REQ-914]
// A preview sends at most ten requests of the child and a check one, however the pages and the units are: pages with no row that
// never end, and pages of one row.
func TestHxeAPreviewAndACheckCountTheRequestsThatWereSent(t *testing.T) {
	cursor := `"pagination":{"kind":"cursor","cursor_param":"c","cursor_in":"query","next_cursor_path":"next","max_pages":5000},`
	paged := `"pagination":{"kind":"page","page_param":"p","size_param":"s","size":1,"first_page":1,"max_pages":5000},`
	for _, c := range []struct {
		name, extra string
		body        func(n int) string
	}{
		{"pages with no row", cursor, func(n int) string { return fmt.Sprintf(`{"items":[],"next":"c%d"}`, n) }},
		{"pages of one row", paged, func(n int) string { return fmt.Sprintf(`{"items":[{"id":"r%d","name":"n"}]}`, n) }},
	} {
		handle := func(n int, r hsRequest) hxeReply {
			if r.hsPath() == "/v1/vm" {
				return hxeOK(hxeList("a", "b", "c"))
			}
			return hxeOK(c.body(n))
		}
		g := hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", c.extra)), handle)
		if _, err := g.reader.Preview(context.Background(), "src_hosts", g.dataset("ds_vm"), ingest.PreviewOptions{MaxRows: 1000, MaxPages: 1000}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := len(g.srv.seen) - 1; got > 10 {
			t.Errorf("%s: a preview sent %d requests of the child", c.name, got)
		}
		g = hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", c.extra)), handle)
		g.reader.Check(context.Background(), "src_hosts", g.dataset("ds_vm"))
		if got := len(g.srv.seen) - 1; got > 1 {
			t.Errorf("%s: a check sent %d requests of the child", c.name, got)
		}
	}
}

// [REQ-915]
// The bytes of the values of a parent that are held are counted with what each value costs besides its text.
func TestHxeTheBytesOfTheValuesOfAParentAreCountedWithWhatEachValueCosts(t *testing.T) {
	g := hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", "")), hxeVM(hxeManyIDs(20)...), func(c *Config) { c.Ceilings.ParentBytes = 1000 })
	pages, err := g.read("ds_vm")
	if end := hxeEndOf(pages); err != nil || end.Complete || !strings.Contains(end.Reason, "1000 bytes") || len(g.srv.seen) != 1 {
		t.Errorf("%v, the end %+v, %d requests", err, end, len(g.srv.seen))
	}
}

// [REQ-914]
// The 404 of a check says that the path may be wrong, even when the definition passes over a value that is gone.
func TestHxeACheckWhoseFirstDetailIsNotFoundTellsTheSettingIsWrong(t *testing.T) {
	g := hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", `"on_not_found":"skip",`)), func(_ int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			return hxeOK(hxeList("a", "b"))
		}
		return hxeReply{status: 404, body: "no"}
	})
	res := g.reader.Check(context.Background(), "src_hosts", g.dataset("ds_vm"))
	if res.Kind != ingest.CheckConfig || !strings.Contains(res.Message, "404") {
		t.Errorf("%+v", res)
	}
}

// [REQ-911]
// The 404 of the request for a token is not a value that is gone: a child that passes over a value of the parent that is not found
// does not pass over every value when the token cannot be had.
func TestHxeThe404OfTheRequestForATokenIsNotAValueThatIsGone(t *testing.T) {
	doc := `{"version":1,"id":"src_inventory","adapter":"http","connection":{"base_url":"https://api.example.test/v1","auth":{"kind":"token_request","credential":{"secret_id":"sec_pw"},` +
		`"request":{"path":"/login","format":"json","body":{"username":"{{credential.username}}","password":"{{credential.password}}"}},"token_path":"token"}},"datasets":[` +
		hxeIDs + `,` + hxeDetail("", `"on_not_found":"skip",`) + `]}`
	g := hsNew(t, doc, func(n int, r hsRequest) hsReply {
		if r.hsPath() == "/v1/login" {
			return hsReply{hsStatus: 404, hsBody: "no"}
		}
		return hsJSON(hxeList("a", "b"))
	})
	pages, err := g.hsReadDS(g.hsDef.Datasets[1], ingest.PageOptions{})
	if err == nil || hxeEndOf(pages).Complete || hxeEndOf(pages).SeenOnly {
		t.Errorf("%v, the end %+v", err, hxeEndOf(pages))
	}
}

// [REQ-913]
// The values of a batch that JSON writes longer (< > & are six bytes) are cut so that the body is not over its limit.
func TestHxeABatchOfValuesThatJSONWritesLongerFitsTheBody(t *testing.T) {
	var ids []string
	for i := 0; i < 40; i++ {
		ids = append(ids, fmt.Sprintf("%02d%s", i, strings.Repeat("<", 30)))
	}
	g := hxeNew(t, hxeDoc("", hxeIDs, hxeBatch(`,"size":100`)), func(_ int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			return hxeOK(hxeList(ids...))
		}
		return hxeOK(`{"items":[]}`)
	}, func(c *Config) { c.Ceilings.BodyBytes = 4000 })
	pages, err := g.read("ds_vm")
	if err != nil || len(pages) == 0 {
		t.Fatalf("%v", err)
	}
	if len(g.srv.seen) < 3 {
		t.Errorf("%d requests: the batch was not cut", len(g.srv.seen))
	}
	for _, r := range g.srv.seen {
		if len(r.hsBody) > 4000 {
			t.Errorf("a body of %d bytes", len(r.hsBody))
		}
	}
}

// [REQ-912]
// A parent with more values than the ceiling of values is not read: the child is not read at all and the dataset is not complete.
func TestHxeAParentWithMoreValuesThanTheCeilingIsNotRead(t *testing.T) {
	g := hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", "")), hxeVM(hxeManyIDs(20)...), func(c *Config) { c.Ceilings.ParentValues = 5 })
	pages, err := g.read("ds_vm")
	if end := hxeEndOf(pages); err != nil || end.Complete || end.SeenOnly || !strings.Contains(end.Reason, "more distinct values than 5") || len(g.srv.seen) != 1 {
		t.Errorf("%v, the end %+v, %d requests", err, end, len(g.srv.seen))
	}
}

// [REQ-912]
// Values passed over for a 404 are seen, not complete, up to five percent of the values of the parent; past five percent (and more than
// three) they are not complete. Here 100 values, so 5 is the line that is still seen and 6 is not complete.
func TestHxeTheFivePercentOfTheValuesIsTheLineBetweenSeenAndNotComplete(t *testing.T) {
	ids := hxeManyIDs(100)
	for _, c := range []struct {
		gone int
		want string
	}{{5, "seen"}, {6, "incomplete"}} {
		gone := map[string]bool{}
		for _, id := range ids[:c.gone] {
			gone[id] = true
		}
		g := hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", `"on_not_found":"skip",`)), func(_ int, r hsRequest) hxeReply {
			if r.hsPath() == "/v1/vm" {
				return hxeOK(hxeList(ids...))
			}
			id := strings.TrimPrefix(r.hsPath(), "/v1/vm/")
			if gone[id] {
				return hxeReply{status: 404, body: "no"}
			}
			return hxeOK(hxeDetailBody(id))
		})
		pages, err := g.read("ds_vm")
		if err != nil {
			t.Fatalf("%d gone: %v", c.gone, err)
		}
		end := hxeEnd(t, pages)
		got := "incomplete"
		switch {
		case end.Complete:
			got = "complete"
		case end.SeenOnly:
			got = "seen"
		}
		if got != c.want {
			t.Errorf("%d of 100 gone: %s, the end %+v", c.gone, got, end)
		}
	}
}

// [REQ-914]
// A preview and a check read the parent for twenty requests at most, whatever its pages give: the same two values in turn, one value on
// every page, or no value at all. A check of a parent with no value says so.
func TestHxeAPreviewAndACheckReadTheParentForTwentyRequestsAtMost(t *testing.T) {
	endless := `"pagination":{"kind":"cursor","cursor_param":"c","cursor_in":"query","next_cursor_path":"next","max_pages":5000}`
	parent := `{"id":"ds_ids","collect":false,"request":{"path":"/vm"},"records_path":"items",` + endless + `}`
	for _, c := range []struct {
		name string
		body func(n int) string
	}{
		{"two values in turn", func(n int) string {
			return fmt.Sprintf(`{"items":[{"id":"%s"}],"next":"c%d"}`, []string{"a", "b"}[n%2], n)
		}},
		{"one value on every page", func(n int) string { return fmt.Sprintf(`{"items":[{"id":"x"}],"next":"c%d"}`, n) }},
		{"no value on any page", func(n int) string { return fmt.Sprintf(`{"items":[],"next":"c%d"}`, n) }},
	} {
		handle := func(n int, r hsRequest) hxeReply {
			if r.hsPath() == "/v1/vm" {
				return hxeOK(c.body(n))
			}
			return hxeOK(hxeDetailBody("v"))
		}
		g := hxeNew(t, hxeDoc("", parent, hxeDetail("", "")), handle)
		if _, err := g.reader.Preview(context.Background(), "src_hosts", g.dataset("ds_vm"), ingest.PreviewOptions{MaxRows: 1000, MaxPages: 1000}); err != nil {
			t.Fatalf("%s: a preview: %v", c.name, err)
		}
		if got := hxeParentRequests(g); got == 0 || got > previewParentRequests {
			t.Errorf("%s: a preview sent %d requests of the parent", c.name, got)
		}
		g = hxeNew(t, hxeDoc("", parent, hxeDetail("", "")), handle)
		res := g.reader.Check(context.Background(), "src_hosts", g.dataset("ds_vm"))
		if got := hxeParentRequests(g); got == 0 || got > previewParentRequests {
			t.Errorf("%s: a check sent %d requests of the parent", c.name, got)
		}
		if c.name == "no value on any page" && (res.Kind != ingest.CheckTransient || !strings.Contains(res.Message, "gave no value")) {
			t.Errorf("%s: the check said %+v", c.name, res)
		}
	}
}

// hxeParentRequests is how many requests of the parent (the list at /v1/vm) the source saw.
func hxeParentRequests(g *hxeRig) int {
	n := 0
	for _, r := range g.srv.seen {
		if r.hsPath() == "/v1/vm" {
			n++
		}
	}
	return n
}

// [REQ-914]
// A check whose first value is one object with no natural key says the setting is wrong, not that the first page was read.
func TestHxeACheckOfADetailWithNoKeyTellsTheSettingIsWrong(t *testing.T) {
	g := hxeNew(t, hxeDoc("", hxeIDs, hxeRootDetail("")), func(_ int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			return hxeOK(hxeList("a", "b"))
		}
		return hxeOK(`{"error":"x"}`)
	})
	res := g.reader.Check(context.Background(), "src_hosts", g.dataset("ds_vm"))
	if res.Kind != ingest.CheckConfig || !strings.Contains(res.Message, "natural key") {
		t.Errorf("%+v", res)
	}
}

// [REQ-914]
// A retry that the check's limit of one request stops says what the first answer was (here 503), not only the limit.
func TestHxeACheckWhoseRetryIsStoppedSaysWhatTheFirstAnswerWas(t *testing.T) {
	g := hxeNew(t, hxeDoc("", hxeIDs, hxeDetail("", "")), func(_ int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			return hxeOK(hxeList("a", "b"))
		}
		return hxeReply{status: 503, body: "busy"}
	})
	res := g.reader.Check(context.Background(), "src_hosts", g.dataset("ds_vm"))
	if res.Kind != ingest.CheckTransient || !strings.Contains(res.Message, "503") {
		t.Errorf("%+v", res)
	}
}

// [REQ-914]
// The request for a token is not one of the requests of the child that a preview may send: ten of the child are sent after it.
func TestHxeTheRequestForATokenIsNotCountedAmongThePreviewsRequestsOfTheChild(t *testing.T) {
	cursor := `"pagination":{"kind":"cursor","cursor_param":"c","cursor_in":"query","next_cursor_path":"next","max_pages":5000},`
	doc := `{"version":1,"id":"src_inventory","adapter":"http","connection":{"base_url":"https://api.example.test/v1","auth":{"kind":"token_request","credential":{"secret_id":"sec_pw"},` +
		`"request":{"path":"/login","format":"json","body":{"username":"{{credential.username}}","password":"{{credential.password}}"}},"token_path":"token"}},"datasets":[` +
		hxeIDs + `,` + hxeDetail("", cursor) + `]}`
	g := hsNew(t, doc, func(n int, r hsRequest) hsReply {
		switch {
		case r.hsPath() == "/v1/login":
			return hsJSON(`{"token":"t"}`)
		case r.hsPath() == "/v1/vm":
			return hsJSON(hxeList("a"))
		}
		return hsJSON(fmt.Sprintf(`{"items":[],"next":"c%d"}`, n))
	})
	if _, err := g.hsReader.Preview(context.Background(), hsSource, g.hsDef.Datasets[1], ingest.PreviewOptions{MaxRows: 1000, MaxPages: 1000}); err != nil {
		t.Fatalf("a preview: %v", err)
	}
	children := 0
	for _, r := range g.hsSrv.hsSeen {
		if strings.HasPrefix(r.hsPath(), "/v1/vm/") {
			children++
		}
	}
	if children != previewChildRequests {
		t.Errorf("%d requests of the child, want %d", children, previewChildRequests)
	}
}
