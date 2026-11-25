package hookin

import (
	"io"
	"net/http"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// The room of the entrance (step 3) and the read of the body (step 5). Everything here happens before a caller is proven,
// so what a caller who is not proven can hold is kept small: a place for each call, memory that matches the bytes that are
// really held, and a time that ends.

// reserve is step 3: the place of the address first (a count, and nothing is held), then the entrance's own places for the
// calls at once and for the memory of the body, then the place of the webhook.
func (q *request) reserve() (release func(), ok bool) {
	h := q.h
	if !h.takeAddress(q.group) {
		h.counts.add("busy_address")
		q.w.Header().Set("Retry-After", "1")
		h.problem(q.w, "rate_limited.concurrency", nil, 0)
		return nil, false
	}
	q.copies = 1
	if q.known && q.hook.Auth.Type == ingest.WebhookAuthHMAC && q.hook.Auth.Profile != ingest.ProfileBodyOnly {
		q.copies = 2 // the signed message (id.time.body) is a copy of the body
	}
	if h.inflight.Add(1) > int64(h.cfg.MaxInFlight) {
		h.inflight.Add(-1)
		h.dropAddress(q.group)
		q.busy("busy")
		return nil, false
	}
	if !q.hold(q.initialBytes() * q.copies) {
		h.inflight.Add(-1)
		h.dropAddress(q.group)
		q.busy("busy")
		return nil, false
	}
	h.mu.Lock()
	over := h.perHook[q.key] >= h.cfg.PerWebhookInFlight
	if !over {
		h.perHook[q.key]++
	}
	h.mu.Unlock()
	if over {
		q.unhold()
		h.inflight.Add(-1)
		h.dropAddress(q.group)
		h.counts.add("busy_webhook")
		q.w.Header().Set("Retry-After", "1")
		h.problem(q.w, "rate_limited.concurrency", nil, 0)
		return nil, false
	}
	return func() {
		h.mu.Lock()
		if h.perHook[q.key]--; h.perHook[q.key] <= 0 {
			delete(h.perHook, q.key)
		}
		h.mu.Unlock()
		q.unhold()
		h.inflight.Add(-1)
		h.dropAddress(q.group)
	}, true
}

// busy answers that the entrance has no room now: 503 with Retry-After.
func (q *request) busy(counter string) {
	q.h.counts.add(counter)
	q.w.Header().Set("Retry-After", "1")
	q.h.problem(q.w, "server.unavailable", nil, 0)
}

// capacityBytes is the most the buffer of the body can need: the length it declares and one byte to see the end, or, when
// it declares none, the limit and one byte (a body that fills that is over the limit). A length over the limit is refused
// without a read and needs nothing.
func (q *request) capacityBytes() int64 {
	switch n := q.r.ContentLength; {
	case n > q.limit:
		return 0
	case n >= 0:
		return n + 1
	default:
		return q.limit + 1
	}
}

// initialBytes is what the buffer starts with, which is what the call is charged for at first: the most it can need, and
// at most a small part of that. What a caller says about the length of the body is not what it holds: the buffer grows as
// the bytes come, and each growth is charged then, so a caller that declares four megabytes and sends none holds sixty-four
// kilobytes.
func (q *request) initialBytes() int64 { return min(unknownLengthBytes, q.capacityBytes()) }

// hold charges n bytes to the memory of the bodies, or says there is no room for them (and charges nothing).
func (q *request) hold(n int64) bool {
	h := q.h
	if h.memory.Add(n) > h.cfg.MaxBodyMemory {
		h.memory.Add(-n)
		return false
	}
	q.held += n
	return true
}

// unhold gives back everything the call holds.
func (q *request) unhold() {
	q.h.memory.Add(-q.held)
	q.held = 0
}

// setDeadline sets the deadline of the read of the call to the start of the call and d. A server that cannot set one
// (its response writer is wrapped by something that does not pass it on) is a server on which no body is read: a read with
// no deadline is the way a slow sender holds a place for good, so the call is refused, and the entrance says so once.
func (q *request) setDeadline(d time.Duration) bool {
	err := http.NewResponseController(q.w).SetReadDeadline(q.start.Add(d))
	if err == nil {
		return true
	}
	if q.h.noDeadlineLogged.CompareAndSwap(false, true) {
		q.h.cfg.Logger.Error("the server cannot set a read deadline on a call of a webhook, so no call of a webhook is taken", "error", err.Error())
	}
	q.busy("no_deadline")
	return false
}

// readBody is step 5: the body, to the limit and no further. A declared length over the limit is refused without a read.
// The buffer is what the call is charged for: it starts small and grows by doubling as the body comes, and each growth is
// charged before the buffer is made. The first byte has the deadline that was
// set when the call came in; when it is there the deadline of the whole body replaces it.
func (q *request) readBody() bool {
	if q.r.ContentLength > q.limit {
		q.tooLarge()
		return false
	}
	buf := make([]byte, 0, q.initialBytes())
	first := true
	for {
		if len(buf) == cap(buf) {
			var ok bool
			if buf, ok = q.grow(buf); !ok {
				return false
			}
		}
		n, err := q.r.Body.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if n > 0 && first {
			first = false
			if !q.setDeadline(q.h.cfg.ReadTimeout) {
				return false
			}
		}
		if int64(len(buf)) > q.limit {
			q.tooLarge()
			return false
		}
		if err == io.EOF {
			q.body = buf
			return true
		}
		if err != nil {
			q.fail("read")
			return false
		}
	}
}

// grow makes the buffer twice as large, up to what the body can need, and charges the growth before it is made.
func (q *request) grow(buf []byte) ([]byte, bool) {
	next := min(int64(cap(buf))*2, q.capacityBytes())
	if next <= int64(cap(buf)) {
		q.tooLarge()
		return nil, false
	}
	if !q.hold((next - int64(cap(buf))) * q.copies) {
		q.busy("busy")
		return nil, false
	}
	out := make([]byte, len(buf), next)
	copy(out, buf)
	return out, true
}

func (q *request) tooLarge() {
	q.h.counts.add("too_large")
	q.w.Header().Set("Connection", "close")
	q.h.problem(q.w, "invalid_input.body_too_large", map[string]any{"limit_bytes": q.limit}, 0)
}
