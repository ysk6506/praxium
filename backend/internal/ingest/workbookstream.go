package ingest

// SheetHead is what a workbook says about one sheet before its rows are read: the place among the sheets, the name, the
// kind (worksheet or another) and the state. The scanner of the rows of a sheet takes its name for the places it tells.
type SheetHead struct {
	Index int
	Name  string
	Kind  string
	State string
}

// SheetLayout is what a sheet says apart from its rows: the merged ranges and the hidden columns. A sheet that is read row
// by row gives it first, because the name of a column and the value of a merged cell depend on a merge that the file lists
// after the rows (ADR-0103).
type SheetLayout struct {
	Merges     []Merge
	HiddenCols []ColSpan
}

// hidden tells whether a column is hidden.
func (l SheetLayout) hidden(col int) bool {
	for _, h := range l.HiddenCols {
		if col >= h.Min && col <= h.Max {
			return true
		}
	}
	return false
}
