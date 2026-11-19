package httpsource

import (
	"context"
	"errors"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The errors of this package are ingest.SourceError values and nothing else (and the error of the context, which is the
// caller's own). Their words are fixed: they hold no address, no query, no header, no body and no sentence of the
// standard library or of adapter.egress other than the fixed reason it gives. What is below them (the egress error) is
// kept for errors.As and never written out. The words go through MaskText once more, with the values of the secrets
// that are open, as a second wall.

var (
	errUnauthorized = errors.New("the source answered 401")
	errTooLarge     = errors.New("the response is larger than the limit")
)

func (rn *run) fail(kind, message string, cause error) *ingest.SourceError {
	return ingest.NewSourceError(kind, rn.clean(message), cause)
}

// transportError says what an error of a request or of reading its answer is. The error of the caller's context is given
// as it is, so that a cancellation is never taken for a failure of the source.
func (rn *run) transportError(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	var ee *egress.Error
	if !errors.As(err, &ee) {
		return rn.fail(ingest.SrcConnectionLost, "the connection to the source failed or broke", err)
	}
	switch ee.Kind {
	case egress.KindPolicy:
		return rn.fail(ingest.SrcPolicyDenied, "the policy of the server does not allow this connection: "+ee.Reason, err)
	case egress.KindResolve:
		if ee.Temporary() {
			return rn.fail(ingest.SrcConnectionLost, "the name of the source could not be resolved for now", err)
		}
		return rn.fail(ingest.SrcNameNotFound, "the name of the source has no address", err)
	case egress.KindConnect:
		return rn.fail(ingest.SrcConnectionLost, "the connection to the source failed or broke", err)
	case egress.KindTLS:
		return rn.fail(ingest.SrcTLSRejected, "the certificate of the source was refused: "+ee.Reason, err)
	case egress.KindTimeout:
		return rn.fail(ingest.SrcTimeout, "the source did not answer in time", err)
	case egress.KindRedirect:
		return rn.fail(ingest.SrcRequestRejected, "the source answered with a redirect, which is not followed; the address of the source or of the request may have moved", err)
	case egress.KindCanceled:
		return context.Canceled
	}
	return rn.fail(ingest.SrcInternal, "the connection to the source failed in a way that is not known", err)
}

// pageValueError says what a failure of reading where the next page is. A value that is too long is a failure of the shape
// of the answer; any other is an address that may not be followed.
func (rn *run) pageValueError(ctx context.Context, err error) error {
	if errors.Is(err, errPageValue) {
		return rn.fail(ingest.SrcResponseFormat, "a value the source gave for the next page (a cursor or an address) is longer than "+strconv.Itoa(pageValueMax)+" bytes", nil)
	}
	return rn.transportError(ctx, err)
}

// statusError says what a status that is not a success is. The body of such an answer is never read.
func (rn *run) statusError(status int) *ingest.SourceError {
	code := strconv.Itoa(status)
	switch {
	case status == 401:
		return rn.fail(ingest.SrcAuthFailed, "the source refused the credentials (401)", errUnauthorized)
	case status == 403:
		return rn.fail(ingest.SrcAuthFailed, "the account may not read this (403)", nil)
	case status == 408:
		return rn.fail(ingest.SrcTimeout, "the source answered that the request took too long (408)", nil)
	case status == 429:
		return rn.fail(ingest.SrcRateLimited, "the source limits the requests (429)", nil)
	case status >= 500:
		return rn.fail(ingest.SrcSourceBusy, "the source failed or is busy ("+code+")", nil)
	}
	return rn.fail(ingest.SrcRequestRejected, "the source answered "+code+" to the request; the request, its path or its parameters may be wrong", nil)
}

// openError says what a failure to open a secret is: the secret may not be used for this source and address, or is not
// there, or is of no use (the setting's); or the store or its key is at fault (the product's). The error of the caller's
// context is given as it is.
func openError(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	switch kernel.CodeOf(err) {
	case secrets.CodeScopeDenied, secrets.CodeAddressDenied, secrets.CodeVersionRetired, kernel.CodeNotFound, kernel.CodeInvalidValue:
		return ingest.NewSourceError(ingest.SrcSecretUnusable, "the secret of the source cannot be used for this source and address, or does not exist", err)
	}
	return ingest.NewSourceError(ingest.SrcInternal, "the secret of the source could not be opened", err)
}

// transientError tells whether a retry of a GET may help: the source or the connection failed in a passing way.
func transientError(err error) bool {
	se, ok := ingest.SourceErrorOf(err)
	return ok && se.Class == ingest.CheckTransient
}
