package egress

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Tracker is a variant of the Dialer for a caller that holds its connections for a while and needs three things the
// plain Dialer does not give (ADR-0093): it can close every connection it made at once, so that a Read or Write that is
// blocked on a silent peer returns at once; it counts the bytes read and fails a connection that reads past the budget
// of the window the caller opened; and it treats every dial as a dial of a connection without TLS, whatever the context
// says, so that a driver that dials a cancel request with a context of its own is still judged by the plaintext rule.
// It is made with Dialer.Tracker and has the methods the PostgreSQL driver looks for. The Dialer and everything that
// uses it are not changed.
type Tracker struct {
	d         *Dialer
	plaintext bool
	remaining atomic.Int64
	mu        sync.Mutex
	closed    bool
	conns     map[*trackedConn]struct{}
}

// TrackerConfig says how a Tracker behaves.
type TrackerConfig struct {
	// Plaintext marks every dial as one without TLS: the dial is made only when every address of the answer is on the
	// loopback or in the plaintext ranges of the policy. Use it for sslmode=disable and nowhere else.
	Plaintext bool
	// Budget is the number of bytes the connections may read in the first window, until Window is called.
	Budget int64
}

// Tracker makes a Tracker over the dialer.
func (d *Dialer) Tracker(cfg TrackerConfig) *Tracker {
	t := &Tracker{d: d, plaintext: cfg.Plaintext, conns: map[*trackedConn]struct{}{}}
	t.remaining.Store(cfg.Budget)
	return t
}

// Window opens a new window: from now the connections may read at most budget bytes in all, counted from zero. A
// connection that reads past it fails with an *Error of the kind KindLimit and is closed.
func (t *Tracker) Window(budget int64) { t.remaining.Store(budget) }

// CloseAll closes every connection the Tracker made and refuses every later dial. It may be called more than once and
// from any goroutine; a Read or Write blocked on a connection returns an error at once.
func (t *Tracker) CloseAll() {
	t.mu.Lock()
	t.closed = true
	all := make([]*trackedConn, 0, len(t.conns))
	for c := range t.conns {
		all = append(all, c)
	}
	t.conns = map[*trackedConn]struct{}{}
	t.mu.Unlock()
	for _, c := range all {
		_ = c.Conn.Close()
	}
}

// Dial connects to address ("host:port") over network.
func (t *Tracker) Dial(network, address string) (net.Conn, error) {
	return t.DialContext(context.Background(), network, address)
}

// DialTimeout is Dial with a limit on the whole dial.
func (t *Tracker) DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return t.DialContext(ctx, network, address)
}

// DialContext dials through the Dialer (every check of the Dialer holds) and keeps the connection in the set.
func (t *Tracker) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if t.isClosed() {
		return nil, &Error{Kind: KindCanceled, Reason: "the connections of this read were closed", ctxErr: context.Canceled}
	}
	if t.plaintext {
		ctx = WithPlaintext(ctx)
	}
	conn, err := t.d.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	tc := &trackedConn{Conn: conn, t: t}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		_ = conn.Close()
		return nil, &Error{Kind: KindCanceled, Reason: "the connections of this read were closed", ctxErr: context.Canceled}
	}
	t.conns[tc] = struct{}{}
	t.mu.Unlock()
	return tc, nil
}

func (t *Tracker) isClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

type trackedConn struct {
	net.Conn
	t *Tracker
}

// Read reads, and spends the budget of the window with what it read.
func (c *trackedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 && c.t.remaining.Add(-int64(n)) < 0 {
		_ = c.Close()
		return 0, &Error{Kind: KindLimit, Reason: "the source sent more than the read may take"}
	}
	return n, err
}

// Close closes the connection and takes it out of the set.
func (c *trackedConn) Close() error {
	c.t.mu.Lock()
	delete(c.t.conns, c)
	c.t.mu.Unlock()
	return c.Conn.Close()
}
