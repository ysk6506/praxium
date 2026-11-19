package pgsource

import (
	"context"
	"database/sql"
	"slices"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// flow is the state of one read of one dataset: the pages being cut, the line counter, the events.
type flow struct {
	r       *Reader
	sess    *session
	pl      *plan
	source  string
	dataset string
	yield   func(ingest.Page, error) bool
	w       *ingest.PageWriter
	line    int
	page    int
}

func (r *Reader) newFlow(source, dataset string, yield func(ingest.Page, error) bool) *flow {
	return &flow{r: r, source: source, dataset: dataset, yield: yield}
}

// emit gives a page or an error to the caller and tells the observer what it was.
func (f *flow) emit(p ingest.Page, err error) bool {
	switch {
	case err != nil:
		f.event(ingest.Event{Kind: ingest.EventFailed, Class: ingest.ClassifyCheckError(err), ErrorKind: kindOf(err)})
	case p.Last && !p.End.Complete:
		f.event(ingest.Event{Kind: ingest.EventIncomplete, Page: p.No, Rows: p.Received(), Reason: p.End.Reason})
		fallthrough
	default:
		f.event(ingest.Event{Kind: ingest.EventPage, Page: p.No, Rows: p.Received(), Bytes: p.Bytes})
	}
	return f.yield(p, err)
}

func kindOf(err error) string {
	if se, ok := ingest.SourceErrorOf(err); ok {
		return se.Kind
	}
	return ""
}

// fail gives the caller the error of a read that could not begin or went wrong.
func (f *flow) fail(ctx context.Context, err error) {
	f.emit(ingest.Page{}, classify(ctx, err))
}

// pages reads the dataset to its end, from the position of the token when there is one that fits.
func (f *flow) pages(ctx context.Context, opts ingest.PageOptions, skipNull bool) {
	opts = opts.WithDefaults()
	f.w = ingest.NewPageWriter(f.pl.ds, opts)
	f.sess.budget = pageBudget(opts.MaxBytes)
	after, resumed := decodeToken(opts.Resume, f.pl.fp, len(f.pl.keys))
	if !f.begin(ctx, resumed, skipNull) {
		return
	}
	for {
		if err := ctx.Err(); err != nil {
			f.emit(ingest.Page{}, err)
			return
		}
		res, err := f.next(ctx, after, opts)
		if err != nil {
			f.fail(ctx, err)
			return
		}
		if !f.afterBatch(ctx, res, opts) {
			return
		}
		after = res.rows[len(res.rows)-1].keys
	}
}

// begin gives the rows without a natural key first, unless the read resumes from a token or passes them over.
func (f *flow) begin(ctx context.Context, resumed, skipNull bool) bool {
	if resumed || skipNull {
		return true
	}
	return f.nullKeys(ctx)
}

// afterBatch puts the rows of a batch on the pages and tells whether the read goes on. A batch that ends the read, or
// that cannot be trusted (the source changed, or one key holds more rows than a page), finishes the pages here.
func (f *flow) afterBatch(ctx context.Context, res batchResult, opts ingest.PageOptions) bool {
	if res.tooBig {
		f.w.Finish(f.emit, ingest.Completeness{Reason: "more than " + itoa(opts.MaxRows) + " rows have one natural key, more than a page holds; the dataset is read up to that key"})
		return false
	}
	if res.changed || (len(res.rows) == 0 && !res.end) {
		f.w.Finish(f.emit, ingest.Completeness{Reason: "the source changed while the dataset was read; the dataset is read up to the page before"})
		return false
	}
	if !f.give(ctx, res.rows) {
		return false
	}
	if res.end {
		f.w.Finish(f.emit, ingest.Completeness{Complete: true})
		return false
	}
	return true
}

// next reads one batch in one transaction and tells the observer about it.
func (f *flow) next(ctx context.Context, after []string, opts ingest.PageOptions) (batchResult, error) {
	f.event(ingest.Event{Kind: ingest.EventRequest, Page: f.page})
	start := f.now()
	var res batchResult
	err := f.sess.readTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		res, err = f.pl.batch(ctx, tx, after, opts.MaxRows, opts.MaxBytes)
		return err
	})
	if err != nil {
		return batchResult{}, err
	}
	f.event(ingest.Event{Kind: ingest.EventResponse, Page: f.page, Rows: len(res.rows), Bytes: res.bytes, ElapsedMillis: f.since(start)})
	f.page++
	return res, nil
}

