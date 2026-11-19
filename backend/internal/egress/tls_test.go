package egress

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// tlServer starts a TLS server on the loopback and returns it with the PEM of its certificate.
func tlServer(t *testing.T, handler http.Handler) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	block := &pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}
	return srv, string(pem.EncodeToMemory(block))
}

// [REQ-661]
func TestTLSConfigAlwaysVerifies(t *testing.T) {
	_, caPEM := tlServer(t, http.NotFoundHandler())
	for _, pemText := range []string{"", caPEM} {
		cfg, err := TLSConfig(pemText)
		if err != nil {
			t.Fatalf("TLSConfig: %v", err)
		}
		if cfg.InsecureSkipVerify || cfg.MinVersion != tls.VersionTLS12 || cfg.VerifyConnection != nil || cfg.VerifyPeerCertificate != nil {
			t.Errorf("config weakens the check: %+v", cfg)
		}
		if (pemText == "") != (cfg.RootCAs == nil) {
			t.Errorf("RootCAs = %v for a PEM given: %v", cfg.RootCAs, pemText != "")
		}
	}
}

// [REQ-661]
func TestTLSConfigTrustsOnlyTheGivenAuthority(t *testing.T) {
	srv, caPEM := tlServer(t, http.NotFoundHandler())
	cfg, err := TLSConfig(caPEM)
	if err != nil {
		t.Fatal(err)
	}
	opts := x509.VerifyOptions{Roots: cfg.RootCAs, DNSName: "example.com"}
	if _, err := srv.Certificate().Verify(opts); err != nil {
		t.Errorf("the given certificate is not trusted: %v", err)
	}
	if _, err := srv.Certificate().Verify(x509.VerifyOptions{DNSName: "example.com"}); err == nil {
		t.Error("the system authorities trust the test certificate")
	}
}

// [REQ-661]
func TestTLSConfigRefusesATextWithoutACertificateAndDoesNotEchoIt(t *testing.T) {
	for _, bad := range []string{"not a pem secret-detail", "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n", "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"} {
		_, err := TLSConfig(bad)
		var e *Error
		if err == nil || !asError(err, &e) || e.Kind != KindTLS {
			t.Errorf("TLSConfig(%q) = %v", bad, err)
			continue
		}
		if strings.Contains(err.Error(), "secret-detail") || strings.Contains(err.Error(), "AAAA") {
			t.Errorf("message %q echoes the input", err.Error())
		}
	}
}

func asError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}
