package egress

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
)

// Kind is the class of a failure of an outgoing connection.
type Kind string

// The kinds of failure. The adapters map them to the three ways a check can end: Policy, TLS and Redirect are
// the definition's to fix, Resolve is too unless it says it is temporary, Connect and Timeout may pass.
const (
	// KindPolicy is a request the policy refuses: an address, a host, a port or a scheme.
	KindPolicy Kind = "policy"
	// KindResolve is a name that did not resolve.
	KindResolve Kind = "resolve"
	// KindConnect is a connection that could not be made or that broke.
	KindConnect Kind = "connect"
	// KindTLS is a certificate that is not trusted or does not match, or a handshake that failed.
	KindTLS Kind = "tls"
	// KindTimeout is a time limit that ran out.
	KindTimeout Kind = "timeout"
	// KindCanceled is a request that the caller cancelled.
	KindCanceled Kind = "canceled"
	// KindRedirect is a redirect that the client does not follow.
	KindRedirect Kind = "redirect"
)

// Error is every failure this package returns. Its text is built from a kind and a fixed reason (and the host
// name the caller gave), never from the standard library's sentence, so it holds no address that was refused,
// no query and no header.
type Error struct {
	Kind   Kind
	Reason string
	// Host is the normalised host name of the request, when it is known and well formed.
	Host      string
	temporary bool
	ctxErr    error
}

// Error implements error.
func (e *Error) Error() string {
	text := "egress " + string(e.Kind) + ": " + e.Reason
	if e.Host != "" {
		text += " (host " + e.Host + ")"
	}
	return text
}

// Timeout reports whether a time limit ran out. The check classifier of the ingestion core asks this.
func (e *Error) Timeout() bool { return e.Kind == KindTimeout }

// Temporary reports whether trying again may help.
func (e *Error) Temporary() bool {
	switch e.Kind {
	case KindTimeout, KindConnect:
		return true
	case KindResolve:
		return e.temporary
	default:
		return false
	}
}

// Unwrap exposes only the context error of a cancellation or a deadline, so that errors.Is can tell the
// caller's own cancellation from a failure of the source.
func (e *Error) Unwrap() error { return e.ctxErr }

func refused(host, reason string) *Error {
	return &Error{Kind: KindPolicy, Reason: reason, Host: host}
}

// Sanitize turns any error of a request or a dial into an *Error. It removes what the standard library puts in
// its errors: the URL (with its query) of a *url.Error, the address of a *net.OpError, the text of a certificate.
// A nil error stays nil. An *Error comes back unchanged.
func Sanitize(err error) *Error {
	if err == nil {
		return nil
	}
	var own *Error
	if errors.As(err, &own) {
		return own
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Kind: KindCanceled, Reason: "the request was cancelled", ctxErr: context.Canceled}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Kind: KindTimeout, Reason: "the time limit ran out", ctxErr: context.DeadlineExceeded}
	}
	if reason, ok := tlsReason(err); ok {
		return &Error{Kind: KindTLS, Reason: reason}
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return &Error{Kind: KindResolve, Reason: "the name did not resolve", temporary: dns.IsTemporary || dns.IsTimeout}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &Error{Kind: KindTimeout, Reason: "the time limit ran out"}
	}
	return &Error{Kind: KindConnect, Reason: "the connection failed or broke"}
}

func tlsReason(err error) (string, bool) {
	var unknown x509.UnknownAuthorityError
	var host x509.HostnameError
	var invalid x509.CertificateInvalidError
	var record tls.RecordHeaderError
	var alert tls.AlertError
	switch {
	case errors.As(err, &unknown):
		return "the certificate is not signed by a trusted authority", true
	case errors.As(err, &host):
		return "the certificate does not match the host name", true
	case errors.As(err, &invalid):
		if invalid.Reason == x509.Expired {
			return "the certificate has expired or is not valid yet", true
		}
		return "the certificate is not valid", true
	case errors.As(err, &record), errors.As(err, &alert):
		return "the TLS handshake failed", true
	}
	var verify *tls.CertificateVerificationError
	if errors.As(err, &verify) {
		return "the certificate could not be verified", true
	}
	return "", false
}
