package hookin_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/hookin"
)

// The numbers that the entrance has when nothing is set, and what it asks of the server with them (ADR-0107).

// [REQ-862]
// The defaults are the numbers of the decision and of the guide, as numbers: a change of one is a change of what an
// unproven call can hold, and it is made on purpose.
func TestHkaTheDefaultsOfTheEntranceAreTheNumbersOfTheDecision(t *testing.T) {
	for _, c := range []struct {
		name      string
		got, want int64
	}{
		{"calls in the entrance", hookin.DefaultMaxInFlight, 128},
		{"memory of the bodies", hookin.DefaultMaxBodyMemory, 33554432},
		{"calls of a webhook", hookin.DefaultPerWebhookInFlight, 16},
		{"calls of an address", hookin.DefaultPerAddressInFlight, 4},
		{"read timeout", int64(hookin.DefaultReadTimeout), int64(10 * time.Second)},
		{"first byte timeout", int64(hookin.DefaultFirstByteTimeout), int64(2 * time.Second)},
		{"highest read timeout", int64(hookin.MaxReadTimeout), int64(30 * time.Second)},
	} {
		if c.got != c.want {
			t.Errorf("%s: %d, want %d", c.name, c.got, c.want)
		}
	}
}

// hkaDeadlines is a response writer that takes read deadlines and keeps all of them, in the order they were set.
type hkaDeadlines struct {
	*httptest.ResponseRecorder
	set []time.Time
}

func (d *hkaDeadlines) SetReadDeadline(t time.Time) error { d.set = append(d.set, t); return nil }

// hkaDeadlinesOf serves a call of the webhook "header" with the body and the entrance that the edit makes, and gives the
// deadlines the entrance set.
func hkaDeadlinesOf(t *testing.T, body string, edit ...func(*hookin.Config)) []time.Time {
	t.Helper()
	rig := hkaNew(t, edit...)
	rec := &hkaDeadlines{ResponseRecorder: httptest.NewRecorder()}
	rig.h.ServeHTTP(rec, hkaReq("header", body, map[string]string{"X-Webhook-Token": rig.secrets["header"]}))
	return rec.set
}

// [REQ-862]
// An entrance made with no times gives a call 2 seconds for its first byte and 10 seconds for the whole body, counted from
// the start of the call on the clock it was given.
func TestHkaAnEntranceWithNoTimesSetGivesTwoSecondsForTheFirstByteAndTenForTheBody(t *testing.T) {
	if got := hkaDeadlinesOf(t, ""); len(got) != 1 || !got[0].Equal(hkaStart.Add(2*time.Second)) {
		t.Errorf("a call with no body: deadlines %v", got)
	}
	got := hkaDeadlinesOf(t, `{}`)
	if len(got) != 2 || !got[0].Equal(hkaStart.Add(2*time.Second)) || !got[1].Equal(hkaStart.Add(10*time.Second)) {
		t.Errorf("a call with a body: deadlines %v", got)
	}
}

// [REQ-862]
// The read timeout is held to 30 seconds, and a first byte timeout that is longer than the read timeout is held to it:
// the first byte does not get more time than the whole body.
func TestHkaTheTimesOfTheEntranceAreHeldToTheHighestAndTheFirstByteToTheBody(t *testing.T) {
	got := hkaDeadlinesOf(t, `{}`, func(c *hookin.Config) { c.ReadTimeout = 60 * time.Second })
	if len(got) != 2 || !got[1].Equal(hkaStart.Add(30*time.Second)) {
		t.Errorf("a read timeout of 60 seconds: deadlines %v", got)
	}
	got = hkaDeadlinesOf(t, "", func(c *hookin.Config) { c.FirstByteTimeout, c.ReadTimeout = 20*time.Second, 5*time.Second })
	if len(got) != 1 || !got[0].Equal(hkaStart.Add(5*time.Second)) {
		t.Errorf("a first byte timeout of 20 seconds and a read timeout of 5: deadlines %v", got)
	}
}

// hkaErrorLines are the lines of the log at the level Error, as maps.
func hkaErrorLines(t *testing.T, logs string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		if m["level"] == "ERROR" {
			out = append(out, m)
		}
	}
	return out
}

// [REQ-862]
// While the verifier is broken every call of a webhook is a fault, and the log has one line of it for the webhook, then
// one a minute with how many calls there were: the callers are able to fill the log with nothing else.
func TestHkaAVerifierThatIsBrokenWritesOneLineForAWebhookAndThenOneAMinute(t *testing.T) {
	rig := hkaNew(t)
	h, err := hookin.New(hookin.Config{Directory: rig.spy, Sink: rig.spy, Verifier: hkaBrokenVerifier{rig.spy}, Clock: rig.clock, Logger: slog.New(slog.NewJSONHandler(rig.logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	call := func(webhook string) int {
		rec := hkaRecord()
		h.ServeHTTP(rec, hkaReq(webhook, `{}`, map[string]string{"X-Webhook-Token": rig.secrets[webhook]}))
		return rec.Code
	}
	for i := 0; i < 50; i++ {
		if code := call("header"); code != http.StatusUnauthorized && code != http.StatusTooManyRequests {
			t.Fatalf("call %d: %d", i, code)
		}
	}
	lines := hkaErrorLines(t, rig.logs.String())
	if len(lines) != 1 || lines[0]["webhook"] != hkaSource+"/header" || lines[0]["calls"] != float64(1) {
		t.Fatalf("50 calls wrote %v", lines)
	}
	call("slow") // another webhook has its own line
	if n := len(hkaErrorLines(t, rig.logs.String())); n != 2 {
		t.Errorf("another webhook: %d lines", n)
	}
	rig.clock.Set(hkaStart.Add(61 * time.Second))
	call("header")
	lines = hkaErrorLines(t, rig.logs.String())
	if len(lines) != 3 || lines[2]["calls"] != float64(50) {
		t.Errorf("a call after the minute wrote %v", lines)
	}
	if strings.Contains(rig.logs.String(), rig.secrets["header"]) {
		t.Error("the secret is in the log")
	}
}