// give puts the rows of a batch on the pages, a key at a time: all the rows of a key go on one page, and the token that
// goes with the page is the last key on it.
func (f *flow) give(ctx context.Context, rows []dbRow) bool {
	for i := 0; i < len(rows); {
		j := i + 1
		for j < len(rows) && slices.Equal(rows[j].keys, rows[i].keys) {
			j++
		}
		if !f.w.Add(ctx, f.table(rows[i:j]), encodeToken(f.pl.fp, rows[i].keys), f.emit) {
			return false
		}
		i = j
	}
	return true
}

// table makes the rows of one key into the record the page writer takes.
func (f *flow) table(rows []dbRow) ingest.Table {
	var t ingest.Table
	for _, row := range rows {
		f.line++
		one := f.pl.builder.Record(row.doc, f.line, "")
		t.Rows = append(t.Rows, one.Rows...)
		t.Unreadable = append(t.Unreadable, one.Unreadable...)
		for reason, n := range one.Skipped {
			if t.Skipped == nil {
				t.Skipped = map[string]int{}
			}
			t.Skipped[reason] += n
		}
	}
	return t
}

// nullKeys gives the rows that have a NULL in a key column, which the pages cannot place: each is a row without a
// natural key (or is passed over when the dataset says to pass over the rows whose key columns are all empty). They
// come before the rows of the pages, and carry no token. It tells whether to go on.
func (f *flow) nullKeys(ctx context.Context) bool {
	var groups []nullGroup
	err := f.sess.readTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		groups, err = f.pl.nullKeys(ctx, tx)
		return err
	})
	if err != nil {
		f.fail(ctx, err)
		return false
	}
	for _, g := range groups {
		if !f.giveNullGroup(ctx, g) {
			return false
		}
	}
	return true
}

func (f *flow) giveNullGroup(ctx context.Context, g nullGroup) bool {
	if g.all && f.pl.ds.IgnoreRowsWithoutKey {
		return f.w.Add(ctx, ingest.Table{Skipped: map[string]int{ingest.SkipNoKey: int(g.n)}}, nil, f.emit)
	}
	one := ingest.Table{Unreadable: []ingest.Unreadable{{Reason: "missing_natural_key", Column: f.pl.ds.NaturalKey[g.first], AllEmpty: g.all}}}
	for i := int64(0); i < g.n; i++ {
		if !f.w.Add(ctx, one, nil, f.emit) {
			return false
		}
	}
	return true
}

func (f *flow) event(e ingest.Event) {
	if f.r.observer == nil {
		return
	}
	e.Source, e.Dataset = f.source, f.dataset
	e.Host, e.Port = f.r.st.host, f.r.st.port
	f.r.observer.Observe(e)
}

func (f *flow) now() int64 {
	if f.r.clock == nil {
		return 0
	}
	return f.r.clock.Now().UnixMilli()
}

func (f *flow) since(start int64) int64 {
	if f.r.clock == nil {
		return 0
	}
	return f.r.clock.Now().UnixMilli() - start
}

func itoa(n int) string { return strconv.Itoa(n) }

// pageBudget is the bytes one transaction may read: four times the bytes of a page and a mebibyte. A transaction of an
// honest source reads a batch (the cells of up to a page and one row over, and the text of the keys and the names of the
// columns round them) and, when one key fills a batch, the rows of that key (up to a page of rows); the cells of the
// statement are counted as the driver gives them, and the wire holds them as text with a few bytes of a message round
// each, so the two reads together come to about twice a page, and the factor gives room for the rest. A row that is
// larger than that is a row the read does not take (it ends with the kind response_format).
func pageBudget(maxBytes int) int64 { return 4*int64(maxBytes) + 1<<20 }
