package ingest

import "sort"

// mergeIndex finds the merged range that covers a cell, for the ranges a scan needs only, and keeps the first cell of each of
// them from the time its row goes by to the time its last row has gone by (ADR-0103). A scan never holds a sheet: the ranges
// are the layout's and are not copied, and what is kept besides is the indexes of the ranges and the first cells.
//
// A range is needed for the name of a column when it touches a header row, and for the value of a cell when the dataset fills
// merged cells and the range reaches the data and covers a column that is read.
type mergeIndex struct {
	merges []Merge
	// hdr is the ranges that touch a header row, in the order of the layout.
	hdr []int
	// starts is the ranges that are needed, ordered by their first row; pos is the next one to come to.
	starts []int
	pos    int
	// active is the needed ranges for the data whose first row has come and whose last has not gone by, in the order of the
	// layout: the first of them that covers a cell is the one that gives it its value.
	active []int
	// cells is the first cell of a range, when the file has one there.
	cells map[int]Cell
	norm  map[int]string
	fill  bool
	from  int
	// covers says whether any column from the first to the last is read, once the header is known; before that every column may be.
	covers func(first, last int) bool
	// keep makes room for n bytes of text, or says that there is none.
	keep func(n int) error
}

func newMergeIndex(lay SheetLayout, hdrFirst, hdrLast, from int, fill bool, keep func(n int) error) *mergeIndex {
	m := &mergeIndex{merges: lay.Merges, cells: map[int]Cell{}, norm: map[int]string{}, fill: fill, from: from, keep: keep}
	for i, g := range lay.Merges {
		touches := g.Row2 >= hdrFirst && g.Row1 <= hdrLast
		if touches {
			m.hdr = append(m.hdr, i)
		}
		if touches || m.forData(g) {
			m.starts = append(m.starts, i)
		}
	}
	sort.SliceStable(m.starts, func(a, b int) bool { return m.merges[m.starts[a]].Row1 < m.merges[m.starts[b]].Row1 })
	return m
}

// forData tells whether a range gives values to the data: the dataset fills, the range reaches the data, and (once the
// header is known) it covers a column that is read.
func (m *mergeIndex) forData(g Merge) bool {
	if !m.fill || g.Row2 < m.from || g.Row2 < g.Row1 {
		return false
	}
	return m.covers == nil || m.covers(g.Col1, g.Col2)
}

// reach takes the row that comes next: it keeps the first cell of the ranges that start in it, brings the ranges that start in
// it or before it into the active ones and lets go of those that ended before it.
func (m *mergeIndex) reach(r SheetRow) error {
	for m.pos < len(m.starts) && m.merges[m.starts[m.pos]].Row1 <= r.Number {
		i := m.starts[m.pos]
		m.pos++
		g := m.merges[i]
		needed := m.forData(g)
		if g.Row1 == r.Number && (m.covers == nil || needed) {
			if err := m.capture(i, g, r); err != nil {
				return err
			}
		}
		if needed {
			j := sort.SearchInts(m.active, i)
			m.active = append(m.active, 0)
			copy(m.active[j+1:], m.active[j:])
			m.active[j] = i
		}
	}
	m.prune(func(g Merge) bool { return g.Row2 < r.Number })
	return nil
}

// capture keeps the cell of the row at the first column of a range.
func (m *mergeIndex) capture(i int, g Merge, r SheetRow) error {
	c, ok := cellIn(r.Cells, g.Col1)
	if !ok {
		return nil
	}
	if err := m.keep(len(c.Text)); err != nil {
		return err
	}
	m.cells[i] = c
	return nil
}

// release lets go of the first cell of a range.
func (m *mergeIndex) release(i int) {
	if c, ok := m.cells[i]; ok {
		_ = m.keep(-len(c.Text))
		delete(m.cells, i)
		delete(m.norm, i)
	}
}

// prune takes out of the active ranges those the test says, and lets go of their cells.
func (m *mergeIndex) prune(gone func(g Merge) bool) {
	kept := m.active[:0]
	for _, i := range m.active {
		if gone(m.merges[i]) {
			m.release(i)
			continue
		}
		kept = append(kept, i)
	}
	m.active = kept
}

// headerKnown says which columns are read. The cells of the ranges that touch a header row only are let go, and so are those of
// the ranges that do not cover a column that is read.
func (m *mergeIndex) headerKnown(covers func(first, last int) bool) {
	m.covers = covers
	m.prune(func(g Merge) bool { return !m.forData(g) })
	for _, i := range m.hdr {
		if !m.forData(m.merges[i]) {
			m.release(i)
		}
	}
	m.hdr = nil
}

// fillOrigin is the first cell of the first range that covers a place, when the place is covered and is not that cell. The
// cell is there when the file has a cell with a value there.
func (m *mergeIndex) fillOrigin(row, col int) (Cell, bool) {
	for _, i := range m.active {
		g := m.merges[i]
		if col >= g.Col1 && col <= g.Col2 && (row != g.Row1 || col != g.Col1) {
			c, ok := m.cells[i]
			return c, ok
		}
	}
	return Cell{}, false
}

// headerOrigin is the normalized text that the first range covering a place of a header row gives it, when the place is
// covered and is not the first cell of the range. A cell that shows an error or a formula with no stored value gives none.
func (m *mergeIndex) headerOrigin(row, col int) (string, error) {
	for _, i := range m.hdr {
		g := m.merges[i]
		if row < g.Row1 || row > g.Row2 || col < g.Col1 || col > g.Col2 || row == g.Row1 && col == g.Col1 {
			continue
		}
		return m.headerText(i)
	}
	return "", nil
}

// headerText is the normalized text of the first cell of a range, made once for all the columns the range gives it to.
func (m *mergeIndex) headerText(i int) (string, error) {
	if t, ok := m.norm[i]; ok {
		return t, nil
	}
	c, ok := m.cells[i]
	if !ok || c.Kind == CellError || c.NoValue {
		return "", nil
	}
	t := NormalizeHeader(c.Text)
	if err := m.keep(len(t)); err != nil {
		return "", err
	}
	m.norm[i] = t
	return t, nil
}

// cellIn finds the cell of a column in the cells of a row, which are in the order of the columns.
func cellIn(cells []Cell, col int) (Cell, bool) {
	i := sort.Search(len(cells), func(i int) bool { return cells[i].Col >= col })
	if i < len(cells) && cells[i].Col == col {
		return cells[i], true
	}
	return Cell{}, false
}
