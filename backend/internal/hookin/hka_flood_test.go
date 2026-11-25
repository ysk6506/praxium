package hookin_test

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/hookin"
	"github.com/ysk6506/praxium/backend/internal/httpapi"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The entrance before a caller is proven (ADR-0107): what a crowd of slow calls that prove nothing can hold of it.

// hkaHeld is a call whose body has not come: it holds its place in the entrance until the body is given or ends.
type hkaHeld struct {
	reply chan hkaReply
	pw    *io.PipeWriter
}

// hold starts a call of a webhook from an address whose body is a pipe, and says nothing until the call is in the entrance
// (it is, or it was answered at once). declared is the length the call says its body has, and -1 is none.
func (rig *hkaRig) hold(webhook, from string, declared int64, headers map[string]string) *hkaHeld {
	pr, pw := io.Pipe()
	r := hkaReq(webhook, "", headers)
	r.RemoteAddr = from
	r.Body, r.ContentLength = pr, declared
	h := &hkaHeld{reply: make(chan hkaReply, 1), pw: pw}
	before := rig.h.InFlight()
	go func() { h.reply <- rig.serve(r) }()
	for i := 0; i < 5000 && rig.h.InFlight() == before && len(h.reply) == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	return h
}

// answered is the reply of a call that did not wait for its body, and false when the call is still in the entrance.
func (h *hkaHeld) answered() (hkaReply, bool) {
	select {
	case r := <-h.reply:
		return r, true
	case <-time.After(50 * time.Millisecond):
		return hkaReply{}, false
	}
}

// end lets a held call finish: its body is cut short, which is a failed read.
func (h *hkaHeld) end(t *testing.T) hkaReply {
	t.Helper()
	_ = h.pw.CloseWithError(io.ErrUnexpectedEOF)
	select {
	case r := <-h.reply:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("a held call did not end")
		return hkaReply{}
	}
}

// [REQ-862]
// Slow calls that prove nothing, from one address, take no more than the places of an address, and the sender that is right
// (from another address) still gets in. The same calls took all the places of the signed webhook before.
func TestHkaSlowCallsOfOneAddressDoNotTakeTheSignedWebhookFromItsRightSender(t *testing.T) {
	rig := hkaNew(t)
	var held []*hkaHeld
	refused := 0
	for i := 0; i < 16; i++ {
		h := rig.hold("signed", "203.0.113.5:1000", 100, nil)
		if got, done := h.answered(); done {
			if got.status != 429 || got.code() != "rate_limited.concurrency" || got.header.Get("Retry-After") == "" {
				t.Fatalf("a call over the places of its address: %d %s", got.status, got.body)
			}
			refused++
			continue
		}
		held = append(held, h)
	}
	if len(held) != hookin.DefaultPerAddressInFlight || refused != 16-hookin.DefaultPerAddressInFlight {
		t.Fatalf("%d calls held and %d refused, want %d and %d", len(held), refused, hookin.DefaultPerAddressInFlight, 16-hookin.DefaultPerAddressInFlight)
	}
	if got := rig.h.InFlight(); got != int64(len(held)) {
		t.Errorf("the entrance holds %d calls", got)
	}
	// The right sender, from another address, is taken: the webhook has room, because the crowd could not fill it.
	if got := rig.serve(rig.signed("signed", "ok1", rig.clock.Now(), `{}`)); got.status != 202 {
		t.Errorf("the right sender while a crowd waits: %d %s", got.status, got.body)
	}
	// The address of the crowd is counted by its network when it is an IPv6 one: a fifth call from another address of it is refused.
	rig6 := hkaNew(t)
	var six []*hkaHeld
	for i := 0; i < 4; i++ {
		six = append(six, rig6.hold("signed", fmt.Sprintf("[2001:db8:0:1::%d]:1000", i+1), 100, nil))
	}
	over := rig6.hold("signed", "[2001:db8:0:1:ffff::9]:1000", 100, nil)
	if got, done := over.answered(); !done || got.status != 429 {
		t.Errorf("a fifth address of one /64: %v %v", got.status, done)
	}
	other := rig6.hold("signed", "[2001:db8:0:2::1]:1000", 100, nil)
	if _, done := other.answered(); done {
		t.Error("an address of another /64 was refused")
	}
	for _, h := range append(append(six, other), held...) {
		if got := h.end(t); got.status != 401 {
			t.Errorf("a call whose body was cut: %d %s", got.status, got.body)
		}
	}
	// Everything is given back.
	if rig.h.InFlight() != 0 || rig6.h.InFlight() != 0 {
		t.Errorf("places held after the calls ended: %d %d", rig.h.InFlight(), rig6.h.InFlight())
	}
	for i := 0; i < hookin.DefaultPerAddressInFlight+1; i++ {
		h := rig.hold("signed", "203.0.113.5:1000", 100, nil)
		if _, done := h.answered(); done {
			if i < hookin.DefaultPerAddressInFlight {
				t.Errorf("the place of call %d was not given back", i)
			}
			continue
		}
		defer h.end(t)
	}
}

