package httpsource

import (
	"context"
	"net/url"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// progress says how far a read got, for the items of a check: which of the things that must work was seen to work.
type progress struct {
	connected, authenticated, responded, recordsFound bool
}

// run is one read of one dataset: the secret opened for it, the client made for it, the pace of its requests and what it
// has done so far. It is made when the read begins and closed when it ends, so nothing of it outlives the read: not the
// client's connections, not the secret's values, not the token the source gave.
type run struct {
	r        *Reader
	set      settings
	dsID     string
	host     string
	port     int
	origin   *url.URL
	doer     Doer
	signer   *signer
	pace     *pacer
	pager    *pager
	maxPages int
	prog     progress
	// keep says that the exchanges are kept, for a preview.
	keep      bool
	exchanges []ingest.Exchange
	// firstOnly ends the read after the first response, for a check.
	firstOnly bool
}

// maxExchanges is the most exchanges a preview keeps.
const maxExchanges = 50

// begin makes the run: it checks that the dataset is the source's to read, opens the secret for the host and port the
// requests go to, and makes the client. Whatever it opened is closed again when it fails.
func (r *Reader) begin(ctx context.Context, source string, ds ingest.Dataset, purpose secrets.Purpose) (*run, error) {
	if err := r.admit(source, ds); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sg, err := openSigner(ctx, r.cfg.Secrets, r.set, purpose)
	if err != nil {
		return nil, err
	}
	host, port := r.set.host, r.set.port
	if a, ok := sg.address(); ok {
		host, port = a.Host, a.Port
	}
	pg := newPager(ds)
	rn := &run{r: r, set: r.set, dsID: ds.ID, host: host, port: port, origin: r.set.origin(host, port), signer: sg, pager: pg,
		pace: &pacer{clock: r.cfg.Clock, interval: r.set.interval, sleep: r.cfg.Sleep}, maxPages: pg.pg.MaxPages}
	if rn.maxPages <= 0 {
		rn.maxPages = ingest.DefaultMaxPages
	}
	doer, err := r.cfg.Connect(egress.ClientOptions{CAPEM: r.set.caPEM, Timeout: r.set.timeout})
	if err != nil {
		err = rn.fail(ingest.SrcTLSRejected, "the certificate authority of the source could not be used (ca_pem)", err)
		sg.close()
		return nil, err
	}
	rn.doer = doer
	rn.prog = progress{}
	return rn, nil
}

// admit refuses what this reader does not read: another source, a source that is turned off, a dataset that is not an
// HTTP dataset.
func (r *Reader) admit(source string, ds ingest.Dataset) error {
	switch {
	case source != r.set.id:
		return ingest.NewSourceError(ingest.SrcInternal, "the reader was asked for a source that is not the one it was made for", nil)
	case r.cfg.Source.Disabled:
		return kernel.Errorf(kernel.CodeNotFound, "source %s is turned off; its datasets are not read this time", r.set.id)
	case ds.Request == nil:
		return kernel.Errorf(kernel.CodeInvalidValue, "dataset %s has no request; it is not a dataset of an http source", ds.ID)
	}
	return nil
}

func (rn *run) close() {
	rn.signer.close()
	rn.doer.CloseIdleConnections()
}

// texts are the values of the secrets that are open, for the masks.
func (rn *run) texts() []string { return rn.signer.mask.list() }

// emit tells the observer what happened, with where it happened.
func (rn *run) emit(e ingest.Event) {
	obs := rn.r.cfg.Observer
	if obs == nil {
		return
	}
	e.Source, e.Dataset, e.Host, e.Port = rn.set.id, rn.dsID, rn.host, rn.port
	obs.Observe(e)
}

// record keeps an exchange for a preview.
func (rn *run) record(p plan, rep *reply) {
	if !rn.keep || len(rn.exchanges) >= maxExchanges {
		return
	}
	rn.exchanges = append(rn.exchanges, ingest.Exchange{
		Method: p.method, Path: p.url.Path, Query: p.shown, Status: rep.status, Headers: ingest.MaskHeaders(rep.header)})
}
