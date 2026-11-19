package pgconn

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// startRetryHandle is a pool over the test server whose new connections wait in a limiter of one a second. The
// sleep of that limiter is where a test makes a connection fail: sleep gives the error of the call, and when it
// gives none it moves the clock on, and the connection is made for real.
func startRetryHandle(t *testing.T, clock kernel.Clock, sleep func(context.Context, time.Duration) error) *Handle {
	t.Helper()
	c, err := newConnector(NewTestDatabase(t), false, time.Second)
	noErr(t, "the connector of the test server", err)
	lim := newLimiter(clock, 1)
	lim.sleep = sleep
	lim.reserve() // the next connection has to wait for its place
	h := newHandle(limitedConnector{inner: c, lim: lim}, 1, 1, 0)
	t.Cleanup(func() { _ = h.close() })
	return h
}

// startRetryClock is the time the limiter of startRetryHandle starts from.
var startRetryClock = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

// [REQ-555]
func TestAStartThatFailsWithALostConnectionIsTriedOnceMoreOnANewPool(t *testing.T) {
	clock := kernel.NewManualClock(startRetryClock)
	calls := 0
	h := startRetryHandle(t, clock, func(_ context.Context, d time.Duration) error {
		calls++
		if calls == 1 {
			return errors.New("read unix @->/tmp/pgt/.s.PGSQL.5432: read: connection reset by peer")
		}
		_, _ = clock.Advance(d)
		return nil
	})
	g := h.Generation()
	tx, gen, err := begin(context.Background(), h, &sql.TxOptions{})
	noErr(t, "the start after the loss", err)
	if tx == nil || gen != g+1 || calls != 2 {
		t.Fatalf("tx %v, generation %d (was %d), connections waited for %d times", tx != nil, gen, g, calls)
	}
	noErr(t, "rollback", tx.Rollback())
}

// [REQ-555]
func TestAStartThatFailsWithALostConnectionTwiceGivesTheLossAndNoThirdTry(t *testing.T) {
	clock := kernel.NewManualClock(startRetryClock)
	lost := errors.New("read: connection reset by peer")
	calls := 0
	h := startRetryHandle(t, clock, func(context.Context, time.Duration) error {
		calls++
		return lost
	})
	g := h.Generation()
	tx, gen, err := begin(context.Background(), h, &sql.TxOptions{})
	if tx != nil || !errors.Is(err, lost) || gen != g {
		t.Fatalf("the second failure: tx %v, error %v, generation %d (was %d)", tx != nil, err, gen, g)
	}
	if Classify(err) != KindLost {
		t.Errorf("the second failure is of the kind %v, want lost", Classify(err))
	}
	if calls != 2 {
		t.Errorf("connections waited for %d times, want 2: one start and one retry", calls)
	}
	if h.Generation() != g+1 {
		t.Errorf("generation %d, want %d: the pool is renewed once, before the retry", h.Generation(), g+1)
	}
}

// [REQ-555]
func TestAStartThatTheCallerEndedIsNotTriedAgain(t *testing.T) {
	clock := kernel.NewManualClock(startRetryClock)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lost := errors.New("read: connection reset by peer")
	calls := 0
	h := startRetryHandle(t, clock, func(context.Context, time.Duration) error {
		calls++
		cancel()
		return lost
	})
	g := h.Generation()
	_, gen, err := begin(ctx, h, &sql.TxOptions{})
	if !errors.Is(err, lost) || gen != g {
		t.Fatalf("the error %v, generation %d (was %d)", err, gen, g)
	}
	if calls != 1 || h.Generation() != g {
		t.Errorf("connections waited for %d times and generation is %d (was %d): a start that the caller ended is not tried again",
			calls, h.Generation(), g)
	}
}

// [REQ-555]
func TestAStartThatFailsWithAnAnswerOfTheServerIsNotTriedAgain(t *testing.T) {
	clock := kernel.NewManualClock(startRetryClock)
	answer := &pq.Error{Code: "42601", Message: "syntax error"}
	calls := 0
	h := startRetryHandle(t, clock, func(context.Context, time.Duration) error {
		calls++
		return answer
	})
	g := h.Generation()
	_, gen, err := begin(context.Background(), h, &sql.TxOptions{})
	if !errors.Is(err, answer) || gen != g {
		t.Fatalf("the error %v, generation %d (was %d)", err, gen, g)
	}
	if calls != 1 || h.Generation() != g {
		t.Errorf("connections waited for %d times and generation is %d (was %d): an answer of the server is not renewed or tried again",
			calls, h.Generation(), g)
	}
}

// [REQ-555]
func TestAStartAfterAnotherCallersRenewalIsTriedOnceOnTheNewPool(t *testing.T) {
	clock := kernel.NewManualClock(startRetryClock)
	calls := 0
	var h *Handle
	h = startRetryHandle(t, clock, func(_ context.Context, d time.Duration) error {
		calls++
		if calls == 1 {
			h.renew(h.Generation()) // another caller renews the pool while this start waits
			return &pq.Error{Code: "42601"}
		}
		_, _ = clock.Advance(d)
		return nil
	})
	g := h.Generation()
	tx, gen, err := begin(context.Background(), h, &sql.TxOptions{})
	noErr(t, "the start on the new pool", err)
	if gen != g+1 || calls != 2 {
		t.Fatalf("generation %d (was %d), connections waited for %d times", gen, g, calls)
	}
	noErr(t, "rollback", tx.Rollback())
}

// [REQ-555]
func TestTheRenewalByAnotherCallerAndTheLossShareOneRetry(t *testing.T) {
	clock := kernel.NewManualClock(startRetryClock)
	lost := errors.New("read: connection reset by peer")
	calls := 0
	var h *Handle
	h = startRetryHandle(t, clock, func(context.Context, time.Duration) error {
		calls++
		if calls == 1 {
			h.renew(h.Generation())
		}
		return lost
	})
	_, _, err := begin(context.Background(), h, &sql.TxOptions{})
	if !errors.Is(err, lost) {
		t.Fatalf("the error of the start: %v", err)
	}
	if calls != 2 {
		t.Errorf("connections waited for %d times, want 2: one start, one retry, no more", calls)
	}
}
