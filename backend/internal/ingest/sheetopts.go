package ingest

import (
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The ways a merged range of cells gives its value to the cells it covers.
const (
	// MergeFirstOnly keeps the value where the file has it, in the first cell of the range; the others are empty.
	MergeFirstOnly = "first_only"
	// MergeFill gives every cell of the range the value of the first, which is what a merged classification column
	// means to the person who made it.
	MergeFill = "fill"
)

// SheetOptions say which sheet of a workbook a dataset is and how its rows are laid out (ADR-0040). Everything has a
// default that reads a plain table: the first sheet, the header in row 1, the data below it to the end.
type SheetOptions struct {
	// Name is the name of the sheet; Index its place among the sheets of the workbook, from 1. One of them, and
	// with neither the first sheet is read.
	Name  string `json:"name,omitempty"`
	Index int    `json:"index,omitempty"`
	// HeaderRow is the row of the header, 1 by default. A header of more than one row (HeaderRows) is joined with a
	// space into one name per column, so "Spec" over "CPU" is the column "Spec CPU".
	HeaderRow  int `json:"header_row,omitempty"`
	HeaderRows int `json:"header_rows,omitempty"`
	// Range is where the data is, from its first cell to its last (A5:F200). The columns of the range are the
	// columns read, header included. Without it the data is below the header to the last row, in every column.
	Range string `json:"range,omitempty"`
	// MergedCells is first_only (default) or fill.
	MergedCells string `json:"merged_cells,omitempty"`
	// StopAfterBlankRows ends the data after that many blank rows in a row; what follows is not read. Blank rows are
	// always passed over. 0 never ends the data.
	StopAfterBlankRows int `json:"stop_after_blank_rows,omitempty"`
	// SkipHiddenRows leaves out the rows that are hidden; by default they are read, and the report says how many.
	SkipHiddenRows bool `json:"skip_hidden_rows,omitempty"`
}

// normalized checks the options against the parser and fills the defaults. It returns a copy.
func (o *SheetOptions) normalized(parser, where string) (*SheetOptions, error) {
	if parser != ParserXLSX {
		if o != nil {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s: sheet options belong to the xlsx parser", where)
		}
		return nil, nil
	}
	n := SheetOptions{}
	if o != nil {
		n = *o
	}
	if n.HeaderRow == 0 {
		n.HeaderRow = 1
	}
	if n.HeaderRows == 0 {
		n.HeaderRows = 1
	}
	if n.MergedCells == "" {
		n.MergedCells = MergeFirstOnly
	}
	if err := n.check(where); err != nil {
		return nil, err
	}
	return &n, nil
}

func (o SheetOptions) check(where string) error {
	bad := func(format string, args ...any) error {
		return kernel.Errorf(kernel.CodeInvalidValue, where+": sheet "+format, args...)
	}
	switch {
	case o.Name != "" && o.Index != 0:
		return bad("has both name and index: give one")
	case o.Index < 0 || o.HeaderRow < 1 || o.HeaderRows < 1 || o.StopAfterBlankRows < 0:
		return bad("options cannot be below their minimum (index 1, header_row 1, header_rows 1, stop_after_blank_rows 0)")
	case o.MergedCells != MergeFirstOnly && o.MergedCells != MergeFill:
		return bad("merged_cells %q is not %s or %s", o.MergedCells, MergeFirstOnly, MergeFill)
	}
	if o.Range == "" {
		return nil
	}
	b, ok := o.bounds()
	if !ok {
		return bad("range %q is not a start cell and an end cell such as A5:F200", o.Range)
	}
	if b.row1 <= o.HeaderRow+o.HeaderRows-1 {
		return bad("range %q starts in the header (rows %d to %d): the range is the data, below it", o.Range, o.HeaderRow, o.HeaderRow+o.HeaderRows-1)
	}
	return nil
}

// bounds are the data range as numbers; col2 and row2 are 0 where the options leave them open.
type bounds struct{ row1, col1, row2, col2 int }

func (o SheetOptions) bounds() (bounds, bool) {
	if o.Range == "" {
		return bounds{}, true
	}
	from, to, ok := strings.Cut(o.Range, ":")
	r1, c1, ok1 := ParseCellRef(from)
	r2, c2, ok2 := ParseCellRef(to)
	if !ok || !ok1 || !ok2 || r2 < r1 || c2 < c1 {
		return bounds{}, false
	}
	return bounds{r1, c1, r2, c2}, true
}
