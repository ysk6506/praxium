package egress

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
)

type etTimeout struct{}

func (etTimeout) Error() string   { return "i/o timeout: secret-detail" }
func (etTimeout) Timeout() bool   { return true }
func (etTimeout) Temporary() bool { return true }

// [REQ-661]
func TestSanitizeNamesTheKindAndDropsTheDetail(t *testing.T) {
	wrap := func(err error) error {
		return &url.Error{Op: "Get", URL: "https://example.test/items?token=secret-detail", Err: err}
	}
	cases := []struct {
		name      string
		err       error
		kind      Kind
		temporary bool
	}{
		{"canceled", wrap(context.Canceled), KindCanceled, false},
		{"deadline", wrap(context.DeadlineExceeded), KindTimeout, true},
		{"unknown authority", wrap(x509.UnknownAuthorityError{}), KindTLS, false},
		{"hostname", wrap(x509.HostnameError{Host: "secret-detail"}), KindTLS, false},
		{"expired", wrap(x509.CertificateInvalidError{Reason: x509.Expired}), KindTLS, false},
		{"other certificate", wrap(x509.CertificateInvalidError{Reason: x509.NotAuthorizedToSign}), KindTLS, false},
		{"record header", wrap(tls.RecordHeaderError{Msg: "secret-detail"}), KindTLS, false},
		{"alert", wrap(tls.AlertError(42)), KindTLS, false},
		{"verification", wrap(&tls.CertificateVerificationError{Err: errors.New("secret-detail")}), KindTLS, false},
		{"dns not found", wrap(&net.DNSError{Err: "no such host", Name: "secret-detail", IsNotFound: true}), KindResolve, false},
		{"dns temporary", wrap(&net.DNSError{Err: "busy", Name: "secret-detail", IsTemporary: true}), KindResolve, true},
		{"dns timeout", wrap(&net.DNSError{Err: "slow", Name: "secret-detail", IsTimeout: true}), KindResolve, true},
		{"net timeout", wrap(&net.OpError{Op: "read", Err: etTimeout{}}), KindTimeout, true},
		{"refused", wrap(&net.OpError{Op: "dial", Err: errors.New("connection refused: secret-detail")}), KindConnect, true},
		{"anything", errors.New("secret-detail"), KindConnect, true},
	}
	for _, c := range cases {
		e := sanitize(c.err)
		if e == nil || e.Kind != c.kind || e.Temporary() != c.temporary {
			t.Errorf("%s: %+v", c.name, e)
			continue
		}
		if text := e.Error(); strings.Contains(text, "secret-detail") || strings.Contains(text, "example.test") || !strings.HasPrefix(text, "egress "+string(c.kind)) {
			t.Errorf("%s: message %q", c.name, text)
		}
	}
}

// [REQ-661]
func TestSanitizeKeepsTheContextErrorForErrorsIs(t *testing.T) {
	if e := Sanitize(context.Canceled); !errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
		t.Errorf("canceled: %v", e)
	}
	if e := sanitize(context.DeadlineExceeded); !errors.Is(e, context.DeadlineExceeded) || !e.Timeout() {
		t.Errorf("deadline: %v", e)
	}
	if e := Sanitize(errors.New("x")); errors.Unwrap(e) != nil {
		t.Errorf("an ordinary error is unwrapped: %v", errors.Unwrap(e))
	}
}

// [REQ-661]
func TestSanitizeReturnsAnOwnErrorUnchangedAndNilForNil(t *testing.T) {
	if Sanitize(nil) != nil {
		t.Error("nil became an error")
	}
	var typedNil *Error
	pass := func(err error) error { return Sanitize(err) }
	if pass(nil) != nil || pass(typedNil) == nil {
		t.Error("nil handling")
	}
	own := refused("example.test", "the port is not one the source may use")
	wrapped := &url.Error{Op: "Get", URL: "https://example.test/?q=1", Err: own}
	if got := sanitize(wrapped); got != own {
		t.Errorf("got %v", got)
	}
	merged := deniedAddress("example.test", "the address is in the private range 10.0.0.0/8")
	if merged.Reason != reasonAddress || strings.Contains(merged.Error(), "10.0.0.0") {
		t.Errorf("merged: %v", merged)
	}
	if !strings.Contains(merged.Detail(), "10.0.0.0/8") || strings.Contains(fmt.Sprintf("%v|%+v|%s", merged, merged, merged), "10.0.0.0") {
		t.Errorf("detail %q", merged.Detail())
	}
	if own.Error() != "egress policy: the port is not one the source may use (host example.test)" {
		t.Errorf("text %q", own.Error())
	}
	if (&Error{Kind: KindRedirect, Reason: "r"}).Error() != "egress redirect: r" {
		t.Error("text without a host")
	}
}

// [REQ-661]
func TestWhichKindsMayPass(t *testing.T) {
	temporary := map[Kind]bool{KindTimeout: true, KindConnect: true}
	for _, k := range []Kind{KindPolicy, KindResolve, KindConnect, KindTLS, KindTimeout, KindCanceled, KindRedirect} {
		if got := (&Error{Kind: k}).Temporary(); got != temporary[k] {
			t.Errorf("%s: Temporary = %v", k, got)
		}
		if got := (&Error{Kind: k}).Timeout(); got != (k == KindTimeout) {
			t.Errorf("%s: Timeout = %v", k, got)
		}
	}
}
