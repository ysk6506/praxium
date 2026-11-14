package ingest

import (
	"fmt"
	"strings"
	"unicode"
)

// NormalizeHeader makes the name of a header comparable: the white space at both ends goes and every run of white
// space inside, a line break included, becomes one space. A header that was typed with a line break in the cell is
// then the name a mapping writes on one line.
func NormalizeHeader(s string) string { return strings.Join(strings.Fields(s), " ") }

// QualifiedRef is the address of a cell or a range with its sheet, as Excel writes it: the name is in quotes when it
// has more than letters, digits and underscores.
func QualifiedRef(sheet, ref string) string {
	for _, r := range sheet {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return "'" + strings.ReplaceAll(sheet, "'", "''") + "'!" + ref
		}
	}
	return sheet + "!" + ref
}

// sheetGrid gives the cells of a sheet by their place, and the merged ranges by the rows they cover.
type sheetGrid struct {
	sh     *Sheet
	rows   map[int]*SheetRow
	merges map[int][]Merge
}

func newGrid(sh *Sheet) *sheetGrid {
	g := &sheetGrid{sh: sh, rows: make(map[int]*SheetRow, len(sh.Rows)), merges: map[int][]Merge{}}
	last := 0
	for i := range sh.Rows {
		g.rows[sh.Rows[i].Number] = &sh.Rows[i]
		last = sh.Rows[i].Number
	}
	for _, m := range sh.Merges {
		for r := m.Row1; r <= m.Row2 && r <= last; r++ {
			g.merges[r] = append(g.merges[r], m)
		}
	}
	return g
}

