package egress

import (
	"context"
	"net"
	"strings"
	"testing"
)

// [REQ-822] A pin that is not in the form is refused with words about the form only, and the text given is not in them.
func TestHxbParsePinRefusesWhatIsNotAPin(t *testing.T) {
	const secret = "sha256/not-base64-at-all-9999"
	if _, err := ParsePin(secret); err == nil || strings.Contains(err.Error(), "9999") {
		t.Errorf("error %v", err)
	}
	if _, err := ParsePin(""); err == nil {
		t.Error("an empty pin was taken")
	}
}

// [REQ-820] A range that is not one is named in the error cut at 64 bytes, so a long text does not fill a log.
func TestHxbAFaultyRangeIsCutInTheError(t *testing.T) {
	_, err := ParseRanges(strings.Repeat("x", 300))
	if err == nil || len(err.Error()) > 400 || !strings.Contains(err.Error(), "…") {
		t.Errorf("error %v", err)
	}
}

// [REQ-824] A probe of a name that is not a host, and of a PostgreSQL server that hangs up before it answers, ends with an
// error of the egress and no summary.
func TestHxbProbeEndsWithAnErrorWhenThereIsNoServerToAsk(t *testing.T) {
	d, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sum, err := d.ProbePostgresCert(context.Background(), "bad host!", 5432); err == nil || sum != nil {
		t.Errorf("a bad host: %v %v", sum, err)
	}
	ln, lerr := net.Listen("tcp", "127.0.0.1:0")
	if lerr != nil {
		t.Fatal(lerr)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			_ = c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	if sum, err := d.ProbePostgresCert(context.Background(), "127.0.0.1", port); err == nil || sum != nil {
		t.Errorf("a server that hangs up: %v %v", sum, err)
	}
}
