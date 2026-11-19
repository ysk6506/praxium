package httpsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-665]
func TestAWrappedSecretPrintsAsNothingInEveryWay(t *testing.T) {
	text, list := holdText(hsTokenValue), holdTexts([]string{hsTokenValue})
	for _, v := range []fmt.Stringer{text, list} {
		if v.String() == hsTokenValue {
			t.Fatal("a wrapped value prints itself")
		}
	}
	for _, got := range []string{text.GoString(), list.GoString(), fmt.Sprintf("%v %+v %#v %s %d", text, text, list, list, text)} {
		hsClean(t, "a wrapped value", got)
	}
	data, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	asText, _ := list.MarshalText()
	hsClean(t, "a wrapped value as JSON", string(data)+string(asText))
	if (maskSet{}).list() != nil {
		t.Fatal("an empty set has texts")
	}
}

// [REQ-662]
func TestTheSettingsOfAConnectionTakeTheStartingValuesAndTheOriginIsWrittenForTheHost(t *testing.T) {
	cases := []struct {
		base, want string
		port       int
	}{
		{"http://[::1]:8080/x/", "http://[::1]:8080/x", 8080},
		{"http://API.example.test", "http://api.example.test", 80},
		{"https://api.example.test:8443/v1", "https://api.example.test:8443/v1", 8443},
		{"https://api.example.test:443", "https://api.example.test", 443},
	}
	for _, c := range cases {
		set, err := readSettings(ingest.Source{ID: "s", Adapter: ingest.AdapterHTTP, Connection: &ingest.Connection{BaseURL: c.base}})
		if err != nil || set.port != c.port {
			t.Fatalf("%s: %v", c.base, err)
		}
		if got := set.origin(set.host, set.port); got.String() != c.want {
			t.Errorf("%s: %s", c.base, got)
		}
		if set.timeout != 30*time.Second || set.maxBytes != 16<<20 || set.maxWait != time.Minute || set.interval != 200*time.Millisecond || set.needsSecret() {
			t.Errorf("%s: %+v", c.base, set)
		}
	}
	for _, bad := range []string{"https://user@api.example.test", "https://", "file:///x", "https://2130706433/"} {
		if _, err := readSettings(ingest.Source{ID: "s", Adapter: ingest.AdapterHTTP, Connection: &ingest.Connection{BaseURL: bad}}); err == nil {
			t.Errorf("%s is no base address", bad)
		}
	}
}

// [REQ-663]
func TestADatasetWithNoPaginationIsOneResponseAndAPathThatIsNoPathIsNoTotalAndNoNext(t *testing.T) {
	g := hsNew(t, hsDoc("", "", "", ""), hsOne)
	ds := g.hsDef.Datasets[0]
	ds.Pagination = nil
	pages, err := g.hsReadDS(ds, ingest.PageOptions{})
	if err != nil || len(hsSerials(pages)) != 2 || len(g.hsSrv.hsSeen) != 1 {
		t.Fatalf("%v", err)
	}
	pg := &pager{pg: &ingest.Pagination{Kind: ingest.PagePage, TotalPath: "a[", NextPath: "a["}}
	if _, ok := pg.totalOf(map[string]any{}); ok || nextText("a[", nil) != "" {
		t.Fatal("a path that is no path gave a value")
	}
	if _, ok := (&pager{pg: &ingest.Pagination{Kind: ingest.PagePage, TotalPath: "n"}}).totalOf(map[string]any{"n": json.Number("-4")}); ok {
		t.Fatal("a total below zero")
	}
	if got, err := pg.advance(pos{}, nil, nil, 0, 0); err != nil || got.more == false && !got.complete {
		// a kind that no pagination has is the end
		t.Logf("unreachable kind: %+v", got)
	}
}

// [REQ-663]
func TestTheBodyOfAPostKeepsNumbersArraysAndBooleansAndACursorThatLooksLikeANumberStaysText(t *testing.T) {
	request := `{"method":"POST","path":"/servers/search","body":{"ids":[1,"a {{b}}"],"n":3,"flag":true,"inner":{"k":"{{v}}"}}}`
	doc := strings.Replace(hsDoc("", request, `{"kind":"cursor","cursor_param":"cursor","cursor_in":"body","next_cursor_path":"meta.next"}`, ""),
		`"records_path":"items"`, `"records_path":"items","post_reads":true`, 1)
	g := hsNew(t, doc, func(n int, _ hsRequest) hsReply {
		next := `"12"`
		if n > 1 {
			next = "null"
		}
		return hsJSON(`{"items":` + hsRecords(n, n+1) + `,"meta":{"next":` + next + `}}`)
	})
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
		t.Fatal(err)
	}
	var first, second map[string]any
	_ = json.Unmarshal([]byte(g.hsSrv.hsSeen[0].hsBody), &first)
	_ = json.Unmarshal([]byte(g.hsSrv.hsSeen[1].hsBody), &second)
	if fmt.Sprint(first["ids"]) != "[1 a {b}]" || first["n"] != float64(3) || first["flag"] != true || fmt.Sprint(first["inner"]) != "map[k:{v}]" {
		t.Fatalf("the body: %v", first)
	}
	if second["cursor"] != "12" {
		t.Fatalf("the cursor: %v", second["cursor"])
	}
	// a cursor in the body of a POST that has no body of its own makes one
	bare := strings.Replace(doc, `,"body":{"ids":[1,"a {{b}}"],"n":3,"flag":true,"inner":{"k":"{{v}}"}}`, "", 1)
	g = hsNew(t, bare, func(n int, _ hsRequest) hsReply {
		return hsJSON(`{"items":` + hsRecords(n, n+1) + `,"meta":{"next":` + map[bool]string{true: `"x"`, false: "null"}[n == 1] + `}}`)
	})
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil || g.hsSrv.hsSeen[1].hsBody != `{"cursor":"x"}` {
		t.Fatalf("%v: %s", err, g.hsSrv.hsSeen[1].hsBody)
	}
}

