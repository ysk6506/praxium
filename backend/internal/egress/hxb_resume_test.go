package egress

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
)

// [REQ-822] No settings made here keep sessions of TLS: a resumed session would be a connection that is not given the whole
// handshake again. The settings that are used for a connection (verified, pinned, and the probe's) have no session cache,
// and a client that is made with pins or with an authority has none either.
func TestHxbNoSettingsOfTLSHoldASessionCache(t *testing.T) {
	cert, caPEM := ctIssue(t, "api.example.test", 1, ctFrom, ctTo)
	pin := hxbPin(t, cert)
	verified, err := tlsConfig(caPEM)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := pinnedConfig([]string{pin})
	if err != nil {
		t.Fatal(err)
	}
	viaConnection, err := connectionTLS("", []string{pin})
	if err != nil {
		t.Fatal(err)
	}
	for name, cfg := range map[string]*tls.Config{
		"verified": verified, "pinned": pinned, "of a connection": viaConnection,
		"of a probe": probeConfig("api.example.test", &presented{}),
	} {
		if cfg.ClientSessionCache != nil {
			t.Errorf("the settings %s hold a session cache", name)
		}
	}
	for name, opt := range map[string]ClientOptions{"pinned": {Pins: []string{pin}}, "verified": {CAPEM: caPEM}, "system": {}} {
		c, _ := ctClient(t, map[string]string{"api.example.test": "127.0.0.1"}, Policy{}, opt)
		tr, ok := c.hc.Transport.(guard).base.(*http.Transport)
		if !ok || tr.TLSClientConfig == nil || tr.TLSClientConfig.ClientSessionCache != nil {
			t.Errorf("the client %s keeps sessions of TLS", name)
		}
	}
}

// [REQ-822] Even when a session could be resumed the pins are checked: a resumed session runs the check of the settings that
// resume it, with the certificate of the first handshake, so a connection made with other pins is refused whatever the session
// cache holds. The test gives the settings a cache by hand, which none of the code does, to show that the check is not what
// stands between a client and a resumed session.
func TestHxbAResumedSessionIsCheckedAgainstThePinsToo(t *testing.T) {
	cert, _ := ctIssue(t, "api.example.test", 1, ctFrom, ctTo)
	other, _ := ctIssue(t, "api.example.test", 2, ctFrom, ctTo)
	port, _ := hxbServe(t, hxbConst(cert), http.NotFoundHandler(), false)
	cache := tls.NewLRUClientSessionCache(4)
	connect := func(pins ...string) (resumed bool, err error) {
		cfg, perr := pinnedConfig(pins)
		if perr != nil {
			t.Fatal(perr)
		}
		cfg.ClientSessionCache = cache
		cfg.ServerName = "api.example.test" // the server picks the certificate by the name it is asked for
		raw, derr := net.Dial("tcp", "127.0.0.1:"+port)
		if derr != nil {
			t.Fatal(derr)
		}
		conn := tls.Client(raw, cfg)
		defer func() { _ = conn.Close() }()
		if err := conn.Handshake(); err != nil {
			return false, err
		}
		_, _ = io.WriteString(conn, "GET / HTTP/1.0\r\n\r\n")
		_, _ = io.Copy(io.Discard, conn) // the tickets of TLS 1.3 come after the handshake
		return conn.ConnectionState().DidResume, nil
	}
	if resumed, err := connect(hxbPin(t, cert)); err != nil || resumed {
		t.Fatalf("the first connection: resumed %v, %v", resumed, err)
	}
	if resumed, err := connect(hxbPin(t, cert)); err != nil || !resumed {
		t.Fatalf("with the cache the second connection is resumed: %v, %v", resumed, err)
	}
	_, err := connect(hxbPin(t, other))
	var mismatch *PinMismatchError
	if !errors.As(err, &mismatch) || mismatch.Presented != hxbPin(t, cert) {
		t.Errorf("another pin, a cache that holds the session: %v", err)
	}
}
