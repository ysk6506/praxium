package httpsource

import (
	"context"
	"errors"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The items of a check, in the order they are tried. Each is a closed phrase of this reader's.
const (
	StepSecret    = "secret"
	StepToken     = "token_request"
	StepConnect   = "connect"
	StepSignIn    = "sign_in"
	StepResponse  = "first_response"
	StepRecordsAt = "records_path"
)

var stepMessages = map[string]string{
	StepSecret:    "the secret was opened for this source and address",
	StepToken:     "the authentication host gave a token",
	StepConnect:   "the name was resolved and the source answered",
	StepSignIn:    "the source accepted the credentials",
	StepResponse:  "the first answer was a success and was JSON",
	StepRecordsAt: "the records were found in the answer",
}

// Check reads the first response of the dataset for real, so that it tells what the setting and the access give and not
// only that a connection opens. It tells it item by item: the secret, the connection, the credentials, the first answer
// and the place of the records. It sends the first request only (and the one that asks for a token, if there is one).
//
// The result carries the notes of the source (the data goes without TLS, a pinned certificate is accepted whatever its
// dates are). When the certificate of the server was the reason the check failed, it carries what the server presented,
// found by one more handshake that sends no request (ADR-0105).
func (r *Reader) Check(ctx context.Context, source string, ds ingest.Dataset) (res ingest.CheckResult) {
	defer func() { res.Notes = r.set.notes() }()
	rn, err := r.begin(ctx, source, ds, secrets.PurposeCheck)
	if err != nil {
		return checkFailure(&progress{}, r.set.needsSecret(), false, err)
	}
	defer rn.close()
	rn.firstOnly = true
	rows, end, err := rn.firstPage(ctx, ds)
	if err != nil {
		res = checkFailure(&rn.prog, r.set.needsSecret(), true, err)
		res.Cert = rn.presentedCertificate(ctx, err)
		return res
	}
	steps := checkSteps(&rn.prog, r.set.needsSecret(), true, nil)
	if end != nil && !end.Complete && end.Reason != reasonFirstOnly {
		return ingest.CheckResult{Kind: ingest.CheckTransient, Message: end.Reason, Rows: rows, Steps: steps}
	}
	return ingest.CheckResult{Kind: ingest.CheckOK, Message: "the first page was read: " + strconv.Itoa(rows) + " rows", Rows: rows, Steps: steps}
}

// firstPage reads the first response and gives the lines it had and how the read ended.
func (rn *run) firstPage(ctx context.Context, ds ingest.Dataset) (int, *ingest.Completeness, error) {
	rows := 0
	var end *ingest.Completeness
	var failed error
	rn.read(ctx, ds, ingest.PageOptions{}, func(p ingest.Page, err error) bool {
		if err != nil {
			failed = err
			return false
		}
		rows += p.Received()
		if p.Last {
			e := p.End
			end = &e
		}
		return true
	})
	return rows, end, failed
}

// checkFailure is the result of a check that stopped at an error.
func checkFailure(p *progress, secret, opened bool, err error) ingest.CheckResult {
	class := ingest.ClassifyCheckError(err)
	message := plainText(err)
	return ingest.CheckResult{Kind: class, Message: message, Steps: checkSteps(p, secret, opened, &ingest.CheckStep{Kind: class, Message: message})}
}

// plainText is the words of an error for a person: those of a SourceError, and a fixed phrase for anything else.
func plainText(err error) string {
	if se, ok := ingest.SourceErrorOf(err); ok {
		return se.Message
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "the check was canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "the check did not finish in time"
	}
	return "the check could not be made"
}

// checkSteps lists the items that worked and, if the check failed, the first that did not, with the kind and the words of
// the failure. An item after the failure was not tried and is not listed.
func checkSteps(p *progress, secret, opened bool, failure *ingest.CheckStep) []ingest.CheckStep {
	reached := []struct {
		name string
		ok   bool
		skip bool
	}{
		{StepSecret, opened, !secret},
		{StepToken, p.tokenAnswered, !p.crossToken},
		{StepConnect, p.connected, false},
		{StepSignIn, p.authenticated, false},
		{StepResponse, p.responded, false},
		{StepRecordsAt, p.recordsFound, false},
	}
	var steps []ingest.CheckStep
	for _, s := range reached {
		switch {
		case s.skip:
		case s.ok:
			steps = append(steps, ingest.CheckStep{Name: s.name, Kind: ingest.CheckOK, Message: stepMessages[s.name]})
		default:
			if failure != nil {
				steps = append(steps, ingest.CheckStep{Name: s.name, Kind: failure.Kind, Message: failure.Message})
			}
			return steps
		}
	}
	return steps
}

// certProber is what a client may be able to do besides sending requests: show the certificate of its server. *egress.Client
// does; the Doer interface does not ask for it, so that a client that cannot is still a Doer.
type certProber interface {
	ProbeCert(ctx context.Context, rawURL string) (*egress.CertSummary, error)
}

// presentedCertificate is the certificate the server presented, when the check failed because of the certificate of the
// server (not trusted, not for the name, not one of the pins): one more handshake to the same host and port that ends
// before any request is sent. It is nil for any other failure and when the server cannot be asked.
func (rn *run) presentedCertificate(ctx context.Context, failure error) *ingest.CertSummary {
	se, ok := ingest.SourceErrorOf(failure)
	if !ok || se.Kind != ingest.SrcTLSRejected {
		return nil
	}
	prober, ok := rn.doer.(certProber)
	if !ok {
		return nil
	}
	sum, err := prober.ProbeCert(ctx, rn.origin.String())
	if err != nil || sum == nil {
		return nil
	}
	return &ingest.CertSummary{Subject: sum.Subject, Issuer: sum.Issuer, SANs: sum.SANs, SANsMore: sum.SANsMore,
		NotBefore: sum.NotBefore, NotAfter: sum.NotAfter, SPKISHA256: sum.SPKISHA256, Truncated: sum.Truncated}
}
