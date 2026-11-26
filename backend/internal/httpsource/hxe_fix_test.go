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
// no value, or a list that is empty for each value, is complete as an empty answer is.
func TestHxeADetailThatHasNoKeyIsAValueThatWasNotRead(t *testing.T) {
	ids := hxeManyIDs(50)
	envelope := func(string) string { return `{"error":"forbidden","status":403}` }
	empty := func(string) string { return `{}` }
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
		{"a parent with no value", hxeRootDetail(""), nil, envelope, "complete"},
		{"an empty list for each value", hxeDetail("", ""), ids, func(string) string { return `{"items":[]}` }, "complete"},
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
	for i := 0; i < 10; i++ {
		ids = append(ids, fmt.Sprintf("%02d%s", i, strings.Repeat("<", 30)))
	}
	g := hxeNew(t, hxeDoc("", hxeIDs, hxeBatch(`,"size":100`)), func(_ int, r hsRequest) hxeReply {
		if r.hsPath() == "/v1/vm" {
			return hxeOK(hxeList(ids...))
		}
		return hxeOK(`{"items":[]}`)
	}, func(c *Config) { c.Ceilings.BodyBytes = 1000 })
	pages, err := g.read("ds_vm")
	if err != nil || len(pages) == 0 {
		t.Fatalf("%v", err)
	}
	if len(g.srv.seen) < 3 {
		t.Errorf("%d requests: the batch was not cut", len(g.srv.seen))
	}
	for _, r := range g.srv.seen {
		if len(r.hsBody) > 1000 {
			t.Errorf("a body of %d bytes", len(r.hsBody))
		}
	}
}
