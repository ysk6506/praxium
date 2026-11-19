package ingest

import (
	"errors"
	"fmt"
)

// The kinds of trouble a source has, a closed list (ADR-0090). Each belongs to one of the three classes a check and a
// run tell the person: the setting is wrong and no retry helps (config), the trouble is passing and a retry may help
// (transient), or the product is at fault (product).
const (
	// Config: the person fixes the definition, the secret or the source's own setting.
	SrcPolicyDenied    = "policy_denied"    // the address, scheme or TLS setting is one the server does not allow
	SrcNameNotFound    = "name_not_found"   // the host name has no address
	SrcTLSRejected     = "tls_rejected"     // the certificate does not match or is not trusted
	SrcAuthFailed      = "auth_failed"      // the source refused the credentials or the account may not read
	SrcRequestRejected = "request_rejected" // the source says the request is wrong (not found, bad syntax, no such table)
	SrcResponseFormat  = "response_format"  // the answer is not the shape the definition expects
	SrcSecretUnusable  = "secret_unusable"  // the secret is of the wrong kind, or is not allowed for this source or address
	// Transient: the same read may work later.
	SrcTimeout        = "timeout"
	SrcConnectionLost = "connection_lost"
	SrcRateLimited    = "rate_limited"
	SrcSourceBusy     = "source_busy" // the source answered that it fails or is full for now (5xx, too many connections)
	// Product: the reader met a state that cannot be.
	SrcInternal = "internal"
)

var sourceKindClass = map[string]CheckKind{
	SrcPolicyDenied: CheckConfig, SrcNameNotFound: CheckConfig, SrcTLSRejected: CheckConfig, SrcAuthFailed: CheckConfig,
	SrcRequestRejected: CheckConfig, SrcResponseFormat: CheckConfig, SrcSecretUnusable: CheckConfig,
	SrcTimeout: CheckTransient, SrcConnectionLost: CheckTransient, SrcRateLimited: CheckTransient, SrcSourceBusy: CheckTransient,
	SrcInternal: CheckProduct,
}

// SourceKinds tells the class of each kind of the closed list.
func SourceKinds() map[string]CheckKind {
	out := make(map[string]CheckKind, len(sourceKindClass))
	for k, c := range sourceKindClass {
		out[k] = c
	}
	return out
}

// SourceError is the error an adapter gives when its source cannot be read: a class (the three ways), a kind from the
// closed list, and the words for the person. It exists because the errors of the standard library and of the drivers
// say what a person must not see (the address with its query, the host, a fragment of a statement) and say nothing of
// whose it is to fix.
//
// Message is what a run record and a log may hold: it never has the query of an address, a header, a body, the text of
// a statement, a key value or an error sentence of a driver. Detail is words only the person who asked sees, in that
// one answer (the sentence of the server about a statement it refused), and it is kept out of the run record and the
// log. Err is for errors.Is and errors.As and is never written out: Error, the %v and %+v verbs and the %#v verb all
// give the Message only.
type SourceError struct {
	Class   CheckKind
	Kind    string
	Message string
	Detail  string
	Err     error
}

// NewSourceError makes the error of a kind of the closed list, with the class that kind has. A kind that is not in the
// list is the product's fault.
func NewSourceError(kind, message string, err error) *SourceError {
	class, ok := sourceKindClass[kind]
	if !ok {
		class, kind = CheckProduct, SrcInternal
	}
	return &SourceError{Class: class, Kind: kind, Message: message, Err: err}
}

// WithDetail returns the error with the words only the asker sees.
func (e *SourceError) WithDetail(detail string) *SourceError {
	c := *e
	c.Detail = detail
	return &c
}

// Error is the message.
func (e *SourceError) Error() string { return e.Message }

// Unwrap gives the error underneath, for errors.Is and errors.As.
func (e *SourceError) Unwrap() error { return e.Err }

// Temporary says whether a retry may help, as the errors of the standard library do.
func (e *SourceError) Temporary() bool { return e.Class == CheckTransient }

// GoString is what %#v writes: the class, the kind and the message, never the error underneath or the detail.
func (e *SourceError) GoString() string {
	return fmt.Sprintf("ingest.SourceError{Class:%q, Kind:%q, Message:%q}", e.Class, e.Kind, e.Message)
}

// SourceErrorOf finds the SourceError in an error.
func SourceErrorOf(err error) (*SourceError, bool) {
	var se *SourceError
	if errors.As(err, &se) {
		return se, true
	}
	return nil, false
}
