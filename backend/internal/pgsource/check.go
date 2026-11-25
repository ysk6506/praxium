package pgsource

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The steps of a check, in the order they are made. Each is a closed phrase of this adapter.
const (
	stepSecret     = "secret"
	stepConnection = "name and connection"
	stepTLS        = "tls"
	stepSignIn     = "sign-in"
	stepQuery      = "statement"
	stepKeys       = "key columns"
	stepFirstPage  = "first page"
)

// Check reads the first page of the dataset for real, step by step, so that it tells which step a trouble is in: the
// secret, the name and the connection, the TLS, the sign-in, the statement, the key columns, and the first page. It does
// not look at what the account may write and does not discover what the source offers (the next change does).
//
// The result carries the notes of the source (no TLS, a pinned certificate accepted whatever its dates are), and when the
// certificate of the server was the reason it failed, what the server presented, found by one more handshake that sends
// no password and no statement (ADR-0105).
func (r *Reader) Check(ctx context.Context, source string, ds ingest.Dataset) (res ingest.CheckResult) {
	defer func() { res.Notes = r.notes() }()
	c := &checker{r: r}
	pl, err := r.checkSetup(ctx, source, ds, c)
	if err != nil {
		return c.fail(ctx, err)
	}
	defer c.sess.close()
	for page, err := range c.firstPage(ctx, r, pl) {
		if err != nil {
			return c.fail(ctx, err)
		}
		return c.done(page)
	}
	return c.fail(ctx, sourceErr(ingest.SrcInternal, "the reader gave no page", nil))
}

type checker struct {
	r     *Reader
	steps []ingest.CheckStep
	sess  *session
	step  string // the step being made, which a failure belongs to
}

func (c *checker) ok(name, msg string) {
	c.steps = append(c.steps, ingest.CheckStep{Name: name, Kind: ingest.CheckOK, Message: msg})
}

// fail ends the check with the error of the step that was being made.
func (c *checker) fail(ctx context.Context, err error) ingest.CheckResult {
	err = classify(ctx, err)
	kind, msg := ingest.ClassifyCheckError(err), "the check was cut short"
	if se, ok := ingest.SourceErrorOf(err); ok {
		msg = se.Message
	}
	if c.step != "" {
		c.steps = append(c.steps, ingest.CheckStep{Name: c.step, Kind: kind, Message: msg})
	}
	res := ingest.CheckResult{Kind: kind, Message: msg, Steps: c.steps}
	if se, ok := ingest.SourceErrorOf(err); ok && se.Kind == ingest.SrcTLSRejected {
		res.Cert = c.r.presentedCertificate(ctx)
	}
	return res
}

// presentedCertificate is what the server presents when asked to start TLS, with nothing sent after it.
func (r *Reader) presentedCertificate(ctx context.Context) *ingest.CertSummary {
	sum, err := r.dialer.ProbePostgresCert(ctx, r.st.host, r.st.port)
	if err != nil || sum == nil {
		return nil
	}
	return &ingest.CertSummary{Subject: sum.Subject, Issuer: sum.Issuer, SANs: sum.SANs, SANsMore: sum.SANsMore,
		NotBefore: sum.NotBefore, NotAfter: sum.NotAfter, SPKISHA256: sum.SPKISHA256, Truncated: sum.Truncated}
}

// notes are what a check says about the source besides how it went.
func (r *Reader) notes() []ingest.CheckNote {
	var out []ingest.CheckNote
	if r.st.disable {
		out = append(out, ingest.CheckNote{Code: ingest.NotePlaintext, Message: "the password and the data of this source cross the network without encryption (sslmode disable)"})
	}
	if len(r.st.pins) > 0 {
		out = append(out, ingest.CheckNote{Code: ingest.NotePinExpiry, Message: "the certificate of the server is accepted by its public key alone: its dates, its name and its chain are not looked at, so an expired certificate is accepted"})
	}
	return out
}

func (c *checker) done(p ingest.Page) ingest.CheckResult {
	rows := p.Received()
	if p.Last && !p.End.Complete {
		c.steps = append(c.steps, ingest.CheckStep{Name: stepFirstPage, Kind: ingest.CheckTransient, Message: p.End.Reason})
		return ingest.CheckResult{Kind: ingest.CheckTransient, Message: p.End.Reason, Rows: rows, Steps: c.steps}
	}
	msg := "the first page was read: " + itoa(rows) + " rows"
	c.ok(stepFirstPage, msg)
	return ingest.CheckResult{Kind: ingest.CheckOK, Message: msg, Rows: rows, Steps: c.steps}
}

// checkSetup makes the steps before the first page. A step that fails is the last; the pool is closed then.
func (r *Reader) checkSetup(ctx context.Context, source string, ds ingest.Dataset, c *checker) (*plan, error) {
	if source != r.src.ID {
		return nil, sourceErr(ingest.SrcInternal, "the reader was asked for a source that is not its own", nil)
	}
	pl, err := newPlan(ds)
	if err != nil {
		c.step = stepQuery
		return nil, err
	}
	c.step = stepSecret
	sess, err := r.open(ctx, secrets.PurposeCheck)
	if err != nil {
		return nil, err
	}
	c.sess = sess
	c.ok(stepSecret, "the secret was opened for this source and this address")
	if err := c.connection(ctx); err != nil {
		sess.close()
		return nil, err
	}
	c.step = stepQuery
	if err := sess.describe(ctx, pl); err != nil {
		sess.close()
		return nil, err
	}
	c.ok(stepQuery, "the server prepared the statement of the dataset")
	c.step = stepKeys
	if err := pl.validate(); err != nil {
		sess.close()
		return nil, err
	}
	c.ok(stepKeys, "the natural key columns are in the statement, and have a type the pages can follow")
	return pl, nil
}

// connection makes the first connection and tells which of the three steps a failure is in.
func (c *checker) connection(ctx context.Context) error {
	c.step = stepConnection
	err := c.sess.connect(ctx)
	if err == nil {
		c.ok(stepConnection, "the name was resolved and the connection was made")
		if c.sess.st.disable {
			c.ok(stepTLS, "no TLS (sslmode disable), which is allowed only to the loopback or where the operator allowed it")
		} else {
			c.ok(stepTLS, "the certificate of the server was verified")
		}
		c.ok(stepSignIn, "the source accepted the account")
		return nil
	}
	if se, ok := ingest.SourceErrorOf(err); ok {
		switch se.Kind {
		case ingest.SrcTLSRejected:
			c.step = stepTLS
		case ingest.SrcAuthFailed, ingest.SrcRequestRejected:
			c.step = stepSignIn
		}
	}
	return err
}

// firstPage is the pages of the dataset, ended after the first. It is the real read, on the connection the steps made.
func (c *checker) firstPage(ctx context.Context, r *Reader, pl *plan) func(yield func(ingest.Page, error) bool) {
	c.step = stepFirstPage
	return func(yield func(ingest.Page, error) bool) {
		f := r.newFlow(r.src.ID, pl.ds.ID, func(p ingest.Page, err error) bool {
			yield(p, err)
			return false
		})
		f.sess, f.pl = c.sess, pl
		f.pages(ctx, ingest.PageOptions{}, true)
	}
}
