package ingest

import (
	"strings"
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

// normalize checks the options against the dataset's parser and fills the defaults. It returns a copy.
func (o *SheetOptions) normalize(c *collector, at string, d Dataset, where string) *SheetOptions {
	if d.Parser != ParserXLSX {
		if o != nil {
			c.add(ecSrcSheetParser, at, Params{"dataset": d.ID, "parser": d.Parser}, "%s: sheet options belong to the xlsx parser", where)
		}
		return nil
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
	n.check(c, at, d.ID, where)
	return &n
}

func (o SheetOptions) check(c *collector, at, dataset, where string) {
	p := Params{"dataset": dataset}
	if o.Name != "" && o.Index != 0 {
		c.add(ecSrcSheetBoth, at, p, "%s: sheet has both name and index: give one", where)
	}
	belowMin := o.Index < 0 || o.HeaderRow < 1 || o.HeaderRows < 1 || o.StopAfterBlankRows < 0
	if belowMin {
		c.add(ecSrcSheetMin, at, p, "%s: sheet options cannot be below their minimum (index 1, header_row 1, header_rows 1, stop_after_blank_rows 0)", where)
	}
	if o.MergedCells != MergeFirstOnly && o.MergedCells != MergeFill {
		c.add(ecSrcSheetMerged, at+ptr("merged_cells"), Params{"dataset": dataset, "merged_cells": o.MergedCells, "allowed": []string{MergeFirstOnly, MergeFill}},
			"%s: sheet merged_cells %q is not %s or %s", where, o.MergedCells, MergeFirstOnly, MergeFill)
	}
	if o.Range != "" {
		o.checkRange(c, at, dataset, where, belowMin)
	}
}

// checkRange checks the range of the data. Where the header rows are not meaningful, whether the range starts in
// them is not asked.
func (o SheetOptions) checkRange(c *collector, at, dataset, where string, belowMin bool) {
	b, ok := o.bounds()
	if !ok {
		c.add(ecSrcSheetRange, at+ptr("range"), Params{"dataset": dataset, "range": o.Range},
			"%s: sheet range %q is not a start cell and an end cell such as A5:F200", where, o.Range)
		return
	}
	if !belowMin && b.row1 <= o.HeaderRow+o.HeaderRows-1 {
		c.add(ecSrcSheetRangeHeader, at+ptr("range"), Params{"dataset": dataset, "range": o.Range, "header_first": o.HeaderRow, "header_last": o.HeaderRow + o.HeaderRows - 1},
			"%s: sheet range %q starts in the header (rows %d to %d): the range is the data, below it", where, o.Range, o.HeaderRow, o.HeaderRow+o.HeaderRows-1)
	}
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