// [REQ-862]
// What a call holds of the memory of the bodies is what it has: a call that says its body is four megabytes and sends none
// holds the start of a buffer, not four megabytes. Eight such calls took the whole memory of the entrance before.
func TestHkaCallsThatPromiseABigBodyHoldOnlyWhatHasComeAndLeaveTheMemoryToOthers(t *testing.T) {
	rig := hkaNew(t)
	rig.spy.mu.Lock()
	rig.spy.hooks[hkaSource+"/big"] = hkaHook(ingest.WebhookAuthHMAC, ingest.ProfileStandardWebhooks, ingest.WebhookLimits{BodyBytes: ingest.MaxWebhookBodyBytes})
	rig.spy.mu.Unlock()
	var held []*hkaHeld
	for i := 0; i < 8; i++ {
		held = append(held, rig.hold("big", fmt.Sprintf("203.0.113.%d:1", i+1), ingest.MaxWebhookBodyBytes, nil))
	}
	for i, h := range held {
		if got, done := h.answered(); done {
			t.Fatalf("call %d was answered while it waited for its body: %d %s", i, got.status, got.body)
		}
	}
	if got := rig.serve(hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": rig.secrets["header"]})); got.status != 202 {
		t.Errorf("a call of another webhook while eight promise four megabytes: %d %s", got.status, got.body)
	}
	for _, h := range held {
		h.end(t)
	}
	// With no length at all the start is 64 KiB, and the memory is still bounded: a room for two of them takes two.
	small := hkaNew(t, func(c *hookin.Config) { c.MaxBodyMemory = 300 << 10 })
	small.spy.mu.Lock()
	small.spy.hooks[hkaSource+"/big"] = hkaHook(ingest.WebhookAuthHMAC, ingest.ProfileStandardWebhooks, ingest.WebhookLimits{BodyBytes: ingest.MaxWebhookBodyBytes})
	small.spy.mu.Unlock()
	a := small.hold("big", "203.0.113.1:1", -1, nil)
	b := small.hold("big", "203.0.113.2:1", -1, nil)
	third := small.hold("big", "203.0.113.3:1", -1, nil)
	if got, done := third.answered(); !done || got.status != 503 || got.code() != "server.unavailable" {
		t.Errorf("a third call with no length in a room for two: %v %v", got.status, done)
	}
	// And a call that is short and says so takes a few bytes of what is left.
	if got := small.serve(small.signed("signed", "m1", small.clock.Now(), `{}`)); got.status != 202 {
		t.Errorf("a short call in what is left of the room: %d %s", got.status, got.body)
	}
	a.end(t)
	b.end(t)
	if small.h.InFlight() != 0 {
		t.Errorf("%d calls still in the entrance", small.h.InFlight())
	}
}

