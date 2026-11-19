package httpsource

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// hsfFormat says that an error is one of the failures of the shape of an answer, with a word of its sentence.
func hsfFormat(t *testing.T, name string, err error, word string) {
	t.Helper()
	se, ok := ingest.SourceErrorOf(err)
	if !ok || se.Kind != ingest.SrcResponseFormat || se.Class != ingest.CheckConfig || !strings.Contains(se.Message, word) {
		t.Errorf("%s: %v", name, err)
	}
}

// hsfPadded is a record of the first dataset with a text of size bytes in it.
func hsfPadded(i, size int) string {
	return fmt.Sprintf(`{"serial":%q,"location":{"rack":%q}}`, hsSerial(i), strings.Repeat("x", size))
}

// [REQ-692]
// A record is read up to 2 MiB as the source wrote it and a bigger one is a failure of the shape of the answer, and so is a
// value that is not a record and is that big.
func TestHsfARecordHasAMostOfTwoMebibytes(t *testing.T) {
	fits := hsNew(t, hsDoc("", "", "", ""), func(int, hsRequest) hsReply {
		return hsJSON(`{"items":[` + hsfPadded(0, 1<<20) + `,` + hsfPadded(1, (2<<20)-100) + `]}`)
	})
	pages, err := fits.hsRead(ingest.PageOptions{})
	if err != nil || len(hsSerials(pages)) != 2 {
		t.Fatalf("records under the most: %v", err)
	}
	for name, body := range map[string]string{
		"a record":                  `{"items":[` + hsfPadded(0, 10) + `,` + hsfPadded(1, (2<<20)+10) + `]}`,
		"the only record":           `{"items":[` + hsfPadded(1, 3<<20) + `]}`,
		"a value that is no record": `{"other":"` + strings.Repeat("y", 3<<20) + `","items":[]}`,
	} {
		g := hsNew(t, hsDoc("", "", "", ""), func(int, hsRequest) hsReply { return hsJSON(body) })
		_, err := g.hsRead(ingest.PageOptions{})
		hsfFormat(t, name, err, strconv.Itoa(2<<20))
		if g.hsSrv.hsBodies != g.hsSrv.hsOpened {
			t.Errorf("%s: a body was left open", name)
		}
	}
}

// [REQ-692]
// The places the pagination reads are found before the records and after them, and in an array.
func TestHsfThePlacesThePaginationReadsAreFoundWhereverTheyAre(t *testing.T) {
	where := map[string]func(items string, next string) string{
		"before the records": func(items, next string) string { return `{"meta":{"next":"` + next + `"},"items":` + items + `}` },
		"after the records":  func(items, next string) string { return `{"items":` + items + `,"meta":{"next":"` + next + `"}}` },
		"in the middle": func(items, next string) string {
			return `{"a":[1,{"b":2}],"meta":{"next":"` + next + `"},"z":null,"items":` + items + `}`
		},
	}
	for name, body := range where {
		handle := func(_ int, r hsRequest) hsReply {
			start := hsCursorStart(r.hsQuery().Get("cursor"), r.hsBody)
			end := min(start+4, 10)
			next := ""
			if end < 10 {
				next = "c" + strconv.Itoa(end)
			}
			return hsJSON(body(hsRecords(start, end), next))
		}
		g := hsNew(t, hsDoc("", "", `{"kind":"cursor","cursor_param":"cursor","cursor_in":"query","next_cursor_path":"meta.next"}`, ""), handle)
		pages, err := g.hsRead(ingest.PageOptions{})
		if err != nil || !reflect.DeepEqual(hsSerials(pages), hsWant(0, 10)) || !hsLast(t, pages).End.Complete || len(g.hsSrv.hsSeen) != 3 {
			t.Errorf("%s: %v, %d requests", name, err, len(g.hsSrv.hsSeen))
		}
	}
	// a total after the records, and a path that goes down through an array
	handle := func(_ int, r hsRequest) hsReply {
		p, _ := strconv.Atoi(r.hsQuery().Get("page"))
		start := (p - 1) * 4
		end := min(start+4, 10)
		return hsJSON(`{"data":[{"items":` + hsRecords(start, end) + `}],"info":[0,{"total":10}]}`)
	}
	doc := strings.Replace(hsDoc("", "", `{"kind":"page","page_param":"page","size_param":"pageSize","size":4,"first_page":1,"total_path":"info.1.total"}`, ""), `"records_path":"items"`, `"records_path":"data.0.items"`, 1)
	g := hsNew(t, doc, handle)
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil || !reflect.DeepEqual(hsSerials(pages), hsWant(0, 10)) || !hsLast(t, pages).End.Complete {
		t.Fatalf("a total in an array: %v", err)
	}
}

