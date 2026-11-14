package identity

import (
	"net/netip"
	"sync"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// LimiterConfig is the rule of the failure limiter. A zero field takes its default.
type LimiterConfig struct {
	// MaxFailures is how many refused tokens in a row, within Window of each other, block a key. Default 10.
	MaxFailures int
	// Window is how long a failure counts toward the block. Default 10 minutes.
	Window time.Duration
	// Lockout is how long a blocked key stays blocked. Default 5 minutes.
	Lockout time.Duration
	// MaxKeys bounds the memory: the oldest keys are dropped beyond it. Default 10000.
	MaxKeys int
}

// Limiter is the FailureLimiter: it counts the refused tokens of each key (where a caller comes from) and blocks
// the key that reaches the limit for a while. A good token clears the count. It lives in memory, per process.
type Limiter struct {
	mu      sync.Mutex
	clock   kernel.Clock
	cfg     LimiterConfig
	entries map[string]*limitEntry
}

type limitEntry struct {
	count int
	last  time.Time
	until time.Time
}

var _ access.FailureLimiter = (*Limiter)(nil)

// NewLimiter makes a limiter.
func NewLimiter(clock kernel.Clock, cfg LimiterConfig) *Limiter {
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 10
	}
	if cfg.Window <= 0 {
		cfg.Window = 10 * time.Minute
	}
	if cfg.Lockout <= 0 {
		cfg.Lockout = 5 * time.Minute
	}
	if cfg.MaxKeys <= 0 {
		cfg.MaxKeys = 10000
	}
	return &Limiter{clock: clock, cfg: cfg, entries: map[string]*limitEntry{}}
}

// Blocked says whether the key is blocked now and for how much longer.
func (l *Limiter) Blocked(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return 0, false
	}
	if wait := e.until.Sub(l.clock.Now()); wait > 0 {
		return wait, true
	}
	return 0, false
}

// Failed counts a refused token. A failure that comes after the window of the last one starts a new count.
func (l *Limiter) Failed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	e, ok := l.entries[key]
	if !ok {
		l.makeRoom(now)
		e = &limitEntry{}
		l.entries[key] = e
	}
	if now.Sub(e.last) > l.cfg.Window {
		e.count = 0
	}
	e.count++
	e.last = now
	if e.count >= l.cfg.MaxFailures {
		e.until, e.count = now.Add(l.cfg.Lockout), 0
	}
}

// Succeeded forgets the failures of the key, unless it is blocked: a block runs out by time alone.
func (l *Limiter) Succeeded(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e, ok := l.entries[key]; ok && !e.until.After(l.clock.Now()) {
		delete(l.entries, key)
	}
}

// makeRoom drops what has run out and, if the table is still full, the key that failed longest ago.
func (l *Limiter) makeRoom(now time.Time) {
	if len(l.entries) < l.cfg.MaxKeys {
		return
	}
	oldest, oldestKey := now, ""
	for key, e := range l.entries {
		if !e.until.After(now) && now.Sub(e.last) > l.cfg.Window {
			delete(l.entries, key)
			continue
		}
		if oldestKey == "" || e.last.Before(oldest) {
			oldest, oldestKey = e.last, key
		}
	}
	if len(l.entries) >= l.cfg.MaxKeys && oldestKey != "" {
		delete(l.entries, oldestKey)
	}
}

// KeyOf is the key a caller's address is counted under: the address itself, and for IPv6 its /64 network, so a
// caller who owns a whole network cannot start over with every new address. Text that is not an address is its own key.
func KeyOf(host string) string {
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	addr = addr.Unmap()
	if addr.Is6() {
		if prefix, err := addr.Prefix(64); err == nil {
			return prefix.String()
		}
	}
	return addr.String()
}
