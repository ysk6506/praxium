package httpsource

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The answer to a request for data is read as a stream (ADR-0097): the body is read token by token, the records are
// taken out of it one at a time and handed on, and the values the pagination reads (the total, the next cursor, the next
// address) are kept wherever they are, before the records or after them. What is held at any time is the record being
// read and what the pages hold, and it does not depend on how big the answer is.
//
// The reader the decoder takes bytes from (window) keeps the decoder from holding more than one record, and counts the
// bytes against max_response_bytes. A value of the answer that is bigger than a record is not read.

const (
	// recordMaxBytes is the most one record may be, as the source wrote it. It is not a field of the definition.
	recordMaxBytes = 2 << 20
	// windowSlack is what the decoder may hold beyond a record: the bytes it read ahead.
	windowSlack = 64 << 10
	// readChunk is the most the decoder is given in one read, so that it never reads far past what it needs.
	readChunk = 16 << 10
	// maxDepth is how deep the parts of the answer that are not records may nest.
	maxDepth = 512
)

var errRecordTooBig = errors.New("a record is larger than the limit")

// formatError is a failure of the shape of an answer, with the words for it.
type formatError struct{ words string }

func (e *formatError) Error() string { return e.words }

var (
	errNotJSON     = &formatError{"the body of the answer is not one JSON value"}
	errNotArray    = &formatError{"the records are not an array; records_path may be missing or wrong"}
	errNoRecords   = &formatError{"records_path leads to nothing in the answer"}
	errTwice       = &formatError{"records_path leads to the records twice in the answer"}
	errTooDeep     = &formatError{"the answer nests deeper than " + strconv.Itoa(maxDepth) + " levels outside its records"}
	errRecordsPath = &formatError{"records_path is not a path"}
	errArrayKind   = &formatError{"a record has a value that is not an array where records_path opens an array in it"}
)

// item is a record given to the one who reads the answer. For a records_path with [*] it is an element of the array of a
// record, rec being the element and outer the record it is in; otherwise rec is the record. index is the place of the record
// (the outer one) in the answer, elem the place of the element in its array, and last says that no element of the same record
// comes after it, so that a read that goes on from here starts at the next record.
type item struct {
	rec, outer any
	index      int
	elem       int
	last       bool
	expanded   bool
}

// window is the reader the decoder reads the body through. It hashes what it gives (the same answer twice is found by it),
// counts it against the limit, and refuses to let the decoder hold more than one record.
type window struct {
	src io.Reader
	sum hash.Hash
	// read is how many bytes came from src; floor is where the decoder is (bytes before it are done with).
	read, floor int
	limit       int
	// err is why the window stopped giving bytes; srcErr is the error src itself gave, a failure of the connection.
	err, srcErr error
}

func (w *window) Read(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if w.read-w.floor > recordMaxBytes+windowSlack {
		w.err = errRecordTooBig
		return 0, w.err
	}
	n, err := w.src.Read(p[:min(len(p), readChunk, w.limit+1-w.read)])
	w.sum.Write(p[:n])
	w.read += n
	if w.read > w.limit {
		w.err = errTooLarge
		return 0, w.err
	}
	if err != nil && !errors.Is(err, io.EOF) {
		w.err, w.srcErr = err, err
	}
	return n, err
}

// walked is what reading an answer gave besides the records: how many there were, what the pagination reads (as a
// document that has only those places), a hash of the whole body, and how far the answer was found to be what it should be.
type walked struct {
	reply *reply
	n     int
	bytes int
	doc   map[string]any
	sum   kernel.Hash
	// valid says the answer was JSON as far as it was read, found says the records were found, and stopped that the one
	// who was given the records left the loop, so the answer was not read to its end.
	valid, found, stopped bool
	// missing is how many records had no array (or null) where the records_path opens one, and cut says that the body ended without
	// a mark of its end or broke, so that what was given is what was seen (a body of JSON lines; lines.go).
	missing int
	cut     bool
	srcErr  error
}

// frame is an open object or array of the document the walker is in.
type frame struct {
	arr     bool
	key     string // the key whose value is read now (an object)
	haveKey bool
	idx     int // the place of the value read now (an array)
}

// walker reads one answer.
type walker struct {
	dec    *json.Decoder
	win    *window
	rp     ingest.RecordsPath
	recs   ingest.Path
	limit  int
	caps   []ingest.Path
	skip   int
	each   func(it item) bool
	out    walked
	stack  []frame
	done   bool
	depths []int
}

