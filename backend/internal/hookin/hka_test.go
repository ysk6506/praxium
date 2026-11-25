package hookin_test

import (
	"encoding/base64"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/hookin"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

const hkaAccepted = `{"data":{"accepted":true,"duplicate":false},"meta":{}}`

// [REQ-862]
func TestHkaACallThatIsProvenByEachTypeIsAcceptedAndMarksTheSignal(t *testing.T) {
	rig := hkaNew(t)
	now := rig.clock.Now()
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("basic:"+rig.secrets["basic"]))
	body := `{"event":"changed"}`
	ts := "1893553445"
	_ = ts
	calls := map[string]*http.Request{
		"signed":   rig.signed("signed", "msg_1", now, body),
		"header":   hkaReq("header", body, map[string]string{"X-Webhook-Token": rig.secrets["header"]}),
		"basic":    hkaReq("basic", body, map[string]string{"Authorization": basic}),
		"url":      hkaReq("url", body, nil),
		"bodyonly": hkaReq("bodyonly", body, map[string]string{"Webhook-Signature": hkaSign(rig.secrets["bodyonly"], body)}),
	}
	calls["url"].URL.RawQuery = "token=" + rig.secrets["url"]
	n := int64(0)
	for name, r := range calls {
		got := rig.serve(r)
		n++
		if got.status != 202 || got.body != hkaAccepted+"\n" && got.body != hkaAccepted || !strings.HasPrefix(got.header.Get("Content-Type"), "application/json") {
			t.Errorf("%s: %d %s", name, got.status, got.body)
		}
		if rig.spy.signals.Load() != n {
			t.Errorf("%s: %d signals marked", name, rig.spy.signals.Load())
		}
	}
	if rig.h.Counts()["accepted"] != 5 {
		t.Errorf("counts %v", rig.h.Counts())
	}
}

// [REQ-862]
func TestHkaNothingBehindTheProofIsReachedByACallThatIsNotProven(t *testing.T) {
	rig := hkaNew(t)
	now := rig.clock.Now()
	body := `{"a":1}`
	bad := rig.signed("signed", "m", now, body)
	bad.Header.Set("Webhook-Signature", hkaSign("whsec_"+base64.StdEncoding.EncodeToString(make([]byte, 32)), "m."+bad.Header.Get("Webhook-Timestamp")+"."+body))
	wrongType := hkaReq("header", body, map[string]string{"X-Webhook-Token": rig.secrets["header"], "Content-Type": "text/plain"})
	wrongType.Header.Set("X-Webhook-Token", "nope")
	calls := map[string]*http.Request{
		"unknown webhook":     hkaReq("nothing", body, map[string]string{"X-Webhook-Token": rig.secrets["header"]}),
		"invalid id":          hkaReq("Bad%20Id", body, nil),
		"wrong signature":     bad,
		"no signature":        hkaReq("signed", body, nil),
		"wrong token":         wrongType,
		"no token":            hkaReq("header", body, nil),
		"token of another":    hkaReq("header", body, map[string]string{"X-Webhook-Token": rig.secrets["url"]}),
		"wrong user of basic": hkaReq("basic", body, map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte("other:"+rig.secrets["basic"]))}),
		"not an address":      hkaReq("allowed", body, map[string]string{"X-Webhook-Token": rig.secrets["allowed"]}),
	}
	calls["not an address"].RemoteAddr = hkaOtherIP
	for name, r := range calls {
		got := rig.serve(r)
		if got.status != 401 || got.code() != "auth.webhook_invalid" {
			t.Errorf("%s: %d %s", name, got.status, got.body)
		}
	}
	if rig.spy.reads.Load() != 0 || rig.spy.signals.Load() != 0 {
		t.Errorf("behind the proof: %d reads, %d signals", rig.spy.reads.Load(), rig.spy.signals.Load())
	}
}

