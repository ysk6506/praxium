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
	// crossToken says that the token is asked for at another host than the API's, and tokenAnswered that the token host gave
	// a token. The API's connection is then told apart from the token host's.
	crossToken, tokenAnswered bool
}

// run is one read of one dataset: the secret opened for it, the client made for it, the pace of its requests and what it
// has done so far. It is made when the read begins and closed when it ends, so nothing of it outlives the read: not the
// client's connections, not the secret's values, not the token the source gave.
type run struct {
	r      *Reader
	set    settings
	dsID   string
	host   string
	port   int
	origin *url.URL
	doer   Doer
	signer *signer
	// The token request when it goes to another host than the API's (crossToken): its own client (its certificate authority is
	// its own, and the connections of the two hosts are never one pool), the address it goes to and the host and the port the
	// secret was opened for there. Without crossToken the token request goes through doer to the origin of the API.
	crossToken bool
	tokenDoer  Doer
	tokenAt    *url.URL
	tokenHost  string
	tokenPort  int
	// where is the host and the port of the request sent last, which the events say; tokenRequests is how many tokens this
	// read asked for, and refreshes how many of those were asked for because the source refused the token it had.
	where                    address
	tokenRequests, refreshes int
	// lifeNoted says that the event of a lifetime that could not be read was told.
	lifeNoted bool
	pace      *pacer
	pager     *pager
	maxPages  int
	prog      progress
	// keep says that the exchanges are kept, for a preview.
	keep      bool
	exchanges []ingest.Exchange
	// firstOnly ends the read after the first response, for a check.
	firstOnly bool
	// open is the answer whose body was opened last, so that a read that ends in the middle of it lets go of it.
	open *reply
	// tokens is how many tokens of pages the read made, and cleaned how many texts had a value of a secret taken out.
	tokens, cleaned int
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
	sg, err := openSigner(ctx, r.cfg.Secrets, r.set, ds, purpose)
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
	rn.where = address{host, port}
	doer, err := r.cfg.Connect(egress.ClientOptions{CAPEM: r.set.caPEM, Timeout: r.set.timeout})
	if err != nil {
		err = rn.fail(ingest.SrcTLSRejected, "the certificate authority of the source could not be used (ca_pem)", err)
		sg.close()
		return nil, err
	}
	rn.doer = doer
	rn.prog = progress{}
	if err := rn.connectToken(); err != nil {
		doer.CloseIdleConnections()
		sg.close()
		return nil, err
	}
	return rn, nil
}

// address is a host and a port a request goes to.
type address struct {
	host string
	port int
}

// connectToken makes what the token request needs when it goes to another host than the API's: a client of its own, with the
// certificate authority of the token host (never the API's), and the address at the host and the port the secret was opened
// for there. Nothing is made for a token request that goes to the API's host.
func (rn *run) connectToken() error {
	if !rn.set.token.cross {
		return nil
	}
	a, ok := rn.signer.tokenAddress()
	if !ok {
		return ingest.NewSourceError(ingest.SrcInternal, "the token request goes to another host and the secret was not opened for it", nil)
	}
	doer, err := rn.r.cfg.Connect(egress.ClientOptions{CAPEM: rn.set.token.caPEM, Timeout: rn.set.timeout})
	if err != nil {
		return rn.fail(ingest.SrcTLSRejected, "the certificate authority of the token host could not be used (auth.request.ca_pem)", err)
	}
	rn.crossToken, rn.tokenDoer, rn.tokenHost, rn.tokenPort = true, doer, a.Host, a.Port
	rn.tokenAt = rn.set.tokenOrigin(a.Host, a.Port)
	rn.prog.crossToken = true
	return nil
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
	rn.open.closeBody()
	rn.signer.close()
	rn.doer.CloseIdleConnections()
	if rn.tokenDoer != nil {
		rn.tokenDoer.CloseIdleConnections()
	}
}

// doerFor is the client a request goes through: the token host's own for the token request when it goes to another host, and
// the API's for every other request.
func (rn *run) doerFor(p plan) Doer {
	if p.token && rn.tokenDoer != nil {
		return rn.tokenDoer
	}
	return rn.doer
}

// destination is the host and the port a request goes to: the plan says it for the token request, and the API's otherwise.
func (rn *run) destination(p plan) address {
	if p.host != "" {
		return address{p.host, p.port}
	}
	return address{rn.host, rn.port}
}

// emit tells the observer what happened, with where it happened: the host and the port of the request sent last, which is
// the token host's while the token request is what is going on. The path may hold what the source put in an address it
// gave, and the reason a sentence of the reader's: both have the values of the open secrets taken out.
func (rn *run) emit(e ingest.Event) {
	obs := rn.r.cfg.Observer
	if obs == nil {
		return
	}
	e.Source, e.Dataset, e.Host, e.Port = rn.set.id, rn.dsID, rn.where.host, rn.where.port
	e.Path, _ = rn.signer.wide.clean(e.Path)
	e.Reason, _ = rn.signer.wide.clean(e.Reason)
	obs.Observe(e)
}

// record keeps an exchange for a preview, with the values of the open secrets taken out of every text of it.
func (rn *run) record(p plan, rep *reply) {
	if !rn.keep || len(rn.exchanges) >= maxExchanges {
		return
	}
	to := rn.destination(p)
	rn.exchanges = append(rn.exchanges, ingest.Exchange{
		Method: p.method, Host: to.host, Port: to.port, Path: rn.clean(p.url.Path), Query: rn.cleanNames(p.shown), Status: rep.status,
		Headers: rn.cleanNames(ingest.MaskHeaders(rep.header))})
}
