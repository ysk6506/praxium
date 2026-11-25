package hookin

import (
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// bucket is a token bucket that has no timer: its tokens are worked out from the time of the last use.
type bucket struct {
	tokens float64
	last   time.Time
}

// take takes one token at the given rate and burst, or says how long to wait for one.
func (b *bucket) take(now time.Time, rate float64, burst int) (time.Duration, bool) {
	if el := now.Sub(b.last).Seconds(); el > 0 {
		b.tokens += el * rate
	}
	if b.tokens > float64(burst) {
		b.tokens = float64(burst)
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return 0, true
	}
	return time.Duration((1 - b.tokens) / rate * float64(time.Second)), false
}

func (h *Handler) now() time.Time { return h.cfg.Clock.Now() }

// takeRate is the rate limit of a webhook (step 7): one bucket for the webhook, from the calls of every address.
func (h *Handler) takeRate(key string, l ingest.WebhookLimits) (time.Duration, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	b := h.buckets[key]
	if b == nil {
		if len(h.buckets) >= maxTrackedKeys {
			h.buckets = map[string]*bucket{}
		}
		b = &bucket{tokens: float64(l.Burst), last: h.now()}
		h.buckets[key] = b
	}
	return b.take(h.now(), float64(l.RatePerSecond), l.Burst)
}

// takeFailure charges one failure to the bucket of (webhook, address). It never locks anything: the bucket fills again
// with time, and a call that is proven does not ask it.
func (h *Handler) takeFailure(key string, now time.Time) (time.Duration, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	b := h.fails[key]
	if b == nil {
		if len(h.fails) >= maxTrackedKeys {
			h.fails = map[string]*bucket{}
		}
		b = &bucket{tokens: float64(h.cfg.FailureBurst), last: now}
		h.fails[key] = b
	}
	return b.take(now, h.cfg.FailureRate, h.cfg.FailureBurst)
}

// failLog groups the failures of one (webhook, address, kind): the first is written at once, then one line a minute with
// the count. Nothing of the call but those three is in it: no secret, no query, no body.
type failLog struct {
	last  time.Time
	count int
}

// groupLog counts a call for the key and says whether a line is to be written now, and with how many calls since the last
// line: the first call of a key writes at once, then one line a minute. The table is emptied when it holds too many keys.
func (h *Handler) groupLog(key string) (calls int, write bool) {
	now := h.now()
	h.mu.Lock()
	defer h.mu.Unlock()
	f := h.logged[key]
	if f == nil {
		if len(h.logged) >= maxTrackedKeys {
			h.logged = map[string]*failLog{}
		}
		f = &failLog{}
		h.logged[key] = f
	}
	f.count++
	write = f.last.IsZero() || now.Sub(f.last) >= logEvery
	calls = f.count
	if write {
		f.last, f.count = now, 0
	}
	return calls, write
}

func (h *Handler) logFailure(webhook, addr, kind string) {
	if n, write := h.groupLog(webhook + "|" + addr + "|" + kind); write {
		h.cfg.Logger.Warn("a call of a webhook was not proven", slog.String("webhook", webhook), slog.String("address", addr), slog.String("kind", kind), slog.Int("calls", n))
	}
}

// logVerifierFault writes the fault of the verifier by the same grouping, one group for each webhook: while the verifier is
// broken every call of the webhook comes here, and a line for each would be the way to fill the log.
func (h *Handler) logVerifierFault(webhook string) {
	if n, write := h.groupLog("fault|" + webhook); write {
		h.cfg.Logger.Error("the proof of a webhook could not be checked", slog.String("webhook", webhook), slog.Int("calls", n))
	}
}

// counters count what the entrance did, by name, for a reader of the numbers.
type counters struct {
	mu sync.Mutex
	m  map[string]int64
}

func (c *counters) add(name string) {
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string]int64{}
	}
	c.m[name]++
	c.mu.Unlock()
}

// Counts returns a copy of the counters: accepted, the failures by kind (fail_*), rate_limited, busy and the others.
func (h *Handler) Counts() map[string]int64 {
	h.counts.mu.Lock()
	defer h.counts.mu.Unlock()
	out := make(map[string]int64, len(h.counts.m))
	for k, v := range h.counts.m {
		out[k] = v
	}
	return out
}

// Kinds lists the names the counters can have for a failure, sorted: the kinds a log line carries.
func Kinds() []string {
	k := []string{"address", "read", "token", "signature", "signature_form", "timestamp", "unknown", "verifier"}
	sort.Strings(k)
	return k
}
