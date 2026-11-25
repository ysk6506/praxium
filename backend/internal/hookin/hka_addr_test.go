package hookin_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/hookin"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

var ctxHka = context.Background()

// forwarded makes a call of the webhook "allowed" (which takes 192.0.2.0/24) from the connection of a proxy, with the lines
// of X-Forwarded-For given.
func (rig *hkaRig) forwarded(lines ...string) *http.Request {
	r := hkaReq("allowed", `{}`, map[string]string{"X-Webhook-Token": rig.secrets["allowed"]})
	r.RemoteAddr = hkaGoodIP // the proxy itself is in the allowed network here: its address must not be the one that counts
	for _, l := range lines {
		r.Header.Add("X-Forwarded-For", l)
	}
	return r
}

// [REQ-862]
// Behind a proxy of the operator the address is the last item of all the lines of X-Forwarded-For, whatever the lines are,
// and a call whose address cannot be read has none: it is not given the address of the connection.
func TestHkaTheForwardedAddressIsTheLastItemOfAllTheLinesAndNothingElseCounts(t *testing.T) {
	rig := hkaNew(t, func(c *hookin.Config) { c.TrustProxy = true })
	for name, c := range map[string]struct {
		lines []string
		want  int
	}{
		"one line, the last item is allowed":             {[]string{"203.0.113.1, 192.0.2.5"}, 202},
		"one line, the last item is not":                 {[]string{"192.0.2.5, 203.0.113.1"}, 401},
		"two lines, the last of the second is allowed":   {[]string{"203.0.113.1", "192.0.2.5"}, 202},
		"two lines, a caller wrote the first and is let": {[]string{"192.0.2.5", "203.0.113.1"}, 401},
		"two lines, the first has more items":            {[]string{"192.0.2.5, 192.0.2.6", "203.0.113.1, 203.0.113.2"}, 401},
		"three lines":                                    {[]string{"192.0.2.5", "192.0.2.6", "203.0.113.1"}, 401},
		"a port is cut off":                              {[]string{"192.0.2.5:4443"}, 202},
		"an address of IPv6 that is not allowed":         {[]string{"[2001:db8::1]"}, 401},
		"an IPv4 mapped into IPv6 is the IPv4":           {[]string{"::ffff:192.0.2.5"}, 202},
		"no line at all":                                 {nil, 401},
		"an empty line":                                  {[]string{""}, 401},
		"a line of spaces":                               {[]string{"   "}, 401},
		"an empty line after an allowed one":             {[]string{"192.0.2.5", ""}, 401},
		"a comma at the end":                             {[]string{"192.0.2.5,"}, 401},
		"a comma at the end of the last line":            {[]string{"192.0.2.5", "192.0.2.6,"}, 401},
		"something that is no address":                   {[]string{"192.0.2.5, not-an-address"}, 401},
		"a name":                                         {[]string{"192.0.2.5, localhost"}, 401},
		"an address with a zone":                         {[]string{"fe80::1%eth0"}, 401},
		"a network":                                      {[]string{"192.0.2.0/24"}, 401},
		"an address that is too long":                    {[]string{strings.Repeat("1", 5000)}, 401},
	} {
		if got := rig.serve(rig.forwarded(c.lines...)); got.status != c.want {
			t.Errorf("%s (%q): %d, want %d", name, c.lines, got.status, c.want)
		}
	}
	// A webhook with no list of addresses takes a call that has none, and the failures of such calls are one group.
	bare := hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": rig.secrets["header"]})
	if got := rig.serve(bare); got.status != 202 {
		t.Errorf("a webhook of no list, a call of no address: %d", got.status)
	}
	wrong := hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": "wrong"})
	rig.serve(wrong)
	if !strings.Contains(rig.logs.String(), `"address":"none"`) {
		t.Errorf("the failure of a call with no address is not logged as one:\n%s", rig.logs.String())
	}
	// Without a proxy of the operator the lines are what a caller wrote and nothing is read from them.
	plain := hkaNew(t)
	if got := plain.serve(plain.forwarded("203.0.113.1")); got.status != 202 {
		t.Errorf("the address of the connection counts when no proxy is trusted: %d", got.status)
	}
}

