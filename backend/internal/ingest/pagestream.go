package ingest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"iter"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// StreamPages reads the bytes of one dataset as a sequence of pages without holding them: a CSV file or a file of JSON
// lines is read record by record, and the rows are cut into pages by the options. It reads a record the way Parse does
// (the header, the quotes, the empty cells, the natural key, the text of a problem), because it calls the same
// functions for what a record means; what it adds is the paging, the row limit of the dataset and the check that the
// bytes are UTF-8 as they pass. An adapter that has the bytes of a file or of a response opens them and calls this.
//
// The sequence ends with a page that has Last set and says whether the dataset was read to its end. A read of the bytes
// that stops short, and a dataset that goes over its row limit, end it with a page that is not complete. Bytes that are
// not UTF-8 text, a parser that is not read as a stream and a canceled context end it with an error. The caller leaves
// the loop to stop; nothing is held beyond the page given. Workbooks are not read here: a workbook is unpacked whole.
func StreamPages(ctx context.Context, src io.Reader, ds Dataset, opts PageOptions) iter.Seq2[Page, error] {
	return func(yield func(Page, error) bool) {
		if ds.Parser != ParserCSV && ds.Parser != ParserJSONLines {
			yield(Page{}, kernel.Errorf(kernel.CodeUnsupported, "dataset %s: parser %s is not read page by page", ds.ID, ds.Parser))
			return
		}
		guard := newEncodingGuard(src, ds.Parser)
		s := &pageStream{ds: ds, opts: opts.WithDefaults(), guard: guard}
		s.run(ctx, yield)
	}
}

// pageStream is the state of reading one dataset page by page: the page being filled and what the dataset has had so far.
type pageStream struct {
	ds    Dataset
	opts  PageOptions
	guard *encodingGuard
	next  func() (Table, error)
	// lines is what the lines of a file that has a records_path left for the end of the dataset.
	lines *lineState
	page  Page
	total int
	count int
	first *Shape
	// token is what the source needs to go on after the last record on the page being filled; an adapter that is not
	// read from the bytes of a file sets it (PageWriter), and the page that is cut takes it.
	token []byte
}

// run reads the records and cuts the pages. A record is a Table of one line: its row, its unreadable line or its skip.
func (s *pageStream) run(ctx context.Context, yield func(Page, error) bool) {
	if err := ctx.Err(); err != nil {
		yield(Page{}, err)
		return
	}
	br := bufio.NewReader(s.guard)
	s.page = Page{Dataset: s.ds.ID}
	if s.ds.Parser == ParserCSV {
		if end, stop := s.openCSV(br); stop {
			s.finish(yield, end)
			return
		}
	} else if end, stop := s.openLines(br); stop {
		s.finish(yield, end)
		return
	}
	for {
		rec, err := s.next()
		if err != nil {
			s.finish(yield, s.endOf(err))
			return
		}
		if !s.add(ctx, rec, yield) {
			return
		}
	}
}

// add puts a record on the page, after cutting the page when the record would not fit. It tells whether to go on.
func (s *pageStream) add(ctx context.Context, rec Table, yield func(Page, error) bool) bool {
	s.total += rec.Received()
	if s.total > s.opts.MaxDatasetRows {
		s.finish(yield, Completeness{Reason: "the dataset has more than " + itoa(s.opts.MaxDatasetRows) + " data rows, the most a dataset may have in this flow; the rest was not read"})
		return false
	}
	cost := rowsBytes(rec.Rows)
	if rec.Received() > 0 && s.count > 0 && (s.count >= s.opts.MaxRows || s.page.Bytes+cost > s.opts.MaxBytes) {
		if err := ctx.Err(); err != nil {
			yield(Page{}, err)
			return false
		}
		if !yield(s.cut(), nil) {
			return false
		}
	}
	s.page.Rows = append(s.page.Rows, rec.Rows...)
	s.page.Unreadable = append(s.page.Unreadable, rec.Unreadable...)
	for reason, n := range rec.Skipped {
		if s.page.Skipped == nil {
			s.page.Skipped = map[string]int{}
		}
		s.page.Skipped[reason] += n
	}
	s.page.Bytes += cost
	s.count += rec.Received()
	return true
}

// cut gives the page that is full and starts the next one, with the next number.
func (s *pageStream) cut() Page {
	p := s.page
	p.Shape, s.first, p.Token = s.first, nil, s.token
	s.page = Page{Dataset: s.ds.ID, No: p.No + 1}
	s.count = 0
	return p
}

// finish gives the last page, with the shape of the header when no page has carried it yet.
func (s *pageStream) finish(yield func(Page, error) bool, end Completeness) {
	if s.guard != nil && s.guard.bad != nil {
		yield(Page{}, kernel.Errorf(kernel.CodeInvalidValue, "dataset %s: %s", s.ds.ID, plainMessage(s.guard.bad)))
		return
	}
	p := s.page
	p.Shape, p.Last, p.End = s.first, true, end
	yield(p, nil)
}

// endOf is how a read that ended with an error ends the dataset: at the end of the bytes it is complete (or, when a record had
// no array to read and the dataset does not say that is none, it is only what was seen), and any other stop is not.
func (s *pageStream) endOf(err error) Completeness {
	var limit *elementLimitError
	switch {
	case errors.Is(err, io.EOF):
		if s.lines != nil {
			if end, ok := s.lines.endOf(s.ds); ok {
				return end
			}
		}
		return Completeness{Complete: true}
	case errors.As(err, &limit):
		return Completeness{Reason: limit.Error() + "; the rest was not read"}
	}
	return Completeness{Reason: "the read of the file stopped before its end"}
}

