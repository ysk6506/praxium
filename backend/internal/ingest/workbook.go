package ingest

import (
	"sort"
	"strconv"
	"strings"
)

// The kinds of a cell of a workbook. A cell that has no value is not stored at all.
const (
	CellText   = "text"
	CellNumber = "number"
	CellBool   = "bool"
	CellDate   = "date"
	CellError  = "error"
)

// The states of a sheet, as the workbook says them.
const (
	SheetVisible    = "visible"
	SheetHidden     = "hidden"
	SheetVeryHidden = "very_hidden"
)

// SheetKindWorksheet is a sheet of cells. A workbook can have other sheets (a chart on a sheet of its own), which are
// listed and never read.
const SheetKindWorksheet = "worksheet"

// Cell is one cell that has a value, in the text form the pipeline works with: an integer has no ".0", a large number
// is never written with an exponent, a date is an ISO 8601 text, a boolean is true or false. For an error cell the
// text is the error the cell shows (#N/A).
type Cell struct {
	Col  int
	Kind string
	Text string
	// Formula is true when the cell holds a formula; the text is then the value the file stored for it.
	Formula bool
	// NoValue is true when a formula has no stored value (the file was never calculated): the text is empty.
	NoValue bool
}

// SheetRow is the cells of one row that have a value, in the order of the columns.
type SheetRow struct {
	Number int
	Hidden bool
	Cells  []Cell
}

// Merge is a range of merged cells, rows and columns counted from 1.
type Merge struct{ Row1, Col1, Row2, Col2 int }

// ColSpan is columns that share a property, counted from 1.
type ColSpan struct{ Min, Max int }

// Sheet is one sheet of a workbook as the reader found it: its cells, the merged ranges and what is hidden.
type Sheet struct {
	Name  string
	Index int
	Kind  string
	State string
	// Rows holds the rows that have a cell with a value, ordered by row number.
	Rows       []SheetRow
	Merges     []Merge
	HiddenCols []ColSpan
	// HiddenRows counts the rows that are hidden, also those that have no cell.
	HiddenRows int
}

// Workbook is what a reader makes of the bytes of a workbook file: the sheets in the order of their tabs.
type Workbook struct {
	Sheets []Sheet
}

// Limits are the safety limits of the reader of a workbook (ingestion.md 3.4). A file over one is refused, and the
// error names the limit. A limit of 0 is the default.
type Limits struct {
	MaxUncompressedBytes int64 `json:"max_uncompressed_bytes,omitempty"`
	MaxRowsPerSheet      int   `json:"max_rows_per_sheet,omitempty"`
	MaxColumnsPerSheet   int   `json:"max_columns_per_sheet,omitempty"`
	MaxCellChars         int   `json:"max_cell_chars,omitempty"`
	MaxSharedStrings     int   `json:"max_shared_strings,omitempty"`
}

// The defaults of the limits. Excel itself allows 1,048,576 rows, 16,384 columns and 32,767 characters in a cell;
// the defaults keep what one run holds in memory to a size a small machine can carry.
const (
	DefaultMaxUncompressedBytes = 256 << 20
	DefaultMaxRowsPerSheet      = 200_000
	DefaultMaxColumnsPerSheet   = 1_024
	DefaultMaxCellChars         = 32_767
	DefaultMaxSharedStrings     = 2_000_000
)

// WithDefaults gives the limits with every unset one at its default.
func (l Limits) WithDefaults() Limits {
	if l.MaxUncompressedBytes <= 0 {
		l.MaxUncompressedBytes = DefaultMaxUncompressedBytes
	}
	if l.MaxRowsPerSheet <= 0 {
		l.MaxRowsPerSheet = DefaultMaxRowsPerSheet
	}
	if l.MaxColumnsPerSheet <= 0 {
		l.MaxColumnsPerSheet = DefaultMaxColumnsPerSheet
	}
	if l.MaxCellChars <= 0 {
		l.MaxCellChars = DefaultMaxCellChars
	}
	if l.MaxSharedStrings <= 0 {
		l.MaxSharedStrings = DefaultMaxSharedStrings
	}
	return l
}

// WorkbookReader is the port of the readers of workbook files. It reads bytes only: it opens no file and follows
// no link of the workbook.
type WorkbookReader interface {
	ReadWorkbook(data []byte, limits Limits) (*Workbook, error)
}

// ColumnName is the letters of a column: 1 is A, 27 is AA.
func ColumnName(col int) string {
	var b []byte
	for col > 0 {
		col--
		b = append([]byte{byte('A' + col%26)}, b...)
		col /= 26
	}
	return string(b)
}

// ColumnNumber is the column the letters name, or 0 when they are not letters of a column.
func ColumnNumber(letters string) int {
	n := 0
	for _, r := range strings.ToUpper(letters) {
		if r < 'A' || r > 'Z' {
			return 0
		}
		n = n*26 + int(r-'A') + 1
		if n > 1<<20 {
			return 0
		}
	}
	return n
}

// CellRef is the address of a cell, such as C12.
func CellRef(row, col int) string { return ColumnName(col) + strconv.Itoa(row) }

// ParseCellRef reads an address such as C12 or $C$12.
func ParseCellRef(ref string) (row, col int, ok bool) {
	ref = strings.ReplaceAll(strings.TrimSpace(ref), "$", "")
	i := 0
	for i < len(ref) && (ref[i] >= 'A' && ref[i] <= 'Z' || ref[i] >= 'a' && ref[i] <= 'z') {
		i++
	}
	col = ColumnNumber(ref[:i])
	n, err := strconv.Atoi(ref[i:])
	if col == 0 || err != nil || n < 1 {
		return 0, 0, false
	}
	return n, col, true
}

// Sorted puts the rows and the cells in order. Readers call it once the sheet is read.
func (s *Sheet) Sorted() {
	sort.SliceStable(s.Rows, func(a, b int) bool { return s.Rows[a].Number < s.Rows[b].Number })
	for i := range s.Rows {
		cells := s.Rows[i].Cells
		sort.SliceStable(cells, func(a, b int) bool { return cells[a].Col < cells[b].Col })
	}
}

// IsColHidden tells whether a column is hidden.
func (s Sheet) IsColHidden(col int) bool {
	for _, h := range s.HiddenCols {
		if col >= h.Min && col <= h.Max {
			return true
		}
	}
	return false
}

// UsedRange is the smallest range that holds every cell with a value: the first and last row and column. It is all
// zero for a sheet that has none.
func (s Sheet) UsedRange() (row1, col1, row2, col2 int) {
	for i, r := range s.Rows {
		if i == 0 {
			row1 = r.Number
		}
		row2 = r.Number
		for _, c := range r.Cells {
			if col1 == 0 || c.Col < col1 {
				col1 = c.Col
			}
			if c.Col > col2 {
				col2 = c.Col
			}
		}
	}
	return row1, col1, row2, col2
}
