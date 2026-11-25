package xlsx

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// readSheet reads the part of a worksheet into the sheet: the row parser gives the rows in the order of the file, and the layout
// it met on the way (merged ranges, hidden columns, the count of hidden rows) is put in the sheet when the rows are over.
func (p *pkg) readSheet(sh *ingest.Sheet, name string, ctx sheetContext) error {
	f := p.part(name)
	if f == nil {
		return kernel.Errorf(kernel.CodeInvalidValue, "the workbook is damaged: it lists sheet %q but the archive has no part %s", sh.Name, name)
	}
	rc, err := p.open(f)
	if err != nil {
		return err
	}
	defer rc.Close()
	rp := newRowParser(xmlDecoder(f.Name, rc), ctx, p.lim, sh.Name)
	for {
		row, ok, err := rp.Next()
		if err != nil {
			return damaged(f.Name, err)
		}
		if !ok {
			break
		}
		sh.Rows = append(sh.Rows, row)
	}
	sh.Merges, sh.HiddenCols, sh.HiddenRows = rp.merges, rp.hiddenCols, rp.hiddenRows
	return nil
}

// convert turns a raw cell into a cell of the pipeline. keep is false for a cell that has no value.
func (r *rowParser) convert(raw rawCell) (ingest.Cell, bool, error) {
	c := ingest.Cell{Formula: raw.formula}
	switch raw.typ {
	case "s":
		i, err := strconv.Atoi(strings.TrimSpace(raw.value))
		if err != nil || i < 0 || i >= len(r.ctx.strings) {
			return c, false, fmt.Errorf("the shared string %q does not exist", raw.value)
		}
		return r.text2(c, r.ctx.strings[i])
	case "inlineStr":
		return r.text2(c, raw.inline)
	case "str":
		return r.text2(c, unescapeXString(raw.value))
	case "b":
		c.Kind, c.Text = ingest.CellBool, strconv.FormatBool(isTrue(strings.TrimSpace(raw.value)))
		return c, true, nil
	case "e":
		c.Kind, c.Text = ingest.CellError, strings.TrimSpace(raw.value)
		return c, true, nil
	case "d":
		return r.dateText(c, raw.value)
	}
	return r.number(c, raw)
}

// text2 is a cell of text; an empty text is no value.
func (r *rowParser) text2(c ingest.Cell, text string) (ingest.Cell, bool, error) {
	if text == "" {
		return c, false, nil
	}
	if len(text) > r.lim.MaxCellChars && utf8.RuneCountInString(text) > r.lim.MaxCellChars {
		return c, false, overLimit("max_cell_chars", r.lim.MaxCellChars, "by a cell of sheet "+fmt.Sprintf("%q", r.name))
	}
	c.Kind, c.Text = ingest.CellText, text
	return c, true, nil
}

func (r *rowParser) dateText(c ingest.Cell, v string) (ingest.Cell, bool, error) {
	if text, ok := isoDate(v); ok {
		c.Kind, c.Text = ingest.CellDate, text
		return c, true, nil
	}
	c.Kind, c.Text = ingest.CellError, "#VALUE!"
	return c, true, nil
}

// number is a cell of the number type (the default): a date when its style says so.
func (r *rowParser) number(c ingest.Cell, raw rawCell) (ingest.Cell, bool, error) {
	v := strings.TrimSpace(raw.value)
	if v == "" {
		if raw.formula {
			c.NoValue = true
			c.Kind = ingest.CellText
			return c, true, nil
		}
		return c, false, nil
	}
	text, ok := plainNumber(v)
	if !ok {
		c.Kind, c.Text = ingest.CellError, "#VALUE!"
		return c, true, nil
	}
	c.Kind, c.Text = ingest.CellNumber, text
	if kind := r.styleKind(raw.style); kind != notDate {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			if d, ok := excelDate(f, r.ctx.date1904, kind); ok {
				c.Kind, c.Text = ingest.CellDate, d
			}
		}
	}
	return c, true, nil
}

func (r *rowParser) styleKind(style string) int {
	i, err := strconv.Atoi(style)
	if err != nil || i < 0 || i >= len(r.ctx.kinds) {
		return notDate
	}
	return r.ctx.kinds[i]
}
