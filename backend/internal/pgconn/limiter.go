package pgconn

import (
	"context"
	"sync"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// limiter spaces the opening of new connections: at most rate a second, after a small burst (a fifth of a second's
// worth, at least one). It is the generic cell rate algorithm on the clock it is given, so a test moves the time itself.
type limiter struct {
	mu        sync.Mutex
	clock     kernel.Clock
	interval  time.Duration
	tolerance time.Duration
	next      time.Time
	sleep     func(ctx context.Context, d time.Duration) error
}

func newLimiter(clock kernel.Clock, perSecond int) *limiter {
	interval := time.Second / time.Duration(perSecond)
	burst := max(perSecond/5, 1)
	return &limiter{clock: clock, interval: interval, tolerance: interval * time.Duration(burst-1), sleep: sleepFor}
}

// reserve takes the next place and says how long the caller has to wait for it.
func (l *limiter) reserve() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	if l.next.Before(now) {
		l.next = now
	}
	wait := l.next.Sub(now) - l.tolerance
	l.next = l.next.Add(l.interval)
	return max(wait, 0)
}

func (l *limiter) wait(ctx context.Context) error {
	if d := l.reserve(); d > 0 {
		return l.sleep(ctx, d)
	}
	return ctx.Err()
}

func sleepFor(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
