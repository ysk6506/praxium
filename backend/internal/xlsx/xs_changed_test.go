package xlsx

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The files that are read differently since the row parser gives the rows in the order of the file (ADR-0103). Before it, the reader
// sorted the rows and the cells and read two rows with the same number as one row's cells twice. The messages below are written by hand
// from the rule: a row whose number is not above the last row that has a value, or a cell whose column is not above the one before it
// in its row, is refused with the place named.

func xsBoolRow(ref string) string { return `<c r="` + ref + `" t="b"><v>1</v></c>` }

// [REQ-806]
func TestXsRowsAndCellsThatAreNotInOrderAreRefusedWithThePlaceNamed(t *testing.T) {
	cell := xsBoolRow
	for _, c := range []struct{ name, sheet, want string }{
		{"rows out of order", `<row r="3">` + cell("A3") + `</row><row r="2">` + cell("A2") + `</row>`, `the rows of sheet "Raw" are not in order (row 2 after row 3)`},
		{"two rows with one number", `<row r="2">` + cell("A2") + `</row><row r="2">` + cell("B2") + `</row>`, `the rows of sheet "Raw" are not in order (row 2 after row 2)`},
		{"a low row between two rows that are high", `<row r="4">` + cell("A4") + `</row><row r="2">` + cell("A2") + `</row><row r="9">` + cell("A9") + `</row>`, `(row 2 after row 4)`},
		{"cells out of order", `<row r="2">` + cell("B2") + cell("A2") + `</row>`, `the cells of row 2 of sheet "Raw" are not in order (column A after column B)`},
		{"two cells in one column", `<row r="5">` + cell("C5") + cell("C5") + `</row>`, `the cells of row 5 of sheet "Raw" are not in order (column C after column C)`},
	} {
		_, err := Reader{}.ReadWorkbook(xsRawBook(`<sheetData>`+c.sheet+`</sheetData>`, "", ""), ingest.Limits{})
		if err == nil || kernel.CodeOf(err) != kernel.CodeInvalidValue || !strings.Contains(err.Error(), "the workbook is damaged: "+c.want) && !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v", c.name, err)
		}
	}
}

// [REQ-806]
func TestXsRowsThatHaveNoValueAreNotPartOfTheOrder(t *testing.T) {
	sheet := `<sheetData><row r="9" hidden="1"/><row r="3">` + xsBoolRow("A3") + `</row><row r="2"/><row r="5">` + xsBoolRow("A5") + `</row><row r="4"><c r="A4"/></row></sheetData>`
	wb, err := Reader{}.ReadWorkbook(xsRawBook(sheet, "", ""), ingest.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	var nums []int
	for _, r := range wb.Sheets[0].Rows {
		nums = append(nums, r.Number)
	}
	if len(nums) != 2 || nums[0] != 3 || nums[1] != 5 || wb.Sheets[0].HiddenRows != 1 {
		t.Fatalf("rows %v, %d hidden", nums, wb.Sheets[0].HiddenRows)
	}
}
