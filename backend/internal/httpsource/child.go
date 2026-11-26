package httpsource

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// The reading of a dataset that asks with the values of another dataset (ADR-0111). It is done in two steps, so that no request of the
// child is sent while the body of the parent is open: the parent is read to its end first, and only its values are kept (without
// repeats, within the limits of how many and how big); then the child is read, one unit (a value, or a batch of values) after the
// other, each with its own pages. The requests the parent and the child make are counted together against the limits of the child
// and of the run (limits.go, run.charge).

// previewChildRequests and checkChildRequests are how many requests of the child a preview and a check make.
const (
	previewChildRequests = 10
	checkChildRequests   = 1
)

// Notes of a check and a preview of a child, which read the first values of the parent only.
const (
	noteFirstValues = "only the first value of the parent was used"
	notePreviewNote = "only the first values of the parent were used, and at most 10 requests of the dataset were made"
)

// kid is what the read of a child counts besides what a loop does: the values that were passed over, the values of a batch that
// had no record in the answer, and the values of the unit being read.
type kid struct {
	ds       ingest.Dataset
	match    ingest.ColumnPath
	hasMatch bool
	u        *unit
	// total is the values of the parent, those used and those passed over; invalid is how many were passed over before a request
	// (a value the product does not send), skipped how many after a 404 (on_not_found skip); unmatched how many of a batch were not
	// in the answer; unread how many answers that were one object made no row (the object had no natural key, as an error that is
	// answered with a 200 has none), which are values of the parent that were not read, as a 404 passed over is.
	total, invalid, skipped, unmatched, unread int
	parentNote                                 string
}

// read notes an answer that is one object and made no row.
func (k *kid) read(it item, t ingest.Table) {
	if k != nil && it.whole && len(t.Rows) == 0 {
		k.unread++
	}
}

// matched notes the value a record of the answer of a batch is for.
func (k *kid) matched(it item) {
	if k == nil || !k.hasMatch || k.u == nil || !k.u.batch {
		return
	}
	if t, ok := k.match.Pick(it.rec, it.outer); ok {
		k.u.seen[t] = true
	}
}

// notFound is what a 404 for the unit is: passed over when the definition says skip (never for a batch), else the dataset is not
// complete.
func (k *kid) notFound(l *loop) {
	if l.rn.firstOnly { // a check read one value, and its 404 is a path that may be wrong, whatever the definition says of a 404
		l.fail(l.rn.fail(ingest.SrcRequestRejected, "the detail of the first value of the parent answered 404; check the path of the request (on_not_found skip passes over a value that is gone, and a check has no other value to try)", nil))
		return
	}
	if k.ds.OnNotFound == ingest.OnNotFoundSkip && !k.u.batch {
		k.skipped++
		l.unitDone = true
		return
	}
	reason := "the source answered 404 for a value of the parent (source.child_not_found), and the dataset does not say on_not_found skip"
	if k.u.batch {
		reason = "the source answered 404 for a batch of values (source.child_not_found); a batch that is not found is never passed over"
	}
	l.finish(ingest.Completeness{Reason: reason})
}

// unitEnded counts the values of a batch that no record of the answer was for.
func (k *kid) unitEnded() {
	if !k.u.batch || !k.hasMatch {
		return
	}
	for _, r := range k.u.rows {
		if !k.u.seen[r[0].text] {
			k.unmatched++
		}
	}
}

// end is how the child ends when all its units were read. A value passed over, a value of a batch with no record and a parent that
// was read as what was seen make it read as what was seen, none of them closes anything; passed over values that are more than 5%
// of the values of the parent and more than three make it not complete (a path that is wrong, or an account that may not read, must
// not look like a few things that are gone). More than three values that all had no record make it read as what was seen too.
func (k *kid) end(l *loop) ingest.Completeness {
	end := l.endOf(step{complete: true})
	if !end.Complete && !end.SeenOnly {
		return end
	}
	over := k.invalid + k.skipped + k.unread
	if over > 3 && over*20 > k.total {
		return ingest.Completeness{Reason: strconv.Itoa(over) + " of " + strconv.Itoa(k.total) + " distinct values of the parent were not read (a 404 with on_not_found skip, a value that is not sent: source.parent_value_invalid, or an answer with no natural key), which is more than 5% and more than 3, so the path or the rights may be wrong"}
	}
	var notes []string
	if end.SeenOnly {
		notes = append(notes, end.Reason)
	}
	// More than three values and not one record in any answer: a path that is wrong or an account that may not read looks like this
	// (every answer is empty, as a 200 with null or a 204 is), so it is read as what was seen. A few of the values empty, the others not,
	// is the normal case of a list that is empty for some values, and is complete.
	if end.Complete && k.total > 3 && l.recs == 0 {
		notes = append(notes, "the answers to all "+strconv.Itoa(k.total-k.invalid)+" values of the parent were empty: check the rights or the path of the request")
	}
	if over > 0 {
		notes = append(notes, strconv.Itoa(over)+" values of the parent were not read (a 404 with on_not_found skip, source.parent_value_invalid, or an answer with no natural key), so what they have was not seen")
	}
	if k.unmatched > 0 {
		notes = append(notes, strconv.Itoa(k.unmatched)+" values of a batch had no record in the answer, so what they have was not seen")
	}
	if k.parentNote != "" {
		notes = append(notes, "the parent was read as what was seen: "+k.parentNote)
	}
	if len(notes) == 0 {
		return end
	}
	return ingest.Completeness{SeenOnly: true, Reason: strings.Join(notes, "; ")}
}

