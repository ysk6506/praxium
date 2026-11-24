package ingest

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// maxKeptBytes is the most text a scan keeps of a sheet while it reads the rows: the cells of the header rows, the first cell of
// each merged range that is needed and the names of the columns. It is a fixed limit and not one of the source definition
// (ADR-0103), so that what a scan holds does not grow with the file.
const maxKeptBytes = 8 << 20

// ScanStep is what one row of a sheet gave: the record of the row as a table of at most one line (a row that gave a natural key,
// an unreadable line, or only a count of what was passed over), and whether the data is over so that the reader may stop.
type ScanStep struct {
	Table Table
	Done  bool
}

// ScanEnd is what the end of a sheet gave. Header is the header when no data row came to set it, so that a sheet that has a
// header and no data is still told its shape. HiddenColsRead counts the hidden columns among those read. Err is an error of
// the scan that the end brought (the header over the limit of what is kept).
type ScanEnd struct {
	Header         *Shape
	HiddenColsRead int
	Err            error
}

// SheetScanner turns the rows of one sheet, given in order, into the records of a table (ADR-0040, ADR-0046, ADR-0103). It is
// the one place that decides what a row of a sheet means: where the header is, which columns are read, what is blank, what
// ends the data, which merged cell gives its value. The sheet is never held: a scan keeps the cells of the header rows until
// the first data row comes, the first cell of each merged range that is needed, and a few counters.
type SheetScanner struct {
	d    Dataset
	o    SheetOptions
	head SheetHead
	lay  SheetLayout
	b    bounds
	// c1 and c2 are the first and last column of the table; c2 is 0 when the data has no range and its columns are not set.
	c1, c2 int
	// from is the first row of the data and hdrEnd the last row of the header.
	from, hdrEnd int
	merges       *mergeIndex
	hdr          []SheetRow
	kept         int
	// prev is the number of the row given last, last the row the data has gone to (a gap of rows is blank rows).
	prev, last int
	shape      *Shape
	cols       []mappedCol
	// where1 and where2 are the letters of the first and the last column read, for the range of a row.
	where1, where2 string
	hiddenCols     int
	blankRun       int
	stopped        bool
	done           bool
	err            error
	// pending is the cells of the row being read that gave no value.
	pending []CellIssue
}

type mappedCol struct {
	name string
	col  int
}

// NewSheetScanner begins the scan of the sheet that the dataset names. The options are those the dataset has, or the default
// ones, and layout is what the sheet says apart from its rows.
func NewSheetScanner(d Dataset, o SheetOptions, head SheetHead, lay SheetLayout) *SheetScanner {
	s := &SheetScanner{d: d, o: o, head: head, lay: lay}
	s.b, _ = o.bounds()
	s.c1 = 1
	if s.b.col1 > 0 {
		s.c1, s.c2 = s.b.col1, s.b.col2
	}
	s.hdrEnd = o.HeaderRow + o.HeaderRows - 1
	s.from = max(s.hdrEnd+1, s.b.row1)
	s.last = s.from - 1
	s.merges = newMergeIndex(lay, o.HeaderRow, s.hdrEnd, s.from, o.MergedCells == MergeFill, s.keepBytes)
	return s
}

// keepBytes makes room for n more bytes of what the scan keeps, or says that the limit is passed. A negative n gives room back.
func (s *SheetScanner) keepBytes(n int) error {
	s.kept += n
	if n > 0 && s.kept > maxKeptBytes {
		return kernel.Errorf(kernel.CodeInvalidValue,
			"the workbook is over the safety limit max_kept_bytes (%d) by sheet %q: its header and its merged cells hold more text than a read keeps; this limit is fixed", maxKeptBytes, s.head.Name)
	}
	return nil
}

// Row takes the next row of the sheet. It gives the record of the row and says when the data is over (the end of the range, or a
// header that lacks a column), so that the reader may stop. A row out of order, or a header over the limit, is an error and the
// scan is over.
func (s *SheetScanner) Row(r SheetRow) (ScanStep, error) {
	if s.err != nil {
		return ScanStep{}, s.err
	}
	if s.done {
		return ScanStep{Done: true}, nil
	}
	if err := s.advance(r); err != nil {
		s.err = err
		return ScanStep{}, err
	}
	if r.Number < s.from {
		return ScanStep{}, nil
	}
	if s.shape == nil {
		if err := s.open(r.Number); err != nil {
			s.err = err
			return ScanStep{}, err
		}
		if len(s.shape.Missing) > 0 {
			s.done = true
			return ScanStep{Done: true}, nil
		}
	}
	if s.b.row2 > 0 && r.Number > s.b.row2 {
		s.done = true
		return ScanStep{Done: true}, nil
	}
	return ScanStep{Table: s.data(r)}, nil
}

// Header is the shape of the table, once the first data row, or the end of the sheet, has set it.
func (s *SheetScanner) Header() (*Shape, bool) { return s.shape, s.shape != nil }

// Finish ends the sheet. A sheet whose data rows never came has its header set now, from what the sheet has.
func (s *SheetScanner) Finish() ScanEnd {
	var end ScanEnd
	if s.err == nil && s.shape == nil {
		if err := s.open(s.prev); err != nil {
			s.err = err
		} else {
			end.Header = s.shape
		}
	}
	end.Err, end.HiddenColsRead = s.err, s.hiddenCols
	return end
}

// advance takes a row in: it is in order, it is kept when it is a header row, and the merged ranges that start in it are kept.
func (s *SheetScanner) advance(r SheetRow) error {
	if err := s.checkOrder(r); err != nil {
		return err
	}
	s.prev = r.Number
	if r.Number >= s.o.HeaderRow && r.Number <= s.hdrEnd {
		if err := s.keepHeaderRow(r); err != nil {
			return err
		}
	}
	return s.merges.reach(r)
}

