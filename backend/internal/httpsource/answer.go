package httpsource

import (
	"context"
	"errors"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// tokenAnswerMax is the most the answer to the request for a token may be. It is read whole, and a token is small
// (ADR-0097).
const tokenAnswerMax = 1 << 20

// answer is the response to a request for data whose status was a success. Its body is open and is read once, as a stream,
// by records.
type answer struct {
	rn      *run
	ds      ingest.Dataset
	p       plan
	rep     *reply
	attempt int
}

// data sends the request for a position and gives the answer of a success, with the body not read yet. A token that the
// source no longer accepts is asked for once more.
func (rn *run) data(ctx context.Context, ds ingest.Dataset, at pos) (*answer, error) {
	p, err := rn.dataPlan(ds, at)
	if err != nil {
		return nil, err
	}
	for refreshed := false; ; refreshed = true {
		if err := rn.signer.ensure(ctx, rn); err != nil {
			return nil, err
		}
		rep, attempt, err := rn.exchangeFrom(ctx, p, 0)
		if err == nil {
			return &answer{rn: rn, ds: ds, p: p, rep: rep, attempt: attempt}, nil
		}
		// A token the source refuses is asked for once more, for this request and at most maxTokenRefreshes times in the read: a
		// source that refuses every other token would otherwise be asked for tokens as often as for data (ADR-0106).
		if !errors.Is(err, errUnauthorized) || !rn.signer.refreshable() || refreshed || rn.refreshes >= maxTokenRefreshes {
			return nil, err
		}
		rn.refreshes++
		rn.signer.drop()
	}
}

// records reads the body as JSON and gives its records one at a time to each, from the one at place skip on, and then
// what the pagination reads. When the body breaks while it is read, a GET is sent again and the records that were given are
// passed over in the answer to it, as the ones a token says were given are. each tells whether to go on; if it does not,
// the answer is not read to its end (walked.stopped).
func (a *answer) records(ctx context.Context, skip int, each func(rec any, index int) bool) (*walked, error) {
	rn := a.rn
	if !jsonType(a.rep.header) {
		a.rep.closeBody()
		return nil, rn.fail(ingest.SrcResponseFormat, "the source answered with something that is not JSON", nil)
	}
	if a.rep.status == 204 {
		a.rep.closeBody()
		rn.after(a.p, a.rep, 0)
		rn.prog.responded, rn.prog.recordsFound = true, true
		return &walked{reply: a.rep, doc: map[string]any{}, valid: true, found: true}, nil
	}
	given := skip
	counted := func(rec any, i int) bool {
		given = i + 1
		return each(rec, i)
	}
	for {
		w, err := a.read(ctx, given, counted)
		if err == nil {
			return w, nil
		}
		if err = a.again(ctx, err); err != nil {
			return nil, err
		}
	}
}

// read reads the body once and says what the answer was.
func (a *answer) read(ctx context.Context, skip int, each func(rec any, index int) bool) (*walked, error) {
	rn := a.rn
	w, err := walk(a.rep.rc, rn.set.maxBytes, a.ds.RecordsPath, rn.pager.reads(), skip, each)
	if w.valid {
		rn.prog.responded = true
	}
	if w.found {
		rn.prog.recordsFound = true
	}
	w.reply = a.rep
	if err != nil {
		a.rep.closeBody()
		return nil, rn.answerError(ctx, err)
	}
	a.rep.closeBody()
	rn.after(a.p, a.rep, w.bytes)
	return w, nil
}

// again decides whether a read that failed is made again: it is when the failure is a passing one and a GET may be sent
// again, and then the request is sent again after the wait. It gives the error to end the read with when it is not.
func (a *answer) again(ctx context.Context, err error) error {
	rn := a.rn
	wait, ok := rn.retryWait(nil, err, a.p, a.attempt)
	if !ok {
		return err
	}
	rn.emit(ingest.Event{Kind: ingest.EventRetry, Attempt: a.attempt + 1, Reason: retryReason(err)})
	if serr := rn.r.cfg.Sleep(ctx, wait); serr != nil {
		return serr
	}
	// The token may have run out while the body was read and the wait was waited for; it is asked for again before the request
	// is sent again, as before every request.
	if eerr := rn.signer.ensure(ctx, rn); eerr != nil {
		return eerr
	}
	rep, attempt, xerr := rn.exchangeFrom(ctx, a.p, a.attempt+1)
	if xerr != nil {
		return xerr
	}
	a.rep, a.attempt = rep, attempt
	return nil
}

// answerError says what a failure of reading an answer is.
func (rn *run) answerError(ctx context.Context, err error) error {
	var fe *formatError
	switch {
	case errors.Is(err, errTooLarge):
		return rn.fail(ingest.SrcResponseFormat, "an answer is larger than max_response_bytes ("+strconv.Itoa(rn.set.maxBytes)+" bytes after it is unpacked); lower the size of a page or raise the limit", nil)
	case errors.Is(err, errRecordTooBig):
		return rn.fail(ingest.SrcResponseFormat, "a record of the answer, or a value in it that is not a record, is larger than "+strconv.Itoa(recordMaxBytes)+" bytes", nil)
	case errors.Is(err, errRecordsPath):
		return kernel.Errorf(kernel.CodeInvalidValue, "records_path is not a path")
	case errors.As(err, &fe):
		return rn.fail(ingest.SrcResponseFormat, fe.Error(), nil)
	}
	return rn.transportError(ctx, err)
}

// smallBody reads the body of an answer that is small (the answer to the request for a token) whole.
func (rn *run) smallBody(ctx context.Context, p plan, rep *reply, limit int) ([]byte, error) {
	data, err := readBody(rep.rc, limit)
	rep.rc = nil
	switch {
	case errors.Is(err, errTooLarge):
		return nil, rn.fail(ingest.SrcResponseFormat, "the answer to the token request is larger than "+strconv.Itoa(limit)+" bytes", nil)
	case err != nil:
		return nil, rn.transportError(ctx, err)
	}
	rn.after(p, rep, len(data))
	return data, nil
}