// [REQ-862]
// The failures of an address are counted together with the others of its network when it is IPv6, in the bucket and in the
// log, so that a caller who has a /64 and tries one address after another is one caller.
func TestHkaTheFailuresOfAnIPv6NetworkAreOneBucketAndOneLine(t *testing.T) {
	rig := hkaNew(t)
	counts := map[int]int{}
	for i := 0; i < 200; i++ {
		r := hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": "wrong"})
		r.RemoteAddr = fmt.Sprintf("[2001:db8:0:1:%x::%x]:1", i+1, i+7)
		counts[rig.serve(r).status]++
	}
	if counts[401] != 50 || counts[429] != 150 {
		t.Errorf("200 failures from one /64: %v, want 50 answers of 401 (the burst) and 150 of 429", counts)
	}
	if n := strings.Count(rig.logs.String(), "was not proven"); n != 1 || !strings.Contains(rig.logs.String(), `"address":"2001:db8:0:1::/64"`) {
		t.Errorf("%d lines, want one for the network:\n%s", n, rig.logs.String())
	}
	r := hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": "wrong"})
	r.RemoteAddr = "[2001:db8:0:2::1]:1"
	if got := rig.serve(r); got.status != 401 {
		t.Errorf("another network: %d", got.status)
	}
	// IPv4 addresses are what they are: one address, one bucket.
	r = hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": "wrong"})
	r.RemoteAddr = "192.0.2.77:1"
	if got := rig.serve(r); got.status != 401 {
		t.Errorf("an IPv4 address next to the first: %d", got.status)
	}
}

// [REQ-862]
// A number that JSON allows and that float64 cannot hold is a number: the body of a call that is proven is not refused for
// it. The body is thrown away, so it is read as text and not as a value.
func TestHkaABodyWithANumberThatIsTooBigForAFloatIsStillJSON(t *testing.T) {
	rig := hkaNew(t)
	for _, body := range []string{`{"v":1e999}`, `[1e400, -1e999, 123456789012345678901234567890]`, `1e999`, `{"v":{"w":[0.000000000000000000001e-9999]}}`} {
		if got := rig.serve(hkaReq("header", body, map[string]string{"X-Webhook-Token": rig.secrets["header"]})); got.status != 202 {
			t.Errorf("%s: %d %s", body, got.status, got.body)
		}
	}
	for _, body := range []string{`{"v":1e}`, `{"v":01}`, `{"v":--1}`, `{"v":1e999`} {
		if got := rig.serve(hkaReq("header", body, map[string]string{"X-Webhook-Token": rig.secrets["header"]})); got.status != 400 {
			t.Errorf("%s: %d, want 400", body, got.status)
		}
	}
}

// [REQ-862]
// The ways of the entrance that a mutation of the code would not show to the older tests: a webhook of a kind it does not
// handle is not taken even with the right proof, a proof that is too long or a message id that is too long is not given
// to the verifier, and a webhook that is not known costs the same check as one that is.
func TestHkaWhatIsRefusedBeforeTheVerifierIsAskedAndWhatIsAskedForAWebhookThatIsNotKnown(t *testing.T) {
	rig := hkaNew(t)
	issued, err := rig.svc.IssueWebhookSecret(ctxHka, hkaSource, "records", "header_token")
	if err != nil {
		t.Fatal(err)
	}
	rig.spy.mu.Lock()
	rig.spy.hooks[hkaSource+"/records"] = hookin.Hook{Kind: ingest.WebhookRecords, Auth: ingest.WebhookAuth{Type: ingest.WebhookAuthHeaderToken}}
	rig.spy.mu.Unlock()
	if got := rig.serve(hkaReq("records", `{}`, map[string]string{"X-Webhook-Token": issued.Value})); got.status != 401 || rig.spy.signals.Load() != 0 {
		t.Errorf("a webhook of the kind records with the right token: %d, %d signals", got.status, rig.spy.signals.Load())
	}
	asked := rig.spy.asked.Load()
	long := strings.Repeat("a", 2049)
	if got := rig.serve(hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": long})); got.status != 401 || rig.spy.asked.Load() != asked {
		t.Errorf("a token of 2049 characters was given to the verifier: %d", got.status)
	}
	if rig.spy.dummies.Load() == 0 {
		t.Error("a token that is too long cost no dummy check")
	}
	now := rig.clock.Now()
	id := strings.Repeat("i", 257)
	if got := rig.serve(rig.signed("signed", id, now, `{}`)); got.status != 401 || rig.spy.asked.Load() != asked {
		t.Errorf("a message id of 257 characters, with a right signature, was given to the verifier: %d", got.status)
	}
	if got := rig.serve(rig.signed("signed", strings.Repeat("i", 256), now, `{}`)); got.status != 202 {
		t.Errorf("a message id of 256 characters: %d", got.status)
	}
	before := rig.spy.dummies.Load()
	if got := rig.serve(hkaReq("nothing", `{"a":1}`, map[string]string{"X-Webhook-Token": "x"})); got.status != 401 {
		t.Errorf("a webhook that is not known: %d", got.status)
	}
	if rig.spy.dummies.Load() != before+1 {
		t.Errorf("a webhook that is not known cost %d dummy checks, want one", rig.spy.dummies.Load()-before)
	}
}
