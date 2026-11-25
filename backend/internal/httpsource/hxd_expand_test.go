package httpsource

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

var hxdAll = []string{"h1/i1", "h1/i2", "h1/i3", "h3/i4", "h3/i5", "h4/i6", "h5/i7", "h5/i8", "h5/i9", "h5/i10"}

// [REQ-903]
// The records of a dataset whose records_path opens an array in each record are the elements, in the order of the answer, and
// the value of the record a path that begins with ^ reads is the record the element is in.
func TestHxdTheElementsOfTheArraysAreTheRecordsInTheOrderOfTheAnswer(t *testing.T) {
	g := hxdNew(t, hxdDoc("", hxdHostsDS(`,"total_path":"total"`, "")), hxdHostsServer(hxdFive()))
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(hxdKeys(pages), hxdAll) {
		t.Errorf("rows %v", hxdKeys(pages))
	}
	if end := hxdEnd(t, pages); !end.Complete || end.SeenOnly {
		t.Errorf("end %+v", end)
	}
	if first := pages[0].Rows[0]; first.Where != "page 1, record 1, element 1" || first.Fields["kind"] != "k" {
		t.Errorf("the first row is %+v", first)
	}
}

// [REQ-904]
// The pages go by the records of the answer and not by the elements: the offset moves by the number of hosts, a short page is a short
// number of hosts, and the total the source gives is the number of hosts, with or without a total and for an empty host among them.
func TestHxdThePagesAreCountedByTheOuterRecords(t *testing.T) {
	for _, c := range []struct {
		name, extra string
		hosts       []hxdHost
		offsets     []string
	}{
		{"with a total", `,"total_path":"total"`, hxdFive(), []string{"0", "2", "4"}},
		{"with no total, the last page short", ``, hxdFive(), []string{"0", "2", "4"}},
		{"with no total, an empty page ends it", ``, hxdFive()[:4], []string{"0", "2", "4"}},
	} {
		g := hxdNew(t, hxdDoc("", hxdHostsDS(c.extra, "")), hxdHostsServer(c.hosts))
		pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !slices.Equal(hxdOffsets(g), c.offsets) || !hxdEnd(t, pages).Complete {
			t.Errorf("%s: offsets %v, end %+v", c.name, hxdOffsets(g), hxdEnd(t, pages))
		}
	}
	// A total that is not the number of hosts read is a dataset that is not complete.
	g := hxdNew(t, hxdDoc("", hxdHostsDS(`,"total_path":"total"`, "")), func(_ int, r hsRequest) hxdReply {
		return hxdJSON(strings.Replace(hxdHostsBody(hxdFive(), r), `"total":5`, `"total":7`, 1))
	})
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if end := hxdEnd(t, pages); err != nil || end.Complete || end.SeenOnly {
		t.Errorf("a total of 7 hosts and 5 read: %+v, %v", end, err)
	}
}

