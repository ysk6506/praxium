package kernel

import (
	"sync"
	"time"
)

// Clock is the port for the current time. Code outside the kernel receives a
// Clock and never reads the wall clock itself (REQ-001).
type Clock interface {
	Now() time.Time
}

// ManualClock is a Clock that moves only when told to, so a run is
// reproducible. It is safe for concurrent use.
type ManualClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewManualClock starts a ManualClock at start, in UTC.
func NewManualClock(start time.Time) *ManualClock {
	return &ManualClock{now: start.UTC()}
}

// Now returns the current manual time.
func (c *ManualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d. A clock cannot run backwards.
func (c *ManualClock) Advance(d time.Duration) (time.Time, error) {
	if d < 0 {
		return time.Time{}, Errorf(CodeInvalidValue, "a clock cannot run backwards: %s", d)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now, nil
}

// Set jumps to t, for restoring a snapshot. It may go back in time.
func (c *ManualClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t.UTC()
}
