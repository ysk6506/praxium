package pgsource

import (
	"context"
	"database/sql"
	"iter"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// Config is what a Reader is made from.
type Config struct {
	// Source is the definition of the source: adapter postgres, with its connection and its datasets.
	Source ingest.Source
	// Secrets opens the secret the connection names. It is the opening port only: this adapter cannot write a secret.
	Secrets secrets.Opener
	// Dialer is the dialer of adapter.egress; every connection and every cancel request of the driver goes through it.
	Dialer *egress.Dialer
	// Observer is told what happens in a read. Nil tells nobody.
	Observer ingest.Observer
	// Clock gives the time the events carry. Nil leaves the time out of them.
	Clock kernel.Clock
}

// Reader reads the datasets of one postgres source. It holds no connection: every read makes its own and closes it.
// It is safe for use from several goroutines (each read has its own pool), though a source is read one at a time.
type Reader struct {
	src      ingest.Source
	conn     *ingest.Connection
	st       settings
	secrets  secrets.Opener
	dialer   *egress.Dialer
	observer ingest.Observer
	clock    kernel.Clock
}

var (
	_ ingest.PageReader = (*Reader)(nil)
	_ ingest.Previewer  = (*Reader)(nil)
)

// New makes the reader of a postgres source.
func New(cfg Config) (*Reader, error) {
	cn := cfg.Source.Connection
	switch {
	case cfg.Source.Adapter != ingest.AdapterPostgres:
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "pgsource reads a source of the adapter %s", ingest.AdapterPostgres)
	case cn == nil || cn.Credential == nil || cn.Credential.SecretID == "":
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a postgres source needs a connection with the secret of its account")
	case cfg.Secrets == nil || cfg.Dialer == nil:
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "pgsource needs the port that opens secrets and the dialer of adapter.egress")
	}
	return &Reader{src: cfg.Source, conn: cn, st: settingsOf(cn), secrets: cfg.Secrets, dialer: cfg.Dialer, observer: cfg.Observer, clock: cfg.Clock}, nil
}

// ReadPages gives the dataset page by page, in the order of its natural key; see the package comment.
func (r *Reader) ReadPages(ctx context.Context, source string, ds ingest.Dataset, opts ingest.PageOptions) iter.Seq2[ingest.Page, error] {
	return func(yield func(ingest.Page, error) bool) {
		r.read(ctx, secrets.PurposeRun, source, ds, opts, false, yield)
	}
}

// Preview reads the first pages for a person to look at. The rows are the same a read gives, with the value of every
// column whose name says it is a secret's left out. A preview does not count the rows without a key, which is a scan of
// the whole table.
func (r *Reader) Preview(ctx context.Context, source string, ds ingest.Dataset, opts ingest.PreviewOptions) (ingest.Preview, error) {
	opts = opts.WithDefaults()
	var out ingest.Preview
	for page, err := range r.previewPages(ctx, source, ds, opts) {
		if err != nil {
			return ingest.Preview{}, err
		}
		masked, was := ingest.MaskPage(page)
		out.Pages = append(out.Pages, masked)
		out.Masked = out.Masked || was
		if len(out.Pages) >= opts.MaxPages {
			break
		}
	}
	return out, nil
}

func (r *Reader) previewPages(ctx context.Context, source string, ds ingest.Dataset, opts ingest.PreviewOptions) iter.Seq2[ingest.Page, error] {
	return func(yield func(ingest.Page, error) bool) {
		r.read(ctx, secrets.PurposeCheck, source, ds, ingest.PageOptions{MaxRows: opts.MaxRows}, true, yield)
	}
}

// read makes the connection, describes the statement and gives the pages.
func (r *Reader) read(ctx context.Context, purpose secrets.Purpose, source string, ds ingest.Dataset, opts ingest.PageOptions, skipNull bool, yield func(ingest.Page, error) bool) {
	f := r.newFlow(source, ds.ID, yield)
	sess, pl, err := r.begin(ctx, purpose, source, ds)
	if err != nil {
		f.fail(ctx, err)
		return
	}
	defer sess.close()
	f.sess, f.pl = sess, pl
	f.pages(ctx, opts, skipNull)
}

// begin gets everything a read needs before it reads a row: the plan, the pool, the first connection, the description.
func (r *Reader) begin(ctx context.Context, purpose secrets.Purpose, source string, ds ingest.Dataset) (*session, *plan, error) {
	if source != r.src.ID {
		return nil, nil, sourceErr(ingest.SrcInternal, "the reader was asked for a source that is not its own", nil)
	}
	pl, err := newPlan(ds)
	if err != nil {
		return nil, nil, err
	}
	sess, err := r.open(ctx, purpose)
	if err != nil {
		return nil, nil, err
	}
	if err := sess.connect(ctx); err != nil {
		sess.close()
		return nil, nil, err
	}
	if err := sess.prepare(ctx, pl); err != nil {
		sess.close()
		return nil, nil, err
	}
	return sess, pl, nil
}

// prepare describes the statement of the plan in a transaction and checks that the dataset can be read from it.
func (s *session) prepare(ctx context.Context, pl *plan) error {
	if err := s.describe(ctx, pl); err != nil {
		return err
	}
	return pl.validate()
}

// describe asks the server for the columns of the statement of the plan.
func (s *session) describe(ctx context.Context, pl *plan) error {
	err := s.readTx(ctx, func(ctx context.Context, tx *sql.Tx) error { return pl.describe(ctx, tx) })
	return classify(ctx, err)
}