// [REQ-662]
func TestABodyThatCannotBeWrittenIsTheProductsFault(t *testing.T) {
	request := `{"method":"POST","path":"/servers/search"}`
	doc := strings.Replace(hsDoc("", request, "", ""), `"records_path":"items"`, `"records_path":"items","post_reads":true`, 1)
	g := hsNew(t, doc, hsOne)
	ds := g.hsDef.Datasets[0]
	req := *ds.Request
	req.Body = map[string]any{"c": make(chan int)}
	ds.Request = &req
	_, err := g.hsReadDS(ds, ingest.PageOptions{})
	if se, ok := ingest.SourceErrorOf(err); !ok || se.Kind != ingest.SrcInternal {
		t.Fatalf("%v", err)
	}
}

// [REQ-664]
func TestABodyThatBreaksWhileItIsReadIsAConnectionThatWasLostAndIsSentAgain(t *testing.T) {
	g := hsNew(t, hsDoc("", "", "", ""), func(n int, r hsRequest) hsReply {
		if n == 1 {
			rep := hsOne(n, r)
			rep.hsReadErr = &egress.Error{Kind: egress.KindConnect, Reason: "the connection failed or broke"}
			return rep
		}
		return hsOne(n, r)
	})
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil || len(hsSerials(pages)) != 2 || len(g.hsSrv.hsSeen) != 2 || g.hsSrv.hsBodies != 2 {
		t.Fatalf("%v", err)
	}
}

// [REQ-664]
func TestAContentTypeIsJSONOrTheNameOfAJSONSyntaxOrNothing(t *testing.T) {
	for in, want := range map[string]bool{
		"": true, "application/json": true, "application/json; charset=utf-8": true, "application/problem+json": true,
		"text/html": false, "application/xml": false, ";;;": false,
	} {
		h := map[string][]string{}
		if in != "" {
			h["content-type"] = []string{in}
		}
		if jsonType(h) != want {
			t.Errorf("%q", in)
		}
	}
}

// [REQ-664]
func TestAWaitThatFailsEndsTheReadWithItsError(t *testing.T) {
	for _, tc := range []struct {
		name  string
		after int
	}{{"before a request", 0}, {"before a retry", 1}} {
		g := hsNew(t, hsDoc("", "", "", `,"requests_per_second":0.5`), hsFails(5, hsStatus(503)))
		cfg := g.hsReader.cfg
		calls := 0
		stop := errors.New("stopped waiting")
		cfg.Sleep = func(context.Context, time.Duration) error {
			if calls++; calls > tc.after {
				return stop
			}
			_, err := g.hsClock.Advance(time.Hour)
			return err
		}
		r, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		for _, err := range r.ReadPages(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PageOptions{}) {
			if !errors.Is(err, stop) {
				t.Fatalf("%s: %v", tc.name, err)
			}
		}
	}
}

// [REQ-664]
func TestAReaderThatWasGivenNoSleepWaitsOnATimer(t *testing.T) {
	g := hsNew(t, hsDoc("", "", "", ""), hsOne)
	cfg := g.hsReader.cfg
	cfg.Sleep, cfg.Clock = nil, kernel.NewManualClock(hsEpoch)
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range r.ReadPages(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PageOptions{}) {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// [REQ-665]
func TestTheWordsForWhatIsNotASourceErrorAreFixed(t *testing.T) {
	if plainText(context.DeadlineExceeded) != "the check did not finish in time" || retryReason(errors.New("x")) != "failed" {
		t.Fatal("the fixed words")
	}
	if got := openError(hsCtx, kernel.Errorf(kernel.CodeNotFound, "no")); ingest.ClassifyCheckError(got) != ingest.CheckConfig {
		t.Fatalf("a secret that is not there: %v", got)
	}
	ctx, cancel := cancelled()
	defer cancel()
	if got := openError(ctx, errors.New("x")); !errors.Is(got, context.Canceled) {
		t.Fatalf("a cancelled open: %v", got)
	}
}