// checkOrder refuses a row that does not come after the one before, and a cell that does not come after the one before it in its
// row: a scan goes by the order of the file and cannot sort it (ADR-0103).
func (s *SheetScanner) checkOrder(r SheetRow) error {
	if s.prev > 0 && r.Number <= s.prev {
		return kernel.Errorf(kernel.CodeInvalidValue, "the workbook is damaged: the rows of sheet %q are not in order (row %d after row %d)", s.head.Name, r.Number, s.prev)
	}
	for i := 1; i < len(r.Cells); i++ {
		if r.Cells[i].Col <= r.Cells[i-1].Col {
			return kernel.Errorf(kernel.CodeInvalidValue, "the workbook is damaged: the cells of row %d of sheet %q are not in order (column %s after column %s)",
				r.Number, s.head.Name, ColumnName(r.Cells[i].Col), ColumnName(r.Cells[i-1].Col))
		}
	}
	return nil
}

// keepHeaderRow keeps a copy of the cells of a header row: the reader may use the slice again for the next row.
func (s *SheetScanner) keepHeaderRow(r SheetRow) error {
	n := 0
	for _, c := range r.Cells {
		n += len(c.Text)
	}
	if err := s.keepBytes(n); err != nil {
		return err
	}
	s.hdr = append(s.hdr, SheetRow{Number: r.Number, Cells: slices.Clone(r.Cells)})
	return nil
}

// data takes a row that is in the data range: the rows left out before it (a gap) are blank rows, and it is a row of the table or
// is passed over by a rule.
func (s *SheetScanner) data(r SheetRow) Table {
	var t Table
	if !s.stopped {
		gap := r.Number - s.last - 1
		s.blankRun += gap
		t.addSkipped("blank_row", gap)
	}
	s.last = r.Number
	s.row(&r, &t)
	return t
}

func (s *SheetScanner) row(r *SheetRow, t *Table) {
	if s.o.StopAfterBlankRows > 0 && s.blankRun >= s.o.StopAfterBlankRows {
		s.stopped = true
	}
	blank := s.isBlank(r)
	switch {
	case s.stopped:
		if !blank {
			t.addSkipped("after_end", 1)
		}
	case blank:
		s.blankRun++
		t.addSkipped("blank_row", 1)
	case r.Hidden && s.o.SkipHiddenRows:
		t.addSkipped("hidden_row", 1)
	default:
		s.blankRun = 0
		s.read(r, t)
	}
}

// addSkipped counts rows passed over with their reason.
func (t *Table) addSkipped(reason string, n int) {
	if n <= 0 {
		return
	}
	if t.Skipped == nil {
		t.Skipped = map[string]int{}
	}
	t.Skipped[reason] += n
}

// isBlank tells whether a row has nothing in the columns that are read: no cell, or only white space.
func (s *SheetScanner) isBlank(r *SheetRow) bool {
	for _, c := range r.Cells {
		if c.Col < s.c1 || s.c2 > 0 && c.Col > s.c2 {
			continue
		}
		if !c.blank() {
			return false
		}
	}
	return true
}

// read makes a row of the cells of the mapped columns.
func (s *SheetScanner) read(r *SheetRow, t *Table) {
	if r.Hidden {
		t.HiddenRowsRead++
	}
	fields := make(map[string]string, len(s.cols))
	s.pending = nil
	for _, c := range s.cols {
		fields[c.name] = s.value(r, c)
	}
	parts, empty, all := naturalKeyOf(s.d, fields)
	if empty != "" {
		if all && s.d.IgnoreRowsWithoutKey {
			t.skip(SkipNoKey)
			return
		}
		ref := QualifiedRef(s.head.Name, CellRef(r.Number, s.colOf(empty)))
		t.Unreadable = append(t.Unreadable, Unreadable{Line: r.Number, Where: ref, Reason: "missing_natural_key", Column: empty, AllEmpty: all})
		t.CellIssues = append(t.CellIssues, s.pending...)
		return
	}
	where := QualifiedRef(s.head.Name, fmt.Sprintf("%s%d:%s%d", s.where1, r.Number, s.where2, r.Number))
	t.CellIssues = append(t.CellIssues, s.pending...)
	t.Rows = append(t.Rows, Row{Key: strings.Join(parts, keySep), Line: r.Number, Where: where, Fields: fields, Issues: s.pending})
}

func (s *SheetScanner) colOf(name string) int {
	for _, c := range s.cols {
		if c.name == name {
			return c.col
		}
	}
	return 0
}

// value is the text of the cell of a column in a row. A merged range gives its value to the cells it covers when the dataset
// says so. A cell that shows an error, or a formula the file has no value for, is no value and is listed.
func (s *SheetScanner) value(r *SheetRow, c mappedCol) string {
	cell, ok := cellIn(r.Cells, c.col)
	if !ok {
		if s.o.MergedCells == MergeFill {
			if o, ok := s.merges.fillOrigin(r.Number, c.col); ok && o.Kind != CellError && !o.NoValue {
				return o.Text
			}
		}
		return ""
	}
	ref := QualifiedRef(s.head.Name, CellRef(r.Number, c.col))
	switch {
	case cell.Kind == CellError:
		s.pending = append(s.pending, CellIssue{Kind: ProblemCellError, Where: ref, Row: r.Number,
			Reason: fmt.Sprintf("the cell of column %s shows the error %s", c.name, cell.Text)})
		return ""
	case cell.NoValue:
		s.pending = append(s.pending, CellIssue{Kind: ProblemFormulaNoValue, Where: ref, Row: r.Number,
			Reason: "the cell of column " + c.name + " is a formula the file stored no value for (it was never calculated)"})
		return ""
	}
	return cell.Text
}