// walk reads the body of an answer: the records go to each one at a time, from the one at place skip on (the ones before
// were given by an answer that broke), and the places the pagination reads are kept. each tells whether to go on.
func walk(body io.Reader, limit int, recordsPath string, caps []ingest.Path, skip int, each func(it item) bool) (*walked, error) {
	return walkRecords(body, limit, recordSpec{path: recordsPath, maxElements: ingest.DefaultMaxElements, caps: caps, skip: skip}, each)
}

// recordSpec is what walkRecords reads: the records_path, the most elements of a record, the places the pagination reads, and
// the records to leave out at the front.
type recordSpec struct {
	path        string
	maxElements int
	caps        []ingest.Path
	skip        int
}

// walkRecords is walk with the most elements one record may have (for a records_path that opens an array in each record).
func walkRecords(body io.Reader, limit int, spec recordSpec, each func(it item) bool) (*walked, error) {
	caps, skip, maxElements := spec.caps, spec.skip, spec.maxElements
	rp, err := ingest.ParseRecordsPath(spec.path)
	if err != nil {
		return &walked{}, errRecordsPath
	}
	win := &window{src: body, sum: sha256.New(), limit: limit}
	dec := json.NewDecoder(win)
	dec.UseNumber()
	w := &walker{dec: dec, win: win, rp: rp, recs: rp.Outer, limit: maxElements, caps: caps, skip: skip, each: each, out: walked{doc: map[string]any{}}}
	w.depths = append(w.depths, len(w.recs))
	for _, c := range caps {
		w.depths = append(w.depths, len(c))
	}
	err = w.run()
	copy(w.out.sum[:], win.sum.Sum(nil))
	w.out.bytes = win.read
	switch {
	case err == nil:
		return &w.out, nil
	case win.srcErr != nil:
		return &w.out, win.srcErr
	case win.err != nil:
		return &w.out, win.err
	case errors.Is(err, errRecordTooBig):
		return &w.out, err
	}
	var fe *formatError
	if errors.As(err, &fe) {
		return &w.out, err
	}
	return &w.out, errNotJSON
}

func (w *walker) run() error {
	for !w.done {
		var err error
		switch top := w.top(); {
		case top == nil:
			err = w.readValue()
		case !top.arr && !top.haveKey:
			err = w.readKey(top)
		case top.arr && !w.dec.More():
			err = w.closeArray()
		default:
			err = w.readValue()
		}
		if err != nil {
			return err
		}
		w.win.floor = int(w.dec.InputOffset())
		if w.out.stopped {
			return nil
		}
	}
	return w.finish()
}

func (w *walker) top() *frame {
	if len(w.stack) == 0 {
		return nil
	}
	return &w.stack[len(w.stack)-1]
}

// valueDone says that the value that was being read is read.
func (w *walker) valueDone() {
	switch top := w.top(); {
	case top == nil:
		w.done = true
	case top.arr:
		top.idx++
	default:
		top.haveKey = false
	}
}

func (w *walker) readKey(top *frame) error {
	tok, err := w.dec.Token()
	if err != nil {
		return err
	}
	if key, ok := tok.(string); ok {
		top.key, top.haveKey = key, true
		return nil
	}
	w.stack = w.stack[:len(w.stack)-1]
	w.valueDone()
	return nil
}

func (w *walker) closeArray() error {
	if _, err := w.dec.Token(); err != nil {
		return err
	}
	w.stack = w.stack[:len(w.stack)-1]
	w.valueDone()
	return nil
}

func (w *walker) readValue() error {
	if w.interesting() && w.matches(w.recs) {
		return w.readRecords()
	}
	tok, err := w.dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); ok {
		if len(w.stack) >= maxDepth {
			return errTooDeep
		}
		w.stack = append(w.stack, frame{arr: d == '['})
		return nil
	}
	w.scalar(tok)
	w.valueDone()
	return nil
}

// interesting says whether a place this deep is one the walker looks for something at.
func (w *walker) interesting() bool {
	for _, d := range w.depths {
		if d == len(w.stack) {
			return true
		}
	}
	return false
}

// matches says whether the value being read is at the path. A step is a key of an object or a place in an array.
func (w *walker) matches(p ingest.Path) bool {
	if len(p) != len(w.stack) {
		return false
	}
	for i, f := range w.stack {
		if f.arr {
			if n, err := strconv.Atoi(p[i]); err != nil || n != f.idx {
				return false
			}
		} else if p[i] != f.key {
			return false
		}
	}
	return true
}

// scalar keeps a value the pagination reads. It keeps what Path.Pick would give: a text that is not empty, a number,
// a boolean.
func (w *walker) scalar(tok json.Token) {
	if !w.interesting() {
		return
	}
	var text string
	switch v := tok.(type) {
	case string:
		text = v
	case json.Number:
		text = v.String()
	case bool:
		text = strconv.FormatBool(v)
	}
	if text == "" {
		return
	}
	for _, c := range w.caps {
		if w.matches(c) {
			setPath(w.out.doc, c, text)
		}
	}
}

