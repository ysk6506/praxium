package httpsource

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// A body of JSON lines (response_format json_lines, ADR-0109) is JSON values one after the other, a record each, separated by an
// end of line or by any white space, as an export that is streamed gives them. It is read through the same window as a body that
// is one JSON value, so the limits are the same: the bytes of the answer, a record of 2 MiB, and the 64 KiB the decoder may hold
// beyond it. There are no pages and no token: the read is one response, and it is not sent again.
//
// What the body shows of its own end is what makes the read whole. A body that is read to its end with a mark of it (a length that
// was read in full, the last chunk) is the whole export. A body that ends with no mark, or breaks in the middle, may have been cut
// where a connection closed: the lines that were read to their end are records, the line that was cut is left out, and the dataset
// is read as what was seen. A value that is not JSON, a line with nothing at records_path, a limit that is passed, are failures:
// the dataset is not used.

// walkLines reads a body of JSON lines. ended says that the end of the body is marked by the protocol. Each record goes to each
// one at a time, and index counts the values of the body from 0.
func walkLines(body io.Reader, limit int, rp ingest.RecordsPath, maxElements int, ended bool, each func(it item) bool) (*walked, error) {
	win := &window{src: body, sum: sha256.New(), limit: limit}
	dec := json.NewDecoder(win)
	dec.UseNumber()
	out := &walked{doc: map[string]any{}}
	err := readLines(lineSrc{dec, win}, rp, maxElements, ended, each, out)
	copy(out.sum[:], win.sum.Sum(nil))
	out.bytes = win.read
	switch {
	case err == nil:
		return out, nil
	case isShapeError(err):
		return out, err
	case win.srcErr != nil:
		out.cut, out.srcErr = true, win.srcErr
		return out, nil
	case win.err != nil:
		return out, win.err
	}
	return out, err
}

// isShapeError tells a failure of the shape of the answer (a value that is not JSON, a record or an array that is not what records_path
// reads, a limit that was passed) from one of the connection. A body that broke and had such a fault is not read as what was seen:
// the fault is the failure (it is not made smaller by the break).
func isShapeError(err error) bool {
	var fe *formatError
	return errors.As(err, &fe) || errors.Is(err, errRecordTooBig)
}

// lineSrc is the decoder of a body of lines and the window it reads through.
type lineSrc struct {
	dec *json.Decoder
	win *window
}

// readLines reads the values until the end of the body. A body that is cut is not an error here: it says so in out.
func readLines(src lineSrc, rp ingest.RecordsPath, maxElements int, ended bool, each func(it item) bool, out *walked) error {
	dec, win := src.dec, src.win
	for {
		start := dec.InputOffset()
		var v any
		if err := dec.Decode(&v); err != nil {
			return lineEnd(err, win, ended, out)
		}
		end := dec.InputOffset()
		win.floor = int(end)
		if end-start > recordMaxBytes {
			return errRecordTooBig
		}
		out.valid = true
		more, err := lineRecords(rp, v, out.n, maxElements, out, each)
		out.n++
		if err != nil {
			return err
		}
		if !more {
			out.stopped = true
			return nil
		}
	}
}

// lineEnd says what a failure to read the next value is. The end of the body, and a body that was cut in the middle of a value
// where nothing marks its end, are the end of the lines (out says whether the body was whole); a limit and a failure of the
// connection are the window's to tell; anything else is a value that is not JSON, or a body that its mark ended in the middle of one.
func lineEnd(err error, win *window, ended bool, out *walked) error {
	switch {
	case errors.Is(err, io.EOF):
		out.valid, out.found, out.cut = true, true, !ended
		out.noLines = out.n == 0
		return nil
	case win.srcErr != nil || win.err != nil:
		return err
	case errors.Is(err, io.ErrUnexpectedEOF) && !ended:
		out.valid, out.found, out.cut = true, true, true
		return nil
	case errors.Is(err, io.ErrUnexpectedEOF):
		return &formatError{"the answer ends in the middle of value " + strconv.Itoa(out.n+1)}
	}
	return &formatError{"value " + strconv.Itoa(out.n+1) + " of the answer is not JSON"}
}

// lineRecords gives the record of one value to each, or the elements of the array it has. It tells whether to go on.
func lineRecords(rp ingest.RecordsPath, v any, index, maxElements int, out *walked, each func(it item) bool) (bool, error) {
	rec := v
	if len(rp.Outer) > 0 {
		var ok bool
		if rec, ok = rp.Outer.Lookup(v); !ok || rec == nil {
			return false, &formatError{"value " + strconv.Itoa(index+1) + " of the answer has nothing at records_path"}
		}
	}
	out.found = true
	if !rp.Expand {
		return each(item{rec: rec, index: index, last: true}), nil
	}
	return giveElements(rp, rec, index, maxElements, out, each)
}

// lines reads the body of the answer as JSON lines and gives its records to each. The content type is not looked at (an export
// that is streamed is typed as ndjson, as JSON or as text), and a body that breaks is not read again: what was read is what
// was seen (walked.cut).
func (a *answer) lines(ctx context.Context, each func(it item) bool) (*walked, error) {
	rn := a.rn
	if a.rep.status == 204 {
		w := a.empty()
		w.noLines = true
		return w, nil
	}
	rp, err := a.ds.Records()
	if err != nil {
		a.rep.closeBody()
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "records_path is not a path")
	}
	w, err := walkLines(a.rep.rc, rn.set.maxBytes, rp, a.ds.ElementLimit(), a.rep.ended, each)
	if w.valid {
		rn.prog.responded = true
	}
	if w.found {
		rn.prog.recordsFound = true
	}
	w.reply = a.rep
	a.rep.closeBody()
	switch {
	case err != nil:
		return nil, rn.answerError(ctx, err)
	case w.cut && ctx.Err() != nil:
		return nil, rn.transportError(ctx, w.srcErr)
	}
	rn.after(a.p, a.rep, w.bytes)
	return w, nil
}
