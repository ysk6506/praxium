package pgconn

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// stubConnector counts the connections it is asked for.
type stubConnector struct{ opened int }

func (c *stubConnector) Connect(context.Context) (driver.Conn, error) {
	c.opened++
	return nil, errors.New("stub")
}
func (c *stubConnector) Driver() driver.Driver { return nil }

// [REQ-558]
func TestNewConnectionsAreSpacedByTheLimitOnTheClockOfTheTest(t *testing.T) {
	clock := kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	lim := newLimiter(clock, 10)
	var waits []time.Duration
	lim.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		_, _ = clock.Advance(d)
		return nil
	}
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		noErr(t, "wait", lim.wait(ctx))
	}
	// 10 a second with a burst of two: the first two go at once, then one every 100 ms.
	want := []time.Duration{0, 0, 100 * time.Millisecond, 100 * time.Millisecond, 100 * time.Millisecond}
	if len(waits) != 3 || waits[0] != 100*time.Millisecond-0 {
		t.Fatalf("the waits %v, wanted three of 100ms (%v)", waits, want)
	}
	if _, err := clock.Advance(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	waits = nil
	noErr(t, "after a quiet time", lim.wait(ctx))
	noErr(t, "again", lim.wait(ctx))
	if len(waits) != 0 {
		t.Fatalf("a quiet time did not clear the limit: %v", waits)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := newLimiter(clock, 1).wait(cancelled); err == nil {
		t.Error("a cancelled context waited")
	}
	if err := sleepFor(cancelled, time.Hour); err == nil {
		t.Error("a sleep went on after its context ended")
	}
	noErr(t, "a short sleep", sleepFor(ctx, time.Millisecond))
}

// [REQ-558]
func TestTheConnectorOpensNoFasterThanTheLimitAllows(t *testing.T) {
	clock := kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	lim := newLimiter(clock, 5)
	slept := time.Duration(0)
	lim.sleep = func(_ context.Context, d time.Duration) error { slept += d; _, _ = clock.Advance(d); return nil }
	inner := &stubConnector{}
	c := limitedConnector{inner: inner, lim: lim}
	for i := 0; i < 6; i++ {
		if _, err := c.Connect(context.Background()); err == nil {
			t.Fatal("the stub gave a connection")
		}
	}
	if inner.opened != 6 || slept != time.Second {
		t.Fatalf("%d opened, %s waited; wanted 6 and 1s (five a second, burst of one, five gaps of 200ms)", inner.opened, slept)
	}
	if c.Driver() != nil {
		t.Error("the driver of the stub")
	}
}

// [REQ-558]
func TestAConnectionThatWaitsForItsPlaceGivesUpWhenItsContextEnds(t *testing.T) {
	clock := kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	inner := &stubConnector{}
	c := limitedConnector{inner: inner, lim: newLimiter(clock, 1)}
	_, _ = c.Connect(context.Background())
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Connect(cancelled); err == nil || inner.opened != 1 {
		t.Fatalf("a connection was opened for a context that had ended: %v, %d opened", err, inner.opened)
	}
}
