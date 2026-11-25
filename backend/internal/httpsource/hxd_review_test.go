package httpsource

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// The tests of what an independent review of ADR-0109 found: what is read as whole must be whole. Every name begins with hxd.

const hxdBlobDS = `{"id":"ds_a","request":{"path":"/hosts"},"records_path":"result[*].interfaces","natural_key":["id"],"columns":[{"name":"blob","paths":["^.blob"]},{"name":"id"}]}`

func hxdBlobBody(blob string, n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = `{"id":"i` + strconv.Itoa(i) + `"}`
	}
	return `{"result":[{"hostid":"h1","blob":"` + blob + `","interfaces":[` + strings.Join(parts, ",") + `]}]}`
}

func hxdIsFormat(err error) bool {
	se, ok := ingest.SourceErrorOf(err)
	return ok && se.Kind == ingest.SrcResponseFormat
}

// [REQ-909]
// The rows made of one response may hold at most four times max_response_bytes: a value of the outer record that is read by ^. is in
// every row of its elements, so a response of 1 KB would be a hundred times that. More is a failure of the shape of the answer, and a
// response whose rows are under it is read as before.
func TestHxdTheRowsOfOneResponseAreBoundedByFourTimesTheBytesOfTheResponseLimit(t *testing.T) {
	conn := `,"max_response_bytes":1024`
	big := strings.Repeat("b", 400)
	g := hxdNew(t, hxdDoc(conn, hxdBlobDS), func(int, hsRequest) hxdReply { return hxdJSON(hxdBlobBody(big, 20)) })
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if !hxdIsFormat(err) || strings.Contains(strings.Join(hxdKeys(pages), ","), "i19") {
		t.Errorf("20 rows of 400 bytes from a response of under 1 KB (8000 bytes against 4096): %v", err)
	}
	g = hxdNew(t, hxdDoc(conn, hxdBlobDS), func(int, hsRequest) hxdReply { return hxdJSON(hxdBlobBody(big, 5)) })
	pages, err = g.hxdRead(hsCtx, ingest.PageOptions{})
	if end := hxdEnd(t, pages); err != nil || !end.Complete || len(hxdKeys(pages)) != 5 {
		t.Errorf("5 rows of 400 bytes (2000 against 4096): %+v, %v", end, err)
	}
	// The same in a response of JSON lines.
	lines := strings.Replace(strings.Replace(hxdBlobDS, `"records_path":"result[*].interfaces"`, `"response_format":"json_lines","records_path":"interfaces[*]"`, 1), `"interfaces[*]"`, `"[*].interfaces"`, 1)
	body := strings.Replace(hxdBlobBody(big, 20), `{"result":[`, "", 1)
	body = strings.TrimSuffix(body, `]}`)
	g = hxdNew(t, hxdDoc(conn, lines), func(int, hsRequest) hxdReply { return hxdOK(body+"\n", true) })
	if _, err = g.hxdRead(hsCtx, ingest.PageOptions{}); !hxdIsFormat(err) {
		t.Errorf("lines: %v", err)
	}
}

// [REQ-901]
// A body of JSON lines that has no value at all is only what was seen, with a mark of its end or without (an export that has no
// record and one that was not sent cannot be told apart), and so is an answer of no content.
func TestHxdABodyOfLinesWithNoValueIsOnlyWhatWasSeen(t *testing.T) {
	for _, body := range []string{"", "\n", " \n\n  "} {
		g := hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply { return hxdOK(body, true) })
		pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
		if end := hxdEnd(t, pages); err != nil || end.Complete || !end.SeenOnly || len(hxdKeys(pages)) != 0 {
			t.Errorf("%q: %+v, %v", body, end, err)
		}
	}
	g := hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply { return hxdOK(hxdLine(1)+"\n", true) })
	if pages, err := g.hxdRead(hsCtx, ingest.PageOptions{}); err != nil || !hxdEnd(t, pages).Complete {
		t.Errorf("one value: %v", err)
	}
}

