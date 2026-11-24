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

// ParseSheet turns the sheet of a workbook that a dataset names into a table of rows, by the options of the dataset.
// It is a function of the workbook and the dataset. A row is a received row when it is not blank; every row that
// is passed over is counted with its reason (blank_row, hidden_row, after_end), and a cell that gave no value is
// listed with its address. It feeds the rows of the sheet to a SheetScanner, which decides what they mean (ADR-0103). A
// sheet whose rows are not in order cannot be read: the table says so as its reason.
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
	sc := NewSheetScanner(d, *o, SheetHead{Index: sh.Index, Name: sh.Name, Kind: sh.Kind, State: sh.State}, SheetLayout{Merges: sh.Merges, HiddenCols: sh.HiddenCols})
	for _, r := range sh.Rows {
		step, err := sc.Row(r)
		if err != nil {
			return unusableSheet(t, err)
		}
		t.fold(step.Table)
		if step.Done {
			break
		}
	}
	end := sc.Finish()
	if end.Err != nil {
		return unusableSheet(t, end.Err)
	}
	if shape, ok := sc.Header(); ok {
		t.Missing, t.Extra, t.Header, t.HeaderRow = shape.Missing, shape.Extra, shape.Header, o.HeaderRow
	}
	t.HiddenColsRead = end.HiddenColsRead
	return t
}

// unusableSheet is the table of a sheet that cannot be read: nothing of it is used, and the reason says why.
func unusableSheet(t Table, err error) Table {
	return Table{Sheet: t.Sheet, SheetState: t.SheetState, Reason: plainMessage(err)}
}

// fold adds what one row of a scan gave to the table of the sheet.
func (t *Table) fold(step Table) {
	t.Rows = append(t.Rows, step.Rows...)
	t.Unreadable = append(t.Unreadable, step.Unreadable...)
	t.CellIssues = append(t.CellIssues, step.CellIssues...)
	t.HiddenRowsRead += step.HiddenRowsRead
	for reason, n := range step.Skipped {
		t.Skipped[reason] += n
	}
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
