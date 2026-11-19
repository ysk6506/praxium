package egress

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

// trkListen is a loopback listener whose connections the test holds.
func trkListen(t *testing.T) (string, chan net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			got <- c
		}
	}()
	return ln.Addr().String(), got
}

func trkDialer(t *testing.T, cfg Config) *Dialer {
	t.Helper()
	d, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// [REQ-673] A Read that waits on a silent peer returns at once when every connection is closed.
func TestTrkCloseAllReleasesABlockedRead(t *testing.T) {
	addr, peers := trkListen(t)
	tr := trkDialer(t, Config{}).Tracker(TrackerConfig{Budget: 1 << 20})
	conn, err := tr.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	peer := <-peers
	defer peer.Close()
	done := make(chan error, 1)
	go func() {
		_, err := conn.Read(make([]byte, 8))
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	tr.CloseAll()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the read returned without an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a blocked read was not released by CloseAll")
	}
	tr.CloseAll() // twice is fine
	_, err = tr.Dial("tcp", addr)
	var ee *Error
	if !errors.As(err, &ee) || ee.Kind != KindCanceled || !errors.Is(err, context.Canceled) {
		t.Fatalf("a dial after CloseAll: %v", err)
	}
}

// [REQ-673] Reading past the budget of the window fails the connection with the kind limit and closes it; a new window
// counts from zero.
func TestTrkBudgetOfAWindow(t *testing.T) {
	addr, peers := trkListen(t)
	tr := trkDialer(t, Config{}).Tracker(TrackerConfig{Budget: 100})
	conn, err := tr.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	peer := <-peers
	defer peer.Close()
	buf := make([]byte, 64)
	if _, err := peer.Write(make([]byte, 60)); err != nil {
		t.Fatal(err)
	}
	if n, err := conn.Read(buf); err != nil || n != 60 {
		t.Fatalf("%d %v", n, err)
	}
	tr.Window(50)
	if _, err := peer.Write(make([]byte, 60)); err != nil {
		t.Fatal(err)
	}
	_, err = conn.Read(buf)
	var ee *Error
	if !errors.As(err, &ee) || ee.Kind != KindLimit {
		t.Fatalf("past the budget: %v", err)
	}
	if _, err := conn.Write([]byte("x")); err == nil {
		t.Fatal("the connection was not closed")
	}
	// The text of the error has no address and no byte of the peer.
	if s := ee.Error(); s != "egress limit: the source sent more than the read may take" {
		t.Fatalf("%q", s)
	}
}

// [REQ-673] The plaintext variant judges every dial as a dial without TLS, with no mark on the context; the Dialer
// that is not asked for the variant is as it was.
func TestTrkPlaintextVariantNeedsNoMark(t *testing.T) {
	res := &dtResolver{answers: map[string][][]netip.Addr{"db.example.test": {{netip.MustParseAddr("203.0.113.9")}}}}
	d := trkDialer(t, Config{Resolver: res})
	dialed := 0
	d.connect = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed++
		c, _ := net.Pipe()
		return c, nil
	}
	tr := d.Tracker(TrackerConfig{Plaintext: true})
	_, err := tr.DialContext(context.Background(), "tcp", "db.example.test:5432")
	var ee *Error
	if !errors.As(err, &ee) || ee.Kind != KindPolicy || dialed != 0 {
		t.Fatalf("a plaintext dial to a public address: %v (%d dials)", err, dialed)
	}
	// Dial and DialTimeout are judged the same way (the driver dials a cancel request with these).
	if _, err := tr.Dial("tcp", "db.example.test:5432"); err == nil || dialed != 0 {
		t.Fatalf("%v", err)
	}
	if _, err := tr.DialTimeout("tcp", "db.example.test:5432", time.Second); err == nil || dialed != 0 {
		t.Fatalf("%v", err)
	}
	// The same address with a Tracker that is not plaintext, and with the plain Dialer, goes through.
	c, err := d.Tracker(TrackerConfig{}).Dial("tcp", "db.example.test:5432")
	if err != nil || dialed != 1 {
		t.Fatalf("%v %d", err, dialed)
	}
	c.Close()
	c, err = d.Dial("tcp", "db.example.test:5432")
	if err != nil || dialed != 2 {
		t.Fatalf("%v %d", err, dialed)
	}
	c.Close()
	// The policy that allows a connection without TLS lets the variant through.
	open := trkDialer(t, Config{Resolver: res, Policy: Policy{AllowInsecure: true}})
	open.connect = d.connect
	c, err = open.Tracker(TrackerConfig{Plaintext: true}).Dial("tcp", "db.example.test:5432")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

// [REQ-673] A connection that the caller closes is out of the set, and the Tracker keeps the checks of the Dialer.
func TestTrkKeepsTheChecksOfTheDialer(t *testing.T) {
	d := trkDialer(t, Config{Policy: Policy{DenyLoopback: true}})
	_, err := d.Tracker(TrackerConfig{}).Dial("tcp", "127.0.0.1:5432")
	var ee *Error
	if !errors.As(err, &ee) || ee.Kind != KindPolicy {
		t.Fatalf("%v", err)
	}
	addr, peers := trkListen(t)
	tr := trkDialer(t, Config{}).Tracker(TrackerConfig{})
	conn, err := tr.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	peer := <-peers
	defer peer.Close()
	_ = conn.Close()
	tr.mu.Lock()
	n := len(tr.conns)
	tr.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d connections in the set", n)
	}
}
