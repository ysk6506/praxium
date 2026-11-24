package ingest

import "strings"

// xsSkipVariant is the workbook of the case with the cells of the data rows that the dataset does not read left without text, as
// a reader that builds only the columns that are wanted gives them: a cell that is there, with whether the rule of a blank cell
// calls it blank (white space only, or a formula with no stored value). The first cell of a merged range keeps its text. It is
// nil when there is nothing to leave out.
func xsSkipVariant(c xsCase) *Workbook {
	if c.read == nil || c.ds.Sheet == nil {
		return nil
	}
	first := c.ds.Sheet.HeaderRow + c.ds.Sheet.HeaderRows
	changed := false
	wb := &Workbook{Sheets: make([]Sheet, len(c.wb.Sheets))}
	for i, sh := range c.wb.Sheets {
		wb.Sheets[i] = sh
		if sh.Name != "Target" {
			continue
		}
		origin := map[[2]int]bool{}
		for _, m := range sh.Merges {
			origin[[2]int{m.Row1, m.Col1}] = true
		}
		rows := make([]SheetRow, len(sh.Rows))
		for j, r := range sh.Rows {
			rows[j] = r
			rows[j].Cells = append([]Cell(nil), r.Cells...)
			if r.Number < first {
				continue
			}
			for k, cell := range rows[j].Cells {
				if c.read[cell.Col] || origin[[2]int{r.Number, cell.Col}] {
					continue
				}
				rows[j].Cells[k] = Cell{Col: cell.Col, Skipped: true, Blank: cell.Kind == CellText && strings.TrimSpace(cell.Text) == ""}
				changed = true
			}
		}
		wb.Sheets[i].Rows = rows
	}
	if !changed {
		return nil
	}
	return wb
}
