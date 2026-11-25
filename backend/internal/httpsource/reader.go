package httpsource

import (
	"context"
	"iter"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// Doer is what the adapter asks of the client that makes its requests: to send one, and to let go of its connections.
// *egress.Client is one. The adapter is given a Connector and not a client, because it makes a new client for each read
// and closes it at the end, so that no connection outlives a read (ADR-0091, the sixth risk).
type Doer interface {
	Do(ctx context.Context, req egress.Request) (*egress.Response, error)
	CloseIdleConnections()
}

// Connector makes the client of one read.
type Connector func(opts egress.ClientOptions) (Doer, error)

// ViaDialer makes the clients of adapter.egress on a dialer. The dialer is the server's: it holds the policy that
// decides which addresses a source may be connected to.
func ViaDialer(d *egress.Dialer) Connector {
	return func(opts egress.ClientOptions) (Doer, error) {
		c, err := egress.NewClient(d, opts)
		if err != nil {
			return nil, err
		}
		return c, nil
	}
}

// Sleeper waits for d, or until the context is done. It is given to the adapter so that a test does not wait.
type Sleeper func(ctx context.Context, d time.Duration) error

// Config is what a Reader is made from.
type Config struct {
	// Source is the definition of the source, as ParseAdapterSource read it. Its adapter is http.
	Source ingest.Source
	// Secrets opens the secret of the authentication. It is the only port of the secrets the adapter has, and it may be
	// nil for a source whose authentication is none.
	Secrets secrets.Opener
	Connect Connector
	Clock   kernel.Clock
	// Rng spreads the waits of the retries that the source gave no Retry-After for.
	Rng kernel.Rng
	// Sleep waits. Nil waits on a real timer.
	Sleep Sleeper
	// Observer is told what happens. Nil tells nobody.
	Observer ingest.Observer
	// Ceilings are what no definition may pass and the limits of the whole run; a zero field is its starting value (limits.go).
	// A server setting that fills it is made when the server is put together.
	Ceilings Ceilings
}

// Reader reads the datasets of one HTTP source. It implements the read port (ingest.PageReader) and the optional
// ingest.Previewer, and it has no method that writes to the source. It holds the definition and nothing of a read: every
// read opens its own secret and makes its own client.
//
// A Reader is made for each run of the source. The one thing it keeps from one read to the next is what the run has spent
// against the limits of the whole run (the requests it sent and the time since the first one, Ceilings): the datasets of a run
// are read one after the other by the same Reader and share them, and a second run with the same Reader goes on from what the first
// spent (ADR-0111).
type Reader struct {
	cfg  Config
	set  settings
	ceil Ceilings
	bud  *budget
}

var (
	_ ingest.PageReader = (*Reader)(nil)
	_ ingest.Previewer  = (*Reader)(nil)
)

// New makes the Reader of a source.
func New(cfg Config) (*Reader, error) {
	set, err := readSettings(cfg.Source)
	if err != nil {
		return nil, err
	}
	switch {
	case cfg.Connect == nil || cfg.Clock == nil || cfg.Rng == nil:
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the http source reader needs a connector, a clock and a rng")
	case set.needsSecret() && cfg.Secrets == nil:
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "source %s signs in with a secret and the reader was given no way to open one", set.id)
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleepFor
	}
	return &Reader{cfg: cfg, set: set, ceil: cfg.Ceilings.withDefaults(), bud: &budget{}}, nil
}

// parentOf is the dataset that gives the values of a child, from the definition the reader was made with.
func (r *Reader) parentOf(ds ingest.Dataset) (ingest.Dataset, bool) {
	if ds.Parent == nil {
		return ingest.Dataset{}, false
	}
	for _, d := range r.cfg.Source.Datasets {
		if d.ID == ds.Parent.Dataset && d.ID != ds.ID {
			return d, true
		}
	}
	return ingest.Dataset{}, false
}

func sleepFor(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ReadPages gives the dataset page by page, as many requests as the pagination and the limits allow. The sequence ends
// with a page that has Last set, whose End says whether the dataset was read to its end, or with an error. A dataset of a
// source that is turned off is not read (not_found). The caller leaves the loop to stop, and what the read has open (the
// secret, the client and its connections) is closed then.
func (r *Reader) ReadPages(ctx context.Context, source string, ds ingest.Dataset, opts ingest.PageOptions) iter.Seq2[ingest.Page, error] {
	return func(yield func(ingest.Page, error) bool) {
		rn, err := r.begin(ctx, source, ds, secrets.PurposeRun)
		if err != nil {
			yield(ingest.Page{}, err)
			return
		}
		defer rn.close()
		rn.read(ctx, ds, opts, yield)
	}
}
