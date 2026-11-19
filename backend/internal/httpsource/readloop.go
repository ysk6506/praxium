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
// pages. It reads in one goroutine and holds one response at a time.
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
		if l.requests >= l.rn.maxPages {
			l.finish(ingest.Completeness{Reason: "the source has more than " + strconv.Itoa(l.rn.maxPages) + " pages (max_pages), the most one read fetches; the rest was not read"})
			return
		}
		if err := l.ctx.Err(); err != nil {
			l.yield(ingest.Page{}, err)
			return
		}
		px, err := l.rn.data(l.ctx, l.ds, l.at)
		if err != nil {
			l.fail(err)
			return
		}
		l.requests++
		if !l.take(px) {
			return
		}
	}
}

// take gives the records of a response to the pages and finds where the read goes on. It tells whether it does.
func (l *loop) take(px *parsed) bool {
	sum := kernel.HashBytes(px.reply.body)
	if l.haveLast && sum == l.last {
		l.finish(ingest.Completeness{Reason: "the source gave the same answer twice in a row, so the parameters of the pages are not read by it; the rest was not read"})
		return false
	}
	l.last, l.haveLast = sum, true
	n := len(px.records)
	sp, err := l.rn.pager.advance(l.at, px.reply, px.doc, n, l.seen+n)
	if err != nil {
		l.fail(l.rn.transportError(l.ctx, err))
		return false
	}
	if !l.feed(px, sp) {
		return false
	}
	l.at, l.skip, l.seen = sp.next, 0, l.seen+n
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

// feed puts the records of a response on the pages, each with the token of the position after it. It tells whether to go on.
func (l *loop) feed(px *parsed, sp step) bool {
	n := len(px.records)
	for i := l.skip; i < n; i++ {
		where := "page " + strconv.Itoa(l.requests) + ", record " + strconv.Itoa(i+1)
		rec := l.rb.Record(px.records[i], l.seen+i+1, where)
		if !l.pw.Add(l.ctx, rec, l.tokenAfter(i, n, sp), l.yield) {
			return false
		}
	}
	return true
}

// tokenAfter is the token of the position after record i of the response that has n.
func (l *loop) tokenAfter(i, n int, sp step) []byte {
	switch {
	case !l.rn.pager.resumable():
		return nil
	case i < n-1:
		return tokenAt(l.fp, l.at, i+1, l.seen)
	case sp.more:
		return tokenAt(l.fp, sp.next, 0, l.seen+n)
	}
	return tokenEnd(l.fp, l.seen+n)
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