// readChild reads the datasets of a child: the parent to its end, then the units.
func (rn *run) readChild(ctx context.Context, ds ingest.Dataset, opts ingest.PageOptions, yield func(ingest.Page, error) bool) {
	rb, err := ingest.NewRowBuilder(ds)
	if err != nil {
		yield(ingest.Page{}, err)
		return
	}
	parent, ok := rn.r.parentOf(ds)
	if !ok {
		yield(ingest.Page{}, ingest.NewSourceError(ingest.SrcInternal, "the parent of the dataset is not a dataset of the source", nil))
		return
	}
	out := func(p ingest.Page, err error) bool {
		if err == nil {
			rn.emit(ingest.Event{Kind: ingest.EventPage, Page: p.No, Rows: p.Received()})
		}
		return yield(p, err)
	}
	opts.Resume = nil // a child is read again from its first value: no token of it holds a place in the values of the parent
	l := &loop{rn: rn, ctx: ctx, ds: ds, rb: rb, pw: ingest.NewPageWriter(ds, opts), yield: out, fp: fingerprint(rn.set, ds), lines: ds.JSONLines()}
	l.mint = l.tokenOfTail
	k := newKid(ds)
	l.kid = k
	size, sep := rn.batchOf(ds)
	maxReq := min(orDefault(ds.Parent.MaxRequests, ingest.DefaultChildRequests), rn.r.ceil.ChildRequests)
	span := min(time.Duration(orDefault(ds.Parent.MaxSeconds, ingest.DefaultChildSeconds))*time.Second, rn.r.ceil.ChildTime)
	rn.child = &childBudget{max: maxReq, deadline: rn.r.cfg.Clock.Now().Add(span)}
	need, caps := rn.valuesNeeded(size), min(rn.r.ceil.ParentValues, maxReq*size)
	sc, err := rn.scanParent(ctx, ds, parent, sep, need, caps)
	if err != nil {
		l.fail(err)
		return
	}
	k.invalid, k.total, k.parentNote = sc.invalid, sc.invalid+len(sc.vals), sc.seenNote
	if sc.invalid > 0 {
		rn.emit(ingest.Event{Kind: ingest.EventSkipped, Rows: sc.invalid, Reason: strconv.Itoa(sc.invalid) + " values of the parent were not used (source.parent_value_invalid)"})
	}
	if sc.reason != "" {
		l.finish(ingest.Completeness{Reason: sc.reason})
		return
	}
	if sc.partial && len(sc.vals) == 0 {
		l.finish(ingest.Completeness{Reason: "the first " + strconv.Itoa(previewParentRequests) + " requests of the parent gave no value, so no request of the dataset was made"})
		return
	}
	rn.readUnits(l, sc.vals, size, sep)
}

// checkEnded ends a check after the first response it reads. A value whose detail is one object with no natural key made no row, which
// is a setting that is wrong (a check says so), not a read that went on.
func (l *loop) checkEnded() {
	if l.kid != nil && l.kid.unread > 0 {
		l.fail(l.rn.fail(ingest.SrcRequestRejected, "the detail of the first value of the parent is one object with no natural key, so it makes no row: check the path of the request and natural_key", nil))
		return
	}
	l.finish(ingest.Completeness{Reason: reasonFirstOnly})
}

