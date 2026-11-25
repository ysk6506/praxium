package xlsx

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The lists that a reader keeps in memory, and the length of each text in them, are bounded by fixed values (ADR-0104). Each limit is
// tried at its value, which is read, and one over it, which is refused with the name of the limit.

func xsSheets(n int) string {
	var b strings.Builder
	b.WriteString(`<workbook><sheets>`)
	for i := range n {
		fmt.Fprintf(&b, `<sheet name="s%d" r:id="r%d"/>`, i, i)
	}
	b.WriteString(`</sheets></workbook>`)
	return b.String()
}

func xsRels(n int) string {
	var b strings.Builder
	b.WriteString(`<Relationships>`)
	for i := range n {
		fmt.Fprintf(&b, `<Relationship Id="r%d" Type="t" Target="x"/>`, i)
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

func xsFormats(n int) string {
	var b strings.Builder
	b.WriteString(`<styleSheet><numFmts>`)
	for i := range n {
		fmt.Fprintf(&b, `<numFmt numFmtId="%d" formatCode="0"/>`, 200+i)
	}
	b.WriteString(`</numFmts></styleSheet>`)
	return b.String()
}

func xsXfs(n int) string {
	return `<styleSheet><cellXfs>` + strings.Repeat(`<xf numFmtId="0"/>`, n) + `</cellXfs></styleSheet>`
}

// xsLimitErr runs a part through the reader of its kind and gives the error.
func xsLimitErr(kind, text string) error {
	r := strings.NewReader(text)
	switch kind {
	case "workbook":
		_, _, err := parseWorkbookPart("xl/workbook.xml", r)
		return err
	case "rels":
		_, err := parseRels("xl/_rels/workbook.xml.rels", r)
		return err
	}
	_, err := dateKinds(xmlDecoder("xl/styles.xml", r))
	return err
}

// [REQ-808]
func TestXsEveryListHasALimitThatIsReadAtItsValueAndRefusedOverIt(t *testing.T) {
	over := func(name string, limit int) string {
		return fmt.Sprintf("the workbook is over the safety limit %s (%d)", name, limit)
	}
	for _, c := range []struct {
		name, kind string
		at, over   string
		want       string
	}{
		{"sheets", "workbook", xsSheets(maxSheets), xsSheets(maxSheets + 1), over("max_sheets", maxSheets)},
		{"relationships", "rels", xsRels(maxRelationships), xsRels(maxRelationships + 1), over("max_relationships", maxRelationships)},
		{"number formats", "styles", xsFormats(maxNumberFormats), xsFormats(maxNumberFormats + 1), over("max_number_formats", maxNumberFormats)},
		{"styles", "styles", xsXfs(maxStyles), xsXfs(maxStyles + 1), over("max_styles", maxStyles)},
	} {
		if err := xsLimitErr(c.kind, c.at); err != nil {
			t.Errorf("%s at the limit: %v", c.name, err)
		}
		err := xsLimitErr(c.kind, c.over)
		if err == nil || kernel.CodeOf(err) != kernel.CodeInvalidValue || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "this limit is fixed") {
			t.Errorf("%s over the limit: %v", c.name, err)
		}
	}
}

// [REQ-808]
func TestXsEveryTextOfAListHasALengthThatIsReadAtItsValueAndRefusedOverIt(t *testing.T) {
	rep := func(n int) string { return strings.Repeat("a", n) }
	sheet := func(attrs string) string { return `<workbook><sheets><sheet ` + attrs + `/></sheets></workbook>` }
	rel := func(attrs string) string {
		return `<Relationships><Relationship Type="t" Target="x" Id="i" ` + attrs + `/></Relationships>`
	}
	format := func(code string) string {
		return `<styleSheet><numFmts><numFmt numFmtId="200" formatCode="` + code + `"/></numFmts></styleSheet>`
	}
	for _, c := range []struct {
		name, kind string
		text       func(n int) string
		limit      int
		want       string
	}{
		{"a sheet name", "workbook", func(n int) string { return sheet(`name="` + rep(n) + `"`) }, maxSheetNameBytes, "max_sheet_name_bytes"},
		{"the state of a sheet", "workbook", func(n int) string { return sheet(`name="s" state="` + rep(n) + `"`) }, maxSheetStateBytes, "max_sheet_state_bytes"},
		{"the relationship of a sheet", "workbook", func(n int) string { return sheet(`name="s" r:id="` + rep(n) + `"`) }, maxRelIDBytes, "max_relationship_id_bytes"},
		{"the id of a relationship", "rels", func(n int) string { return strings.Replace(rel(""), `Id="i"`, `Id="`+rep(n)+`"`, 1) }, maxRelIDBytes, "max_relationship_id_bytes"},
		{"the type of a relationship", "rels", func(n int) string { return strings.Replace(rel(""), `Type="t"`, `Type="`+rep(n)+`"`, 1) }, maxRelTypeBytes, "max_relationship_type_bytes"},
		{"the target of a relationship", "rels", func(n int) string { return strings.Replace(rel(""), `Target="x"`, `Target="`+rep(n)+`"`, 1) }, maxRelTargetBytes, "max_relationship_target_bytes"},
		{"a number format", "styles", func(n int) string { return format(rep(n)) }, maxFormatCodeBytes, "max_number_format_bytes"},
	} {
		if err := xsLimitErr(c.kind, c.text(c.limit)); err != nil {
			t.Errorf("%s of the length of the limit: %v", c.name, err)
		}
		err := xsLimitErr(c.kind, c.text(c.limit+1))
		if err == nil || kernel.CodeOf(err) != kernel.CodeInvalidValue || !strings.Contains(err.Error(), "the safety limit "+c.want+" ("+fmt.Sprint(c.limit)+")") {
			t.Errorf("%s over the limit: %v", c.name, err)
		}
	}
}

// [REQ-808]
func TestXsASheetHasALimitOnItsColumnEntriesAndItsMergedRanges(t *testing.T) {
	cols := func(n int) string { return `<cols>` + strings.Repeat(`<col min="1" max="1"/>`, n) + `</cols>` }
	merges := func(n int) string {
		return `<mergeCells>` + strings.Repeat(`<mergeCell ref="A1:B2"/>`, n) + `</mergeCells>`
	}
	for _, c := range []struct {
		name, want string
		limit      int
		xml        func(n int) string
	}{
		{"column entries", "max_col_elements", maxColElements, cols},
		{"merged ranges", "max_merge_ranges", maxMergeRanges, merges},
	} {
		book := func(n int) []byte { return xsRawBook(c.xml(n)+`<sheetData/>`, "", "") }
		if _, err := (Reader{}).ReadWorkbook(book(c.limit), ingest.Limits{}); err != nil {
			t.Errorf("%s at the limit: %v", c.name, err)
		}
		_, err := Reader{}.ReadWorkbook(book(c.limit+1), ingest.Limits{})
		if err == nil || !strings.Contains(err.Error(), "the safety limit "+c.want+" ("+fmt.Sprint(c.limit)+") by sheet \"Raw\"") {
			t.Errorf("%s over the limit: %v", c.name, err)
		}
	}
}

// [REQ-808]
func TestXsTheTextOfACellIsBoundedWhileItIsGatheredFromPieces(t *testing.T) {
	// a text of the most characters, written as escapes of seven bytes each, is read; a text that grows by many pieces over the
	// bound is refused before it is whole
	escaped := strings.Repeat("_x0041_", ingest.DefaultMaxCellChars)
	book := xsRawBook(`<sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>`+escaped+`</t></is></c><c r="B1" t="str"><v>`+escaped+`</v></c></row></sheetData>`, "", "")
	wb, err := Reader{}.ReadWorkbook(book, ingest.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got := wb.Sheets[0].Rows[0].Cells; len(got[0].Text) != ingest.DefaultMaxCellChars || len(got[1].Text) != ingest.DefaultMaxCellChars {
		t.Fatalf("the cells have %d and %d characters", len(got[0].Text), len(got[1].Text))
	}
	pieces := strings.Repeat(`<![CDATA[`+strings.Repeat("a", 1000)+`]]>`, 700)
	for name, cell := range map[string]string{
		"an inline text": `<c r="A1" t="inlineStr"><is><t>` + pieces + `</t></is></c>`,
		"a value":        `<c r="A1" t="str"><v>` + pieces + `</v></c>`,
	} {
		_, err := Reader{}.ReadWorkbook(xsRawBook(`<sheetData><row r="1">`+cell+`</row></sheetData>`, "", ""), ingest.Limits{})
		if err == nil || !strings.Contains(err.Error(), "max_cell_chars (32767)") {
			t.Errorf("%s: %v", name, err)
		}
	}
	shared := `<si><t>` + pieces + `</t></si>`
	_, err = Reader{}.ReadWorkbook(xsRawBook(`<sheetData/>`, shared, ""), ingest.Limits{})
	if err == nil || !strings.Contains(err.Error(), "max_cell_chars (32767)") {
		t.Errorf("a shared string: %v", err)
	}
}

// [REQ-809]
func TestXsAnErrorOfThePlaceTheBytesComeFromIsNotSaidToBeAnErrorOfTheFile(t *testing.T) {
	source := ingest.NewSourceError(ingest.SrcConnectionLost, "the storage lost the connection", nil)
	for name, err := range map[string]error{
		"the error itself":         source,
		"the error in a wrapper":   fmt.Errorf("read: %w", source),
		"a context that is done":   context.Canceled,
		"a context that timed out": fmt.Errorf("slow: %w", context.DeadlineExceeded),
	} {
		if got := damaged("xl/p.xml", err); got != err {
			t.Errorf("%s: got %v", name, got)
		}
	}
	if got := damaged("xl/p.xml", io.ErrUnexpectedEOF); !strings.Contains(got.Error(), "the workbook is damaged: the part xl/p.xml cannot be read") {
		t.Errorf("an error of the file: %v", got)
	}
	limit := overLimit("max_cell_chars", 5, "by a cell")
	if got := damaged("xl/p.xml", fmt.Errorf("cell A1: %w", limit)); kernel.CodeOf(got) != kernel.CodeInvalidValue || !strings.Contains(got.Error(), "max_cell_chars") {
		t.Errorf("a limit: %v", got)
	}

	// the same error, coming out of the bytes of a part that is being read, reaches the one who asked
	dec := xmlDecoder("xl/p.xml", io.MultiReader(strings.NewReader("<a>"), iotest.ErrReader(source)))
	var err error
	for err == nil {
		_, err = dec.Token()
	}
	if got := damaged("xl/p.xml", err); got != error(source) && !strings.Contains(got.Error(), "lost the connection") {
		t.Errorf("an error of the reader of a part: %v", got)
	}
}