// [REQ-906]
// A read that goes on from a token has not read the records before it, so the token carries how many of them had no array: the
// dataset still ends as what was seen, whichever page it goes on from.
func TestHxdAReadThatGoesOnFromATokenKnowsTheRecordsBeforeItThatHadNoArray(t *testing.T) {
	hosts := hxdFive()
	hosts[1] = hxdHost{id: "h2", omit: true}
	doc := hxdDoc("", hxdHostsDS(`,"total_path":"total"`, ""))
	g := hxdNew(t, doc, hxdHostsServer(hosts))
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{MaxRows: 2})
	if end := hxdEnd(t, pages); err != nil || !end.SeenOnly {
		t.Fatalf("from the start: %+v, %v", end, err)
	}
	for i, p := range pages[:len(pages)-1] {
		again := hxdNew(t, doc, hxdHostsServer(hosts))
		rest, err := again.hxdRead(hsCtx, ingest.PageOptions{MaxRows: 2, Resume: p.Token})
		if end := hxdEnd(t, rest); err != nil || end.Complete || !end.SeenOnly {
			t.Errorf("from the token of page %d: %+v, %v", i+1, end, err)
		}
	}
	// And with missing_array empty, where every record before the token had none, the guard still knows.
	none := hxdFive()
	for i := range none[:3] {
		none[i] = hxdHost{id: none[i].id, omit: true}
	}
	emptyDoc := hxdDoc("", hxdHostsDS(`,"total_path":"total"`, `"missing_array":"empty",`))
	g = hxdNew(t, emptyDoc, hxdHostsServer(none))
	pages, err = g.hxdRead(hsCtx, ingest.PageOptions{MaxRows: 1})
	if end := hxdEnd(t, pages); err != nil || !end.Complete {
		t.Fatalf("empty, some have arrays: %+v, %v", end, err)
	}
	for i, p := range pages[:len(pages)-1] {
		again := hxdNew(t, emptyDoc, hxdHostsServer(none))
		rest, err := again.hxdRead(hsCtx, ingest.PageOptions{MaxRows: 1, Resume: p.Token})
		if end := hxdEnd(t, rest); err != nil || !end.Complete {
			t.Errorf("empty, from the token of page %d: %+v, %v", i+1, end, err)
		}
	}
}

// [REQ-905]
// A record that is not an object, or a step on the way to the array that is a value of another kind, is not "no array": it is a
// failure of the shape of the answer, with missing_array empty as without.
func TestHxdARecordOfAnotherKindIsNotARecordWithNoArray(t *testing.T) {
	for _, c := range []struct {
		name, path, body string
	}{
		{"a record that is a text", "result[*].interfaces", `{"result":[{"hostid":"h1","interfaces":[{"id":"i1"}]},"denied"]}`},
		{"a record that is a number", "result[*].interfaces", `{"result":[17]}`},
		{"a record that is null", "result[*].interfaces", `{"result":[null]}`},
		{"a step that is a text", "result[*].net.interfaces", `{"result":[{"hostid":"h1","net":"permission denied"}]}`},
		{"a step that is a number", "result[*].net.interfaces", `{"result":[{"hostid":"h1","net":7}]}`},
	} {
		for _, extra := range []string{``, `"missing_array":"empty",`} {
			ds := strings.Replace(hxdBlobDS, `result[*].interfaces`, c.path, 1)
			ds = strings.Replace(ds, `"natural_key"`, extra+`"natural_key"`, 1)
			g := hxdNew(t, hxdDoc("", ds), func(int, hsRequest) hxdReply { return hxdJSON(c.body) })
			if _, err := g.hxdRead(hsCtx, ingest.PageOptions{}); !hxdIsFormat(err) {
				t.Errorf("%s %s: %v", c.name, extra, err)
			}
		}
	}
	// A step that is null, or a key that is not there, is no array.
	ds := strings.Replace(hxdBlobDS, `result[*].interfaces`, `result[*].net.interfaces`, 1)
	g := hxdNew(t, hxdDoc("", ds), func(int, hsRequest) hxdReply { return hxdJSON(`{"result":[{"hostid":"h1","net":null}]}`) })
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if end := hxdEnd(t, pages); err != nil || !end.SeenOnly {
		t.Errorf("a null step: %+v, %v", end, err)
	}
	// In lines, a line that is no object is the same.
	lines := strings.Replace(hxdBlobDS, `"records_path"`, `"response_format":"json_lines","records_path"`, 1)
	lines = strings.Replace(lines, `result[*].interfaces`, `[*].interfaces`, 1)
	g = hxdNew(t, hxdDoc("", lines), func(int, hsRequest) hxdReply { return hxdOK("17\n", true) })
	if _, err = g.hxdRead(hsCtx, ingest.PageOptions{}); !hxdIsFormat(err) {
		t.Errorf("a line that is a number: %v", err)
	}
}

