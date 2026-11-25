package ingest

import (
	"context"
	"errors"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// What an adapter that reads records and not bytes needs from the pipeline (ADR-0090): a way to turn a record into a row
// by the rules a file of JSON lines is read by, and a way to cut the rows into pages by the rules every adapter shares.
// A response of an HTTP API gives records that are JSON objects already, and a database gives rows that the adapter
// makes into the same; neither is parsed again.

// ReasonNotAnObject is the reason a record that is no JSON object is unreadable.
const ReasonNotAnObject = "not_an_object"

// RowBuilder turns the records of one dataset into rows: the cells are the declared columns, each read from the first
// of its paths that has a value, and the natural key is made of the key columns as it is for a line of JSON lines. A
// record that gives no natural key is an unreadable line, never a dropped one. The values that count are the text, the
// number (json.Number), the boolean; an object, an array, null and the empty text are no value.
type RowBuilder struct {
	ds   Dataset
	cols []builderColumn
}

type builderColumn struct {
	name  string
	paths []ColumnPath
}

// NewRowBuilder reads the paths of the dataset's columns. It refuses a path the grammar does not read.
func NewRowBuilder(ds Dataset) (*RowBuilder, error) {
	b := &RowBuilder{ds: ds, cols: make([]builderColumn, 0, len(ds.Columns))}
	outer := ds.OpensArray()
	for _, c := range ds.Columns {
		paths := c.Paths
		if len(paths) == 0 {
			paths = []string{c.Name}
		}
		col := builderColumn{name: c.Name}
		for _, text := range paths {
			p, err := ParseColumnPath(text, outer)
			var pe *PathError
			if errors.As(err, &pe) {
				return nil, kernel.Errorf(kernel.CodeInvalidValue, "dataset %s, column %s: the path %q %s", ds.ID, c.Name, clip(text, idClipAt), pe.Reason)
			}
			col.paths = append(col.paths, p)
		}
		b.cols = append(b.cols, col)
	}
	return b, nil
}

// Record turns one record into the Table of one line: its row, its unreadable line or its skip. line is the number the
// problem list shows (the place of the record in the page or the response) and where says the same in words ("page 3,
// record 12") when a number does not.
func (b *RowBuilder) Record(rec any, line int, where string) Table {
	return b.Element(nil, rec, line, where)
}

// Element is Record for a record that is an element of an array inside another record (a records_path with [*]): outer is the
// record the element is in, which a path that begins with ^ reads.
func (b *RowBuilder) Element(outer, rec any, line int, where string) Table {
	var t Table
	doc, ok := rec.(map[string]any)
	if !ok {
		t.Unreadable = append(t.Unreadable, Unreadable{Line: line, Where: where, Reason: ReasonNotAnObject})
		return t
	}
	fields := make(map[string]string, len(b.cols))
	for _, c := range b.cols {
		for _, p := range c.paths {
			if v, ok := p.Pick(doc, outer); ok {
				fields[c.name] = v
				break
			}
		}
	}
	t.addRow(b.ds, line, fields)
	for i := range t.Rows {
		t.Rows[i].Where = where
	}
	for i := range t.Unreadable {
		t.Unreadable[i].Where = where
	}
	return t
}

// UnescapeBraces turns the {{ and }} of a text of a definition into the { and } they stand for. A definition has no
// other brace (a variable has no name that is defined yet), so a text that is no longer escaped is the text to send.
func UnescapeBraces(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "{{", "{"), "}}", "}")
}

// PageWriter cuts the records an adapter reads into pages by the rules every reader shares: the page is full at the row
// count or at the size of its cells, a record is never split, a dataset has a limit of rows, and the last page says
// whether the dataset was read to its end. It is the same cutting StreamPages does for a file, so that the size of a
// page changes nothing about what is read (ingestion-at-scale.md, P1).
type PageWriter struct {
	s *pageStream
	// after, yield and give belong to AddLazy: the token function of the record added last, the caller's yield, and the
	// function that fills the token of a page that is cut (made once).
	after func() []byte
	yield func(Page, error) bool
	give  func(Page, error) bool
}

// NewPageWriter begins the pages of a dataset. A zero value of the options is the default.
func NewPageWriter(ds Dataset, opts PageOptions) *PageWriter {
	s := &pageStream{ds: ds, opts: opts.WithDefaults()}
	s.page = Page{Dataset: ds.ID}
	return &PageWriter{s: s}
}

// Add puts one record on the page being filled, and gives the page first when the record does not fit on it. after is the
// token of the position just after this record: the source needs it to go on from there, and it goes with the page that
// ends with this record. Add tells whether to read on; it is false when the caller left the loop, when the context is
// done (the error was given) and when the dataset went over its row limit (the last page was given).
func (w *PageWriter) Add(ctx context.Context, rec Table, after []byte, yield func(Page, error) bool) bool {
	if !w.s.add(ctx, rec, yield) {
		return false
	}
	w.s.token = after
	return true
}

// AddLazy is Add for a token that costs something to make: after makes the token of the position just after this record,
// and it is called only when a page is cut at this record, once for each page and never for a record in the middle of
// one (ADR-0097). A nil after gives the page no token. A writer is used with Add or with AddLazy, not with both.
func (w *PageWriter) AddLazy(ctx context.Context, rec Table, after func() []byte, yield func(Page, error) bool) bool {
	w.yield = yield
	if w.give == nil {
		w.give = w.fill
	}
	if !w.s.add(ctx, rec, w.give) {
		return false
	}
	w.s.token, w.after = nil, after
	return true
}

// fill gives a page that was cut the token of the record it ends with.
func (w *PageWriter) fill(p Page, err error) bool {
	if err == nil && !p.Last && w.after != nil {
		p.Token = w.after()
	}
	return w.yield(p, err)
}

// Finish gives the last page. end says whether the dataset was read to its end and, if not, why.
func (w *PageWriter) Finish(yield func(Page, error) bool, end Completeness) {
	w.s.finish(yield, end)
}