// [REQ-862]
func TestHkaAWebhookThatIsNotKnownAnswersAsAWrongSecretDoes(t *testing.T) {
	rig := hkaNew(t)
	body := `{"a":1}`
	unknown := rig.serve(hkaReq("nothing", body, map[string]string{"X-Webhook-Token": "x"}))
	wrong := rig.serve(hkaReq("header", body, map[string]string{"X-Webhook-Token": "x"}))
	none := rig.serve(hkaReq("header", body, nil))
	badSig := rig.serve(hkaReq("signed", body, map[string]string{"Webhook-Signature": "v1," + base64.StdEncoding.EncodeToString(make([]byte, 32)), "Webhook-Id": "m", "Webhook-Timestamp": "1"}))
	other := hkaReq("allowed", body, map[string]string{"X-Webhook-Token": rig.secrets["allowed"]})
	other.RemoteAddr = hkaOtherIP
	elsewhere := rig.serve(other)
	if unknown.status != 401 {
		t.Fatalf("%d %s", unknown.status, unknown.body)
	}
	for name, r := range map[string]hkaReply{"wrong token": wrong, "no token": none, "wrong signature": badSig, "other address": elsewhere} {
		if r.same() != unknown.same() {
			t.Errorf("%s answers differently from a webhook that is not known:\n%s\n%s", name, r.same(), unknown.same())
		}
	}
	if strings.Contains(unknown.body, "nothing") || strings.Contains(unknown.body, "crm") {
		t.Errorf("the answer echoes the path: %s", unknown.body)
	}
}

// [REQ-862]
func TestHkaTheSignatureOfStandardWebhooksIsOverTheIDTheTimeAndTheBody(t *testing.T) {
	rig := hkaNew(t)
	now := rig.clock.Now()
	body := `{"a":1}`
	cases := map[string]struct {
		req  *http.Request
		want int
	}{
		"right":             {rig.signed("signed", "m1", now, body), 202},
		"299 seconds old":   {rig.signed("signed", "m2", now.Add(-299*time.Second), body), 202},
		"299 seconds ahead": {rig.signed("signed", "m3", now.Add(299*time.Second), body), 202},
		"301 seconds old":   {rig.signed("signed", "m4", now.Add(-301*time.Second), body), 401},
		"301 seconds ahead": {rig.signed("signed", "m5", now.Add(301*time.Second), body), 401},
		"a day old":         {rig.signed("signed", "m6", now.Add(-24*time.Hour), body), 401},
		"body changed": {func() *http.Request {
			r := rig.signed("signed", "m7", now, body)
			r.Body = io.NopCloser(strings.NewReader(`{"a":2}`))
			r.ContentLength = 7
			return r
		}(), 401},
		"id changed": {func() *http.Request {
			r := rig.signed("signed", "m8", now, body)
			r.Header.Set("Webhook-Id", "m9")
			return r
		}(), 401},
		"time changed": {func() *http.Request {
			r := rig.signed("signed", "m10", now, body)
			r.Header.Set("Webhook-Timestamp", "1")
			return r
		}(), 401},
		"no id": {func() *http.Request {
			r := rig.signed("signed", "m11", now, body)
			r.Header.Del("Webhook-Id")
			return r
		}(), 401},
		"time not a number": {func() *http.Request {
			r := rig.signed("signed", "m12", now, body)
			r.Header.Set("Webhook-Timestamp", "soon")
			return r
		}(), 401},
		"signature of the body": {hkaReq("signed", body, map[string]string{"Webhook-Id": "m", "Webhook-Timestamp": "1893553445", "Webhook-Signature": hkaSign(rig.secrets["signed"], body)}), 401},
		"version v2 only": {func() *http.Request {
			r := rig.signed("signed", "m13", now, body)
			r.Header.Set("Webhook-Signature", "v2"+r.Header.Get("Webhook-Signature")[2:])
			return r
		}(), 401},
		"not base64": {func() *http.Request {
			r := rig.signed("signed", "m14", now, body)
			r.Header.Set("Webhook-Signature", "v1,@@@")
			return r
		}(), 401},
		"signature too short": {func() *http.Request {
			r := rig.signed("signed", "m15", now, body)
			r.Header.Set("Webhook-Signature", "v1,AAAA")
			return r
		}(), 401},
		"header too long": {func() *http.Request {
			r := rig.signed("signed", "m16", now, body)
			r.Header.Set("Webhook-Signature", r.Header.Get("Webhook-Signature")+" "+strings.Repeat("v1,AAAA ", 400))
			return r
		}(), 401},
		"id too long": {func() *http.Request { r := rig.signed("signed", strings.Repeat("i", 300), now, body); return r }(), 202},
	}
	_ = cases["id too long"]
	delete(cases, "id too long")
	for name, c := range cases {
		if got := rig.serve(c.req); got.status != c.want {
			t.Errorf("%s: %d %s", name, got.status, got.body)
		}
	}
	// Several signatures: one of them right is enough; the right one past the eighth is not read.
	r := rig.signed("signed", "mm", now, body)
	right := r.Header.Get("Webhook-Signature")
	wrong := "v1," + base64.StdEncoding.EncodeToString(make([]byte, 32))
	r.Header.Set("Webhook-Signature", wrong+" v2,xx "+right)
	if got := rig.serve(r); got.status != 202 {
		t.Errorf("one right among several: %d", got.status)
	}
	r = rig.signed("signed", "mn", now, body)
	right = r.Header.Get("Webhook-Signature")
	r.Header.Set("Webhook-Signature", strings.Repeat(wrong+" ", 8)+right)
	if got := rig.serve(r); got.status != 401 {
		t.Errorf("a right one after eight: %d", got.status)
	}
	// The weak profile signs the body alone and has no time: a call that was caught is accepted again.
	first := hkaReq("bodyonly", body, map[string]string{"Webhook-Signature": hkaSign(rig.secrets["bodyonly"], body)})
	again := hkaReq("bodyonly", body, map[string]string{"Webhook-Signature": hkaSign(rig.secrets["bodyonly"], body)})
	rig.clock.Advance(48 * time.Hour)
	if a, b := rig.serve(first), rig.serve(again); a.status != 202 || b.status != 202 {
		t.Errorf("body_only: %d %d", a.status, b.status)
	}
}