// setPath puts a text at a path of a document that is made of objects only (Path.Lookup follows a step that is a number
// into an object as the key it is).
func setPath(doc map[string]any, p ingest.Path, text string) {
	for _, step := range p[:len(p)-1] {
		next, ok := doc[step].(map[string]any)
		if !ok {
			next = map[string]any{}
			doc[step] = next
		}
		doc = next
	}
	doc[p[len(p)-1]] = text
}

// readRecords reads the value at records_path: an array, whose records are given on one at a time, or null, which is none.
func (w *walker) readRecords() error {
	if w.out.found {
		return errTwice
	}
	tok, err := w.dec.Token()
	if err != nil {
		return err
	}
	w.out.valid = true
	switch v := tok.(type) {
	case json.Delim:
		if v == '[' {
			w.out.found = true
			return w.records()
		}
	case nil:
		if len(w.recs) > 0 {
			w.out.found = true
			w.valueDone()
			return nil
		}
	}
	return errNotArray
}

// records gives the records of the array that has been opened, and reads its end.
func (w *walker) records() error {
	for idx := 0; w.dec.More(); idx++ {
		more, err := w.record(idx)
		if err != nil {
			return err
		}
		if !more {
			w.out.stopped = true
			return nil
		}
	}
	if _, err := w.dec.Token(); err != nil {
		return err
	}
	w.valueDone()
	return nil
}

// record reads the record at idx and gives it on, unless it is one that was given already. It tells whether to go on.
func (w *walker) record(idx int) (bool, error) {
	start := w.dec.InputOffset()
	var rec any
	var skipped json.RawMessage
	var into any = &rec
	if idx < w.skip {
		into = &skipped
	}
	if err := w.dec.Decode(into); err != nil {
		return false, err
	}
	end := w.dec.InputOffset()
	w.win.floor = int(end)
	w.out.n++
	if end-start > recordMaxBytes {
		return false, errRecordTooBig
	}
	if idx < w.skip {
		return true, nil
	}
	w.keepFrom(idx, rec)
	if !w.rp.Expand {
		return w.each(item{rec: rec, index: idx, last: true}), nil
	}
	return w.elements(idx, rec)
}

// elements gives the elements of the array a record has to each, one at a time (see giveElements).
func (w *walker) elements(idx int, outer any) (bool, error) {
	return giveElements(w.rp, outer, idx, w.limit, &w.out.missing, w.each)
}

// giveElements gives the elements of the array the record outer has at the place records_path opens one to each. A record that has
// no array (or null) gives none and is counted in missing; a value that is no array, or more elements than the limit, is a failure
// of the shape of the answer. It tells whether to go on.
func giveElements(rp ingest.RecordsPath, outer any, idx, limit int, missing *int, each func(it item) bool) (bool, error) {
	elems, state := rp.Elements(outer)
	switch {
	case state == ingest.ArrayMissing:
		*missing++
		return true, nil
	case state == ingest.ArrayWrongKind:
		return false, errArrayKind
	case len(elems) > limit:
		return false, &formatError{"a record has more than " + strconv.Itoa(limit) + " elements in the array records_path opens (max_elements)"}
	}
	for j, el := range elems {
		if !each(item{rec: el, outer: outer, index: idx, elem: j, last: j == len(elems)-1, expanded: true}) {
			return false, nil
		}
	}
	return true, nil
}

// keepFrom keeps the values the pagination reads that are inside the record at idx (a path that goes down through the
// records, which is no common thing to write, reads the same as it does in a document that is read whole).
func (w *walker) keepFrom(idx int, rec any) {
	for _, c := range w.caps {
		if len(c) <= len(w.recs) || !pathStartsWith(c, w.recs) {
			continue
		}
		if n, err := strconv.Atoi(c[len(w.recs)]); err != nil || n != idx {
			continue
		}
		if text, ok := ingest.Path(c[len(w.recs)+1:]).Pick(rec); ok {
			setPath(w.out.doc, c, text)
		}
	}
}

func pathStartsWith(p, prefix ingest.Path) bool {
	if len(p) < len(prefix) {
		return false
	}
	for i := range prefix {
		if p[i] != prefix[i] {
			return false
		}
	}
	return true
}

// finish reads what follows the one value, which is nothing, and sees that the records were found.
func (w *walker) finish() error {
	if _, err := w.dec.Token(); !errors.Is(err, io.EOF) {
		return errNotJSON
	}
	w.out.valid = true
	if !w.out.found {
		return errNoRecords
	}
	return nil
}