// readUnits reads the units, one value or one batch after the other, and ends the read.
func (rn *run) readUnits(l *loop, vals []string, size int, sep string) {
	ds, k := l.ds, l.kid
	names := sortedValueNames(ds)
	for _, chunk := range rn.chunks(ds, vals, size) {
		k.u = &unit{names: names, rows: decodeRows(chunk), batch: ds.Parent.Batch != nil, sep: sep, seen: map[string]bool{}}
		rn.unit = k.u
		l.startUnit()
		l.runUnit()
		if l.halt {
			return
		}
		k.unitEnded()
		if rn.firstOnly {
			l.checkEnded()
			return
		}
	}
	l.finish(k.end(l))
}

// newKid is what the read of a child counts, with the path of the batch that tells which value a record is for.
func newKid(ds ingest.Dataset) *kid {
	k := &kid{ds: ds}
	if ds.Parent.Batch != nil && ds.Parent.Batch.Match != "" {
		if mp, err := ds.MatchPath(); err == nil {
			k.match, k.hasMatch = mp, true
		}
	}
	return k
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// batchOf is how many values go in one request, and the separator of the query, within what the server allows.
func (rn *run) batchOf(ds ingest.Dataset) (int, string) {
	b := ds.Parent.Batch
	if b == nil {
		return 1, ""
	}
	sep := b.Separator
	if sep == "" {
		sep = ","
	}
	return min(orDefault(b.Size, ingest.DefaultBatchSize), rn.r.ceil.BatchSize), sep
}

// valuesNeeded is how many values of the parent a read needs: all of them (0), or for a check and a preview the values of the
// requests they make.
func (rn *run) valuesNeeded(size int) int {
	switch {
	case rn.firstOnly:
		return size * checkChildRequests
	case rn.keep:
		return size * previewChildRequests
	}
	return 0
}

func sortedValueNames(ds ingest.Dataset) []string {
	names := make([]string, 0, len(ds.Parent.Values))
	for k := range ds.Parent.Values {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// startUnit puts the loop at the first page of a unit.
func (l *loop) startUnit() {
	l.at, l.skip, l.seen, l.requests = l.rn.pager.first(), 0, 0, 0
	l.haveLast, l.unitDone = false, false
}

// runUnit reads the pages of one unit. It ends with the loop halted (an error, a limit, the last page) or with the unit done.
func (l *loop) runUnit() {
	for {
		if l.requests >= l.rn.maxPages {
			l.finish(ingest.Completeness{Reason: l.pagesReason()})
			return
		}
		if err := l.ctx.Err(); err != nil {
			l.halt = true
			l.yield(ingest.Page{}, err)
			return
		}
		if !l.fetch() {
			return
		}
	}
}

// chunks cuts the values into the units: one value each, or the values of a batch that fit in the size of a batch and in the
// address and the body a request may have.
func (rn *run) chunks(ds ingest.Dataset, vals []string, size int) [][]string {
	if ds.Parent.Batch == nil {
		out := make([][]string, len(vals))
		for i, v := range vals {
			out[i] = []string{v}
		}
		return out
	}
	room := rn.batchRoom(ds)
	var out [][]string
	var cur []string
	used := 0
	for _, v := range vals {
		cost := 6*len(v) + 8 // a value escaped (3 times as long in an address, 6 in a body where JSON writes < > & as <), and what joins it
		if len(cur) > 0 && (len(cur) >= size || used+cost > room) {
			out, cur, used = append(out, cur), nil, 0
		}
		cur, used = append(cur, v), used+cost
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// batchRoom is how many bytes of values a batch may have: what an address and a body have left after the parts of the request that
// are not the values.
func (rn *run) batchRoom(ds ingest.Dataset) int {
	room := 1 << 30
	req := ds.Request
	inQuery := false
	for _, v := range req.Query {
		inQuery = inQuery || ingest.HasParentVar(v)
	}
	if inQuery {
		fixed := len(rn.origin.String()) + len(req.Path) + 512
		for k, v := range req.Query {
			fixed += len(k) + len(v) + 2
		}
		room = min(room, rn.r.ceil.URLBytes-fixed)
	}
	if req.Body != nil {
		fixed := 512
		if b, err := json.Marshal(req.Body); err == nil {
			fixed += len(b)
		}
		room = min(room, rn.r.ceil.BodyBytes-fixed)
	}
	return max(room, 64)
}

// decodeRows makes the values of a unit from the values the scan kept.
func decodeRows(chunk []string) [][]pval {
	rows := make([][]pval, len(chunk))
	for i, key := range chunk {
		parts := strings.Split(key, "\x1f")
		row := make([]pval, len(parts))
		for j, p := range parts {
			row[j] = pval{text: p[1:], num: p[0] == 'i'}
		}
		rows[i] = row
	}
	return rows
}