// [REQ-862]
// The entrance on the stack of the server (with the writer of the access log between): a body that does not come ends at
// the first-byte deadline, a body that starts and stops ends at the deadline of the whole, and the places are given back.
func TestHkaSlowBodiesOnTheStackOfTheServerEndAtTheirDeadlinesAndLeaveTheEntranceOpen(t *testing.T) {
	rig := hkaNew(t, func(c *hookin.Config) {
		c.Clock = kernel.SystemClock{}
		c.FirstByteTimeout = 200 * time.Millisecond
		c.ReadTimeout = 900 * time.Millisecond
		c.TrustProxy = true
	})
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := httpapi.New(httpapi.Config{
		Registry: reg, Clock: kernel.NewManualClock(hkaStart), IDs: kernel.NewSequentialIDs(), Version: "test",
		Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
		Mounts: []httpapi.Mount{{Match: hookin.Match, Handler: rig.h}},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.MarkStarted()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	secret := rig.secrets["header"]
	clock := kernel.SystemClock{}
	// slow makes a call that promises a body of 1000 bytes: it sends none, or one byte, and nothing more. It says the status
	// of the answer and how long the answer took.
	slow := func(from string, sendByte bool) (int, time.Duration) {
		pr, pw := io.Pipe()
		defer pw.Close()
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/sources/crm/webhooks/header:receive", pr)
		if err != nil {
			t.Error(err)
			return 0, 0
		}
		req.ContentLength = 1000
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Token", secret)
		req.Header.Set("X-Forwarded-For", from)
		if sendByte {
			go func() { _, _ = pw.Write([]byte("{")) }()
		}
		start := clock.Now()
		resp, err := ts.Client().Do(req)
		took := clock.Now().Sub(start)
		if err != nil {
			t.Errorf("no answer to a body that does not come: %v", err)
			return 0, took
		}
		defer resp.Body.Close()
		return resp.StatusCode, took
	}
	// Eight slow callers at once from two addresses: four places each at most. Every one of them ends by its deadline.
	var wg sync.WaitGroup
	results := make([]string, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code, took := slow(fmt.Sprintf("203.0.113.%d", i%2+1), false)
			results[i] = fmt.Sprintf("%d in %s", code, took.Round(10*time.Millisecond))
			if code != 401 && code != 429 || took > 700*time.Millisecond {
				t.Errorf("a call that sends no byte: %s (the deadline of the first byte is 200 ms)", results[i])
			}
		}(i)
	}
	wg.Wait()
	// One byte, then nothing: the first byte moves the deadline to that of the whole body, not past it.
	code, took := slow("203.0.113.9", true)
	if code != 401 || took < 400*time.Millisecond || took > 3*time.Second {
		t.Errorf("a call that sends a byte and stops: %d in %s, want a 401 at about 900 ms", code, took)
	}
	if rig.h.InFlight() != 0 {
		t.Errorf("the entrance holds %d calls after they ended", rig.h.InFlight())
	}
	// And the right call goes through the same stack at once.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/sources/crm/webhooks/header:receive", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Token", secret)
	req.Header.Set("X-Forwarded-For", "198.51.100.1")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Errorf("the right call after the slow ones: %d", resp.StatusCode)
	}
}

// [REQ-862]
// A server that cannot set a read deadline is one on which no call is taken: a body read with no deadline is how a slow
// sender holds a place for good. The entrance says so once in its log, and reads nothing.
func TestHkaAServerThatCannotSetAReadDeadlineTakesNoCallAndSaysSoOnce(t *testing.T) {
	rig := hkaNew(t)
	counting := &hkaCounting{r: strings.NewReader(`{}`)}
	for i := 0; i < 3; i++ {
		r := hkaReq("header", ``, map[string]string{"X-Webhook-Token": rig.secrets["header"]})
		r.Body, r.ContentLength = io.NopCloser(counting), 2
		rec := httptest.NewRecorder() // a recorder takes no deadline, as a writer that wraps another and does not pass it on
		rig.h.ServeHTTP(rec, r)
		if rec.Code != 503 || !strings.Contains(rec.Body.String(), "server.unavailable") {
			t.Errorf("call %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	if counting.n != 0 || rig.spy.signals.Load() != 0 || rig.spy.asked.Load() != 0 {
		t.Errorf("a call was handled: %d bytes read, %d signals, %d checks", counting.n, rig.spy.signals.Load(), rig.spy.asked.Load())
	}
	if n := strings.Count(rig.logs.String(), "cannot set a read deadline"); n != 1 {
		t.Errorf("the entrance said it %d times:\n%s", n, rig.logs.String())
	}
	if rig.h.Counts()["no_deadline"] != 3 || rig.h.InFlight() != 0 {
		t.Errorf("counts %v, %d in flight", rig.h.Counts(), rig.h.InFlight())
	}
}
