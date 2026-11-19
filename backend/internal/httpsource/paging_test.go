package httpsource

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

func hsKinds() map[string]struct{ doc, request string } {
	return map[string]struct{ doc, request string }{
		ingest.PagePage:       {hsPageKind(4, 1, true), ""},
		ingest.PageOffset:     {`{"kind":"offset","offset_param":"offset","size_param":"limit","size":4,"total_path":"pageInfo.totalCount"}`, ""},
		ingest.PageCursor:     {`{"kind":"cursor","cursor_param":"cursor","cursor_in":"query","next_cursor_path":"meta.next","size_param":"limit","size":4}`, ""},
		ingest.PageNextURL:    {`{"kind":"next_url","next_path":"next"}`, ""},
		ingest.PageLinkHeader: {`{"kind":"link_header"}`, ""},
	}
}

// [REQ-663]
func TestEveryKindOfPaginationReadsTheWholeDatasetAndEndsComplete(t *testing.T) {
	for kind, def := range hsKinds() {
		t.Run(kind, func(t *testing.T) {
			g := hsNew(t, hsDoc("", def.request, def.doc, ""), hsAPI(kind, 10, 4, 1))
			pages, err := g.hsRead(ingest.PageOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if got := hsSerials(pages); !reflect.DeepEqual(got, hsWant(0, 10)) {
				t.Fatalf("rows: %v", got)
			}
			if end := hsLast(t, pages).End; !end.Complete {
				t.Fatalf("not complete: %s", end.Reason)
			}
			if got := len(g.hsSrv.hsSeen); got != 3 {
				t.Fatalf("requests: %d", got)
			}
			if g.hsSrv.hsClients != 1 || g.hsSrv.hsShut != 1 {
				t.Fatalf("the client was made %d times and closed %d times", g.hsSrv.hsClients, g.hsSrv.hsShut)
			}
		})
	}
}

// [REQ-663]
func TestPageNumbersStartWhereTheDefinitionSays(t *testing.T) {
	for _, first := range []int{0, 1} {
		g := hsNew(t, hsDoc("", "", hsPageKind(5, first, false), ""), hsAPI(ingest.PagePage, 7, 5, first))
		pages, err := g.hsRead(ingest.PageOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := hsSerials(pages); !reflect.DeepEqual(got, hsWant(0, 7)) {
			t.Fatalf("first_page %d: %v", first, got)
		}
		q := g.hsSrv.hsSeen[0].hsQuery()
		if q.Get("page") != string(rune('0'+first)) || q.Get("pageSize") != "5" || q.Get("status") != "active" {
			t.Fatalf("the first request: %v", q)
		}
		if g.hsSrv.hsSeen[0].hsPath() != "/api/v2/servers" {
			t.Fatalf("the path: %s", g.hsSrv.hsSeen[0].hsPath())
		}
	}
}

// [REQ-663]
func TestAFullLastPageIsFollowedByAnEmptyOneAndEndsComplete(t *testing.T) {
	g := hsNew(t, hsDoc("", "", hsPageKind(5, 1, false), ""), hsAPI(ingest.PagePage, 10, 5, 1))
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.hsSrv.hsSeen) != 3 || !hsLast(t, pages).End.Complete || len(hsSerials(pages)) != 10 {
		t.Fatalf("requests %d, rows %d", len(g.hsSrv.hsSeen), len(hsSerials(pages)))
	}
}

// [REQ-663]
func TestAnEmptyDatasetIsOneEmptyCompletePage(t *testing.T) {
	g := hsNew(t, hsDoc("", "", hsPageKind(5, 1, false), ""), hsAPI(ingest.PagePage, 0, 5, 1))
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil || len(pages) != 1 || !pages[0].Last || !pages[0].End.Complete || len(pages[0].Rows) != 0 {
		t.Fatalf("pages %v, err %v", pages, err)
	}
}

// [REQ-663]
func TestATotalThatIsNotWhatWasReadMakesTheReadNotComplete(t *testing.T) {
	handle := func(_ int, r hsRequest) hsReply {
		if r.hsQuery().Get("page") == "1" {
			return hsJSON(`{"items":` + hsRecords(0, 3) + `,"pageInfo":{"totalCount":8}}`)
		}
		return hsJSON(`{"items":[],"pageInfo":{"totalCount":8}}`)
	}
	g := hsNew(t, hsDoc("", "", hsPageKind(3, 1, true), ""), handle)
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	end := hsLast(t, pages).End
	if end.Complete || !strings.Contains(end.Reason, "8 records and 3 were read") {
		t.Fatalf("end: %+v", end)
	}
}

// [REQ-663]
func TestAShortPageDoesNotEndTheReadWhileTheTotalIsNotReached(t *testing.T) {
	g := hsNew(t, hsDoc("", "", hsPageKind(5, 1, true), ""), func(_ int, r hsRequest) hsReply {
		// the source gives at most 3 a page, whatever size is asked
		p := map[string]int{"1": 0, "2": 3, "3": 6}[r.hsQuery().Get("page")]
		return hsJSON(`{"items":` + hsRecords(p, min(p+3, 8)) + `,"pageInfo":{"totalCount":8}}`)
	})
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil || len(hsSerials(pages)) != 8 || !hsLast(t, pages).End.Complete {
		t.Fatalf("rows %d, err %v", len(hsSerials(pages)), err)
	}
}

// [REQ-663]
func TestTheSameAnswerTwiceInARowStopsTheReadAsNotComplete(t *testing.T) {
	g := hsNew(t, hsDoc("", "", hsPageKind(2, 1, false), ""), func(_ int, _ hsRequest) hsReply {
		return hsJSON(`{"items":` + hsRecords(0, 2) + `}`)
	})
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if end := hsLast(t, pages).End; end.Complete || !strings.Contains(end.Reason, "same answer twice") || len(g.hsSrv.hsSeen) != 2 {
		t.Fatalf("end %+v after %d requests", end, len(g.hsSrv.hsSeen))
	}
}

// [REQ-663]
func TestMaxPagesStopsTheReadAsNotComplete(t *testing.T) {
	doc := hsDoc("", "", `{"kind":"page","page_param":"page","size_param":"pageSize","size":2,"first_page":1,"max_pages":3}`, "")
	g := hsNew(t, doc, hsAPI(ingest.PagePage, 100, 2, 1))
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	end := hsLast(t, pages).End
	if end.Complete || !strings.Contains(end.Reason, "max_pages") || len(g.hsSrv.hsSeen) != 3 || len(hsSerials(pages)) != 6 {
		t.Fatalf("end %+v, %d requests, %d rows", end, len(g.hsSrv.hsSeen), len(hsSerials(pages)))
	}
}

// [REQ-663]
func TestACursorThatDoesNotMoveStopsTheReadAsNotComplete(t *testing.T) {
	doc := hsDoc("", "", `{"kind":"cursor","cursor_param":"cursor","cursor_in":"query","next_cursor_path":"meta.next"}`, "")
	g := hsNew(t, doc, func(n int, _ hsRequest) hsReply {
		return hsJSON(`{"items":` + hsRecords(n, n+1) + `,"meta":{"next":"stuck"}}`)
	})
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if end := hsLast(t, pages).End; end.Complete || !strings.Contains(end.Reason, "same cursor") {
		t.Fatalf("end %+v", end)
	}
}

// [REQ-663]
func TestACursorInTheBodyOfAPostReadsThroughThePages(t *testing.T) {
	request := `{"method":"POST","path":"/servers/search","body":{"filter":{"status":"active"},"note":"{{kept}}"}}`
	doc := strings.Replace(hsDoc("", request, `{"kind":"cursor","cursor_param":"cursor","cursor_in":"body","next_cursor_path":"meta.next","size_param":"limit","size":4}`, ""),
		`"records_path":"items"`, `"records_path":"items","post_reads":true`, 1)
	g := hsNew(t, doc, hsAPI(ingest.PageCursor, 9, 4, 1))
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil || len(hsSerials(pages)) != 9 {
		t.Fatalf("rows %d, err %v", len(hsSerials(pages)), err)
	}
	for _, r := range g.hsSrv.hsSeen {
		if r.hsMethod != "POST" || r.hsFirst("Content-Type") != "application/json" || r.hsURL != "https://api.example.test/api/v2/servers/search" ||
			!strings.Contains(r.hsBody, `"note":"{kept}"`) || !strings.Contains(r.hsBody, `"limit":4`) {
			t.Fatalf("request: %+v", r)
		}
	}
	if !strings.Contains(g.hsSrv.hsSeen[1].hsBody, `"cursor":"c4"`) || strings.Contains(g.hsSrv.hsSeen[0].hsBody, "cursor") {
		t.Fatalf("the cursor: %s / %s", g.hsSrv.hsSeen[0].hsBody, g.hsSrv.hsSeen[1].hsBody)
	}
}

// [REQ-663]
func TestAnAddressOfAnotherOriginIsNotFollowed(t *testing.T) {
	for _, tc := range []struct{ kind, doc string }{
		{"next_url", `{"kind":"next_url","next_path":"next"}`},
		{"link_header", `{"kind":"link_header"}`},
	} {
		g := hsNew(t, hsDoc("", "", tc.doc, ""), func(_ int, _ hsRequest) hsReply {
			rep := hsJSON(`{"items":` + hsRecords(0, 2) + `,"next":"https://elsewhere.example.test/api/v2/servers?after=2"}`)
			rep.hsHeader["Link"] = []string{`<https://elsewhere.example.test/api/v2/servers?after=2>; rel="next"`}
			return rep
		})
		pages, err := g.hsRead(ingest.PageOptions{})
		se, ok := ingest.SourceErrorOf(err)
		if !ok || se.Kind != ingest.SrcPolicyDenied || se.Class != ingest.CheckConfig || len(g.hsSrv.hsSeen) != 1 || len(pages) != 0 {
			t.Fatalf("%s: %v after %d requests", tc.kind, err, len(g.hsSrv.hsSeen))
		}
		if strings.Contains(se.Message, "elsewhere") {
			t.Fatalf("the message holds the address: %s", se.Message)
		}
	}
}

// [REQ-663]
func TestAnAddressThatPointsAtItselfStopsTheReadAsNotComplete(t *testing.T) {
	g := hsNew(t, hsDoc("", `{"path":"/servers"}`, `{"kind":"next_url","next_path":"next"}`, ""), func(_ int, _ hsRequest) hsReply {
		return hsJSON(`{"items":` + hsRecords(0, 2) + `,"next":"/api/v2/servers"}`)
	})
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil || hsLast(t, pages).End.Complete {
		t.Fatalf("err %v", err)
	}
}

// [REQ-663]
func TestLinkHeadersAreReadWithManyLinksAndRelations(t *testing.T) {
	cases := []struct {
		header []string
		rel    string
		want   string
	}{
		{[]string{`</a>; rel="next"`}, "next", "/a"},
		{[]string{`</a>; rel="prev", </b,c>; title="x, y"; rel="next last"`}, "next", "/b,c"},
		{[]string{`</a>; rel="prev"`, `</b>; REL=NEXT`}, "next", "/b"},
		{[]string{`</a>; rel="prev"`}, "next", ""},
		{[]string{`/a; rel="next"`}, "next", ""},
		{[]string{`</a`}, "next", ""},
		{[]string{`</a>; title="only"`}, "next", ""},
		{[]string{`</a>; rel="first"`}, "first", "/a"},
	}
	for _, c := range cases {
		if got := linkTarget(map[string][]string{"link": c.header}, c.rel); got != c.want {
			t.Errorf("%v: got %q, want %q", c.header, got, c.want)
		}
	}
}

// [REQ-663]
func TestAnAnswerThatIsNotWhatTheDefinitionExpectsIsAFormatError(t *testing.T) {
	cases := map[string]hsReply{
		"records_path leads to nothing": hsJSON(`{"other":[]}`),
		"records are not an array":      hsJSON(`{"items":{"a":1}}`),
		"not JSON":                      hsJSON(`<html>`),
		"two values":                    hsJSON(`{"items":[]} {"items":[]}`),
		"another type":                  {hsStatus: 200, hsHeader: map[string][]string{"Content-Type": {"text/html"}}, hsBody: `{"items":[]}`},
	}
	for name, rep := range cases {
		g := hsNew(t, hsDoc("", "", "", ""), func(_ int, _ hsRequest) hsReply { return rep })
		_, err := g.hsRead(ingest.PageOptions{})
		se, ok := ingest.SourceErrorOf(err)
		if !ok || se.Kind != ingest.SrcResponseFormat || se.Class != ingest.CheckConfig {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// [REQ-663]
func TestTheRecordsMayBeTheWholeDocumentOrNullOrNothing(t *testing.T) {
	doc := strings.Replace(hsDoc("", "", "", ""), `"records_path":"items",`, "", 1)
	g := hsNew(t, doc, func(_ int, _ hsRequest) hsReply { return hsJSON(hsRecords(0, 3)) })
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil || len(hsSerials(pages)) != 3 {
		t.Fatalf("the document as the array: %v", err)
	}
	g = hsNew(t, hsDoc("", "", "", ""), func(_ int, _ hsRequest) hsReply { return hsJSON(`{"items":null}`) })
	if pages, err = g.hsRead(ingest.PageOptions{}); err != nil || len(hsSerials(pages)) != 0 || !hsLast(t, pages).End.Complete {
		t.Fatalf("null: %v", err)
	}
	g = hsNew(t, doc, func(_ int, _ hsRequest) hsReply { return hsJSON(`null`) })
	if _, err = g.hsRead(ingest.PageOptions{}); err == nil {
		t.Fatal("a null document is no array")
	}
	g = hsNew(t, hsDoc("", "", "", ""), func(_ int, _ hsRequest) hsReply { return hsReply{hsStatus: 204} })
	if pages, err = g.hsRead(ingest.PageOptions{}); err != nil || len(pages) != 1 || !pages[0].End.Complete {
		t.Fatalf("204: %v", err)
	}
}

// [REQ-663]
func TestARecordThatIsNotARowIsUnreadableWithWhereItWas(t *testing.T) {
	g := hsNew(t, hsDoc("", "", "", ""), func(_ int, _ hsRequest) hsReply {
		return hsJSON(`{"items":[` + hsRecord(1) + `,{"location":{"rack":"r1"}},7,` + hsRecord(2) + `]}`)
	})
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p := hsLast(t, pages)
	if len(p.Rows) != 2 || len(p.Unreadable) != 2 || p.Unreadable[0].Where != "page 1, record 2" || p.Unreadable[1].Reason != ingest.ReasonNotAnObject || p.Rows[1].Line != 4 {
		t.Fatalf("rows %d unreadable %+v", len(p.Rows), p.Unreadable)
	}
	if p.Rows[0].Fields["rack"] != "r1" {
		t.Fatalf("fields %v", p.Rows[0].Fields)
	}
}

// [REQ-663]
func TestThePagesAreCutByTheSharedRulesWhateverTheSizeOfAResponse(t *testing.T) {
	g := hsNew(t, hsDoc("", "", hsPageKind(4, 1, true), ""), hsAPI(ingest.PagePage, 10, 4, 1))
	pages, err := g.hsRead(ingest.PageOptions{MaxRows: 3})
	if err != nil {
		t.Fatal(err)
	}
	var sizes []int
	for _, p := range pages {
		sizes = append(sizes, len(p.Rows))
	}
	if !reflect.DeepEqual(sizes, []int{3, 3, 3, 1}) || !reflect.DeepEqual(hsSerials(pages), hsWant(0, 10)) {
		t.Fatalf("sizes %v", sizes)
	}
	capped, _ := g.hsRead(ingest.PageOptions{MaxDatasetRows: 5})
	if end := hsLast(t, capped).End; end.Complete || len(hsSerials(capped)) != 5 {
		t.Fatalf("the dataset limit: %+v", end)
	}
}

// resume reads every page of a dataset by its token and compares the rest it gives with the rest of the full read.
func hsResumeAll(t *testing.T, g *hsRig, opts ingest.PageOptions, all []string) {
	t.Helper()
	pages, err := g.hsRead(opts)
	if err != nil {
		t.Fatal(err)
	}
	given := 0
	for i, p := range pages {
		given += len(p.Rows)
		if p.Last {
			break
		}
		if len(p.Token) == 0 {
			t.Fatalf("page %d has no token", i)
		}
		o := opts
		o.Resume = p.Token
		rest, err := g.hsRead(o)
		if err != nil {
			t.Fatalf("resume after page %d: %v", i, err)
		}
		if got := hsSerials(rest); !reflect.DeepEqual(got, all[given:]) {
			t.Fatalf("resume after page %d: got %v, want %v", i, got, all[given:])
		}
		if !hsLast(t, rest).End.Complete {
			t.Fatalf("resume after page %d is not complete", i)
		}
	}
}

// [REQ-663]
func TestAReadGoesOnFromTheTokenOfAnyPageAndGivesTheSameRows(t *testing.T) {
	for kind, def := range hsKinds() {
		if kind == ingest.PageNextURL || kind == ingest.PageLinkHeader {
			continue
		}
		t.Run(kind, func(t *testing.T) {
			g := hsNew(t, hsDoc("", def.request, def.doc, ""), hsAPI(kind, 10, 4, 1))
			hsResumeAll(t, g, ingest.PageOptions{MaxRows: 3}, hsWant(0, 10))
		})
	}
	g := hsNew(t, hsDoc("", "", "", ""), func(_ int, _ hsRequest) hsReply { return hsJSON(`{"items":` + hsRecords(0, 7) + `}`) })
	hsResumeAll(t, g, ingest.PageOptions{MaxRows: 2}, hsWant(0, 7))
}

// [REQ-663]
func TestATokenGoesOnWithTheNumbersOfTheLinesAndTheTotal(t *testing.T) {
	g := hsNew(t, hsDoc("", "", hsPageKind(4, 1, true), ""), hsAPI(ingest.PagePage, 10, 4, 1))
	pages, _ := g.hsRead(ingest.PageOptions{MaxRows: 5})
	rest, err := g.hsRead(ingest.PageOptions{Resume: pages[0].Token})
	if err != nil {
		t.Fatal(err)
	}
	if first := rest[0].Rows[0]; first.Line != 6 || first.Fields["serial"] != hsSerial(5) {
		t.Fatalf("the line of the first row after the resume: %d", first.Line)
	}
	if !hsLast(t, rest).End.Complete {
		t.Fatal("the total was not checked against the records of both reads")
	}
}

// [REQ-663]
func TestAnEndTokenReadsNothingAndAnUnusableTokenReadsFromTheStart(t *testing.T) {
	g := hsNew(t, hsDoc("", "", hsPageKind(4, 1, true), ""), hsAPI(ingest.PagePage, 8, 4, 1))
	pages, _ := g.hsRead(ingest.PageOptions{MaxRows: 4})
	if len(pages) != 2 {
		t.Fatalf("pages: %d", len(pages))
	}
	before := len(g.hsSrv.hsSeen)
	end := encodeToken(resumeToken{FP: fingerprint(g.hsReader.set, g.hsDef.Datasets[0]), Base: 8, End: true})
	none, err := g.hsRead(ingest.PageOptions{Resume: end})
	if err != nil || len(none) != 1 || len(none[0].Rows) != 0 || !none[0].End.Complete || len(g.hsSrv.hsSeen) != before {
		t.Fatalf("an end token read %d pages: %v", len(none), err)
	}
	for name, token := range map[string][]byte{
		"not JSON":          []byte("x"),
		"another version":   []byte(`{"v":9,"fp":"x"}`),
		"another dataset":   encodeToken(resumeToken{FP: "other", Page: 2}),
		"a negative skip":   encodeToken(resumeToken{FP: fingerprint(g.hsReader.set, g.hsDef.Datasets[0]), Skip: -1}),
		"a negative number": encodeToken(resumeToken{FP: fingerprint(g.hsReader.set, g.hsDef.Datasets[0]), Base: -3}),
	} {
		pages, err := g.hsRead(ingest.PageOptions{Resume: token})
		if err != nil || !reflect.DeepEqual(hsSerials(pages), hsWant(0, 8)) {
			t.Errorf("%s: read %v, %v", name, hsSerials(pages), err)
		}
	}
}

// [REQ-663]
func TestAddressesTheSourceGaveAreNeverPutInAToken(t *testing.T) {
	for _, kind := range []string{ingest.PageNextURL, ingest.PageLinkHeader} {
		g := hsNew(t, hsDoc("", "", hsKinds()[kind].doc, ""), hsAPI(kind, 10, 4, 1))
		pages, err := g.hsRead(ingest.PageOptions{MaxRows: 3})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range pages {
			if p.Token != nil {
				t.Fatalf("%s: a token %s", kind, p.Token)
			}
		}
		again, err := g.hsRead(ingest.PageOptions{Resume: []byte(`{"v":1,"p":2}`)})
		if err != nil || !reflect.DeepEqual(hsSerials(again), hsWant(0, 10)) {
			t.Fatalf("%s: a token is ignored: %v", kind, err)
		}
	}
}

// [REQ-663]
func TestATokenHoldsPositionsAndNothingElse(t *testing.T) {
	doc := hsDoc(hsTokReq, "", `{"kind":"cursor","cursor_param":"cursor","cursor_in":"query","next_cursor_path":"meta.next"}`, "")
	g := hsNew(t, doc, func(n int, r hsRequest) hsReply {
		if strings.HasSuffix(r.hsPath(), "/auth/token") {
			return hsJSON(`{"token":"` + hsMintedValue + `"}`)
		}
		return hsAPI(ingest.PageCursor, 6, 3, 1)(n, r)
	})
	pages, err := g.hsRead(ingest.PageOptions{MaxRows: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pages {
		for _, secret := range []string{hsMintedValue, hsPassValue, hsUserValue, hsTokenValue, "api.example.test", "status"} {
			if strings.Contains(string(p.Token), secret) {
				t.Fatalf("the token %s holds %q", p.Token, secret)
			}
		}
	}
}

// [REQ-665]
func TestACancelledReadEndsWithTheErrorOfTheContextAndClosesWhatItOpened(t *testing.T) {
	g := hsNew(t, hsDoc(hsBearer, "", hsPageKind(2, 1, false), ""), hsAPI(ingest.PagePage, 100, 2, 1))
	ctx, cancel := context.WithCancel(hsCtx)
	defer cancel()
	var got error
	pages := 0
	for _, err := range g.hsReader.ReadPages(ctx, hsSource, g.hsDef.Datasets[0], ingest.PageOptions{MaxRows: 2}) {
		if err != nil {
			got = err
			break
		}
		pages++
		cancel()
	}
	if !errors.Is(got, context.Canceled) || pages != 1 {
		t.Fatalf("pages %d, err %v", pages, got)
	}
	if g.hsSrv.hsShut != 1 || g.hsSrv.hsClients != 1 {
		t.Fatalf("the client was made %d and closed %d times", g.hsSrv.hsClients, g.hsSrv.hsShut)
	}
	ctx2, cancel2 := context.WithCancel(hsCtx)
	cancel2()
	for _, err := range g.hsReader.ReadPages(ctx2, hsSource, g.hsDef.Datasets[0], ingest.PageOptions{}) {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("a read that begins cancelled: %v", err)
		}
	}
}

// [REQ-665]
func TestLeavingTheLoopClosesTheClientAndTheBodies(t *testing.T) {
	g := hsNew(t, hsDoc(hsBearer, "", hsPageKind(2, 1, false), ""), hsAPI(ingest.PagePage, 100, 2, 1))
	for _, err := range g.hsReader.ReadPages(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PageOptions{MaxRows: 2}) {
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if g.hsSrv.hsShut != 1 || g.hsSrv.hsBodies != g.hsSrv.hsOpened {
		t.Fatalf("closed clients %d, bodies %d of %d", g.hsSrv.hsShut, g.hsSrv.hsBodies, g.hsSrv.hsOpened)
	}
}
