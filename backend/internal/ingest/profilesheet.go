package ingest

import (
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// profileWorkbook describes every sheet of a workbook and the table of each sheet that is asked for (all of them, or the
// one named or numbered).
func profileWorkbook(fp FileProfile, wb *Workbook, o InspectOptions) (FileProfile, error) {
	picked, err := pickSheets(wb, o.Sheet)
	if err != nil {
		return fp, err
	}
	for i := range wb.Sheets {
		sh := &wb.Sheets[i]
		info := sheetInfo(sh)
		if picked[i] && sh.Kind == SheetKindWorksheet && len(sh.Rows) > 0 {
			row, rows, guessed := headerOf(sh, o)
			info.HeaderRow = row
			t := profileSheet(sh, row, rows, o.SampleRows)
			t.HeaderGuess = guessed
			fp.Tables = append(fp.Tables, t)
		}
		fp.Sheets = append(fp.Sheets, info)
	}
	return fp, nil
}

// pickSheets says which sheets to make a table of: all, or one by name or by number.
func pickSheets(wb *Workbook, which string) (map[int]bool, error) {
	picked := map[int]bool{}
	if which == "" {
		for i := range wb.Sheets {
			picked[i] = true
		}
		return picked, nil
	}
	n, numErr := strconv.Atoi(which)
	for i, s := range wb.Sheets {
		if s.Name == which || (numErr == nil && s.Index == n) {
			picked[i] = true
		}
	}
	if len(picked) == 0 {
		return nil, kernel.Errorf(kernel.CodeNotFound, "the workbook has no sheet %q (it has %s)", which, sheetNames(wb))
	}
	return picked, nil
}

func sheetInfo(sh *Sheet) SheetInfo {
	info := SheetInfo{Index: sh.Index, Name: sh.Name, Kind: sh.Kind, State: sh.State, HiddenRows: sh.HiddenRows, MergedRanges: len(sh.Merges)}
	r1, c1, r2, c2 := sh.UsedRange()
	if r2 > 0 {
		info.UsedRange = CellRef(r1, c1) + ":" + CellRef(r2, c2)
		info.Rows, info.Columns = r2, c2
	}
	for _, h := range sh.HiddenCols {
		info.HiddenColumns += max(0, min(h.Max, max(c2, 1))-h.Min+1)
	}
	return info
}

// headerOf is the header the options give, or the one guessed.
func headerOf(sh *Sheet, o InspectOptions) (row, rows int, guessed bool) {
	row, rows = o.HeaderRow, max(o.HeaderRows, 1)
	if row == 0 {
		row, guessed = guessHeaderRow(sh), true
	}
	return row, rows, guessed
}

// guessHeaderRow finds the row that looks like a header: of the first rows, the first one that has as many cells as
// the widest of them (or nearly), all of them text and none the same as another. A title in one merged cell above the
// table is no header, since it is narrower. Without such a row it is the first row that has anything.
func guessHeaderRow(sh *Sheet) int {
	const look = 30
	widest := 0
	for i, r := range sh.Rows {
		if i >= look {
			break
		}
		widest = max(widest, len(r.Cells))
	}
	for i, r := range sh.Rows {
		if i >= look || i == len(sh.Rows)-1 {
			break
		}
		if len(r.Cells) >= 2 && len(r.Cells)*2 >= widest && allDistinctText(r.Cells) {
			return r.Number
		}
	}
	return sh.Rows[0].Number
}

func allDistinctText(cells []Cell) bool {
	seen := map[string]bool{}
	for _, c := range cells {
		key := strings.ToLower(NormalizeHeader(c.Text))
		if c.Kind != CellText || key == "" || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

// profileSheet makes the table of a sheet whose header is in the given rows. Every column from the first to the last
// that has a value is a column, named by its header or by its place when the header has no name.
func profileSheet(sh *Sheet, headerRow, headerRows, sample int) TableProfile {
	g := newGrid(sh)
	scan := &sheetScan{o: SheetOptions{HeaderRow: headerRow, HeaderRows: headerRows, MergedCells: MergeFirstOnly}, g: g}
	_, c1, _, c2 := sh.UsedRange()
	tab := &grid{name: sh.Name, sampleSize: sample}
	for col := c1; col <= c2; col++ {
		name := scan.headerName(col)
		if name == "" {
			name = "(column " + ColumnName(col) + ")"
		}
		tab.accs = append(tab.accs, newAcc(name, ColumnName(col), sh.IsColHidden(col)))
	}
	first := headerRow + headerRows
	for i := range sh.Rows {
		r := &sh.Rows[i]
		if r.Number < first || blankIn(r, c1, c2) {
			continue
		}
		cells := make([]pcell, len(tab.accs))
		for _, c := range r.Cells {
			cells[c.Col-c1] = profileCell(c)
		}
		tab.addRow(cells)
	}
	t := tab.profile()
	t.HeaderRow, t.HeaderRows = headerRow, headerRows
	return t
}

func profileCell(c Cell) pcell {
	switch {
	case c.Kind == CellError:
		return pcell{issue: CellError, text: c.Text}
	case c.NoValue:
		return pcell{issue: "formula"}
	}
	return pcell{kind: cellType(c), text: c.Text}
}

func blankIn(r *SheetRow, c1, c2 int) bool {
	for _, c := range r.Cells {
		if c.Col >= c1 && c.Col <= c2 && (c.Kind != CellText || strings.TrimSpace(c.Text) != "") {
			return false
		}
	}
	return true
}