// [REQ-862]
func TestHkaATokenIsProvenWhereItIsMeantToBeAndNowhereElse(t *testing.T) {
	rig := hkaNew(t)
	body := `{}`
	url := hkaReq("url", body, nil)
	url.URL.RawQuery = "token=" + rig.secrets["url"] + "&token=" + rig.secrets["url"]
	inHeader := hkaReq("url", body, map[string]string{"X-Webhook-Token": rig.secrets["url"], "Authorization": "Bearer " + rig.secrets["url"]})
	inPath := hkaReq("url", body, nil)
	inPath.URL.Path += "/" + rig.secrets["url"]
	onSigned := hkaReq("signed", body, map[string]string{"X-Webhook-Token": rig.secrets["signed"]})
	onSigned.URL.RawQuery = "token=" + rig.secrets["signed"]
	headerOnQuery := hkaReq("header", body, nil)
	headerOnQuery.URL.RawQuery = "token=" + rig.secrets["header"]
	long := hkaReq("header", body, map[string]string{"X-Webhook-Token": strings.Repeat("a", 5000)})
	for name, r := range map[string]*http.Request{"two tokens": url, "token in a header for a url token": inHeader, "token in the path": inPath, "key of a signed webhook as a token": onSigned, "a header token in the query": headerOnQuery, "a very long token": long} {
		if got := rig.serve(r); got.status != 401 {
			t.Errorf("%s: %d %s", name, got.status, got.body)
		}
	}
	good := hkaReq("url", body, nil)
	good.URL.RawQuery = "token=" + rig.secrets["url"]
	if got := rig.serve(good); got.status != 202 {
		t.Errorf("good: %d", got.status)
	}
	// The length of what is presented is not told: a short and a long wrong token are answered alike.
	short := rig.serve(hkaReq("header", body, map[string]string{"X-Webhook-Token": "a"}))
	longer := rig.serve(hkaReq("header", body, map[string]string{"X-Webhook-Token": strings.Repeat("a", 1000)}))
	if short.same() != longer.same() {
		t.Error("the answer depends on the length of the token")
	}
}