// openLines sets up the reading of the lines of a file of JSON lines. A dataset that has a records_path is read by it.
func (s *pageStream) openLines(br *bufio.Reader) (Completeness, bool) {
	if s.ds.RecordsPath == "" {
		s.next = jsonLineRecords(br, s.ds)
		return Completeness{}, false
	}
	rp, err := s.ds.Records()
	if err == nil {
		s.lines = &lineState{}
		s.next, err = recordLines(br, s.ds, rp, s.lines)
	}
	if err != nil {
		return Completeness{Reason: "the records_path or a path of a column is not one that can be read"}, true
	}
	return Completeness{}, false
}

func rowsBytes(rows []Row) int {
	n := 0
	for _, r := range rows {
		for _, v := range r.Fields {
			n += len(v)
		}
	}
	return n
}

// openCSV reads the header as Parse does and sets up the records. A header that cannot be read, or that lacks a
// declared column, is the end of the dataset: the one page says so in its shape.
func (s *pageStream) openCSV(br *bufio.Reader) (Completeness, bool) {
	if head, err := br.Peek(3); err == nil && bytes.Equal(head, []byte("\xef\xbb\xbf")) {
		_, _ = br.Discard(3)
	}
	r := csv.NewReader(br)
	r.Comma = s.ds.comma()
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	header, err := r.Read()
	if err != nil {
		if !errors.Is(err, io.EOF) {
			s.page.Unreadable = append(s.page.Unreadable, Unreadable{Line: 1, Reason: "unreadable_header"})
		}
		s.first = &Shape{Missing: requiredColumns(s.ds)}
		if errors.Is(err, io.EOF) || isParse(err) {
			return Completeness{Complete: true}, true
		}
		return s.endOf(err), true
	}
	idx, missing := csvIndex(s.ds, header)
	s.first = &Shape{Missing: missing, Extra: extraHeader(s.ds, header), Header: nonBlank(header)}
	if len(missing) > 0 {
		s.first.Hint = delimiterHint(header, r.Comma)
		return Completeness{Complete: true}, true
	}
	s.next = csvRecords(r, s.ds, idx, len(header))
	return Completeness{}, false
}

func isParse(err error) bool {
	var pe *csv.ParseError
	return errors.As(err, &pe)
}

// csvRecords reads the records of a CSV file after its header, one Table each.
func csvRecords(r *csv.Reader, ds Dataset, idx map[string]int, width int) func() (Table, error) {
	return func() (Table, error) {
		for {
			rec, err := r.Read()
			if err != nil && !isParse(err) {
				return Table{}, err
			}
			line, _ := r.FieldPos(0)
			var t Table
			switch {
			case err != nil:
				t.Unreadable = append(t.Unreadable, Unreadable{Line: line, Reason: "malformed_csv"})
			case len(rec) == 1 && strings.TrimSpace(rec[0]) == "":
				continue
			case len(rec) != width:
				t.Unreadable = append(t.Unreadable, Unreadable{Line: line, Reason: "wrong_field_count"})
			default:
				t.addRow(ds, line, csvFields(ds, idx, rec))
			}
			return t, nil
		}
	}
}

// jsonLineRecords reads the lines of a file of JSON lines, one Table each. The last line may have no end of line; an
// error of the read comes after the line it came with.
func jsonLineRecords(br *bufio.Reader, ds Dataset) func() (Table, error) {
	line := 0
	var stopped error
	return func() (Table, error) {
		for stopped == nil {
			text, err := br.ReadBytes('\n')
			line++
			stopped = err
			if len(bytes.TrimSpace(text)) > 0 {
				var t Table
				t.jsonLine(ds, line, text)
				return t, nil
			}
		}
		return Table{}, stopped
	}
}

// endOf is how the end of the bytes ends a dataset whose lines were read by records_path, when it is not complete: a line that could
// not be read makes it not complete (a line is one record or the many elements of an array, and what it had is not known); no
// record that had an array, with some that had none, or no row of the records there were, makes it what was seen. ok is false when
// none of these is so.
func (st *lineState) endOf(ds Dataset) (Completeness, bool) {
	switch {
	case st.bad > 0:
		return Completeness{Reason: itoa(st.bad) + " lines or elements could not be read (not JSON, nothing or another kind of value where records_path reads, or no natural key), so the dataset is not complete"}, true
	case st.missing > 0 && st.missing == st.outers:
		return Completeness{SeenOnly: true, Reason: "none of the " + itoa(st.outers) + " lines had an array at the place of records_path (or null); check the records_path"}, true
	case st.missing > 0 && !ds.MissingIsEmpty():
		return Completeness{SeenOnly: true, Reason: itoa(st.missing) + " lines had no array at the place of records_path (or null), so what they may have had was not seen"}, true
	case st.recs > 0 && st.rows == 0:
		return Completeness{SeenOnly: true, Reason: itoa(st.recs) + " records were read and none made a row, so the file may not be what the definition reads"}, true
	}
	return Completeness{}, false
}