// own is the cell the file has at a place.
func (g *sheetGrid) own(row, col int) (Cell, bool) {
	r := g.rows[row]
	if r == nil {
		return Cell{}, false
	}
	lo, hi := 0, len(r.Cells)
	for lo < hi {
		mid := (lo + hi) / 2
		if r.Cells[mid].Col < col {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(r.Cells) && r.Cells[lo].Col == col {
		return r.Cells[lo], true
	}
	return Cell{}, false
}

// origin is the first cell of the merged range that covers a place, when the place is covered and is not that cell.
func (g *sheetGrid) origin(row, col int) (Cell, bool) {
	for _, m := range g.merges[row] {
		if col >= m.Col1 && col <= m.Col2 && (row != m.Row1 || col != m.Col1) {
			return g.own(m.Row1, m.Col1)
		}
	}
	return Cell{}, false
}

// ParseSheet turns the sheet of a workbook that a dataset names into a table of rows, by the options of the dataset.
// It is a function of the workbook and the dataset. A row is a received row when it is not blank; every row that
// is passed over is counted with its reason (blank_row, hidden_row, after_end), and a cell that gave no value is
// listed with its address.
func ParseSheet(d Dataset, wb *Workbook) Table {
	o := d.Sheet
	if o == nil {
		o = &SheetOptions{HeaderRow: 1, HeaderRows: 1, MergedCells: MergeFirstOnly}
	}
	sh, why := pickSheet(wb, *o)
	if sh == nil {
		return Table{Missing: requiredColumns(d), Reason: why}
	}
	t := Table{Sheet: sh.Name, SheetState: sh.State, Skipped: map[string]int{}}
	g := newGrid(sh)
	scan := newScan(d, *o, g, &t)
	if len(t.Missing) == 0 {
		scan.run()
	}
	return t
}

// pickSheet finds the sheet by name, or by place, or the first one.
func pickSheet(wb *Workbook, o SheetOptions) (*Sheet, string) {
	var sh *Sheet
	switch {
	case o.Name != "":
		for i := range wb.Sheets {
			if wb.Sheets[i].Name == o.Name {
				sh = &wb.Sheets[i]
			}
		}
		if sh == nil {
			return nil, fmt.Sprintf("the workbook has no sheet %q (it has %s)", o.Name, sheetNames(wb))
		}
	default:
		i := max(o.Index, 1)
		if i > len(wb.Sheets) {
			return nil, fmt.Sprintf("the workbook has no sheet number %d (it has %s)", i, sheetNames(wb))
		}
		sh = &wb.Sheets[i-1]
	}
	if sh.Kind != SheetKindWorksheet {
		return nil, fmt.Sprintf("sheet %q is not a worksheet of cells (a chart sheet, for one)", sh.Name)
	}
	return sh, ""
}

func sheetNames(wb *Workbook) string {
	var names []string
	for i, s := range wb.Sheets {
		if i == 10 {
			names = append(names, "...")
			break
		}
		names = append(names, fmt.Sprintf("%q", s.Name))
	}
	if len(names) == 0 {
		return "no sheet"
	}
	return strings.Join(names, ", ")
}

// sheetScan reads the rows of one sheet for one dataset.
type sheetScan struct {
	d    Dataset
	o    SheetOptions
	g    *sheetGrid
	t    *Table
	b    bounds
	c1   int
	c2   int
	cols []mappedCol
	// where1 and where2 are the letters of the first and the last column read, for the range of a row.
	where1, where2 string
	blankRun       int
	stopped        bool
}

type mappedCol struct {
	name string
	col  int
}

// newScan reads the header and finds the columns of the dataset in it.
func newScan(d Dataset, o SheetOptions, g *sheetGrid, t *Table) *sheetScan {
	s := &sheetScan{d: d, o: o, g: g, t: t}
	s.b, _ = o.bounds()
	_, _, _, usedMax := g.sh.UsedRange()
	s.c1, s.c2 = 1, max(usedMax, 1)
	if s.b.col1 > 0 {
		s.c1, s.c2 = s.b.col1, s.b.col2
	}
	header := make([]string, s.c2-s.c1+1)
	for col := s.c1; col <= s.c2; col++ {
		header[col-s.c1] = s.headerName(col)
	}
	nd := d
	nd.Columns = make([]Column, len(d.Columns))
	for i, c := range d.Columns {
		c.Paths = normalizedPaths(c.Paths)
		nd.Columns[i] = c
	}
	idx, missing := csvIndex(nd, header)
	t.Missing = missing
	for _, h := range extraHeader(nd, header) {
		if h != "" {
			t.Extra = append(t.Extra, h)
		}
	}
	s.mapColumns(idx)
	return s
}

func normalizedPaths(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = NormalizeHeader(p)
	}
	return out
}

// headerName is the name of the column from the header rows: the text of each row, normalized, joined with a space.
// A cell that a merged range covers has the text of the range, so a name that spans columns is every column's.
func (s *sheetScan) headerName(col int) string {
	var parts []string
	for r := s.o.HeaderRow; r < s.o.HeaderRow+s.o.HeaderRows; r++ {
		c, ok := s.g.own(r, col)
		if !ok {
			c, ok = s.g.origin(r, col)
		}
		if ok && c.Kind != CellError && !c.NoValue {
			if text := NormalizeHeader(c.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, " ")
}

func (s *sheetScan) mapColumns(idx map[string]int) {
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
		if s.g.sh.IsColHidden(col) {
			s.t.HiddenColsRead++
		}
	}
	s.where1, s.where2 = ColumnName(lo), ColumnName(hi)
}

func (s *sheetScan) colOf(name string) int {
	for _, c := range s.cols {
		if c.name == name {
			return c.col
		}
	}
	return 0
}

// run goes through the rows of the data range.
func (s *sheetScan) run() {
	from := max(s.o.HeaderRow+s.o.HeaderRows, s.b.row1)
	if s.b.row1 > 0 {
		from = s.b.row1
	}
	last := from - 1
	for i := range s.g.sh.Rows {
		r := &s.g.sh.Rows[i]
		if r.Number < from {
			continue
		}
		if s.b.row2 > 0 && r.Number > s.b.row2 {
			break
		}
		if !s.stopped {
			s.blankRun += r.Number - last - 1
			s.t.Skipped["blank_row"] += r.Number - last - 1
		}
		last = r.Number
		s.row(r)
	}
	for k, n := range s.t.Skipped {
		if n == 0 {
			delete(s.t.Skipped, k)
		}
	}
}

func (s *sheetScan) row(r *SheetRow) {
	if s.o.StopAfterBlankRows > 0 && s.blankRun >= s.o.StopAfterBlankRows {
		s.stopped = true
	}
	blank := s.isBlank(r)
	switch {
	case s.stopped:
		if !blank {
			s.t.Skipped["after_end"]++
		}
	case blank:
		s.blankRun++
		s.t.Skipped["blank_row"]++
	case r.Hidden && s.o.SkipHiddenRows:
		s.t.Skipped["hidden_row"]++
	default:
		s.blankRun = 0
		s.read(r)
	}
}

// isBlank tells whether a row has nothing in the columns that are read: no cell, or only white space.
func (s *sheetScan) isBlank(r *SheetRow) bool {
	for _, c := range r.Cells {
		if c.Col < s.c1 || c.Col > s.c2 {
			continue
		}
		if c.Kind != CellText || strings.TrimSpace(c.Text) != "" {
			return false
		}
	}
	return true
}

// read makes a row of the cells of the mapped columns.
func (s *sheetScan) read(r *SheetRow) {
	if r.Hidden {
		s.t.HiddenRowsRead++
	}
	fields := make(map[string]string, len(s.cols))
	for _, c := range s.cols {
		fields[c.name] = s.value(r.Number, c)
	}
	where := QualifiedRef(s.g.sh.Name, fmt.Sprintf("%s%d:%s%d", s.where1, r.Number, s.where2, r.Number))
	parts := make([]string, len(s.d.NaturalKey))
	for i, k := range s.d.NaturalKey {
		parts[i] = strings.TrimSpace(fields[k])
		if parts[i] == "" {
			ref := QualifiedRef(s.g.sh.Name, CellRef(r.Number, s.colOf(k)))
			s.t.Unreadable = append(s.t.Unreadable, Unreadable{Line: r.Number, Where: ref, Reason: "missing_natural_key"})
			return
		}
	}
	s.t.Rows = append(s.t.Rows, Row{Key: strings.Join(parts, keySep), Line: r.Number, Where: where, Fields: fields})
}

// value is the text of the cell of a column in a row. A merged range gives its value to the cells it covers when the
// dataset says so. A cell that shows an error, or a formula the file has no value for, is no value and is listed.
func (s *sheetScan) value(row int, c mappedCol) string {
	cell, ok := s.g.own(row, c.col)
	if !ok {
		if s.o.MergedCells == MergeFill {
			if o, ok := s.g.origin(row, c.col); ok && o.Kind != CellError && !o.NoValue {
				return o.Text
			}
		}
		return ""
	}
	ref := QualifiedRef(s.g.sh.Name, CellRef(row, c.col))
	switch {
	case cell.Kind == CellError:
		s.t.CellIssues = append(s.t.CellIssues, CellIssue{Kind: ProblemCellError, Where: ref, Row: row,
			Reason: fmt.Sprintf("the cell of column %s shows the error %s", c.name, cell.Text)})
		return ""
	case cell.NoValue:
		s.t.CellIssues = append(s.t.CellIssues, CellIssue{Kind: ProblemFormulaNoValue, Where: ref, Row: row,
			Reason: "the cell of column " + c.name + " is a formula the file stored no value for (it was never calculated)"})
		return ""
	}
	return cell.Text
}