// [REQ-692]
// An answer that is not the shape the definition says is a failure of that shape, in whatever order its parts come.
func TestHsfAnAnswerThatIsNotTheShapeIsAFailureAtTheFirstPlaceItIsSeen(t *testing.T) {
	deep := strings.Repeat("[", 600) + strings.Repeat("]", 600)
	nested := `{"serial":"s-1","deep":` + strings.Repeat("[", 3000) + strings.Repeat("]", 3000) + `}`
	for name, c := range map[string]struct{ body, word string }{
		"the records twice":              {`{"items":[],"items":[]}`, "twice"},
		"parts nested too deep":          {`{"a":` + deep + `,"items":[]}`, "deeper"},
		"an object where the array is":   {`{"items":{"a":1},"x":[`, "not an array"},
		"a string where the array is":    {`{"items":"a"}`, "not an array"},
		"nothing":                        {``, "JSON"},
		"an end that is cut":             {`{"items":[` + hsRecord(0) + `,`, "JSON"},
		"something after the one value":  {`{"items":[]} x`, "JSON"},
		"records_path that is not there": {`{"x":[]}`, "records_path"},
	} {
		g := hsNew(t, hsDoc("", "", "", ""), func(int, hsRequest) hsReply { return hsJSON(c.body) })
		_, err := g.hsRead(ingest.PageOptions{})
		hsfFormat(t, name, err, c.word)
	}
	g := hsNew(t, hsDoc("", "", "", ""), func(int, hsRequest) hsReply { return hsJSON(`{"items":[` + nested + `]}`) })
	if pages, err := g.hsRead(ingest.PageOptions{}); err != nil || len(pages) != 1 {
		t.Errorf("a record that nests deep is a record: %v", err)
	}
}

// [REQ-692]
// An answer whose body breaks after some of its records were given is asked for again, and the records that were given are
// passed over in the new answer: every record is on the pages once.
func TestHsfARecordIsNeverGivenTwiceWhenABodyBreaksAndTheRequestIsSentAgain(t *testing.T) {
	body := `{"items":` + hsRecords(0, 40) + `}`
	g := hsNew(t, hsDoc("", "", "", ""), func(n int, _ hsRequest) hsReply {
		rep := hsJSON(body)
		if n == 1 {
			rep.hsReadErr = &egress.Error{Kind: egress.KindConnect, Reason: "the connection failed or broke"}
		}
		return rep
	})
	pages, err := g.hsRead(ingest.PageOptions{MaxRows: 7})
	if err != nil || !reflect.DeepEqual(hsSerials(pages), hsWant(0, 40)) || len(g.hsSrv.hsSeen) != 2 || g.hsSrv.hsBodies != 2 {
		t.Fatalf("%d rows after %d requests: %v", len(hsSerials(pages)), len(g.hsSrv.hsSeen), err)
	}
	retries := 0
	for _, e := range g.hsEvents {
		if e.Kind == ingest.EventRetry {
			retries++
		}
	}
	if retries != 1 || len(g.hsSleeps) != 1 {
		t.Errorf("%d retries, waits %v", retries, g.hsSleeps)
	}
	// a POST is never sent again, whatever the body did
	post := strings.Replace(hsDoc("", `{"method":"POST","path":"/servers/search","body":{"a":1}}`, "", ""), `"records_path":"items"`, `"records_path":"items","post_reads":true`, 1)
	g = hsNew(t, post, func(int, hsRequest) hsReply {
		rep := hsJSON(body)
		rep.hsReadErr = &egress.Error{Kind: egress.KindConnect, Reason: "x"}
		return rep
	})
	if _, err := g.hsRead(ingest.PageOptions{}); err == nil || len(g.hsSrv.hsSeen) != 1 {
		t.Errorf("a POST whose body broke: %d requests, %v", len(g.hsSrv.hsSeen), err)
	}
}