// [REQ-862]
func TestHkaTheEntranceKeepsToTheLimitsOfTheBody(t *testing.T) {
	rig := hkaNew(t)
	now := rig.clock.Now()
	big := `{"a":"` + strings.Repeat("x", 4096) + `"}`
	if got := rig.serve(rig.signed("signed", "b1", now, big)); got.status != 413 || got.code() != "invalid_input.body_too_large" {
		t.Errorf("a body over the limit: %d %s", got.status, got.body)
	}
	// A declared length over the limit is refused without reading a byte.
	counting := &hkaCounting{r: strings.NewReader(big)}
	r := hkaReq("signed", "", map[string]string{"X-Webhook-Token": "x"})
	r.Body, r.ContentLength = io.NopCloser(counting), int64(len(big))
	if got := rig.serve(r); got.status != 413 || counting.n != 0 {
		t.Errorf("declared length: %d, %d bytes read", got.status, counting.n)
	}
	// Without a length (chunked) the entrance reads to the limit and one byte more, and no further.
	counting = &hkaCounting{r: strings.NewReader(strings.Repeat("y", 1<<20))}
	r = hkaReq("signed", "", nil)
	r.Body, r.ContentLength = io.NopCloser(counting), -1
	if got := rig.serve(r); got.status != 413 || counting.n > 4097+512 {
		t.Errorf("no length: %d, %d bytes read", got.status, counting.n)
	}
	// A webhook that is not known is read to the smallest limit.
	counting = &hkaCounting{r: strings.NewReader(strings.Repeat("y", 1<<20))}
	r = hkaReq("nothing", "", nil)
	r.Body, r.ContentLength = io.NopCloser(counting), -1
	rig.serve(r)
	if counting.n > 1025+512 {
		t.Errorf("a webhook that is not known was read for %d bytes", counting.n)
	}
	if got := rig.serve(rig.signed("signed", "b2", now, strings.Repeat("a", 4096))); got.status == 413 {
		t.Error("a body of exactly the limit was refused as too large")
	}
}

type hkaCounting struct {
	r io.Reader
	n int
}

func (c *hkaCounting) Read(p []byte) (int, error) { n, err := c.r.Read(p); c.n += n; return n, err }

// [REQ-862]
func TestHkaTheBodyIsCheckedAsJSONOnlyForACallThatIsProven(t *testing.T) {
	rig := hkaNew(t)
	tok := map[string]string{"X-Webhook-Token": rig.secrets["header"]}
	with := func(body string, extra map[string]string) hkaReply {
		h := map[string]string{}
		for k, v := range tok {
			h[k] = v
		}
		for k, v := range extra {
			h[k] = v
		}
		return rig.serve(hkaReq("header", body, h))
	}
	deep := strings.Repeat("[", hookin.MaxJSONDepth+1) + strings.Repeat("]", hookin.MaxJSONDepth+1)
	ok := strings.Repeat("[", hookin.MaxJSONDepth) + strings.Repeat("]", hookin.MaxJSONDepth)
	many := "[" + strings.TrimSuffix(strings.Repeat("1,", hookin.MaxJSONTokens), ",") + "]"
	for name, body := range map[string]string{"empty": "", "text": "hello", "cut": `{"a":`, "trailing": `{"a":1} x`, "two values": `{} {}`, "too deep": deep, "too many tokens": many} {
		if got := with(body, nil); got.status != 400 || got.code() != "invalid_input.webhook_body" {
			t.Errorf("%s: %d %s", name, got.status, got.body)
		}
	}
	for name, body := range map[string]string{"an object": `{"a":[1,2,{"b":null}]}`, "an array": `[1]`, "a string": `"x"`, "deepest": ok} {
		if got := with(body, nil); got.status != 202 {
			t.Errorf("%s: %d %s", name, got.status, got.body)
		}
	}
	for _, ct := range []string{"text/plain", "application/xml", "", "application/jsonx"} {
		if got := with(`{}`, map[string]string{"Content-Type": ct}); got.status != 415 {
			t.Errorf("content type %q: %d", ct, got.status)
		}
	}
	if got := with(`{}`, map[string]string{"Content-Type": "application/json; charset=utf-8"}); got.status != 202 {
		t.Errorf("a charset: %d", got.status)
	}
	// A call that is not proven learns nothing of the body: the wrong media type and the wrong body are the 401.
	bad := hkaReq("header", `not json`, map[string]string{"X-Webhook-Token": "wrong", "Content-Type": "text/plain"})
	if got := rig.serve(bad); got.status != 401 {
		t.Errorf("not proven: %d", got.status)
	}
}

