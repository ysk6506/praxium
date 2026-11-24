package ingest

import (
	"sort"
	"strings"
)

// headerCells gives the cells a header name is read from: the text, normalized, of the cell the file has at a place, and whether
// it has one (a cell that shows an error or has no stored value is a cell that gives no text); and the text that a merged range
// gives to a place it covers when the place is not the first cell of the range.
type headerCells interface {
	own(row, col int) (text string, ok bool)
	origin(row, col int) string
}

// headerParts are the texts of a column in the header rows from first to last, normalized: the text of the cell, or the text of
// the range that covers the place when the file has no cell there. A name that spans columns is every column's.
func headerParts(g headerCells, first, last, col int) []string {
	var parts []string
	for r := first; r <= last; r++ {
		text, ok := g.own(r, col)
		if !ok {
			text = g.origin(r, col)
		}
		if text != "" {
			parts = append(parts, text)
		}
	}
	return parts
}

// headerName is the name of the column from the header rows: the text of each row, joined with a space.
func headerName(g headerCells, first, last, col int) string {
	return strings.Join(headerParts(g, first, last, col), " ")
}

// own and origin make a scan the header cells of its kept rows and ranges. A limit passed while they work is kept in err.
func (s *SheetScanner) own(row, col int) (string, bool) {
	i := sort.Search(len(s.hdr), func(i int) bool { return s.hdr[i].Number >= row })
	if i == len(s.hdr) || s.hdr[i].Number != row {
		return "", false
	}
	c, ok := cellIn(s.hdr[i].Cells, col)
	if !ok {
		return "", false
	}
	if c.Kind == CellError || c.NoValue {
		return "", true
	}
	text := NormalizeHeader(c.Text)
	s.note(s.keepBytes(len(text)))
	return text, true
}

func (s *SheetScanner) origin(row, col int) string {
	text, err := s.merges.headerOrigin(row, col)
	s.note(err)
	return text
}

func (s *SheetScanner) note(err error) {
	if err != nil && s.err == nil {
		s.err = err
	}
}

// open sets the header: the names of the columns from the header rows, the columns of the dataset found in them, and the shape.
// last is the last row of the sheet that is known, which no merged range reaches past in the header (a header row beyond it is
// no row).
func (s *SheetScanner) open(last int) error {
	rows := min(s.hdrEnd, last)
	names := make([]string, max(s.headerEnd(rows)-s.c1+1, 0))
	for i := range names {
		names[i] = s.nameOf(s.c1+i, rows)
		if s.err != nil {
			return s.err
		}
	}
	nd := s.d
	nd.Columns = make([]Column, len(s.d.Columns))
	for i, c := range s.d.Columns {
		c.Paths = normalizedPaths(c.Paths)
		nd.Columns[i] = c
	}
	idx, missing := csvIndex(nd, names)
	s.shape = &Shape{Missing: missing, Header: distinctNames(names)}
	for _, h := range extraHeader(nd, names) {
		if h != "" {
			s.shape.Extra = append(s.shape.Extra, h)
		}
	}
	s.mapColumns(idx)
	s.merges.headerKnown(s.readsAny)
	s.dropHeaderRows()
	return nil
}

// nameOf is the name of a column. The text of a range that spans columns is made once and is every column's, and a name that
// joins the texts of several rows is made after it is known to fit in what a scan keeps.
func (s *SheetScanner) nameOf(col, last int) string {
	parts := headerParts(s, s.o.HeaderRow, last, col)
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	n := len(parts) - 1
	for _, p := range parts {
		n += len(p)
	}
	if err := s.keepBytes(n); err != nil {
		s.note(err)
		return ""
	}
	return strings.Join(parts, " ")
}

// headerEnd is the last column that has a name in the header rows up to last: the last column of a cell, and the end of a merged
// range, which is cut at the last column a cell may have, so that a range that covers a million columns does not give a million
// names (ADR-0103). A range of the data gives the columns of the range and no more.
func (s *SheetScanner) headerEnd(last int) int {
	end := 0
	for _, r := range s.hdr {
		if r.Number <= last && len(r.Cells) > 0 {
			end = max(end, r.Cells[len(r.Cells)-1].Col)
		}
	}
	for _, i := range s.merges.hdr {
		if g := s.merges.merges[i]; g.Row1 <= last && g.Row2 >= g.Row1 {
			end = max(end, min(g.Col2, DefaultMaxColumnsPerSheet))
		}
	}
	if s.c2 > 0 {
		end = min(end, s.c2)
	}
	return end
}

// distinctNames is the names of a header that are not empty. A title in a merged cell is every column's header; it is listed
// once. The names are normalized already.
func distinctNames(header []string) []string {
	var out []string
	for _, h := range header {
		if h != "" && (len(out) == 0 || out[len(out)-1] != h) {
			out = append(out, h)
		}
	}
	return out
}

func normalizedPaths(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = NormalizeHeader(p)
	}
	return out
}

func (s *SheetScanner) mapColumns(idx map[string]int) {
	lo, hi := 0, 0
	for _, c := range s.d.Columns {
		i, ok := idx[c.Name]
		if !ok {
			continue
		}
		col := s.c1 + i
		s.cols = append(s.cols, mappedCol{c.Name, col})
		if lo == 0 || col < lo {
			lo = col
		}
		hi = max(hi, col)
		if s.lay.hidden(col) {
			s.hiddenCols++
		}
	}
	s.where1, s.where2 = ColumnName(lo), ColumnName(hi)
}

// readsAny tells whether a column from first to last is one that is read.
func (s *SheetScanner) readsAny(first, last int) bool {
	for _, c := range s.cols {
		if c.col >= first && c.col <= last {
			return true
		}
	}
	return false
}

// dropHeaderRows gives back the cells of the header rows, which the names have taken what they need of.
func (s *SheetScanner) dropHeaderRows() {
	for _, r := range s.hdr {
		for _, c := range r.Cells {
			s.kept -= len(c.Text)
		}
	}
	s.hdr = nil
}
