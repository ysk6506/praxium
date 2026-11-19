package httpsource

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

const hsHTTPDate = "Mon, 02 Jan 2006 15:04:05 GMT"

func hsFails(times int, first hsReply) func(int, hsRequest) hsReply {
	return func(n int, r hsRequest) hsReply {
		if n <= times {
			return first
		}
		return hsOne(n, r)
	}
}

func hsWithHeader(rep hsReply, name, value string) hsReply {
	rep.hsHeader = map[string][]string{name: {value}}
	return rep
}

// [REQ-664]
func TestA429IsSentAgainAfterTheTimeTheSourceAsksFor(t *testing.T) {
	g := hsNew(t, hsDoc("", "", "", ""), hsFails(2, hsWithHeader(hsStatus(429), "Retry-After", "7")))
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil || len(hsSerials(pages)) != 2 || len(g.hsSrv.hsSeen) != 3 {
		t.Fatalf("rows %d after %d requests: %v", len(hsSerials(pages)), len(g.hsSrv.hsSeen), err)
	}
	if len(g.hsSleeps) != 2 || g.hsSleeps[0] != 7*time.Second || g.hsSleeps[1] != 7*time.Second {
		t.Fatalf("waits: %v", g.hsSleeps)
	}
	retries := 0
	for _, e := range g.hsEvents {
		if e.Kind == ingest.EventRetry {
			retries++
			if e.Reason != "rate limited" || e.Status != 429 {
				t.Fatalf("retry event: %+v", e)
			}
		}
	}
	if retries != 2 {
		t.Fatalf("%d retry events", retries)
	}
}