// [REQ-692]
// The event of a response tells how many bytes the body had, and it comes when the body has been read.
func TestHsfTheEventOfAResponseHasTheBytesOfTheBodyRead(t *testing.T) {
	body := `{"items":` + hsRecords(0, 30) + `}`
	g := hsNew(t, hsDoc("", "", "", ""), func(int, hsRequest) hsReply { return hsJSON(body) })
	if _, err := g.hsRead(ingest.PageOptions{MaxRows: 5}); err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, e := range g.hsEvents {
		if e.Kind == ingest.EventResponse {
			seen = true
			if e.Bytes != len(body) || e.Status != 200 {
				t.Errorf("%+v, the body has %d bytes", e, len(body))
			}
		}
	}
	if !seen {
		t.Fatal("no event of a response")
	}
}

// [REQ-693]
// A token is made when a page is cut and not for every record: for an answer of twenty thousand records the function that
// makes it is called once for each page that is not the last.
func TestHsfATokenIsMadeOncePerPageAndNotOncePerRecord(t *testing.T) {
	var recs []string
	for i := 0; i < 20000; i++ {
		recs = append(recs, `{"serial":"`+hsSerial(i)+`"}`)
	}
	body := `{"items":[` + strings.Join(recs, ",") + `],"meta":{"next":""}}`
	g := hsNew(t, hsDoc("", "", `{"kind":"cursor","cursor_param":"cursor","cursor_in":"query","next_cursor_path":"meta.next"}`, ""), func(int, hsRequest) hsReply { return hsJSON(body) })
	ds := g.hsDef.Datasets[0]
	rn, err := g.hsReader.begin(hsCtx, hsSource, ds, "run")
	if err != nil {
		t.Fatal(err)
	}
	defer rn.close()
	pages, rows := 0, 0
	rn.read(hsCtx, ds, ingest.PageOptions{MaxRows: 100}, func(p ingest.Page, err error) bool {
		if err != nil {
			t.Fatal(err)
		}
		pages++
		rows += len(p.Rows)
		return true
	})
	if rows != 20000 || pages != 200 || rn.tokens != pages-1 {
		t.Fatalf("%d rows in %d pages, the token was made %d times", rows, pages, rn.tokens)
	}
	// the tokens that were made go on from where the pages end, as they did when one was made for every record
	all, err := g.hsRead(ingest.PageOptions{MaxRows: 100})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := g.hsRead(ingest.PageOptions{MaxRows: 100, Resume: all[49].Token})
	if err != nil || !reflect.DeepEqual(hsSerials(resumed), hsWant(5000, 20000)) {
		t.Fatalf("a read that goes on from the token of the 50th page: %d rows, %v", len(hsSerials(resumed)), err)
	}
}

