package httpsource

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// [REQ-900]
// The records of a body of JSON lines are the values of it, whether an end of line or any other white space comes between them,
// and an empty line is passed over. The read is one request, its pages carry no token, and the type of the body is not looked at.
func TestHxdJSONLinesAreRecordsAndTheReadIsOneRequestWithNoToken(t *testing.T) {
	body := hxdLine(1) + "\n" + hxdLine(2) + " " + hxdLine(3) + "\n\n\n" + hxdLine(4) + "\r\n" + hxdLine(5)
	g := hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply {
		return hxdReply{status: 200, header: map[string][]string{"Content-Type": {"text/plain"}}, body: body, ended: true}
	})
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{MaxRows: 2})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"r001", "r002", "r003", "r004", "r005"}; !slices.Equal(hxdKeys(pages), want) {
		t.Errorf("rows %v, want %v", hxdKeys(pages), want)
	}
	if end := hxdEnd(t, pages); !end.Complete || end.SeenOnly {
		t.Errorf("a body that ended with its mark is complete: %+v", end)
	}
	for _, p := range pages {
		if len(p.Token) != 0 {
			t.Errorf("page %d has a token: a response of lines is not read from the middle", p.No)
		}
	}
	if len(g.srv.seen) != 1 {
		t.Errorf("%d requests, want 1", len(g.srv.seen))
	}
}

// [REQ-900]
// The record of a line is what records_path leads to in it, and a line with nothing there is a failure that names the line and not
// its content.
func TestHxdTheRecordOfALineIsAtRecordsPathAndALineWithNothingThereFailsTheDataset(t *testing.T) {
	ds := strings.Replace(hxdLines, `"response_format"`, `"records_path":"result","response_format"`, 1)
	wrap := func(i int) string { return `{"preview":false,"result":` + hxdLine(i) + `}` }
	g := hxdNew(t, hxdDoc("", ds), func(int, hsRequest) hxdReply { return hxdOK(wrap(1)+"\n"+wrap(2)+"\n", true) })
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if err != nil || !slices.Equal(hxdKeys(pages), []string{"r001", "r002"}) {
		t.Fatalf("rows %v, err %v", hxdKeys(pages), err)
	}
	const secretLike = "NOT-A-SECRET-CONTENT-OF-A-LINE"
	g = hxdNew(t, hxdDoc("", ds), func(int, hsRequest) hxdReply {
		return hxdOK(wrap(1)+"\n"+`{"other":"`+secretLike+`"}`+"\n", true)
	})
	_, err = g.hxdRead(hsCtx, ingest.PageOptions{})
	se, ok := ingest.SourceErrorOf(err)
	if !ok || se.Kind != ingest.SrcResponseFormat || !strings.Contains(se.Message, "value 2") || strings.Contains(se.Message, secretLike) {
		t.Errorf("a line with nothing at records_path: %v", err)
	}
}

// [REQ-901]
// A body that is read to its end with no mark of the end may have been cut where a connection closed: what was read is the dataset
// as it was seen, and it is not sent again.
func TestHxdABodyWithNoMarkOfItsEndIsOnlyWhatWasSeen(t *testing.T) {
	g := hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply { return hxdOK(hxdLine(1)+"\n"+hxdLine(2)+"\n", false) })
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	end := hxdEnd(t, pages)
	if end.Complete || !end.SeenOnly || end.Reason == "" || !slices.Equal(hxdKeys(pages), []string{"r001", "r002"}) || len(g.srv.seen) != 1 {
		t.Errorf("end %+v, rows %v, requests %d", end, hxdKeys(pages), len(g.srv.seen))
	}
}

// [REQ-901]
// A body that breaks in the middle gives the lines that were read to their end, the line that was cut is left out, and the
// request is not sent again to pass over what was given.
func TestHxdABodyThatBreaksGivesTheLinesThatWereReadAndIsNotSentAgain(t *testing.T) {
	body := hxdLine(1) + "\n" + hxdLine(2) + "\n" + hxdLine(3) + "\n"
	cut := strings.Index(body, hxdLine(3)) + len(hxdLine(3))/2
	g := hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply {
		r := hxdOK(body, true)
		r.cutAt = cut
		return r
	})
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	end := hxdEnd(t, pages)
	if !end.SeenOnly || end.Complete || !slices.Equal(hxdKeys(pages), []string{"r001", "r002"}) {
		t.Errorf("end %+v, rows %v", end, hxdKeys(pages))
	}
	if len(g.srv.seen) != 1 {
		t.Errorf("the request was sent %d times; a body of lines is not read again", len(g.srv.seen))
	}
}

// [REQ-901]
// A last line that is cut and a body that ends with its mark in the middle of a value are told apart: with no mark the line is
// left out and the dataset is what was seen; with the mark it is the source that sent a value that is not whole, which is a failure.
func TestHxdACutLastLineIsLeftOutWithNoMarkAndAFailureWithOne(t *testing.T) {
	body := hxdLine(1) + "\n" + hxdLine(2)[:10]
	g := hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply { return hxdOK(body, false) })
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if err != nil || !hxdEnd(t, pages).SeenOnly || !slices.Equal(hxdKeys(pages), []string{"r001"}) {
		t.Errorf("no mark: rows %v, err %v", hxdKeys(pages), err)
	}
	g = hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply { return hxdOK(body, true) })
	if _, err := g.hxdRead(hsCtx, ingest.PageOptions{}); err == nil || !strings.Contains(err.Error(), "ends in the middle") {
		t.Errorf("with the mark: %v", err)
	}
}

