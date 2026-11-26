package httpsource

import (
	"context"
	"errors"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// reasonFirstOnly is the reason a check gives when it stops after the first response; the check takes it for a success.
const reasonFirstOnly = "only the first response was read"

// loop is the reading of one dataset: the requests, one after the other, and the records of their answers cut into
// pages. It reads in one goroutine and holds one record at a time and the page being filled (ADR-0097).
type loop struct {
	rn    *run
	ctx   context.Context
	ds    ingest.Dataset
	rb    *ingest.RowBuilder
	pw    *ingest.PageWriter
	yield func(ingest.Page, error) bool
	fp    string
	at    pos
	// skip is how many records of the first response were given already; seen is how many records came before the
	// response being read, and requests how many responses this read has fetched.
	skip, seen, requests int
	last                 kernel.Hash
	haveLast             bool
	// tail is the record added last, whose token a page that ends with it takes (mint).
	tail tail
	mint func() []byte
	// lines says that the response is read as JSON lines: it has no token. bad counts the records that could not be read, missing
	// the records that had no array where records_path opens one, and cut says that a body ended without a mark of its end or broke;
	// the first makes the read not complete, the other two make it read as what was seen (ADR-0109). early says that a check
	// stopped after the lines it is given.
	lines, early, cut         bool
	bad, missing, outers, fed int
	// noLines says that a body of JSON lines had no value. recs counts the records the answers gave (the elements, or the records
	// that have none to open) and rows the rows they made; a read that had records and no row did not see what it was meant to.
	// cells is the bytes of the cells of the rows of the response being read, and over says it passed the limit of ingest.RowBytesFactor.
	noLines    bool
	recs, rows int
	cells      int
	over       bool
	// A loop that reads a child dataset (child.go) reads one unit after the other, each with its own pages: halt says that the read
	// has ended (the last page or an error was given, or the caller left), and unitDone that the unit being read has.
	halt, unitDone bool
	kid            *kid
}

// checkLines is how many records a check reads of a response of JSON lines: the answer of an export may be very long, and a check
// looks at the first of it.
const checkLines = 100

// tail is what the token of a record is made of: where the record is in its response, and the response.
type tail struct {
	i    int
	at   pos
	seen int
	end  *respEnd
	// mid says that elements of the same record come after, so that a read goes on from the start of the record. count is how many
	// records before the position had no array, and had their array looked for.
	mid   bool
	count arrayCount
}

// respEnd is what the end of a response says, which is known when the whole answer is read: how many records it had and
// what comes next. The token of the last record of a response needs it, and the token is made when a page is cut, which
// may be after the next response began.
type respEnd struct {
	done bool
	n    int
	sp   step
	// count is the same as the one of a tail, at the end of the response.
	count arrayCount
}

// read gives the pages of a dataset to yield. It ends with a page that has Last set, or with an error.
func (rn *run) read(ctx context.Context, ds ingest.Dataset, opts ingest.PageOptions, yield func(ingest.Page, error) bool) {
	if ds.IsChild() {
		rn.readChild(ctx, ds, opts, yield)
		return
	}
	rb, err := ingest.NewRowBuilder(ds)
	if err != nil {
		yield(ingest.Page{}, err)
		return
	}
	out := func(p ingest.Page, err error) bool {
		if err == nil {
			rn.emit(ingest.Event{Kind: ingest.EventPage, Page: p.No, Rows: p.Received()})
		}
		return yield(p, err)
	}
	fp := fingerprint(rn.set, ds)
	lines := ds.JSONLines()
	if lines {
		opts.Resume = nil // a response of JSON lines is not sent again to be passed over
	}
	l := &loop{rn: rn, ctx: ctx, ds: ds, rb: rb, pw: ingest.NewPageWriter(ds, opts), yield: out, fp: fp, lines: lines}
	l.mint = l.tokenOfTail
	start := rn.startAt(fp, opts.Resume)
	l.at, l.skip, l.seen = start.at, start.skip, start.base
	l.missing, l.outers = start.miss, start.outers
	if start.done {
		l.finish(l.endOf(step{complete: true}))
		return
	}
	l.run()
}

func (l *loop) run() {
	for {
		if l.requests+l.rn.tokenRequests >= l.rn.maxPages {
			l.finish(ingest.Completeness{Reason: l.pagesReason()})
			return
		}
		if err := l.ctx.Err(); err != nil {
			l.yield(ingest.Page{}, err)
			return
		}
		if !l.fetch() {
			return
		}
	}
}

// pagesReason says why a read ends at max_pages. The requests for tokens are among the requests max_pages counts, so the reason
// says how many there were when the source gave any.
func (l *loop) pagesReason() string {
	limit := strconv.Itoa(l.rn.maxPages)
	if n := l.rn.tokenRequests; n > 0 {
		return "the source has more than " + limit + " pages (max_pages), the most one read fetches, and " + strconv.Itoa(n) + " of those requests asked for a token; the rest was not read"
	}
	return "the source has more than " + limit + " pages (max_pages), the most one read fetches; the rest was not read"
}

// fetch sends the request for the position, reads the records of the answer onto the pages as they come and finds where
// the read goes on. It tells whether it does.
func (l *loop) fetch() bool {
	a, err := l.rn.data(l.ctx, l.ds, l.at)
	if err != nil {
		l.dataFailed(err)
		return false
	}
	l.requests++
	end := &respEnd{}
	l.cells = 0
	w, err := a.records(l.ctx, l.skip, func(it item) bool {
		it.missing, it.outers = it.missing+a.missing, it.outers+a.outers
		return l.feed(it, end)
	})
	switch {
	case l.over:
		l.fail(l.rn.answerError(l.ctx, errRowsTooBig))
		return false
	case err != nil:
		l.dataFailed(err)
		return false
	}
	if w.stopped {
		if l.early {
			l.finish(ingest.Completeness{Reason: reasonFirstOnly})
		}
		l.halt = true
		return false
	}
	l.missing += w.missing
	l.outers += w.outers
	l.cut, l.noLines = l.cut || w.cut, l.noLines || w.noLines
	return l.take(w, end)
}

// take finds where the read goes on after an answer that was read to its end. It tells whether it does.
func (l *loop) take(w *walked, end *respEnd) bool {
	if l.haveLast && w.sum == l.last {
		l.finish(ingest.Completeness{Reason: "the source gave the same answer twice in a row, so the parameters of the pages are not read by it; the rest was not read"})
		return false
	}
	l.last, l.haveLast = w.sum, true
	sp, err := l.rn.pager.advance(l.at, w.reply, w.doc, w.n, l.seen+w.n)
	if err != nil {
		l.fail(l.rn.pageValueError(l.ctx, err))
		return false
	}
	end.n, end.sp, end.done, end.count = w.n, sp, true, arrayCount{l.missing, l.outers}
	l.at, l.skip, l.seen = sp.next, 0, l.seen+w.n
	switch {
	case !sp.more && l.kid != nil && sp.complete:
		l.unitDone = true // the next unit is read, and the dataset ends when the last one is (child.go)
	case !sp.more:
		l.finish(l.endOf(sp))
	case l.rn.firstOnly:
		l.checkEnded()
	default:
		return true
	}
	return false
}

// endOf is how the dataset ends when the pages are over. A read that stopped short, or that had a record it could not read in a
// response of JSON lines, is not complete. One that did not see all there is (a record that had no array where records_path opens one,
// unless the definition says that is none, or a body of JSON lines that ended with no mark of its end or broke) is read as what
// was seen. Else it is complete as the pages say.
func (l *loop) endOf(sp step) ingest.Completeness {
	switch {
	case !sp.complete:
		return ingest.Completeness{Reason: sp.reason}
	case l.bad > 0:
		return ingest.Completeness{Reason: strconv.Itoa(l.bad) + " records of the answer could not be read (not an object, or with no natural key), and the lines of an export are not read again, so the dataset is not complete"}
	}
	if end, ok := l.seenOnly(); ok {
		return end
	}
	return ingest.Completeness{Complete: true}
}

// seenOnly says whether the read did not see all there is, and so ends as what was seen (see endOf).
func (l *loop) seenOnly() (ingest.Completeness, bool) {
	switch {
	case l.missing > 0 && l.missing == l.outers:
		// Not one record has an array: with missing_array empty that would be a dataset of no records that is complete, and every
		// object of it would be closed. A records_path that is wrong reads the same, so it is read as what was seen.
		return ingest.Completeness{SeenOnly: true, Reason: "none of the " + strconv.Itoa(l.outers) + " records had an array (or null) where records_path opens one; check the records_path"}, true
	case l.missing > 0 && !l.ds.MissingIsEmpty():
		return ingest.Completeness{SeenOnly: true, Reason: strconv.Itoa(l.missing) + " records had no array (or null) where records_path opens one, so what they may have had was not seen"}, true
	case l.noLines:
		return ingest.Completeness{SeenOnly: true, Reason: "the answer has no value at all: an export with no record and one that was not sent cannot be told apart, so nothing is taken as seen"}, true
	case l.cut:
		return ingest.Completeness{SeenOnly: true, Reason: "the answer ended with no mark of its end, or broke, so the lines that were read to their end are what was seen"}, true
	case l.recs > 0 && l.rows == 0 && (l.lines || l.ds.OpensArray() || l.kid != nil):
		return ingest.Completeness{SeenOnly: true, Reason: strconv.Itoa(l.recs) + " records were read and none made a row (all were passed over, as a row with no natural key is when the definition says so), so the answer may not be what the definition reads"}, true
	}
	return ingest.Completeness{}, false
}

// feed puts a record of the response on the pages. The values of the open secrets are taken out of it first, and the token
// of the position after it is made only if a page is cut there. It tells whether to go on.
func (l *loop) feed(it item, end *respEnd) bool {
	table := l.table(it)
	if l.lines {
		l.bad += len(table.Unreadable)
	}
	l.recs += table.Received() + skipped(table)
	l.rows += len(table.Rows)
	if l.cells += ingest.CellBytes(table); ingest.RowsOver(l.cells, l.rn.set.maxBytes, 0) {
		l.over = true
		return false
	}
	l.kid.matched(it)
	l.kid.read(it, table)
	var after func() []byte
	if l.rn.pager.resumable() && !l.lines && l.kid == nil {
		after = l.mint // a child dataset gives no token: a read of it starts again from its first value (ADR-0111)
	}
	ok := l.pw.AddLazy(l.ctx, table, after, l.yield)
	l.tail = tail{i: it.index, at: l.at, seen: l.seen, end: end, mid: !it.last, count: l.countAt(it)}
	if l.rn.firstOnly && l.lines && it.last && it.index+1 >= checkLines {
		l.early = true
		return false
	}
	return ok
}

// table makes the Table of one record of the response, with the words that say where it was.
func (l *loop) table(it item) ingest.Table {
	where := "page " + strconv.Itoa(l.requests) + ", record " + strconv.Itoa(it.index+1)
	if l.lines {
		where = "line " + strconv.Itoa(it.index+1)
	}
	line := l.seen + it.index + 1
	if !it.expanded {
		return l.rb.Record(l.rn.cleanValue(it.rec), line, where)
	}
	if it.elem == 0 {
		l.rn.cleanValue(it.outer) // the record is cleaned once, and the elements are in it
	}
	return l.rb.Element(it.outer, it.rec, line, where+", element "+strconv.Itoa(it.elem+1))
}

// tokenOfTail is the token of the position after the record added last, made when a page ends with it. A record in the
// middle of a response is followed by the next of it; the last is followed by the next response. The last record of the
// last response ends no page that is cut, so no token is made for it. An element that is not the last of its record is followed by
// the rest of the record, and a read that goes on from there starts at the record again (the order of the elements of a record that
// is sent again is not known, so none of them is passed over).
func (l *loop) tokenOfTail() []byte {
	l.rn.tokens++
	t := l.tail
	switch {
	case t.mid:
		return l.rn.safeToken(tokenAt(l.fp, t.at, t.i, t.seen, t.count), t.at.cursor)
	case !t.end.done || t.i < t.end.n-1:
		return l.rn.safeToken(tokenAt(l.fp, t.at, t.i+1, t.seen, t.count), t.at.cursor)
	case t.end.sp.more:
		return l.rn.safeToken(tokenAt(l.fp, t.end.sp.next, 0, t.seen+t.end.n, t.end.count), t.end.sp.next.cursor)
	}
	return nil
}

func (l *loop) finish(end ingest.Completeness) {
	l.halt = true
	if !end.Complete {
		l.rn.emit(ingest.Event{Kind: ingest.EventIncomplete, Reason: end.Reason})
	}
	l.pw.Finish(l.yield, end)
}

// fail ends the read with an error. The error of the caller's context is not a failure of the source.
func (l *loop) fail(err error) {
	l.halt = true
	if se, ok := ingest.SourceErrorOf(err); ok {
		l.rn.emit(ingest.Event{Kind: ingest.EventFailed, Class: se.Class, ErrorKind: se.Kind})
	}
	l.yield(ingest.Page{}, err)
}

// dataFailed ends the read of a request that failed. A limit that was reached is not a failure of the source: the dataset ends
// not complete. A 404 for the value of a child is passed over when the definition says on_not_found skip, and is a dataset that is
// not complete when it does not (child.go).
func (l *loop) dataFailed(err error) {
	if le, ok := limitOf(err); ok {
		l.finish(ingest.Completeness{Reason: le.reason})
		return
	}
	if l.kid != nil && errors.Is(err, errNotFound) {
		l.kid.notFound(l)
		return
	}
	l.fail(err)
}

// countAt is how many records before the position after the record it was given had no array, and had their array looked for. A
// record that has more elements after this one is read again from its start, so it is not counted.
func (l *loop) countAt(it item) arrayCount {
	c := arrayCount{missing: l.missing + it.missing, outers: l.outers + it.outers}
	if !it.last && c.outers > 0 {
		c.outers--
	}
	return c
}

// skipped is how many records of a table were passed over by a rule of the definition (a row with no key, when it says so).
func skipped(t ingest.Table) int {
	n := 0
	for _, c := range t.Skipped {
		n += c
	}
	return n
}
