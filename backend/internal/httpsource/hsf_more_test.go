package httpsource

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// [REQ-690]
// A secret is taken out of a record at every depth, and a number that held one becomes the text it is.
func TestHsfASecretIsTakenOutOfNumbersAndOfEveryDepthOfARecord(t *testing.T) {
	g := hsNew(t, hsDoc(hsBearer, "", "", ""), hsOne)
	rn, err := g.hsReader.begin(hsCtx, hsSource, g.hsDef.Datasets[0], "run")
	if err != nil {
		t.Fatal(err)
	}
	defer rn.close()
	rn.signer.mask = holdTexts(maskForms([]string{"123456"}))
	rec := map[string]any{"a": json.Number("9123456"), "b": json.Number("42"), "c": []any{map[string]any{"d": "x123456y"}, true, nil}}
	got := rn.cleanValue(rec).(map[string]any)
	if got["a"] != "9***" || got["b"] != json.Number("42") || got["c"].([]any)[0].(map[string]any)["d"] != "x***y" || rn.cleaned != 2 {
		t.Errorf("%v, %d texts cleaned", got, rn.cleaned)
	}
}

// [REQ-692]
// A place the pagination reads may be inside a record, and a value that is true or false is read as its text.
func TestHsfAPlaceInsideARecordAndABooleanAreReadByThePagination(t *testing.T) {
	inRecord := func(n int, _ hsRequest) hsReply {
		if n == 1 {
			return hsJSON(`{"items":[` + hsRecord(0) + `,{"serial":"s-0001","nxt":"c2"}]}`)
		}
		return hsJSON(`{"items":` + hsRecords(2, 3) + `}`)
	}
	g := hsNew(t, hsDoc("", "", `{"kind":"cursor","cursor_param":"cursor","cursor_in":"query","next_cursor_path":"items.1.nxt"}`, ""), inRecord)
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil || len(hsSerials(pages)) != 3 || !hsLast(t, pages).End.Complete || g.hsSrv.hsSeen[1].hsQuery().Get("cursor") != "c2" {
		t.Fatalf("a cursor in a record: %v", err)
	}
	flag := func(n int, _ hsRequest) hsReply {
		if n == 1 {
			return hsJSON(`{"items":` + hsRecords(0, 1) + `,"meta":{"next":true}}`)
		}
		return hsJSON(`{"items":` + hsRecords(1, 2) + `,"meta":{"next":""}}`)
	}
	g = hsNew(t, hsDoc("", "", `{"kind":"cursor","cursor_param":"cursor","cursor_in":"query","next_cursor_path":"meta.next"}`, ""), flag)
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil || g.hsSrv.hsSeen[1].hsQuery().Get("cursor") != "true" {
		t.Fatalf("a boolean: %v", err)
	}
}

// [REQ-692]
// A document that is cut inside an object or an array outside the records, one that has a record that is not JSON, and a
// records_path that goes through an array by a name, are failures of the shape of the answer.
func TestHsfFailuresOfTheShapeInsideTheParts(t *testing.T) {
	for name, body := range map[string]string{
		"a cut array":           `{"x":[1,2,`,
		"a cut object":          `{"x":{"k":`,
		"a record that is not":  `{"items":[` + hsRecord(0) + `,{"b":}]}`,
		"a name through array":  `{"data":[{"items":[]}]}`,
		"a stray end":           `]`,
		"the array is not read": `{"items":[1,2,3`,
	} {
		doc := hsDoc("", "", "", "")
		if name == "a name through array" {
			doc = strings.Replace(doc, `"records_path":"items"`, `"records_path":"data.x.items"`, 1)
		}
		g := hsNew(t, doc, func(int, hsRequest) hsReply { return hsJSON(body) })
		_, err := g.hsRead(ingest.PageOptions{})
		if se, ok := ingest.SourceErrorOf(err); !ok || se.Kind != ingest.SrcResponseFormat {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := walk(strings.NewReader("{}"), 100, "a[", nil, 0, nil); !errors.Is(err, errRecordsPath) {
		t.Errorf("a path that is none: %v", err)
	}
	rn := &run{}
	if err := rn.answerError(hsCtx, errRecordsPath); err == nil || strings.Contains(err.Error(), "records_path is not a path") == false {
		t.Errorf("the words for it: %v", err)
	}
}

// [REQ-692]
// When the request is sent again after a body broke, a wait that fails ends the read with its error, and an answer to the
// second request that is a failure is the failure.
func TestHsfARequestSentAgainAfterABrokenBodyMayFailInItsWaitOrItsAnswer(t *testing.T) {
	broken := func(n int, _ hsRequest) hsReply {
		rep := hsJSON(`{"items":` + hsRecords(0, 10) + `}`)
		if n == 1 {
			rep.hsReadErr = &egress.Error{Kind: egress.KindConnect, Reason: "x"}
		}
		return rep
	}
	g := hsNew(t, hsDoc("", "", "", ""), broken)
	boom := errors.New("the wait failed")
	g.hsReader.cfg.Sleep = func(_ context.Context, _ time.Duration) error { return boom }
	if _, err := g.hsRead(ingest.PageOptions{}); !errors.Is(err, boom) {
		t.Errorf("a wait that fails: %v", err)
	}
	g = hsNew(t, hsDoc("", "", "", ""), func(n int, r hsRequest) hsReply {
		if n == 1 {
			return broken(n, r)
		}
		return hsStatus(404)
	})
	_, err := g.hsRead(ingest.PageOptions{})
	if se, ok := ingest.SourceErrorOf(err); !ok || se.Kind != ingest.SrcRequestRejected || len(g.hsSrv.hsSeen) != 2 {
		t.Errorf("an answer that is a failure: %v after %d requests", err, len(g.hsSrv.hsSeen))
	}
}

// [REQ-692]
// The answer to the request for a token is read whole up to 1 MiB; a bigger one, and one that breaks, end the read.
func TestHsfTheAnswerToTheRequestForATokenIsSmallAndWhole(t *testing.T) {
	big := func(int, hsRequest) hsReply {
		return hsJSON(`{"token":"` + strings.Repeat("t", tokenAnswerMax) + `"}`)
	}
	g := hsNew(t, hsDoc(hsTokReq, "", "", ""), big)
	_, err := g.hsRead(ingest.PageOptions{})
	hsfFormat(t, "a token answer that is big", err, "token request is larger")
	cut := func(int, hsRequest) hsReply {
		rep := hsJSON(`{"token":"` + hsMintedValue + `"}`)
		rep.hsReadErr = &egress.Error{Kind: egress.KindConnect, Reason: "x"}
		return rep
	}
	g = hsNew(t, hsDoc(hsTokReq, "", "", ""), cut)
	_, err = g.hsRead(ingest.PageOptions{})
	if se, ok := ingest.SourceErrorOf(err); !ok || se.Kind != ingest.SrcConnectionLost || len(g.hsSrv.hsSeen) != 1 {
		t.Errorf("a token answer that breaks: %v after %d requests", err, len(g.hsSrv.hsSeen))
	}
}