// [REQ-901]
// An element that is not an object is a failure of the shape of the answer, and a read that had records and made no row (all of
// them passed over by a rule of the definition) is only what was seen, with one event.
func TestHxdAnElementThatIsNoObjectFailsAndRecordsThatMadeNoRowAreOnlyWhatWasSeen(t *testing.T) {
	g := hxdNew(t, hxdDoc("", hxdBlobDS), func(int, hsRequest) hxdReply {
		return hxdJSON(`{"result":[{"hostid":"h1","interfaces":["eth0","eth1"]}]}`)
	})
	if _, err := g.hxdRead(hsCtx, ingest.PageOptions{}); !hxdIsFormat(err) {
		t.Errorf("elements that are texts: %v", err)
	}
	skip := strings.Replace(hxdLines, `"natural_key"`, `"ignore_rows_without_key":true,"natural_key"`, 1)
	g = hxdNew(t, hxdDoc("", skip), func(int, hsRequest) hxdReply { return hxdOK(`{"error":"quota exceeded"}`+"\n", true) })
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if end := hxdEnd(t, pages); err != nil || end.Complete || !end.SeenOnly || len(hxdKeys(pages)) != 0 || !strings.Contains(end.Reason, "none made a row") {
		t.Errorf("lines, all passed over: %+v, %v", end, err)
	}
	events := 0
	for _, e := range g.events {
		if e.Kind == ingest.EventIncomplete {
			events++
		}
	}
	if events != 1 {
		t.Errorf("%d events, want 1", events)
	}
	g = hxdNew(t, hxdDoc("", skip), func(int, hsRequest) hxdReply { return hxdOK(hxdLine(1)+"\n"+`{"error":"x"}`+"\n", true) })
	if pages, err = g.hxdRead(hsCtx, ingest.PageOptions{}); err != nil || !hxdEnd(t, pages).Complete || len(hxdKeys(pages)) != 1 {
		t.Errorf("lines, one row: %v", err)
	}
	// A path that opens an array, whose elements have no natural key and are all passed over.
	exp := strings.Replace(hxdBlobDS, `"natural_key":["id"]`, `"ignore_rows_without_key":true,"natural_key":["id"]`, 1)
	g = hxdNew(t, hxdDoc("", exp), func(int, hsRequest) hxdReply {
		return hxdJSON(`{"result":[{"hostid":"h1","interfaces":[{"name":"a"},{"name":"b"}]}]}`)
	})
	pages, err = g.hxdRead(hsCtx, ingest.PageOptions{})
	if end := hxdEnd(t, pages); err != nil || end.Complete || !end.SeenOnly {
		t.Errorf("an array, all passed over: %+v, %v", end, err)
	}
}

// [REQ-901]
// A limit or a fault of the shape that comes when the connection had broken is the failure it is, not what was seen: the break
// does not make it smaller.
func TestHxdAFaultOfTheShapeWinsOverABreak(t *testing.T) {
	lines := strings.Replace(hxdBlobDS, `"records_path"`, `"response_format":"json_lines","max_elements":2,"records_path"`, 1)
	lines = strings.Replace(lines, `result[*].interfaces`, `[*].interfaces`, 1)
	body := `{"hostid":"h1","interfaces":[{"id":"a"},{"id":"b"},{"id":"c"}]}` + "\n"
	g := hxdNew(t, hxdDoc("", lines), func(int, hsRequest) hxdReply {
		rep := hxdOK(body, false)
		rep.cutAt, rep.together = len(body), true
		return rep
	})
	if _, err := g.hxdRead(hsCtx, ingest.PageOptions{}); !hxdIsFormat(err) {
		t.Errorf("more elements than the limit, and a break: %v", err)
	}
	// A break alone is what was seen.
	g = hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply {
		rep := hxdOK(hxdLine(1)+"\n"+hxdLine(2)+"\n", false)
		rep.cutAt, rep.together = len(hxdLine(1))+1, true
		return rep
	})
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if end := hxdEnd(t, pages); err != nil || !end.SeenOnly || !slices.Equal(hxdKeys(pages), []string{"r001"}) {
		t.Errorf("a break: %+v, %v, %v", end, err, hxdKeys(pages))
	}
}
