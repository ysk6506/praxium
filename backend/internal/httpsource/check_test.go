package httpsource

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

func hsStepNames(r ingest.CheckResult) []string {
	var out []string
	for _, s := range r.Steps {
		out = append(out, s.Name)
	}
	return out
}

// [REQ-665]
func TestACheckReadsTheFirstResponseOnlyAndTellsItemByItem(t *testing.T) {
	g := hsNew(t, hsDoc(hsBearer, "", hsPageKind(2, 1, false), ""), hsAPI(ingest.PagePage, 100, 2, 1))
	r := g.hsReader.Check(hsCtx, hsSource, g.hsDef.Datasets[0])
	if r.Kind != ingest.CheckOK || r.Rows != 2 || len(g.hsSrv.hsSeen) != 1 {
		t.Fatalf("%+v after %d requests", r, len(g.hsSrv.hsSeen))
	}
	if got := hsStepNames(r); !reflect.DeepEqual(got, []string{StepSecret, StepConnect, StepSignIn, StepResponse, StepRecordsAt}) {
		t.Fatalf("steps: %v", got)
	}
	for _, s := range r.Steps {
		if s.Kind != ingest.CheckOK || s.Message == "" {
			t.Fatalf("step %+v", s)
		}
	}
	if g.hsUses[0].Purpose != "check" || g.hsSrv.hsShut != 1 {
		t.Fatalf("purpose %v, closed %d", g.hsUses[0].Purpose, g.hsSrv.hsShut)
	}
	none := hsNew(t, hsDoc("", "", "", ""), hsOne)
	if got := hsStepNames(none.hsReader.Check(hsCtx, hsSource, none.hsDef.Datasets[0])); !reflect.DeepEqual(got, []string{StepConnect, StepSignIn, StepResponse, StepRecordsAt}) {
		t.Fatalf("steps with no secret: %v", got)
	}
}

// [REQ-665]
func TestACheckSaysWhichItemFailedWithTheKindAndTheWordsOfTheFailure(t *testing.T) {
	cases := []struct {
		name   string
		auth   string
		handle func(int, hsRequest) hsReply
		want   []string
		kind   ingest.CheckKind
		why    string
	}{
		{"a secret that is not for the source", `{"kind":"bearer","credential":{"secret_id":"sec_other"}}`, hsOne, []string{StepSecret}, ingest.CheckConfig, "secret"},
		{"a connection that fails", "", func(int, hsRequest) hsReply { return hsReply{hsErr: &egress.Error{Kind: egress.KindTLS, Reason: "x"}} }, []string{StepConnect}, ingest.CheckConfig, "certificate"},
		{"credentials that are refused", hsBearer, func(int, hsRequest) hsReply { return hsStatus(401) }, []string{StepSecret, StepConnect, StepSignIn}, ingest.CheckConfig, "401"},
		{"a token request that is refused", hsTokReq, func(int, hsRequest) hsReply { return hsStatus(403) }, []string{StepSecret, StepConnect, StepSignIn}, ingest.CheckConfig, "403"},
		{"a token request the connection fails on", hsTokReq, func(int, hsRequest) hsReply {
			return hsReply{hsErr: &egress.Error{Kind: egress.KindTimeout, Reason: "x"}}
		}, []string{StepSecret, StepConnect}, ingest.CheckTransient, "in time"},
		{"an answer that is not JSON", "", func(int, hsRequest) hsReply { return hsJSON("<html>") }, []string{StepConnect, StepSignIn, StepResponse}, ingest.CheckConfig, "JSON"},
		{"a place of records that is not there", "", func(int, hsRequest) hsReply { return hsJSON(`{"other":[]}`) }, []string{StepConnect, StepSignIn, StepResponse, StepRecordsAt}, ingest.CheckConfig, "records_path"},
		{"a source that fails", "", func(int, hsRequest) hsReply { return hsStatus(500) }, []string{StepConnect, StepSignIn, StepResponse}, ingest.CheckTransient, "500"},
	}
	for _, c := range cases {
		g := hsNew(t, hsDoc(c.auth, "", "", ""), c.handle)
		r := g.hsReader.Check(hsCtx, hsSource, g.hsDef.Datasets[0])
		if r.Kind != c.kind || !strings.Contains(r.Message, c.why) {
			t.Errorf("%s: %+v", c.name, r)
			continue
		}
		if got := hsStepNames(r); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: steps %v, want %v", c.name, got, c.want)
			continue
		}
		last := r.Steps[len(r.Steps)-1]
		if last.Kind != c.kind || last.Message != r.Message {
			t.Errorf("%s: the failed item: %+v", c.name, last)
		}
		for _, s := range r.Steps[:len(r.Steps)-1] {
			if s.Kind != ingest.CheckOK {
				t.Errorf("%s: an item before the failure: %+v", c.name, s)
			}
		}
	}
}