// [REQ-862]
func TestHkaTheSourceThatIsStoppedAndTheRateOfTheWebhookAreAnsweredOnlyToACallThatIsProven(t *testing.T) {
	rig := hkaNew(t)
	rig.spy.receiving.Store(false)
	good := func() *http.Request {
		return hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": rig.secrets["header"]})
	}
	got := rig.serve(good())
	if got.status != 503 || got.code() != "stale.source_not_receiving" || got.header.Get("Retry-After") == "" {
		t.Errorf("a stopped source: %d %s", got.status, got.body)
	}
	if rig.spy.signals.Load() != 0 {
		t.Error("a signal was marked for a source that is stopped")
	}
	if wrong := rig.serve(hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": "x"})); wrong.status != 401 {
		t.Errorf("a stopped source is not told to a caller that is not proven: %d", wrong.status)
	}
	rig.spy.receiving.Store(true)
	slow := func() *http.Request {
		return hkaReq("slow", `{}`, map[string]string{"X-Webhook-Token": rig.secrets["slow"]})
	}
	if a, b := rig.serve(slow()), rig.serve(slow()); a.status != 202 || b.status != 202 {
		t.Fatalf("the burst: %d %d", a.status, b.status)
	}
	limited := rig.serve(slow())
	if limited.status != 429 || limited.code() != "rate_limited.webhook_rate" || limited.header.Get("Retry-After") != "1" {
		t.Errorf("over the rate: %d %s %q", limited.status, limited.body, limited.header.Get("Retry-After"))
	}
	if rig.spy.signals.Load() != 2 {
		t.Errorf("%d signals", rig.spy.signals.Load())
	}
	rig.clock.Advance(1100 * time.Millisecond)
	if again := rig.serve(slow()); again.status != 202 {
		t.Errorf("after a second: %d", again.status)
	}
	// A webhook has its own bucket.
	if other := rig.serve(hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": rig.secrets["header"]})); other.status != 202 {
		t.Errorf("another webhook: %d", other.status)
	}
	rig.spy.sinkErr, rig.spy.stateErr = io.ErrClosedPipe, nil
	if f := rig.serve(good()); f.status != 503 || f.code() != "server.unavailable" {
		t.Errorf("a sink that fails: %d %s", f.status, f.body)
	}
	rig.spy.sinkErr, rig.spy.stateErr = nil, io.ErrClosedPipe
	if f := rig.serve(good()); f.status != 503 || f.code() != "server.unavailable" {
		t.Errorf("a state that cannot be read: %d %s", f.status, f.body)
	}
}

// [REQ-862]
func TestHkaFailuresAreCountedByTheBucketAndNeverLockACallerThatIsRight(t *testing.T) {
	rig := hkaNew(t, func(c *hookin.Config) { c.FailureRate, c.FailureBurst = 1, 3 })
	wrong := func(ip string) *http.Request {
		r := hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": "wrong"})
		r.RemoteAddr = ip
		return r
	}
	var statuses []int
	for i := 0; i < 6; i++ {
		statuses = append(statuses, rig.serve(wrong(hkaGoodIP)).status)
	}
	if statuses[0] != 401 || statuses[2] != 401 || statuses[3] != 429 || statuses[5] != 429 {
		t.Errorf("statuses %v", statuses)
	}
	// The bucket is empty for this address, and a call that is proven still passes.
	right := hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": rig.secrets["header"]})
	if got := rig.serve(right); got.status != 202 {
		t.Errorf("a right call after the failures: %d %s", got.status, got.body)
	}
	// Another address has its own bucket, and so has another webhook.
	if got := rig.serve(wrong("198.51.100.77:1")); got.status != 401 {
		t.Errorf("another address: %d", got.status)
	}
	// The bucket fills again with time: nothing is locked.
	rig.clock.Advance(5 * time.Second)
	if got := rig.serve(wrong(hkaGoodIP)); got.status != 401 {
		t.Errorf("after a few seconds: %d", got.status)
	}
	if rig.h.Counts()["fail_token"] == 0 || rig.h.Counts()["fail_limited"] == 0 {
		t.Errorf("counts %v", rig.h.Counts())
	}
}

