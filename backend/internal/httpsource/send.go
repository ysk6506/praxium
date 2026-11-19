package httpsource

import (
	"context"
	"errors"
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
	for attempt := 0; ; attempt++ {
		if err := rn.pace.wait(ctx); err != nil {
			return nil, err
		}
		rep, err := rn.once(ctx, p, attempt)
		if err == nil {
			return rep, nil
		}
		wait, ok := rn.retryWait(rep, err, p, attempt)
		if !ok {
			return nil, err
		}
		rn.emit(ingest.Event{Kind: ingest.EventRetry, Attempt: attempt + 1, Reason: retryReason(err), Status: statusOf(rep)})
		if err := rn.r.cfg.Sleep(ctx, wait); err != nil {
			return nil, err
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

// retryAfter reads the header: a number of seconds, or a date.
func retryAfter(h map[string][]string, now time.Time) (time.Duration, bool) {
	v := strings.TrimSpace(headerValue(h, "Retry-After"))
	if v == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n < 0 {
			return 0, false
		}
		return time.Duration(n) * time.Second, true
	}
	for _, layout := range []string{time.RFC1123, time.RFC850, time.ANSIC} {
		if t, err := time.Parse(layout, v); err == nil {
			return max(t.Sub(now), 0), true
		}
	}
	return 0, false
}

// once sends the request one time and reads the answer. The answer comes back with the error when the status was a
// failure, so that the headers (Retry-After) are there to be read; its body is not read then.
func (rn *run) once(ctx context.Context, p plan, attempt int) (*reply, error) {
	clock := rn.r.cfg.Clock
	started := clock.Now()
	rn.emit(ingest.Event{Kind: ingest.EventRequest, Method: p.method, Path: p.url.Path, Attempt: attempt + 1})
	resp, err := rn.doer.Do(ctx, rn.build(p))
	if err != nil {
		return nil, rn.transportError(ctx, err)
	}
	rn.prog.connected = true
	if !p.token && resp.Status != 401 && resp.Status != 403 {
		rn.prog.authenticated = true
	}
	rep := &reply{status: resp.Status, header: resp.Header, url: p.url}
	if resp.Status < 200 || resp.Status > 299 {
		_ = resp.Body.Close()
		rn.after(p, rep, started, 0)
		return rep, rn.statusError(resp.Status)
	}
	body, err := readBody(resp.Body, rn.set.maxBytes)
	if errors.Is(err, errTooLarge) {
		return rep, rn.fail(ingest.SrcResponseFormat, "an answer is larger than max_response_bytes ("+strconv.Itoa(rn.set.maxBytes)+" bytes after it is unpacked); lower the size of a page or raise the limit", nil)
	}
	if err != nil {
		return nil, rn.transportError(ctx, err)
	}
	rep.body = body
	rn.after(p, rep, started, len(body))
	return rep, nil
}

// after tells the observer and the preview that an answer came.
func (rn *run) after(p plan, rep *reply, started time.Time, size int) {
	rn.emit(ingest.Event{Kind: ingest.EventResponse, Method: p.method, Path: p.url.Path, Status: rep.status, Bytes: size,
		ElapsedMillis: rn.r.cfg.Clock.Now().Sub(started).Milliseconds()})
	rn.record(p, rep)
}

// data sends the request for a position and reads the answer as JSON. A token that the source no longer accepts is asked
// for once more.
func (rn *run) data(ctx context.Context, ds ingest.Dataset, at pos) (*parsed, error) {
	p, err := rn.dataPlan(ds, at)
	if err != nil {
		return nil, err
	}
	for refreshed := false; ; refreshed = true {
		if err := rn.signer.ensure(ctx, rn); err != nil {
			return nil, err
		}
		rep, err := rn.exchange(ctx, p)
		if err == nil {
			return rn.parse(rep, ds.RecordsPath)
		}
		if !errors.Is(err, errUnauthorized) || !rn.signer.refreshable() || refreshed {
			return nil, err
		}
		rn.signer.drop()
	}
}