// [REQ-906]
// A page is cut inside a record when the page is full, and the token of such a page goes on from the start of the record: the
// elements that were given are given again and none of the rest is passed over.
func TestHxdAPageIsCutInsideARecordAndTheTokenGoesOnFromItsStart(t *testing.T) {
	g := hxdNew(t, hxdDoc("", hxdHostsDS(`,"total_path":"total"`, "")), hxdHostsServer(hxdFive()))
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{MaxRows: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 5 || !slices.Equal(hxdKeys(pages), hxdAll) {
		t.Fatalf("%d pages, rows %v", len(pages), hxdKeys(pages))
	}
	// Page 1 ends with the first element of h3, the record that is cut: the next read starts at h3.
	again := hxdNew(t, hxdDoc("", hxdHostsDS(`,"total_path":"total"`, "")), hxdHostsServer(hxdFive()))
	rest, err := again.hxdRead(hsCtx, ingest.PageOptions{MaxRows: 2, Resume: pages[1].Token})
	if err != nil {
		t.Fatal(err)
	}
	if want := hxdAll[3:]; !slices.Equal(hxdKeys(rest), want) {
		t.Errorf("a read that goes on from a page that ends inside h3 gave %v, want %v", hxdKeys(rest), want)
	}
	// Page 2 ends with the last element of h4, so the next read starts at h5.
	again = hxdNew(t, hxdDoc("", hxdHostsDS(`,"total_path":"total"`, "")), hxdHostsServer(hxdFive()))
	rest, err = again.hxdRead(hsCtx, ingest.PageOptions{MaxRows: 2, Resume: pages[2].Token})
	if want := hxdAll[6:]; err != nil || !slices.Equal(hxdKeys(rest), want) {
		t.Errorf("a read that goes on from a page that ends with a whole record gave %v (%v), want %v", hxdKeys(rest), err, want)
	}
}

// [REQ-905]
// A record that has no array, or null, is not seen: the dataset is read as what was seen, and the elements of the others are rows.
// An empty array is a record with no elements and takes nothing from the dataset.
func TestHxdARecordWithNoArrayMakesTheDatasetOnlyWhatWasSeen(t *testing.T) {
	for _, c := range []struct {
		name string
		host hxdHost
		seen bool
	}{
		{"no field", hxdHost{id: "h2", omit: true}, true},
		{"null", hxdHost{id: "h2", raw: "null"}, true},
		{"an empty array", hxdHost{id: "h2", ifs: []string{}}, false},
	} {
		hosts := hxdFive()
		hosts[1] = c.host
		g := hxdNew(t, hxdDoc("", hxdHostsDS(`,"total_path":"total"`, "")), hxdHostsServer(hosts))
		pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
		end := hxdEnd(t, pages)
		if err != nil || !slices.Equal(hxdKeys(pages), hxdAll) || end.SeenOnly != c.seen || end.Complete == c.seen {
			t.Errorf("%s: end %+v, rows %v, err %v", c.name, end, hxdKeys(pages), err)
		}
		if c.seen && end.Reason == "" {
			t.Errorf("%s: no reason is given", c.name)
		}
	}
}

// [REQ-905]
// A source that leaves out an empty array says so (missing_array empty): a record with no array has no elements and the dataset is
// as complete as it was. A value that is not an array is a failure either way.
func TestHxdMissingArrayEmptyKeepsTheDatasetCompleteAndAValueThatIsNoArrayFails(t *testing.T) {
	hosts := hxdFive()
	hosts[1] = hxdHost{id: "h2", omit: true}
	g := hxdNew(t, hxdDoc("", hxdHostsDS(`,"total_path":"total"`, `"missing_array":"empty",`)), hxdHostsServer(hosts))
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if end := hxdEnd(t, pages); err != nil || !end.Complete || end.SeenOnly || !slices.Equal(hxdKeys(pages), hxdAll) {
		t.Errorf("end %+v, rows %v, err %v", end, hxdKeys(pages), err)
	}
	for _, extra := range []string{``, `"missing_array":"empty",`} {
		for _, raw := range []string{`{"a":1}`, `"i1"`, `3`} {
			hosts[1] = hxdHost{id: "h2", raw: raw}
			g = hxdNew(t, hxdDoc("", hxdHostsDS(`,"total_path":"total"`, extra)), hxdHostsServer(hosts))
			_, err = g.hxdRead(hsCtx, ingest.PageOptions{})
			if se, ok := ingest.SourceErrorOf(err); !ok || se.Kind != ingest.SrcResponseFormat {
				t.Errorf("%s %s: %v", extra, raw, err)
			}
		}
	}
}

// [REQ-905]
// When not one record of the dataset has an array, missing_array empty does not make it complete: a records_path that is wrong reads
// the same, and the dataset would close every object it had. It is read as what was seen, with an event that says to check the path.
func TestHxdMissingArrayEmptyDoesNotMakeADatasetOfRecordsWithNoArrayAtAllComplete(t *testing.T) {
	hosts := hxdFive()
	for i := range hosts {
		hosts[i].omit = true
	}
	g := hxdNew(t, hxdDoc("", hxdHostsDS(`,"total_path":"total"`, `"missing_array":"empty",`)), hxdHostsServer(hosts))
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	end := hxdEnd(t, pages)
	if err != nil || !end.SeenOnly || end.Complete || !strings.Contains(end.Reason, "records_path") || len(hxdKeys(pages)) != 0 {
		t.Errorf("end %+v, rows %v, err %v", end, hxdKeys(pages), err)
	}
	seen := 0
	for _, e := range g.events {
		if e.Kind == ingest.EventIncomplete && strings.Contains(e.Reason, "records_path") {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("%d events say to check the records_path, want 1", seen)
	}
}

// [REQ-905]
// A record that has more elements than max_elements is a failure of the shape of the answer, and the limit the definition gives
// is the one that holds.
func TestHxdARecordWithMoreElementsThanTheLimitFails(t *testing.T) {
	g := hxdNew(t, hxdDoc("", hxdHostsDS(`,"total_path":"total"`, `"max_elements":2,`)), hxdHostsServer(hxdFive()))
	_, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if se, ok := ingest.SourceErrorOf(err); !ok || se.Kind != ingest.SrcResponseFormat || !strings.Contains(se.Message, "max_elements") {
		t.Errorf("%v", err)
	}
	g = hxdNew(t, hxdDoc("", hxdHostsDS(`,"total_path":"total"`, `"max_elements":4,`)), hxdHostsServer(hxdFive()))
	if pages, err := g.hxdRead(hsCtx, ingest.PageOptions{}); err != nil || len(hxdKeys(pages)) != 10 {
		t.Errorf("the most elements of a record is 4: %v", err)
	}
}

// [REQ-906]
// A body that breaks in the middle of a record is read again, and what is passed over is the records that were given whole: no
// element of the record that was cut, or of any after it, is missed, however the source orders the elements the second time.
func TestHxdABodyThatBreaksInsideARecordLosesNoElement(t *testing.T) {
	hosts := hxdFive()
	doc := strings.Replace(hxdHostsDS(`,"total_path":"total"`, ""), `"size":2`, `"size":5`, 1)
	g := hxdNew(t, hxdDoc("", doc), func(n int, r hsRequest) hxdReply {
		if n == 1 {
			rep := hxdJSON(hxdHostsBody(hosts, r))
			rep.cutAt = strings.Index(rep.body, `"h3"`) + 40
			return rep
		}
		reversed := make([]hxdHost, len(hosts))
		for i, h := range hosts {
			h.ifs = slices.Clone(h.ifs)
			slices.Reverse(h.ifs)
			reversed[i] = h
		}
		return hxdJSON(hxdHostsBody(reversed, r))
	})
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if err != nil || len(g.srv.seen) != 2 {
		t.Fatalf("requests %d, err %v", len(g.srv.seen), err)
	}
	got := map[string]bool{}
	for _, k := range hxdKeys(pages) {
		got[k] = true
	}
	for _, k := range hxdAll {
		if !got[k] {
			t.Errorf("%s is missing", k)
		}
	}
	if end := hxdEnd(t, pages); !end.Complete {
		t.Errorf("end %+v", end)
	}
}

// [REQ-903]
// For JSON lines the part of records_path before [*] says where the record that has the array is in the line, and when it is empty
// the line is that record; for one JSON value it is the array of those records, and it may be the value itself.
func TestHxdTheOuterRecordIsTheLineForLinesAndTheArrayOfThemForAValue(t *testing.T) {
	lines := `{"id":"ds_a","request":{"path":"/export"},"response_format":"json_lines","records_path":"[*].interfaces","natural_key":["host","id"],"columns":[{"name":"host","paths":["^.hostid"]},{"name":"id"}]}`
	g := hxdNew(t, hxdDoc("", lines), func(int, hsRequest) hxdReply {
		return hxdOK(hxdFive()[0].hxdJSON()+"\n"+hxdFive()[1].hxdJSON()+"\n"+hxdFive()[2].hxdJSON()+"\n", true)
	})
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if want := []string{"h1/i1", "h1/i2", "h1/i3", "h3/i4", "h3/i5"}; err != nil || !slices.Equal(hxdKeys(pages), want) || !hxdEnd(t, pages).Complete {
		t.Errorf("lines: rows %v, err %v", hxdKeys(pages), err)
	}
	nested := strings.Replace(lines, `"records_path":"[*].interfaces"`, `"records_path":"data.hosts[*].interfaces"`, 1)
	g = hxdNew(t, hxdDoc("", nested), func(int, hsRequest) hxdReply {
		return hxdOK(`{"data":{"hosts":`+hxdFive()[0].hxdJSON()+`}}`+"\n", true)
	})
	if pages, err = g.hxdRead(hsCtx, ingest.PageOptions{}); err != nil || len(hxdKeys(pages)) != 3 {
		t.Errorf("lines with a place: rows %v, err %v", hxdKeys(pages), err)
	}
	value := strings.Replace(strings.Replace(lines, `"response_format":"json_lines",`, "", 1), `"request"`, `"pagination":{"kind":"none"},"request"`, 1)
	g = hxdNew(t, hxdDoc("", value), func(int, hsRequest) hxdReply {
		return hxdJSON(`[` + hxdFive()[0].hxdJSON() + `,` + hxdFive()[2].hxdJSON() + `]`)
	})
	if pages, err = g.hxdRead(hsCtx, ingest.PageOptions{}); err != nil || len(hxdKeys(pages)) != 5 {
		t.Errorf("a value that is the array of records: rows %v, err %v", hxdKeys(pages), err)
	}
}

// [REQ-905]
// A response of lines in which a record has no array is only what was seen, as for one value, and one with an element that is not
// an object leaves the dataset not complete.
func TestHxdLinesWithARecordWithNoArrayAreOnlyWhatWasSeen(t *testing.T) {
	lines := `{"id":"ds_a","request":{"path":"/export"},"response_format":"json_lines","records_path":"[*].interfaces","natural_key":["host","id"],"columns":[{"name":"host","paths":["^.hostid"]},{"name":"id"}]}`
	g := hxdNew(t, hxdDoc("", lines), func(int, hsRequest) hxdReply {
		return hxdOK(hxdFive()[0].hxdJSON()+"\n"+`{"hostid":"h2"}`+"\n", true)
	})
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if end := hxdEnd(t, pages); err != nil || !end.SeenOnly || end.Complete || len(hxdKeys(pages)) != 3 {
		t.Errorf("end %+v, rows %v, err %v", end, hxdKeys(pages), err)
	}
	g = hxdNew(t, hxdDoc("", lines), func(int, hsRequest) hxdReply { return hxdOK(`{"hostid":"h1","interfaces":[{"id":"i1"},7]}`+"\n", true) })
	pages, err = g.hxdRead(hsCtx, ingest.PageOptions{})
	if end := hxdEnd(t, pages); err != nil || end.Complete || end.SeenOnly {
		t.Errorf("end %+v, err %v", end, err)
	}
}

// [REQ-906]
// One record with many elements is not one page: the elements are put on the pages one at a time, so a page never holds more than
// the rows a page may have, however many elements a record has.
func TestHxdOneRecordWithManyElementsIsCutIntoPages(t *testing.T) {
	ids := make([]string, 25_000)
	for i := range ids {
		ids[i] = "i" + strings.Repeat("0", 5-len(strconv.Itoa(i))) + strconv.Itoa(i)
	}
	g := hxdNew(t, hxdDoc("", hxdHostsDS(`,"total_path":"total"`, "")), hxdHostsServer([]hxdHost{{id: "h1", ifs: ids}}))
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	most := 0
	for _, p := range pages {
		most = max(most, len(p.Rows))
	}
	if len(hxdKeys(pages)) != 25_000 || most > ingest.DefaultPageRows || len(pages) < 3 {
		t.Errorf("%d rows on %d pages, the biggest of %d rows", len(hxdKeys(pages)), len(pages), most)
	}
}
