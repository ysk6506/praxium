package xlsx

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// rowParser reads the elements of a worksheet in the order they come and gives the rows that have a value, one at a time, in the order of
// the file. It does not sort: a row whose number is not above the one before, or a cell whose column is not above the one before in its
// row, is a file that is no workbook of a tool, and it is refused (ADR-0103). Both ways of reading a workbook are built on it.
type rowParser struct {
	dec  *xml.Decoder
	ctx  sheetContext
	lim  ingest.Limits
	name string // the name of the sheet, for the messages

	// row is the row being read and prevRow and prevCol are the row and the column of the element before, which an element
	// that has no address takes the next of.
	row     ingest.SheetRow
	prevRow int
	prevCol int
	// lastRow is the number of the last row that was given, lastCol the column of the last cell of the row being read.
	lastRow int
	lastCol int

	// What the parser met besides rows. The layout is complete only when the rows are over.
	merges     []ingest.Merge
	hiddenCols []ingest.ColSpan
	hiddenRows int
	colCount   int
}

func newRowParser(dec *xml.Decoder, ctx sheetContext, lim ingest.Limits, name string) *rowParser {
	return &rowParser{dec: dec, ctx: ctx, lim: lim, name: name}
}

// Next gives the next row that has a value, or false at the end of the sheet.
func (r *rowParser) Next() (ingest.SheetRow, bool, error) {
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return ingest.SheetRow{}, false, eofOrError(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := r.start(t); err != nil {
				return ingest.SheetRow{}, false, err
			}
		case xml.EndElement:
			if t.Name.Local != "row" {
				continue
			}
			if row, ok, err := r.endRow(); ok || err != nil {
				return row, ok, err
			}
		}
	}
}

func (r *rowParser) start(e xml.StartElement) error {
	switch e.Name.Local {
	case "col":
		return r.col(e)
	case "row":
		r.startRow(e)
	case "c":
		return r.cell(e)
	case "mergeCell":
		return r.merge(attr(e, "ref"))
	}
	return nil
}

func (r *rowParser) col(e xml.StartElement) error {
	if r.colCount++; r.colCount > maxColElements {
		return fixedLimit("max_col_elements", maxColElements, fmt.Sprintf("by sheet %q", r.name))
	}
	if isTrue(attr(e, "hidden")) {
		lo, _ := strconv.Atoi(attr(e, "min"))
		hi, _ := strconv.Atoi(attr(e, "max"))
		if lo >= 1 && hi >= lo {
			r.hiddenCols = append(r.hiddenCols, ingest.ColSpan{Min: lo, Max: hi})
		}
	}
	return nil
}

func (r *rowParser) startRow(e xml.StartElement) {
	n, err := strconv.Atoi(attr(e, "r"))
	if err != nil || n < 1 {
		n = r.prevRow + 1
	}
	r.prevRow, r.prevCol, r.lastCol = n, 0, 0
	r.row = ingest.SheetRow{Number: n, Hidden: isTrue(attr(e, "hidden"))}
	if r.row.Hidden {
		r.hiddenRows++
	}
}

// endRow ends the row being read. A row that has no cell with a value is no row.
func (r *rowParser) endRow() (ingest.SheetRow, bool, error) {
	row := r.row
	r.row = ingest.SheetRow{}
	if len(row.Cells) == 0 {
		return row, false, nil
	}
	if row.Number <= r.lastRow {
		return row, false, kernel.Errorf(kernel.CodeInvalidValue, "the workbook is damaged: the rows of sheet %q are not in order (row %d after row %d)", r.name, row.Number, r.lastRow)
	}
	r.lastRow = row.Number
	return row, true, nil
}

func (r *rowParser) merge(ref string) error {
	from, to, ok := strings.Cut(ref, ":")
	r1, c1, ok1 := ingest.ParseCellRef(from)
	r2, c2, ok2 := ingest.ParseCellRef(to)
	if !ok || !ok1 || !ok2 || r2 < r1 || c2 < c1 {
		return nil
	}
	if len(r.merges) >= maxMergeRanges {
		return fixedLimit("max_merge_ranges", maxMergeRanges, fmt.Sprintf("by sheet %q", r.name))
	}
	r.merges = append(r.merges, ingest.Merge{Row1: r1, Col1: c1, Row2: r2, Col2: c2})
	return nil
}

// rawCell is a cell as the XML gives it, before its value is read.
type rawCell struct {
	typ, style string
	value      string
	inline     string
	hasValue   bool
	formula    bool
}

func (r *rowParser) cell(e xml.StartElement) error {
	col := r.prevCol + 1
	if _, c, ok := ingest.ParseCellRef(attr(e, "r")); ok {
		col = c
	}
	r.prevCol = col
	raw := rawCell{typ: attr(e, "t"), style: attr(e, "s")}
	if err := r.cellBody(&raw); err != nil {
		return err
	}
	cell, keep, err := r.convert(raw)
	if err != nil {
		return fmt.Errorf("cell %s: %w", ingest.CellRef(r.row.Number, col), err)
	}
	if !keep {
		return nil
	}
	return r.keep(cell, col)
}

// keep puts a cell that has a value in the row, if the cell is within the limits and comes after the one before.
func (r *rowParser) keep(cell ingest.Cell, col int) error {
	if r.row.Number > r.lim.MaxRowsPerSheet {
		return overLimit("max_rows_per_sheet", r.lim.MaxRowsPerSheet, fmt.Sprintf("by sheet %q (a cell in row %d)", r.name, r.row.Number))
	}
	if col > r.lim.MaxColumnsPerSheet {
		return overLimit("max_columns_per_sheet", r.lim.MaxColumnsPerSheet, fmt.Sprintf("by sheet %q (a cell in column %s)", r.name, ingest.ColumnName(col)))
	}
	if col <= r.lastCol {
		return kernel.Errorf(kernel.CodeInvalidValue, "the workbook is damaged: the cells of row %d of sheet %q are not in order (column %s after column %s)", r.row.Number, r.name, ingest.ColumnName(col), ingest.ColumnName(r.lastCol))
	}
	r.lastCol = col
	cell.Col = col
	r.row.Cells = append(r.row.Cells, cell)
	return nil
}

// cellBody reads the children of a cell up to its end: the value, an inline string and the formula.
func (r *rowParser) cellBody(raw *rawCell) error {
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "v":
				raw.hasValue = true
				if err := r.text("v", &raw.value); err != nil {
					return err
				}
			case "is":
				s, err := richText(r.dec, "is", r.lim.MaxCellChars)
				raw.inline = s
				if err != nil {
					return err
				}
			case "f":
				raw.formula = true
			}
		case xml.EndElement:
			if t.Name.Local == "c" {
				return nil
			}
		}
	}
}

// text reads the characters of a simple element.
func (r *rowParser) text(end string, into *string) error {
	var b strings.Builder
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
			if b.Len() > maxTextBytes(r.lim.MaxCellChars) {
				return overLimit("max_cell_chars", r.lim.MaxCellChars, "by a cell of sheet "+fmt.Sprintf("%q", r.name))
			}
		case xml.EndElement:
			if t.Name.Local == end {
				*into = b.String()
				return nil
			}
		}
	}
}