// [REQ-901]
// A value that is not JSON fails the dataset; a record that cannot be read (it is no object, or it has no natural key) leaves the
// dataset not complete, because a line that says an error looks like a record with no key and must not leave it whole.
func TestHxdALineThatCannotBeReadMakesTheDatasetNotComplete(t *testing.T) {
	g := hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply { return hxdOK(hxdLine(1)+"\n"+`{"id": oops}`+"\n", true) })
	if _, err := g.hxdRead(hsCtx, ingest.PageOptions{}); err == nil || !strings.Contains(err.Error(), "value 2") {
		t.Errorf("a value that is not JSON: %v", err)
	}
	g = hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply {
		return hxdOK(hxdLine(1)+"\n"+`{"error":"the export stopped"}`+"\n", true)
	})
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	end := hxdEnd(t, pages)
	if err != nil || end.Complete || end.SeenOnly || !strings.Contains(end.Reason, "1 records") {
		t.Errorf("a record with no key: end %+v, err %v", end, err)
	}
	g = hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply { return hxdOK(hxdLine(1)+"\n"+`[1,2]`+"\n", true) })
	pages, err = g.hxdRead(hsCtx, ingest.PageOptions{})
	if end := hxdEnd(t, pages); err != nil || end.Complete || end.SeenOnly {
		t.Errorf("a record that is no object: end %+v, err %v", end, err)
	}
}

// [REQ-900]
// The limits of a body that is one value hold for a body of lines: a record of more than 2 MiB and a body of more bytes than
// max_response_bytes fail the dataset, and neither is a cut.
func TestHxdTheLimitsOfTheBodyHoldForLines(t *testing.T) {
	big := `{"id":"r1","name":"` + strings.Repeat("x", 2<<20+10) + `"}`
	g := hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply { return hxdOK(hxdLine(0)+"\n"+big+"\n", true) })
	if _, err := g.hxdRead(hsCtx, ingest.PageOptions{}); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("a record over 2 MiB: %v", err)
	}
	var many strings.Builder
	for i := 0; i < 200; i++ {
		many.WriteString(hxdLine(i) + "\n")
	}
	g = hxdNew(t, hxdDoc(`,"max_response_bytes":2048`, hxdLines), func(int, hsRequest) hxdReply { return hxdOK(many.String(), true) })
	if _, err := g.hxdRead(hsCtx, ingest.PageOptions{}); err == nil || !strings.Contains(err.Error(), "max_response_bytes") {
		t.Errorf("a body over max_response_bytes: %v", err)
	}
}

// [REQ-901]
// A token that was given is not used for a body of lines: the read starts from the first line and sends one request.
func TestHxdATokenIsNotUsedForLines(t *testing.T) {
	g := hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply { return hxdOK(hxdLine(1)+"\n"+hxdLine(2)+"\n", true) })
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{Resume: []byte(`{"v":1,"fp":"x","s":1}`)})
	if err != nil || !slices.Equal(hxdKeys(pages), []string{"r001", "r002"}) || len(g.srv.seen) != 1 {
		t.Errorf("rows %v, requests %d, err %v", hxdKeys(pages), len(g.srv.seen), err)
	}
}

// [REQ-901]
// A body that is cut because the caller canceled the read is the canceled read and not a dataset that was seen.
func TestHxdACanceledReadOfLinesIsNotACut(t *testing.T) {
	ctx, cancel := context.WithCancel(hsCtx)
	defer cancel()
	g := hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply {
		r := hxdOK(hxdLine(1)+"\n"+hxdLine(2)+"\n", true)
		r.cutAt, r.cancel = len(hxdLine(1))+1, cancel
		return r
	})
	_, err := g.hxdRead(ctx, ingest.PageOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want the cancellation", err)
	}
}

// [REQ-902]
// A check reads the first lines of a response of lines and no more: the rest of the body is not taken.
func TestHxdACheckReadsTheFirstLinesOnly(t *testing.T) {
	var body strings.Builder
	for i := 0; i < 2000; i++ {
		body.WriteString(`{"id":"r` + strconv.Itoa(i) + `","name":"` + strings.Repeat("n", 300) + `"}` + "\n")
	}
	g := hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply { return hxdOK(body.String(), true) })
	res := g.reader.Check(hsCtx, "src_export", g.def.Datasets[0])
	if res.Kind != ingest.CheckOK || res.Rows != checkLines {
		t.Errorf("the check: %+v", res)
	}
	if g.srv.taken > body.Len()/4 || g.srv.closed != 1 {
		t.Errorf("%d of %d bytes were taken, %d bodies closed", g.srv.taken, body.Len(), g.srv.closed)
	}
}

// [REQ-900]
// An answer of no content is a dataset with no record, as it is for a body that is one value.
func TestHxdNoContentIsNoRecordsForLines(t *testing.T) {
	g := hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply { return hxdReply{status: 204} })
	pages, err := g.hxdRead(hsCtx, ingest.PageOptions{})
	if err != nil || len(hxdKeys(pages)) != 0 || !hxdEnd(t, pages).Complete {
		t.Errorf("rows %v, err %v", hxdKeys(pages), err)
	}
}

// [REQ-900]
// A preview reads lines like a run does, and its pages are the rows of the lines.
func TestHxdAPreviewReadsLines(t *testing.T) {
	g := hxdNew(t, hxdDoc("", hxdLines), func(int, hsRequest) hxdReply { return hxdOK(hxdLine(1)+"\n", true) })
	prev, err := g.reader.Preview(hsCtx, "src_export", g.def.Datasets[0], ingest.PreviewOptions{})
	if err != nil || len(prev.Pages) == 0 || len(prev.Pages[0].Rows) != 1 {
		t.Fatalf("preview %+v, err %v", prev, err)
	}
}
