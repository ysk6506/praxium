package identity

import (
	"fmt"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func idtNewLimiter(cfg LimiterConfig) (*Limiter, *kernel.ManualClock) {
	clock := kernel.NewManualClock(idtEpoch)
	return NewLimiter(clock, cfg), clock
}

func idtMove(t *testing.T, c *kernel.ManualClock, d time.Duration) {
	t.Helper()
	if _, err := c.Advance(d); err != nil {
		t.Fatalf("advance: %v", err)
	}
}

// [REQ-314]
func TestAKeyThatFailsTooOftenInARowIsBlockedForAWhile(t *testing.T) {
	l, clock := idtNewLimiter(LimiterConfig{MaxFailures: 3, Window: time.Minute, Lockout: 5 * time.Minute})
	for i := 0; i < 2; i++ {
		l.Failed("1.2.3.4")
	}
	if _, blocked := l.Blocked("1.2.3.4"); blocked {
		t.Fatal("blocked after two failures of three")
	}
	l.Failed("1.2.3.4")
	wait, blocked := l.Blocked("1.2.3.4")
	if !blocked || wait != 5*time.Minute {
		t.Fatalf("after the third failure: blocked %v for %s", blocked, wait)
	}
	if _, blocked := l.Blocked("5.6.7.8"); blocked {
		t.Fatal("another address is blocked")
	}
	idtMove(t, clock, 5*time.Minute-time.Second)
	if wait, blocked := l.Blocked("1.2.3.4"); !blocked || wait != time.Second {
		t.Fatalf("one second before the end: blocked %v for %s", blocked, wait)
	}
	idtMove(t, clock, time.Second)
	if _, blocked := l.Blocked("1.2.3.4"); blocked {
		t.Fatal("still blocked after the lockout")
	}
	l.Failed("1.2.3.4")
	if _, blocked := l.Blocked("1.2.3.4"); blocked {
		t.Fatal("the count did not start again after the block")
	}
}

// [REQ-314]
func TestOnlyFailuresWithinTheWindowCountAndTheyRunOutByTime(t *testing.T) {
	l, clock := idtNewLimiter(LimiterConfig{MaxFailures: 3, Window: time.Minute, Lockout: time.Minute})
	l.Failed("k")
	l.Failed("k")
	idtMove(t, clock, 2*time.Minute)
	l.Failed("k")
	if _, blocked := l.Blocked("k"); blocked {
		t.Fatal("failures outside the window were counted")
	}
	for i := 0; i < 3; i++ {
		l.Failed("blocked")
	}
	if _, blocked := l.Blocked("blocked"); !blocked {
		t.Fatal("three failures must block")
	}
	idtMove(t, clock, 2*time.Minute)
	if _, blocked := l.Blocked("blocked"); blocked {
		t.Fatal("a block ends by time alone")
	}
}

// [REQ-314]
func TestTheLimiterKeepsItsMemoryBounded(t *testing.T) {
	l, _ := idtNewLimiter(LimiterConfig{MaxFailures: 2, MaxKeys: 50})
	for i := 0; i < 500; i++ {
		l.Failed(fmt.Sprintf("10.0.%d.%d", i/250, i%250))
	}
	if len(l.entries) > 50 {
		t.Fatalf("%d keys held, the bound is 50", len(l.entries))
	}
	def := NewLimiter(kernel.NewManualClock(idtEpoch), LimiterConfig{})
	if def.cfg.MaxFailures != 10 || def.cfg.Window != 10*time.Minute || def.cfg.Lockout != 5*time.Minute || def.cfg.MaxKeys != 10000 {
		t.Fatalf("the defaults are %+v", def.cfg)
	}
}

// [REQ-314]
func TestAnAddressIsCountedWithItsNetworkForIPv6(t *testing.T) {
	tests := map[string]string{
		"192.0.2.7":                        "192.0.2.7",
		"::ffff:192.0.2.7":                 "192.0.2.7",
		"2001:db8:1:2:aaaa:bbbb:cccc:dddd": "2001:db8:1:2::/64",
		"2001:db8:1:2::1":                  "2001:db8:1:2::/64",
		"not an address":                   "not an address",
	}
	for in, want := range tests {
		if got := KeyOf(in); got != want {
			t.Errorf("KeyOf(%q) = %q, want %q", in, got, want)
		}
	}
}
