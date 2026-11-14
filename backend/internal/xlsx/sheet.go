package xlsx

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// readSheet reads the part of a worksheet into the sheet.
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
	sr := &sheetReader{dec: xmlDecoder(rc), ctx: ctx, sh: sh, lim: p.lim}
	if err := sr.read(); err != nil {
		return damaged(f.Name, err)
	}
	sh.Sorted()
	return nil
}

// sheetReader reads the elements of a worksheet in the order they come.
type sheetReader struct {
	dec *xml.Decoder
	ctx sheetContext
	sh  *ingest.Sheet
	lim ingest.Limits
	// row is the row being read and prevCol the column of the cell before.
	row     ingest.SheetRow
	prevRow int
	prevCol int
}

func (r *sheetReader) read() error {
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return eofOrError(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := r.start(t); err != nil {
				return err
			}
		case xml.EndElement:
			if t.Name.Local == "row" {
				r.endRow()
			}
		}
	}
}

func (r *sheetReader) start(e xml.StartElement) error {
	switch e.Name.Local {
	case "col":
		if isTrue(attr(e, "hidden")) {
			lo, _ := strconv.Atoi(attr(e, "min"))
			hi, _ := strconv.Atoi(attr(e, "max"))
			if lo >= 1 && hi >= lo {
				r.sh.HiddenCols = append(r.sh.HiddenCols, ingest.ColSpan{Min: lo, Max: hi})
			}
		}
	case "row":
		r.startRow(e)
	case "c":
		return r.cell(e)
	case "mergeCell":
		r.merge(attr(e, "ref"))
	}
	return nil
}

func (r *sheetReader) startRow(e xml.StartElement) {
	n, err := strconv.Atoi(attr(e, "r"))
	if err != nil || n < 1 {
		n = r.prevRow + 1
	}
	r.prevRow, r.prevCol = n, 0
	r.row = ingest.SheetRow{Number: n, Hidden: isTrue(attr(e, "hidden"))}
	if r.row.Hidden {
		r.sh.HiddenRows++
	}
}

func (r *sheetReader) endRow() {
	if len(r.row.Cells) > 0 {
		r.sh.Rows = append(r.sh.Rows, r.row)
	}
	r.row = ingest.SheetRow{}
}

func (r *sheetReader) merge(ref string) {
	from, to, ok := strings.Cut(ref, ":")
	r1, c1, ok1 := ingest.ParseCellRef(from)
	r2, c2, ok2 := ingest.ParseCellRef(to)
	if ok && ok1 && ok2 && r2 >= r1 && c2 >= c1 {
		r.sh.Merges = append(r.sh.Merges, ingest.Merge{Row1: r1, Col1: c1, Row2: r2, Col2: c2})
	}
}

// rawCell is a cell as the XML gives it, before its value is read.
type rawCell struct {
	typ, style string
	value      string
	inline     string
	hasValue   bool
	formula    bool
}

func (r *sheetReader) cell(e xml.StartElement) error {
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
	if r.row.Number > r.lim.MaxRowsPerSheet {
		return overLimit("max_rows_per_sheet", r.lim.MaxRowsPerSheet, fmt.Sprintf("by sheet %q (a cell in row %d)", r.sh.Name, r.row.Number))
	}
	if col > r.lim.MaxColumnsPerSheet {
		return overLimit("max_columns_per_sheet", r.lim.MaxColumnsPerSheet, fmt.Sprintf("by sheet %q (a cell in column %s)", r.sh.Name, ingest.ColumnName(col)))
	}
	cell.Col = col
	r.row.Cells = append(r.row.Cells, cell)
	return nil
}

// cellBody reads the children of a cell up to its end: the value, an inline string and the formula.
func (r *sheetReader) cellBody(raw *rawCell) error {
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
				if err := r.inlineText(raw); err != nil {
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
func (r *sheetReader) text(end string, into *string) error {
	var b strings.Builder
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.EndElement:
			if t.Name.Local == end {
				*into = b.String()
				return nil
			}
		}
	}
}

func (r *sheetReader) inlineText(raw *rawCell) error {
	s, err := richText(r.dec, "is")
	raw.inline = s
	return err
}

// convert turns a raw cell into a cell of the pipeline. keep is false for a cell that has no value.
func (r *sheetReader) convert(raw rawCell) (ingest.Cell, bool, error) {
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
func (r *sheetReader) text2(c ingest.Cell, text string) (ingest.Cell, bool, error) {
	if text == "" {
		return c, false, nil
	}
	if len(text) > r.lim.MaxCellChars && utf8.RuneCountInString(text) > r.lim.MaxCellChars {
		return c, false, overLimit("max_cell_chars", r.lim.MaxCellChars, "by a cell of sheet "+fmt.Sprintf("%q", r.sh.Name))
	}
	c.Kind, c.Text = ingest.CellText, text
	return c, true, nil
}

func (r *sheetReader) dateText(c ingest.Cell, v string) (ingest.Cell, bool, error) {
	if text, ok := isoDate(v); ok {
		c.Kind, c.Text = ingest.CellDate, text
		return c, true, nil
	}
	c.Kind, c.Text = ingest.CellError, "#VALUE!"
	return c, true, nil
}

// number is a cell of the number type (the default): a date when its style says so.
func (r *sheetReader) number(c ingest.Cell, raw rawCell) (ingest.Cell, bool, error) {
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

func (r *sheetReader) styleKind(style string) int {
	i, err := strconv.Atoi(style)
	if err != nil || i < 0 || i >= len(r.ctx.kinds) {
		return notDate
	}
	return r.ctx.kinds[i]
}