// [REQ-862]
func TestHkaTheLogHoldsNoSecretNoQueryAndNoBodyAndGroupsTheFailures(t *testing.T) {
	rig := hkaNew(t)
	secret := rig.secrets["url"]
	for i := 0; i < 5; i++ {
		r := hkaReq("url", `{"private":"the-body-text"}`, nil)
		r.URL.RawQuery = "token=wrong-token-value&k=" + secret
		rig.serve(r)
	}
	good := hkaReq("url", `{"private":"the-body-text"}`, nil)
	good.URL.RawQuery = "token=" + secret
	rig.serve(good)
	log := rig.logs.String()
	for _, leak := range []string{secret, "wrong-token-value", "the-body-text", "token=", "private"} {
		if strings.Contains(log, leak) {
			t.Errorf("the log holds %q:\n%s", leak, log)
		}
	}
	if lines := strings.Count(log, "\n"); lines != 1 {
		t.Errorf("five failures of one kind from one address are %d lines, want one:\n%s", lines, log)
	}
	if !strings.Contains(log, `"webhook":"crm/url"`) || !strings.Contains(log, `"address":"192.0.2.10"`) || !strings.Contains(log, `"kind":"token"`) {
		t.Errorf("the line lacks the webhook, the address or the kind: %s", log)
	}
	rig.clock.Advance(2 * time.Minute)
	rig.serve(hkaReq("url", `{}`, nil))
	if !strings.Contains(rig.logs.String(), `"calls":5`) && !strings.Contains(rig.logs.String(), `"calls":1`) {
		t.Errorf("the next line gives the count: %s", rig.logs.String())
	}
}

// [REQ-862]
func TestHkaAddressesAreTakenFromTheConnectionAndFromTheProxyOnlyWhenTheOperatorSaysSo(t *testing.T) {
	plain := hkaNew(t)
	r := hkaReq("allowed", `{}`, map[string]string{"X-Webhook-Token": plain.secrets["allowed"], "X-Forwarded-For": "192.0.2.5"})
	r.RemoteAddr = hkaOtherIP
	if got := plain.serve(r); got.status != 401 {
		t.Errorf("a forwarded address was believed: %d", got.status)
	}
	proxied := hkaNew(t, func(c *hookin.Config) { c.TrustProxy = true })
	r = hkaReq("allowed", `{}`, map[string]string{"X-Webhook-Token": proxied.secrets["allowed"], "X-Forwarded-For": "203.0.113.1, 192.0.2.5"})
	r.RemoteAddr = hkaOtherIP
	if got := proxied.serve(r); got.status != 202 {
		t.Errorf("the last address of a proxy: %d", got.status)
	}
	r = hkaReq("allowed", `{}`, map[string]string{"X-Webhook-Token": proxied.secrets["allowed"], "X-Forwarded-For": "192.0.2.5, 203.0.113.1"})
	if got := proxied.serve(r); got.status != 401 {
		t.Errorf("a first address is what a caller writes: %d", got.status)
	}
	v6 := hkaReq("allowed", `{}`, map[string]string{"X-Webhook-Token": plain.secrets["allowed"]})
	v6.RemoteAddr = "[::ffff:192.0.2.10]:9"
	if got := plain.serve(v6); got.status != 202 {
		t.Errorf("an address mapped into IPv6: %d", got.status)
	}
	junk := hkaReq("allowed", `{}`, map[string]string{"X-Webhook-Token": plain.secrets["allowed"]})
	junk.RemoteAddr = "not an address"
	if got := plain.serve(junk); got.status != 401 {
		t.Errorf("no address: %d", got.status)
	}
}

