package httpsource

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// maxRetries is how many times a GET is sent again when the source fails in a passing way (429, 5xx, a connection that
// breaks or times out). A POST, and the request for a token, are sent once.
const maxRetries = 3

// pacer spaces the requests of a read: at most one in the interval, on the clock it is given, so that a test moves the
// time itself. It is the generic cell rate algorithm with no burst.
type pacer struct {
	clock    kernel.Clock
	interval time.Duration
	next     time.Time
	sleep    Sleeper
}

func (p *pacer) wait(ctx context.Context) error {
	now := p.clock.Now()
	if p.next.Before(now) {
		p.next = now
	}
	d := p.next.Sub(now)
	p.next = p.next.Add(p.interval)
	if d > 0 {
		return p.sleep(ctx, d)
	}
	return ctx.Err()
}

// exchange sends a request and gives the answer of a success, sending it again when it may and the source asks to wait.
func (rn *run) exchange(ctx context.Context, p plan) (*reply, error) {
	rep, _, err := rn.exchangeFrom(ctx, p, 0)
	return rep, err
}

// exchangeFrom is exchange that begins at the attempt first (a read that broke goes on with the attempts it has left) and
// says which attempt gave the answer. The body of the answer is open.
func (rn *run) exchangeFrom(ctx context.Context, p plan, first int) (*reply, int, error) {
	for attempt := first; ; attempt++ {
		if err := rn.pace.wait(ctx); err != nil {
			return nil, attempt, err
		}
		rep, err := rn.once(ctx, p, attempt)
		if err == nil {
			return rep, attempt, nil
		}
		wait, ok := rn.retryWait(rep, err, p, attempt)
		if !ok {
			return nil, attempt, err
		}
		rn.emit(ingest.Event{Kind: ingest.EventRetry, Attempt: attempt + 1, Reason: retryReason(err), Status: statusOf(rep)})
		if err := rn.r.cfg.Sleep(ctx, wait); err != nil {
			return nil, attempt, err
		}
	}
}

func statusOf(rep *reply) int {
	if rep == nil {
		return 0
	}
	return rep.status
}

func retryReason(err error) string {
	se, ok := ingest.SourceErrorOf(err)
	if !ok {
		return "failed"
	}
	return strings.ReplaceAll(se.Kind, "_", " ")
}

// retryWait says whether the request is sent again after this failure, and after how long. The source's Retry-After is
// kept to; one that asks for more than max_retry_wait_seconds ends the read with the failure, and so does one that has used
// the retries.
func (rn *run) retryWait(rep *reply, err error, p plan, attempt int) (time.Duration, bool) {
	if !p.retryable() || attempt >= maxRetries || !transientError(err) || errors.Is(err, context.Canceled) {
		return 0, false
	}
	if rep != nil {
		if d, ok := retryAfter(rep.header, rn.r.cfg.Clock.Now()); ok {
			return d, d <= rn.set.maxWait
		}
	}
	d := time.Duration(float64(time.Second<<attempt) * (1 + 0.25*rn.r.cfg.Rng.Float64()))
	return min(d, rn.set.maxWait), true
}

// longestWait stands for a Retry-After that is more than a duration can hold: longer than any limit.
const longestWait = time.Duration(math.MaxInt64)

// retryAfter reads the header: a number of seconds, or a date. A number that is more than a duration holds is a wait longer
// than any limit, so that it is compared with the limit and never multiplied first (ADR-0097).
func retryAfter(h map[string][]string, now time.Time) (time.Duration, bool) {
	v := strings.TrimSpace(headerValue(h, "Retry-After"))
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	switch {
	case err == nil && n < 0:
		return 0, false
	case err == nil && n > int(math.MaxInt64/int64(time.Second)):
		return longestWait, true
	case err == nil:
		return time.Duration(n) * time.Second, true
	case isHugeNumber(v, err):
		return longestWait, true
	}
	for _, layout := range []string{time.RFC1123, time.RFC850, time.ANSIC} {
		if t, err := time.Parse(layout, v); err == nil {
			return max(t.Sub(now), 0), true
		}
	}
	return 0, false
}

// isHugeNumber tells whether a text is digits that are more than an integer holds.
func isHugeNumber(v string, err error) bool {
	var ne *strconv.NumError
	return errors.As(err, &ne) && errors.Is(ne.Err, strconv.ErrRange) && !strings.HasPrefix(v, "-")
}

// once sends the request one time. The answer comes back with the error when the status was a failure, so that the headers
// (Retry-After) are there to be read; its body is not read then. The body of a success is open, and the one who asked reads
// it, so that an answer is never held whole.
func (rn *run) once(ctx context.Context, p plan, attempt int) (*reply, error) {
	clock := rn.r.cfg.Clock
	started := clock.Now()
	if err := rn.chargeChild(p); err != nil {
		return nil, err
	}
	if err := rn.charge(); err != nil {
		return nil, err
	}
	req, err := rn.build(p)
	if err != nil {
		return nil, err
	}
	rn.where = rn.destination(p)
	rn.emit(ingest.Event{Kind: ingest.EventRequest, Method: p.method, Path: p.logPath(), Attempt: attempt + 1})
	resp, err := rn.doerFor(p).Do(ctx, req)
	if err != nil {
		return nil, rn.ofTokenRequest(p, rn.transportError(ctx, err))
	}
	if !(p.token && rn.crossToken) {
		rn.prog.connected = true
	}
	if !p.token && resp.Status != 401 && resp.Status != 403 {
		rn.prog.authenticated = true
	}
	rep := &reply{status: resp.Status, header: resp.Header, url: p.url, started: started, ended: resp.Ended}
	if resp.Status < 200 || resp.Status > 299 {
		_ = resp.Body.Close()
		rn.after(p, rep, 0)
		return rep, rn.ofTokenRequest(p, rn.statusError(resp.Status, p.token))
	}
	rep.rc = resp.Body
	rn.open = rep
	return rep, nil
}

// after tells the observer and the preview that an answer came, when its body has been read.
func (rn *run) after(p plan, rep *reply, size int) {
	rn.emit(ingest.Event{Kind: ingest.EventResponse, Method: p.method, Path: p.logPath(), Status: rep.status, Bytes: size,
		ElapsedMillis: rn.r.cfg.Clock.Now().Sub(rep.started).Milliseconds()})
	rn.record(p, rep)
}
