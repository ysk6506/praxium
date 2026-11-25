package hookin

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func hkaTableHandler() (*Handler, *bytes.Buffer) {
	clock := kernel.NewManualClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC))
	logs := &bytes.Buffer{}
	cfg := Config{Clock: clock, Logger: slog.New(slog.NewJSONHandler(logs, nil))}.withDefaults()
	return &Handler{cfg: cfg, perHook: map[string]int{}, perAddr: map[string]int{}, buckets: map[string]*bucket{}, fails: map[string]*bucket{}, logged: map[string]*failLog{}}, logs
}

// [REQ-862]
// The three tables of marks (the rate of each webhook, the failures of each (webhook, address), and the lines of the log)
// have a size that does not grow past maxTrackedKeys: the table that is full is emptied, and what was in it starts anew.
// A flood of different keys is therefore a cost in memory that has an end, and costs the mark of every key that was there.
func TestHkaTheTablesOfMarksNeverGrowPastTheirBoundAndAreEmptiedWhenTheyAreFull(t *testing.T) {
	limits := ingest.WebhookLimits{RatePerSecond: 1, Burst: 2, BodyBytes: 1024}
	for name, c := range map[string]struct {
		size  func(h *Handler) int
		add   func(h *Handler, key string)
		spent func(h *Handler, logs *bytes.Buffer, key string) bool // takes one of the key; false when it has none left
	}{
		"the rate of a webhook": {
			size:  func(h *Handler) int { return len(h.buckets) },
			add:   func(h *Handler, key string) { h.takeRate(key, limits) },
			spent: func(h *Handler, _ *bytes.Buffer, key string) bool { _, ok := h.takeRate(key, limits); return ok },
		},
		"the failures of an address": {
			size:  func(h *Handler) int { return len(h.fails) },
			add:   func(h *Handler, key string) { h.takeFailure(key, h.now()) },
			spent: func(h *Handler, _ *bytes.Buffer, key string) bool { _, ok := h.takeFailure(key, h.now()); return ok },
		},
		"the lines of the log": {
			size: func(h *Handler) int { return len(h.logged) },
			add:  func(h *Handler, key string) { h.logFailure(key, "192.0.2.1", "token") },
			spent: func(h *Handler, logs *bytes.Buffer, key string) bool {
				before := logs.Len()
				h.logFailure(key, "192.0.2.1", "token")
				return logs.Len() > before // a call is "had" when a line is written: the first of a minute, and the first after the table was emptied
			},
		},
	} {
		h, logs := hkaTableHandler()
		// One key is used up first (the key of the log has written its line, and the next ones within the minute write none).
		c.add(h, "first")
		for i := 0; i < 100; i++ {
			c.spent(h, logs, "first")
		}
		if c.spent(h, logs, "first") {
			t.Fatalf("%s: a key that was used up still has some left", name)
		}
		for i := 0; i < maxTrackedKeys-1; i++ {
			c.add(h, fmt.Sprintf("k%d", i))
		}
		if got := c.size(h); got != maxTrackedKeys {
			t.Fatalf("%s: the table has %d keys, want %d", name, got, maxTrackedKeys)
		}
		c.add(h, "one more")
		if got := c.size(h); got != 1 {
			t.Errorf("%s: after one key more than the bound the table has %d keys, want it emptied and the new one in it", name, got)
		}
		for i := 0; i < 3*maxTrackedKeys; i++ {
			c.add(h, fmt.Sprintf("flood%d", i))
			if got := c.size(h); got > maxTrackedKeys {
				t.Fatalf("%s: the table grew to %d keys", name, got)
			}
		}
		if !c.spent(h, logs, "first") {
			t.Errorf("%s: the key that was used up before the table was emptied is still used up", name)
		}
	}
}

// [REQ-862]
func TestHkaAnAddressIsCountedAsItselfWhenIPv4AndAsItsNetworkWhenIPv6(t *testing.T) {
	for in, want := range map[string]string{
		"192.0.2.10":                       "192.0.2.10",
		"2001:db8:0:1::1":                  "2001:db8:0:1::/64",
		"2001:db8:0:1:ffff:ffff:ffff:ffff": "2001:db8:0:1::/64",
		"2001:db8:0:2::1":                  "2001:db8:0:2::/64",
		"::1":                              "::/64",
		"fe80::1%eth0":                     "fe80::/64",
		"::ffff:192.0.2.10":                "::ffff:192.0.2.10/64",
		"":                                 noAddress,
	} {
		var a netip.Addr
		if in != "" {
			a = netip.MustParseAddr(in)
		}
		if got := addrGroup(a); got != want && in != "::ffff:192.0.2.10" {
			t.Errorf("%q is counted as %q, want %q", in, got, want)
		}
	}
	// A mapped address never reaches the group: the entrance unmaps it first.
	h, _ := hkaTableHandler()
	r := mappedRequest()
	if got := addrGroup(h.addrOf(r)); got != "192.0.2.10" {
		t.Errorf("a mapped address is counted as %q", got)
	}
}

func mappedRequest() *http.Request {
	r := &http.Request{RemoteAddr: "[::ffff:192.0.2.10]:9", Header: http.Header{}}
	return r
}