// [REQ-862]
func TestHkaTheEntranceHasRoomForSoManyCallsAndSoMuchBody(t *testing.T) {
	// Two calls hold the room while a third and a fourth come in.
	gate := make(chan struct{})
	rig := hkaNew(t, func(c *hookin.Config) { c.MaxInFlight, c.PerWebhookInFlight = 2, 1 })
	hold := func(webhook string, want int64) <-chan hkaReply {
		out := make(chan hkaReply, 1)
		pr, pw := io.Pipe()
		r := hkaReq(webhook, "", map[string]string{"X-Webhook-Token": rig.secrets[webhook]})
		if webhook == "basic" {
			r.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("basic:"+rig.secrets[webhook])))
		}
		r.Body, r.ContentLength = pr, -1
		go func() { out <- rig.serve(r) }()
		go func() { <-gate; _, _ = pw.Write([]byte(`{}`)); _ = pw.Close() }()
		for rig.h.InFlight() < want {
			runtime.Gosched()
		}
		return out
	}
	first := hold("header", 1)
	// The same webhook again: over its own room.
	if got := rig.serve(hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": rig.secrets["header"]})); got.status != 429 || got.code() != "rate_limited.concurrency" || got.header.Get("Retry-After") == "" {
		t.Errorf("a webhook at once: %d %s", got.status, got.body)
	}
	second := hold("basic", 2)
	// Two calls are in; the entrance takes no third.
	if got := rig.serve(hkaReq("url", `{}`, nil)); got.status != 503 || got.code() != "server.unavailable" || got.header.Get("Retry-After") == "" {
		t.Errorf("the whole entrance: %d %s", got.status, got.body)
	}
	close(gate)
	for _, c := range []<-chan hkaReply{first, second} {
		if got := <-c; got.status != 202 {
			t.Errorf("the calls that were let in: %d %s", got.status, got.body)
		}
	}
	// The room is given back.
	if got := rig.serve(hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": rig.secrets["header"]})); got.status != 202 {
		t.Errorf("after: %d", got.status)
	}
	// The sum of the bodies that may be read is bounded too.
	tight := hkaNew(t, func(c *hookin.Config) { c.MaxBodyMemory = 100 << 10 })
	if got := tight.serve(hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": tight.secrets["header"]})); got.status != 503 {
		t.Errorf("a body room that is smaller than one body: %d", got.status)
	}
}

// [REQ-862]
func TestHkaAnythingButAPostIsRefusedAndAHookThatCannotBeHandledIsNotOne(t *testing.T) {
	rig := hkaNew(t)
	r := hkaReq("header", `{}`, nil)
	r.Method = http.MethodGet
	got := rig.serve(r)
	if got.status != 405 || got.header.Get("Allow") != "POST" {
		t.Errorf("GET: %d %v", got.status, got.header)
	}
	rig.spy.mu.Lock()
	rig.spy.hooks[hkaSource+"/records"] = hookin.Hook{Kind: ingest.WebhookRecords, Auth: ingest.WebhookAuth{Type: ingest.WebhookAuthHeaderToken}}
	rig.spy.mu.Unlock()
	if got := rig.serve(hkaReq("records", `{}`, nil)); got.status != 401 {
		t.Errorf("a webhook of a kind that is not handled: %d", got.status)
	}
	for path, want := range map[string]bool{
		"/v1/sources/a/webhooks/b:receive": true, "/v1/sources/a/webhooks/b": false, "/v1/sources/a/webhooks/b:issue-secret": false,
		"/v1/sources/a:receive": false, "/v1/health": false, "/v1/sources/../webhooks/%00:receive": true,
	} {
		if hookin.Match(path) != want {
			t.Errorf("Match(%q) != %v", path, want)
		}
	}
	if _, err := hookin.New(hookin.Config{}); err == nil {
		t.Error("a handler without its parts was made")
	}
	if _, err := hookin.HookOf(ingest.Webhook{ID: "a", Kind: "signal", AllowIPs: []string{"x"}}); err == nil {
		t.Error("an address that is none")
	}
	if d, _ := hookin.HookOf(ingest.Webhook{ID: "a", Kind: "signal"}); d.Limits.BodyBytes != ingest.DefaultWebhookBodyBytes || d.Limits.RatePerSecond != ingest.DefaultWebhookRate || d.Limits.Burst != ingest.DefaultWebhookBurst {
		t.Errorf("defaults %+v", d.Limits)
	}
	if k := hookin.Kinds(); len(k) == 0 {
		t.Error("no kinds")
	}
	hkaDrain(strings.NewReader(""))
}
