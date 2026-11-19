package pgsource

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"strings"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// What a failure is told as. The words are fixed: they hold no host, no user, no database, no statement and no sentence
// of the driver or of the server, so a run record and a log can keep them (ADR-0093). The one thing that may carry the
// server's own sentence is Detail of a refused statement (the SQLSTATE class 42, 22 and 0A), which only the person who asked sees.
func sourceErr(kind, message string, cause error) *ingest.SourceError {
	return ingest.NewSourceError(kind, message, cause)
}

// classify turns an error of the connection, of the driver or of the egress dialer into the error this adapter gives. An
// error that comes because the caller's own context ended is given back as it is: it is not a failure of the source.
func classify(parent context.Context, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := ingest.SourceErrorOf(err); ok {
		return err
	}
	if parent.Err() != nil {
		return parent.Err()
	}
	if out := classifyTyped(err); out != nil {
		return out
	}
	return classifyTransport(err)
}

// classifyTyped maps the errors that carry their own kind: the egress dialer's and the server's (SQLSTATE).
func classifyTyped(err error) error {
	var ee *egress.Error
	if errors.As(err, &ee) {
		return fromEgress(ee, err)
	}
	var pe *pq.Error
	if errors.As(err, &pe) {
		return fromServer(pe, err)
	}
	return nil
}

// classifyTransport maps the errors of the connection and of the driver that have no kind of their own.
func classifyTransport(err error) error {
	if errors.Is(err, pq.ErrSSLNotSupported) {
		return sourceErr(ingest.SrcTLSRejected, "the server does not offer TLS, and the source asks for it (sslmode verify-full)", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return sourceErr(ingest.SrcTimeout, "the source did not answer within the time the read may take", err)
	}
	// The handshake of TLS is made inside the driver, so its failures (a certificate that is not trusted, a name that does
	// not match) are not egress errors yet: egress reads them.
	if ee, ok := egress.Sanitize(err).(*egress.Error); ok && ee.Kind == egress.KindTLS {
		return fromEgress(ee, err)
	}
	if isConnectionLost(err) {
		return sourceErr(ingest.SrcConnectionLost, "the connection to the source broke", err)
	}
	return sourceErr(ingest.SrcInternal, "the read failed in a way the adapter does not know", err)
}

func isConnectionLost(err error) bool {
	return errors.Is(err, driver.ErrBadConn) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || isNetwork(err)
}

func isNetwork(err error) bool {
	var n interface{ Timeout() bool }
	return errors.As(err, &n)
}

// fromEgress maps the kind of an egress failure. Detail is left out on purpose: the finer reason of a refused address
// is for the operator's log, and a person who may only see the message must not learn which range was refused.
func fromEgress(ee *egress.Error, cause error) error {
	switch ee.Kind {
	case egress.KindPolicy:
		return sourceErr(ingest.SrcPolicyDenied, "the server's policy does not allow this connection (the address, the port or a connection without TLS)", cause)
	case egress.KindResolve:
		if ee.Temporary() {
			return sourceErr(ingest.SrcConnectionLost, "the host name could not be looked up for now", cause)
		}
		return sourceErr(ingest.SrcNameNotFound, "the host name of the source has no address", cause)
	case egress.KindTLS:
		return sourceErr(ingest.SrcTLSRejected, "the certificate of the server is not trusted, has expired, or does not match the host name", cause)
	case egress.KindTimeout:
		return sourceErr(ingest.SrcTimeout, "the source did not answer within the time limit of the connection", cause)
	case egress.KindCanceled:
		return context.Canceled
	case egress.KindLimit:
		return sourceErr(ingest.SrcResponseFormat, "the source sent more data than one page of the read may take (a row that is too large, or an answer that never ends)", cause)
	}
	return sourceErr(ingest.SrcConnectionLost, "the source could not be reached, or the connection broke", cause)
}

// fromServer maps a SQLSTATE. The class decides, with a few codes that say more.
func fromServer(pe *pq.Error, cause error) error {
	code := string(pe.Code)
	switch {
	case strings.HasPrefix(code, "28"):
		return sourceErr(ingest.SrcAuthFailed, "the source refused the user name or the password", cause)
	case code == "42501":
		return sourceErr(ingest.SrcAuthFailed, "the account may not read what the dataset reads", cause)
	case code == "3D000":
		return sourceErr(ingest.SrcRequestRejected, "the source has no database of that name", cause)
	case code == "25006":
		return sourceErr(ingest.SrcRequestRejected, "the statement tries to write, and the read is read-only", cause)
	case code == "57014":
		return sourceErr(ingest.SrcTimeout, "the statement ran longer than the time limit of the source", cause)
	case code == "55P03":
		return sourceErr(ingest.SrcSourceBusy, "a lock on the data was held for longer than the lock time limit", cause)
	}
	return fromServerClass(pe, code, cause)
}

func fromServerClass(pe *pq.Error, code string, cause error) error {
	switch {
	case strings.HasPrefix(code, "42"), strings.HasPrefix(code, "22"), strings.HasPrefix(code, "0A"):
		return sourceErr(ingest.SrcRequestRejected, "the source refused the statement of the dataset (a table or column that is not there, a syntax error, a value or a function it does not accept)", cause).
			WithDetail(serverSentence(pe))
	case strings.HasPrefix(code, "08"), code == "57P01", code == "57P02":
		return sourceErr(ingest.SrcConnectionLost, "the connection to the source broke or the source was shut down", cause)
	case strings.HasPrefix(code, "53"), code == "57P03", code == "40001", code == "40P01":
		return sourceErr(ingest.SrcSourceBusy, "the source answered that it is full, starting or in a conflict for now", cause)
	}
	return sourceErr(ingest.SrcInternal, "the source answered with an error the adapter does not know", cause)
}

// serverSentence is what the server said about a statement it refused, with the place in the statement. It is the
// detail the person who asked sees.
func serverSentence(pe *pq.Error) string {
	text := cleanDetail(pe.Message)
	if pe.Position != "" {
		text += " (position " + cleanDetail(pe.Position) + ")"
	}
	return text
}

// openError turns the error of opening the secret into the error this adapter gives: a secret that is not there, is
// not for this source or this address, or has no version, is the person's to fix; the rest is the store's.
func openError(err error) error {
	switch kernel.CodeOf(err) {
	case kernel.CodeNotFound, secrets.CodeScopeDenied, secrets.CodeAddressDenied, secrets.CodeVersionRetired:
		return sourceErr(ingest.SrcSecretUnusable, "the secret of the source is not there, or is not allowed for this source and this address", err)
	case kernel.CodeInvalidValue:
		return sourceErr(ingest.SrcSecretUnusable, "the secret of the source cannot be used", err)
	}
	return sourceErr(ingest.SrcInternal, "the secret of the source could not be opened", err)
}

// maxDetailBytes is how much of the server's own sentence goes to the person who asked.
const maxDetailBytes = 1 << 10

// cleanDetail cuts the sentence of a server at a kibibyte and takes the control characters out, so that what a source
// that is not honest sends cannot fill a screen or move a terminal.
func cleanDetail(s string) string {
	s = strings.ToValidUTF8(s, "\ufffd")
	if len(s) > maxDetailBytes {
		s = s[:maxDetailBytes]
		s = strings.ToValidUTF8(s, "")
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}
