package httpsource

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// scan is what reading the parent gave: the values (each is the values of one record, a type letter before each value, apart by
// \x1f), how many records had a value that is not sent, and why the read of the child must not go on when it must not.
type scan struct {
	vals    []string
	seen    map[string]struct{}
	bytes   int
	invalid int
	// reason is why the child is not read at all (the parent is not complete, has too many values, or a limit was reached); it is
	// the reason of a dataset that is not complete. seenNote says that the parent was read as what was seen. partial says that the
	// parent was read for the first values only (a check or a preview).
	reason, seenNote string
	partial          bool
}

// parentValueOverhead is what a value of a parent costs besides its bytes, counted for each value so that the limit of bytes is the
// memory held and not only the text: the text is held once and named twice (as the key of the map that drops repeats and in the
// slice that keeps the order), each with a header of 16 bytes, and the entry of the map costs about 64 bytes more (its buckets and
// its flags), so 2*16 for the headers and 64 for the entry.
const parentValueOverhead = 96

var intText = regexp.MustCompile(`^-?[0-9]+$`)

// pickTyped reads a value of a record of the parent: a text that is not empty, or an integer (digits, with a sign). Anything else
// (an object, a boolean, 1.0, 1e3) is not a value that is sent.
func pickTyped(cp ingest.ColumnPath, rec, outer any) (string, bool, bool) {
	src := rec
	if cp.Up {
		src = outer
	}
	v, found := cp.Path.Lookup(src)
	if !found {
		return "", false, false
	}
	switch x := v.(type) {
	case string:
		return x, false, x != ""
	case json.Number:
		return x.String(), true, intText.MatchString(x.String())
	}
	return "", false, false
}

// validParentText says whether a value of the parent may be sent: printable ASCII only, up to 256 bytes, and nothing that a
// path, a query or a body could read as more than one value or another place (a step that goes up, a slash, a backslash, an
// escaped one of those, the separator of a batch).
func validParentText(s, sep string) bool {
	if s == "" || len(s) > ingest.ParentValueMax || s != strings.TrimSpace(s) {
		return false
	}
	// A step that goes up is not sent however a server reads what is after a ';' (a parameter of the path).
	if head, _, _ := strings.Cut(s, ";"); head == "." || head == ".." {
		return false
	}
	return printableStep(s) && !encodedSeparator(s) && (sep == "" || !strings.Contains(s, sep))
}

// printableStep tells whether a text is printable ASCII with no slash and no backslash, so that it is one step of a path.
func printableStep(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c > 0x7e || c == '/' || c == '\\' {
			return false
		}
	}
	return true
}

// encodedSeparator tells whether a text has the percent-encoded form of a slash, a dot or a backslash.
func encodedSeparator(s string) bool {
	low := strings.ToLower(s)
	for _, bad := range []string{"%2f", "%2e", "%5c"} {
		if strings.Contains(low, bad) {
			return true
		}
	}
	return false
}

// tupleKey is the values of a record of the parent, each with its kind (a text or an integer), joined; it says false when one of
// them is not a value that is sent.
func tupleKey(names []string, paths map[string]ingest.ColumnPath, rec, outer any, sep string) (string, bool) {
	tuple := make([]string, len(names))
	for i, n := range names {
		text, num, ok := pickTyped(paths[n], rec, outer)
		if !ok || !validParentText(text, sep) {
			return "", false
		}
		kind := "s"
		if num {
			kind = "i"
		}
		tuple[i] = kind + text
	}
	return strings.Join(tuple, "\x1f"), true
}