// [REQ-693]
// A value the source gave for the next page is read up to 8 KiB, and a longer one is a failure of the shape of the answer
// that the read ends with, a cursor and an address and the target of a Link header alike.
func TestHsfAValueForTheNextPageHasAMostOfEightKibibytes(t *testing.T) {
	cursor := func(n int) string { return strings.Repeat("c", n) }
	for _, size := range []int{8 << 10, 8<<10 + 1, 1 << 20} {
		fails := size > 8<<10
		kinds := map[string]struct {
			doc    string
			handle func(n int) hsReply
		}{
			"cursor": {`{"kind":"cursor","cursor_param":"cursor","cursor_in":"query","next_cursor_path":"meta.next"}`, func(n int) hsReply {
				if n == 1 {
					return hsJSON(`{"items":` + hsRecords(0, 2) + `,"meta":{"next":"` + cursor(size) + `"}}`)
				}
				return hsJSON(`{"items":` + hsRecords(2, 3) + `,"meta":{"next":""}}`)
			}},
			"next_url": {`{"kind":"next_url","next_path":"next"}`, func(n int) hsReply {
				if n == 1 {
					return hsJSON(`{"items":` + hsRecords(0, 2) + `,"next":"/api/v2/s/` + cursor(size-len("/api/v2/s/")) + `"}`)
				}
				return hsJSON(`{"items":` + hsRecords(2, 3) + `,"next":""}`)
			}},
			"link_header": {`{"kind":"link_header"}`, func(n int) hsReply {
				if n == 1 {
					rep := hsJSON(`{"items":` + hsRecords(0, 2) + `}`)
					rep.hsHeader["Link"] = []string{`</api/v2/s/` + cursor(size-len("/api/v2/s/")) + `>; rel="next"`}
					return rep
				}
				return hsJSON(`{"items":` + hsRecords(2, 3) + `}`)
			}},
		}
		for kind, c := range kinds {
			g := hsNew(t, hsDoc("", "", c.doc, ""), func(n int, _ hsRequest) hsReply { return c.handle(n) })
			pages, err := g.hsRead(ingest.PageOptions{})
			name := kind + " of " + strconv.Itoa(size)
			if fails {
				hsfFormat(t, name, err, "8192")
				if len(pages) != 0 && len(hsSerials(pages)) > 0 {
					t.Errorf("%s: rows were given although the read failed on the first page", name)
				}
				continue
			}
			if err != nil || len(hsSerials(pages)) != 3 {
				t.Errorf("%s: %v", name, err)
			}
		}
	}
}

// [REQ-694]
// A Retry-After that is more than a duration holds is longer than the limit, whatever it is: the read ends with the
// failure at once, and no wait is negative.
func TestHsfARetryAfterThatOverflowsIsLongerThanTheLimit(t *testing.T) {
	for _, value := range []string{"9223372036", "9223372037", "9223372036854775807", "9223372036854775808", "99999999999999999999"} {
		g := hsNew(t, hsDoc("", "", "", ""), hsFails(3, hsWithHeader(hsStatus(429), "Retry-After", value)))
		_, err := g.hsRead(ingest.PageOptions{})
		se, ok := ingest.SourceErrorOf(err)
		if !ok || se.Kind != ingest.SrcRateLimited || len(g.hsSrv.hsSeen) != 1 || len(g.hsSleeps) != 0 {
			t.Errorf("%s: %v after %d requests, waits %v", value, err, len(g.hsSrv.hsSeen), g.hsSleeps)
		}
	}
	for in, want := range map[string]time.Duration{"3": 3 * time.Second, "0": 0, "2562047": 2562047 * time.Second} {
		if got, ok := retryAfter(map[string][]string{"Retry-After": {in}}, hsEpoch); !ok || got != want {
			t.Errorf("%q: %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"9223372037", "99999999999999999999"} {
		if got, ok := retryAfter(map[string][]string{"Retry-After": {in}}, hsEpoch); !ok || got <= 0 {
			t.Errorf("%q: %v %v", in, got, ok)
		}
	}
	if _, ok := retryAfter(map[string][]string{"Retry-After": {"-99999999999999999999"}}, hsEpoch); ok {
		t.Error("a number below zero is a wait")
	}
}
