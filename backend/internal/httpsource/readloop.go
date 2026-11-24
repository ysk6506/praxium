package httpsource

import (
	"context"
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
}

// tail is what the token of a record is made of: where the record is in its response, and the response.
type tail struct {
	i    int
	at   pos
	seen int
	end  *respEnd
}

// respEnd is what the end of a response says, which is known when the whole answer is read: how many records it had and
// what comes next. The token of the last record of a response needs it, and the token is made when a page is cut, which
// may be after the next response began.
type respEnd struct {
	done bool
	n    int
	sp   step
}

// read gives the pages of a dataset to yield. It ends with a page that has Last set, or with an error.
func (rn *run) read(ctx context.Context, ds ingest.Dataset, opts ingest.PageOptions, yield func(ingest.Page, error) bool) {
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
	l := &loop{rn: rn, ctx: ctx, ds: ds, rb: rb, pw: ingest.NewPageWriter(ds, opts), yield: out, fp: fp}
	l.mint = l.tokenOfTail
	start := rn.startAt(fp, opts.Resume)
	l.at, l.skip, l.seen = start.at, start.skip, start.base
	if start.done {
		l.finish(ingest.Completeness{Complete: true})
		return
	}
	l.run()
}

func (l *loop) run() {
	for {
		if l.requests+l.rn.tokenRequests >= l.rn.maxPages {
			l.finish(ingest.Completeness{Reason: "the source has more than " + strconv.Itoa(l.rn.maxPages) + " pages (max_pages), the most one read fetches; the rest was not read"})
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

// fetch sends the request for the position, reads the records of the answer onto the pages as they come and finds where
// the read goes on. It tells whether it does.
func (l *loop) fetch() bool {
	a, err := l.rn.data(l.ctx, l.ds, l.at)
	if err != nil {
		l.fail(err)
		return false
	}
	l.requests++
	end := &respEnd{}
	w, err := a.records(l.ctx, l.skip, func(rec any, i int) bool { return l.feed(rec, i, end) })
	if err != nil {
		l.fail(err)
		return false
	}
	if w.stopped {
		return false
	}
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
	end.n, end.sp, end.done = w.n, sp, true
	l.at, l.skip, l.seen = sp.next, 0, l.seen+w.n
	switch {
	case !sp.more:
		l.finish(ingest.Completeness{Complete: sp.complete, Reason: sp.reason})
	case l.rn.firstOnly:
		l.finish(ingest.Completeness{Reason: reasonFirstOnly})
	default:
		return true
	}
	return false
}

// feed puts a record of the response on the pages. The values of the open secrets are taken out of it first, and the token
// of the position after it is made only if a page is cut there. It tells whether to go on.
func (l *loop) feed(rec any, i int, end *respEnd) bool {
	where := "page " + strconv.Itoa(l.requests) + ", record " + strconv.Itoa(i+1)
	table := l.rb.Record(l.rn.cleanValue(rec), l.seen+i+1, where)
	var after func() []byte
	if l.rn.pager.resumable() {
		after = l.mint
	}
	ok := l.pw.AddLazy(l.ctx, table, after, l.yield)
	l.tail = tail{i: i, at: l.at, seen: l.seen, end: end}
	return ok
}

// tokenOfTail is the token of the position after the record added last, made when a page ends with it. A record in the
// middle of a response is followed by the next of it; the last is followed by the next response. The last record of the
// last response ends no page that is cut, so no token is made for it.
func (l *loop) tokenOfTail() []byte {
	l.rn.tokens++
	t := l.tail
	switch {
	case !t.end.done || t.i < t.end.n-1:
		return l.rn.safeToken(tokenAt(l.fp, t.at, t.i+1, t.seen), t.at.cursor)
	case t.end.sp.more:
		return l.rn.safeToken(tokenAt(l.fp, t.end.sp.next, 0, t.seen+t.end.n), t.end.sp.next.cursor)
	}
	return nil
}

func (l *loop) finish(end ingest.Completeness) {
	if !end.Complete {
		l.rn.emit(ingest.Event{Kind: ingest.EventIncomplete, Reason: end.Reason})
	}
	l.pw.Finish(l.yield, end)
}

// fail ends the read with an error. The error of the caller's context is not a failure of the source.
func (l *loop) fail(err error) {
	if se, ok := ingest.SourceErrorOf(err); ok {
		l.rn.emit(ingest.Event{Kind: ingest.EventFailed, Class: se.Class, ErrorKind: se.Kind})
	}
	l.yield(ingest.Page{}, err)
}