// scanParent reads the parent to its end, through its own pagination and its own definition, and keeps the values of its records
// (from the records with the values of the secrets taken out). It holds only the values, without repeats: at most caps of them and
// ParentBytes bytes; more is a child that is not read. need is how many values a check or a preview needs (0 for all).
func (rn *run) scanParent(ctx context.Context, ds, parent ingest.Dataset, sep string, need, caps int) (*scan, error) {
	paths, err := ds.ParentValuePaths(parent)
	if err != nil {
		return nil, rn.fail(ingest.SrcInternal, "the values of the parent are not paths of its records", nil)
	}
	names := sortedValueNames(ds)
	saved, savedMax := rn.pager, rn.maxPages
	rn.pager = newPager(parent)
	rn.maxPages = orDefault(rn.pager.pg.MaxPages, ingest.DefaultMaxPages)
	defer func() { rn.pager, rn.maxPages, rn.unit = saved, savedMax, nil }()
	rn.unit = nil
	sc := &scan{seen: map[string]struct{}{}}
	add := func(it item) bool {
		rec := rn.cleanValue(it.rec)
		if it.expanded && it.elem == 0 {
			rn.cleanValue(it.outer)
		}
		key, ok := tupleKey(names, paths, rec, it.outer, sep)
		if !ok {
			sc.invalid++
			return true
		}
		if _, dup := sc.seen[key]; dup {
			return true
		}
		if need == 0 && (len(sc.vals) >= caps || sc.bytes+len(key)+parentValueOverhead > rn.r.ceil.ParentBytes) {
			sc.reason = "the parent has more distinct values than " + strconv.Itoa(caps) + " (parent.max_requests times the size of a batch, at most " + strconv.Itoa(rn.r.ceil.ParentValues) + ") or more than " + strconv.Itoa(rn.r.ceil.ParentBytes) + " bytes of them, so no request of the dataset was made"
			return false
		}
		sc.seen[key] = struct{}{}
		sc.vals = append(sc.vals, key)
		sc.bytes += len(key) + parentValueOverhead
		rn.r.bud.notePeak(len(sc.vals))
		if need > 0 && len(sc.vals) >= need {
			sc.partial = true
			return false
		}
		return true
	}
	return sc, rn.walkParent(ctx, parent, sc, add)
}

// walkParent reads the pages of the parent and gives the records to add. A limit, or a parent that is not complete, is the reason
// of the scan, and an error that is not one is the error of the read.
func (rn *run) walkParent(ctx context.Context, parent ingest.Dataset, sc *scan, add func(it item) bool) error {
	st := &pwalk{at: rn.pager.first()}
	for {
		if st.requests >= rn.maxPages {
			sc.reason = "the parent has more than " + strconv.Itoa(rn.maxPages) + " pages (max_pages), so its values were not all read and no request of the dataset was made"
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		a, err := rn.data(ctx, parent, st.at)
		if err != nil {
			return rn.scanFailed(sc, err)
		}
		st.requests++
		w, err := a.records(ctx, 0, add)
		if err != nil {
			return rn.scanFailed(sc, err)
		}
		if sc.reason != "" || w.stopped {
			return nil
		}
		more, err := rn.parentNext(ctx, sc, st, w)
		if err != nil {
			return err
		}
		if !more {
			break
		}
	}
	if sc.reason == "" {
		sc.seenNote = parentSeenNote(parent, st.missing, st.outers, st.cut, st.noLines)
	}
	return nil
}

// pwalk is where the read of the pages of the parent is, and what the pages told that the end of the read says.
type pwalk struct {
	at                     pos
	requests, seen         int
	last                   kernel.Hash
	haveLast, cut, noLines bool
	missing, outers        int
}

// parentNext takes what a page of the parent told and says whether there is a next page. It gives a reason to the scan when the
// parent is not read to its end.
func (rn *run) parentNext(ctx context.Context, sc *scan, st *pwalk, w *walked) (bool, error) {
	st.missing, st.outers, st.cut, st.noLines = st.missing+w.missing, st.outers+w.outers, st.cut || w.cut, st.noLines || w.noLines
	if st.haveLast && w.sum == st.last {
		sc.reason = "the parent gave the same answer twice in a row, so the parameters of its pages are not read by it; no request of the dataset was made"
		return false, nil
	}
	st.last, st.haveLast = w.sum, true
	sp, err := rn.pager.advance(st.at, w.reply, w.doc, w.n, st.seen+w.n)
	if err != nil {
		return false, rn.pageValueError(ctx, err)
	}
	st.seen, st.at = st.seen+w.n, sp.next
	if !sp.more && !sp.complete {
		sc.reason = "the parent was not read to its end (" + sp.reason + "), so no request of the dataset was made"
	}
	return sp.more, nil
}

// scanFailed says what a failure of reading the parent is: a limit, or a 404 of the parent, is a dataset that is not complete, and
// any other is the error of the read.
func (rn *run) scanFailed(sc *scan, err error) error {
	if le, ok := limitOf(err); ok {
		sc.reason = le.reason
		return nil
	}
	if isNotFound(err) {
		sc.reason = "the parent answered 404, so no request of the dataset was made"
		return nil
	}
	return err
}

// parentSeenNote says why the parent was read as what was seen and not as all there is, or "" when it was all.
func parentSeenNote(parent ingest.Dataset, missing, outers int, cut, noLines bool) string {
	switch {
	case missing > 0 && missing == outers:
		return "none of its records had an array where records_path opens one"
	case missing > 0 && !parent.MissingIsEmpty():
		return strconv.Itoa(missing) + " of its records had no array where records_path opens one"
	case noLines:
		return "its answer had no value at all"
	case cut:
		return "its answer ended with no mark of its end, or broke"
	}
	return ""
}