// [REQ-664]
func TestRetryAfterMayBeADate(t *testing.T) {
	when := hsEpoch.Add(45 * time.Second).UTC().Format(hsHTTPDate)
	g := hsNew(t, hsDoc("", "", "", ""), hsFails(1, hsWithHeader(hsStatus(503), "retry-after", when)))
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil || len(g.hsSleeps) != 1 || g.hsSleeps[0] != 45*time.Second {
		t.Fatalf("waits %v: %v", g.hsSleeps, err)
	}
	for in, want := range map[string]time.Duration{"0": 0, " 3 ": 3 * time.Second, hsEpoch.Add(-time.Hour).UTC().Format(hsHTTPDate): 0} {
		if got, ok := retryAfter(map[string][]string{"Retry-After": {in}}, hsEpoch); !ok || got != want {
			t.Errorf("%q: %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "-4", "soon"} {
		if _, ok := retryAfter(map[string][]string{"Retry-After": {in}}, hsEpoch); ok {
			t.Errorf("%q is no wait", in)
		}
	}
}

// [REQ-664]
func TestARetryAfterLongerThanTheLimitEndsTheReadAtOnce(t *testing.T) {
	g := hsNew(t, hsDoc("", "", "", `,"max_retry_wait_seconds":30`), hsFails(1, hsWithHeader(hsStatus(429), "Retry-After", "31")))
	_, err := g.hsRead(ingest.PageOptions{})
	se, ok := ingest.SourceErrorOf(err)
	if !ok || se.Kind != ingest.SrcRateLimited || se.Class != ingest.CheckTransient || len(g.hsSleeps) != 0 || len(g.hsSrv.hsSeen) != 1 {
		t.Fatalf("%v after %d requests, waits %v", err, len(g.hsSrv.hsSeen), g.hsSleeps)
	}
}

// [REQ-664]
func TestAFailureWithNoRetryAfterIsSentAgainThreeTimesAfterGrowingWaitsThatTheLimitCaps(t *testing.T) {
	g := hsNew(t, hsDoc("", "", "", `,"max_retry_wait_seconds":3`), hsFails(10, hsStatus(503)))
	_, err := g.hsRead(ingest.PageOptions{})
	se, ok := ingest.SourceErrorOf(err)
	if !ok || se.Kind != ingest.SrcSourceBusy || se.Class != ingest.CheckTransient || len(g.hsSrv.hsSeen) != 4 || len(g.hsSleeps) != 3 {
		t.Fatalf("%v after %d requests", err, len(g.hsSrv.hsSeen))
	}
	if w := g.hsSleeps; w[0] < time.Second || w[0] > 1250*time.Millisecond || w[1] < 2*time.Second || w[1] > 2500*time.Millisecond || w[2] != 3*time.Second {
		t.Fatalf("waits %v", w)
	}
}

// [REQ-664]
func TestAConnectionThatBreaksOrTimesOutIsSentAgainButACertificateIsNot(t *testing.T) {
	for _, kind := range []egress.Kind{egress.KindConnect, egress.KindTimeout} {
		g := hsNew(t, hsDoc("", "", "", ""), hsFails(2, hsReply{hsErr: &egress.Error{Kind: kind, Reason: "x"}}))
		pages, err := g.hsRead(ingest.PageOptions{})
		if err != nil || len(hsSerials(pages)) != 2 || len(g.hsSrv.hsSeen) != 3 {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	for _, kind := range []egress.Kind{egress.KindTLS, egress.KindPolicy, egress.KindResolve, egress.KindRedirect} {
		g := hsNew(t, hsDoc("", "", "", ""), hsFails(1, hsReply{hsErr: &egress.Error{Kind: kind, Reason: "x"}}))
		if _, err := g.hsRead(ingest.PageOptions{}); err == nil || len(g.hsSrv.hsSeen) != 1 {
			t.Fatalf("%s was sent again", kind)
		}
	}
}

// [REQ-664]
func TestA408IsSentAgainAndOtherStatusesAreTheRequestsFault(t *testing.T) {
	g := hsNew(t, hsDoc("", "", "", ""), hsFails(1, hsStatus(408)))
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil || len(g.hsSrv.hsSeen) != 2 {
		t.Fatalf("408: %v", err)
	}
	for _, status := range []int{400, 404, 410, 422} {
		g := hsNew(t, hsDoc("", "", "", ""), func(_ int, _ hsRequest) hsReply { return hsStatus(status) })
		_, err := g.hsRead(ingest.PageOptions{})
		se, ok := ingest.SourceErrorOf(err)
		if !ok || se.Kind != ingest.SrcRequestRejected || se.Class != ingest.CheckConfig || len(g.hsSrv.hsSeen) != 1 {
			t.Fatalf("%d: %v", status, err)
		}
		if g.hsSrv.hsBodies != 1 {
			t.Fatalf("the body of the %d was not closed", status)
		}
	}
}

// [REQ-664]
func TestTheRequestsAreSpacedByTheRateOfTheConnection(t *testing.T) {
	doc := strings.Replace(hsDoc("", "", hsPageKind(1, 1, false), ""), `"requests_per_second":1000`, `"requests_per_second":4`, 1)
	g := hsNew(t, doc, hsAPI(ingest.PagePage, 5, 1, 1))
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
		t.Fatal(err)
	}
	// 6 requests (5 pages and the empty one) at 4 a second: 5 waits of a quarter of a second, the first request is free
	var total time.Duration
	for _, d := range g.hsSleeps {
		total += d
	}
	if len(g.hsSrv.hsSeen) != 6 || len(g.hsSleeps) != 5 || total != 1250*time.Millisecond {
		t.Fatalf("%d requests, waits %v", len(g.hsSrv.hsSeen), g.hsSleeps)
	}
}

// [REQ-664]
func TestTheSleepOfTheRealTimerStopsWithTheContext(t *testing.T) {
	if err := sleepFor(hsCtx, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(hsCtx)
	cancel()
	if err := sleepFor(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled sleep: %v", err)
	}
}

// [REQ-664]
func TestAnAnswerBiggerThanTheLimitAfterItIsUnpackedIsAConfigurationError(t *testing.T) {
	big := hsJSON(`{"items":` + hsRecords(0, 40) + `}`)
	g := hsNew(t, hsDoc("", "", "", `,"max_response_bytes":1024`), func(_ int, _ hsRequest) hsReply { return big })
	_, err := g.hsRead(ingest.PageOptions{})
	se, ok := ingest.SourceErrorOf(err)
	if !ok || se.Kind != ingest.SrcResponseFormat || se.Class != ingest.CheckConfig || !strings.Contains(se.Message, "1024") || len(g.hsSrv.hsSeen) != 1 || g.hsSrv.hsBodies != 1 {
		t.Fatalf("%v", err)
	}
	exact := `{"items":[]}`
	exact += strings.Repeat(" ", 1024-len(exact))
	g = hsNew(t, hsDoc("", "", "", `,"max_response_bytes":1024`), func(_ int, _ hsRequest) hsReply { return hsJSON(exact) })
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
		t.Fatalf("an answer of exactly the limit: %v", err)
	}
}

// [REQ-664]
func TestEachReadMakesItsOwnClientWithTheTimeoutAndTheCertificateAuthorityOfTheConnection(t *testing.T) {
	doc := hsDoc("", "", "", `,"timeout_seconds":12`)
	g := hsNew(t, doc, hsOne)
	for i := 0; i < 2; i++ {
		if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if g.hsSrv.hsClients != 2 || g.hsSrv.hsShut != 2 || g.hsSrv.hsTimeouts[0] != 12*time.Second {
		t.Fatalf("clients %d, closed %d, timeouts %v", g.hsSrv.hsClients, g.hsSrv.hsShut, g.hsSrv.hsTimeouts)
	}
	if g.hsSrv.hsCAs[0] != "" {
		t.Fatal("no certificate authority was given")
	}
}

// [REQ-664]
func TestThePlainHeadersOfTheConnectionAreNeverCompressedByTheAdapter(t *testing.T) {
	g := hsNew(t, hsDoc(hsBearer, "", "", ""), hsOne)
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, r := range g.hsSrv.hsSeen {
		if hasHeader(r.hsHeader, "Accept-Encoding") {
			t.Fatal("the adapter asked for a compression")
		}
	}
}