// [REQ-665]
func TestACheckOfASourceThatIsOffOrCancelledOrOfAnIncompleteFirstResponse(t *testing.T) {
	g := hsNew(t, hsDoc("", "", "", ""), hsOne)
	if r := g.hsReader.Check(hsCtx, "src_other", g.hsDef.Datasets[0]); r.Kind != ingest.CheckProduct {
		t.Fatalf("another source: %+v", r)
	}
	off := hsNew(t, hsDoc("", "", "", ""), hsOne)
	off.hsReader.cfg.Source.Disabled = true
	if r := off.hsReader.Check(hsCtx, hsSource, off.hsDef.Datasets[0]); r.Kind != ingest.CheckConfig {
		t.Fatalf("a source that is off: %+v", r)
	}
	// a first response that is all the data and does not match its total is a result that is not good
	mismatch := hsNew(t, hsDoc("", "", hsPageKind(3, 1, true), ""), func(int, hsRequest) hsReply {
		return hsJSON(`{"items":` + hsRecords(0, 2) + `,"pageInfo":{"totalCount":1}}`)
	})
	if r := mismatch.hsReader.Check(hsCtx, hsSource, mismatch.hsDef.Datasets[0]); r.Kind != ingest.CheckTransient || !strings.Contains(r.Message, "1 records") || r.Rows != 2 {
		t.Fatalf("a total that does not match: %+v", r)
	}
	ctx, cancel := cancelled()
	defer cancel()
	if r := g.hsReader.Check(ctx, hsSource, g.hsDef.Datasets[0]); !strings.Contains(r.Message, "canceled") {
		t.Fatalf("a cancelled check: %+v", r)
	}
	if plainText(errNobody) != "the check could not be made" {
		t.Fatal("the words for an error that is no source error")
	}
}

// [REQ-665]
func TestAPreviewGivesMaskedRowsAndTheExchangesThatMadeThem(t *testing.T) {
	handle := func(n int, r hsRequest) hsReply {
		rep := hsJSON(`{"items":[{"serial":"s-1","api_token":"` + hsMintedValue + `","rack":"` + hsTokenValue + `"},{"serial":"s-2","rack":"r2"},{"serial":"s-3","rack":"r3"}]}`)
		rep.hsHeader["Set-Cookie"] = []string{"session=abc"}
		rep.hsHeader["Retry-After"] = []string{"3"}
		return rep
	}
	doc := strings.Replace(hsDoc(hsBearer, `{"path":"/servers","query":{"status":"active","api_key":"visible-by-name"}}`, hsPageKind(3, 1, false), ""),
		`{"name":"rack","paths":["location.rack"]}`, `{"name":"rack"},{"name":"api_token"}`, 1)
	g := hsNew(t, doc, handle)
	p, err := g.hsReader.Preview(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PreviewOptions{MaxRows: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Pages) != 1 || len(p.Pages[0].Rows) != 2 || !p.Masked {
		t.Fatalf("pages %d, masked %v", len(p.Pages), p.Masked)
	}
	row := p.Pages[0].Rows[0].Fields
	if row["api_token"] != ingest.MaskedValue || strings.Contains(row["rack"], hsTokenValue) || row["serial"] != "s-1" {
		t.Fatalf("row %v", row)
	}
	if len(p.Exchanges) != 1 {
		t.Fatalf("%d exchanges", len(p.Exchanges))
	}
	ex := p.Exchanges[0]
	headers := map[string]ingest.NameValue{}
	for _, h := range ex.Headers {
		headers[h.Name] = h
	}
	if ex.Method != "GET" || ex.Path != "/api/v2/servers" || ex.Status != 200 || headers["Retry-After"].Value != "3" || !headers["Set-Cookie"].Masked || headers["Set-Cookie"].Value != "" {
		t.Fatalf("exchange %+v", ex)
	}
	shown := map[string]ingest.NameValue{}
	for _, q := range ex.Query {
		shown[q.Name] = q
	}
	if shown["status"].Value != "active" || !shown["api_key"].Masked || shown["api_key"].Value != "" || shown["pageSize"].Value != "3" {
		t.Fatalf("query %+v", ex.Query)
	}
	if g.hsUses[0].Purpose != "discover" {
		t.Fatalf("purpose %v", g.hsUses[0].Purpose)
	}
}

// [REQ-665]
func TestAPreviewIsAsBigAsAskedAndHidesWhatTheSourceGaveAsAnAddress(t *testing.T) {
	g := hsNew(t, hsDoc(hsBearer, "", `{"kind":"next_url","next_path":"next"}`, ""), hsAPI(ingest.PageNextURL, 9, 3, 1))
	p, err := g.hsReader.Preview(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PreviewOptions{MaxPages: 2, MaxRows: 3})
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, page := range p.Pages {
		rows += len(page.Rows)
	}
	if len(p.Pages) != 2 || rows != 3 {
		t.Fatalf("%d pages, %d rows", len(p.Pages), rows)
	}
	if len(p.Exchanges) < 2 {
		t.Fatalf("%d exchanges", len(p.Exchanges))
	}
	second := p.Exchanges[1]
	if len(second.Query) != 1 || second.Query[0].Name != "after" || !second.Query[0].Masked || second.Query[0].Value != "" {
		t.Fatalf("the query of an address the source gave: %+v", second.Query)
	}
}

// [REQ-665]
func TestAPreviewThatFailsGivesTheErrorAndNoRows(t *testing.T) {
	g := hsNew(t, hsDoc(hsBearer, "", "", ""), func(int, hsRequest) hsReply { return hsStatus(404) })
	p, err := g.hsReader.Preview(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PreviewOptions{})
	if se, ok := ingest.SourceErrorOf(err); !ok || se.Kind != ingest.SrcRequestRejected || len(p.Pages) != 0 {
		t.Fatalf("%v", err)
	}
	other := hsNew(t, hsDoc("", "", "", ""), hsOne)
	if _, err := other.hsReader.Preview(hsCtx, "src_other", other.hsDef.Datasets[0], ingest.PreviewOptions{}); err == nil {
		t.Fatal("another source")
	}
	capped := hsNew(t, hsDoc("", "", hsPageKind(1, 1, false), ""), hsAPI(ingest.PagePage, 5000, 1, 1))
	cp, err := capped.hsReader.Preview(hsCtx, hsSource, capped.hsDef.Datasets[0], ingest.PreviewOptions{MaxRows: 5000})
	if err != nil || len(cp.Pages) != 1 || len(cp.Pages[0].Rows) != ingest.MaxPreviewRows {
		t.Fatalf("a preview is never more than the most: %v", err)
	}
	if len(cp.Exchanges) != maxExchanges {
		t.Fatalf("%d exchanges are kept", len(cp.Exchanges))
	}
}
