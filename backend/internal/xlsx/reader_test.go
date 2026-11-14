package xlsx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func mustBytes(t testing.TB, b *Book) []byte {
	t.Helper()
	data, err := b.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func read(t testing.TB, b *Book, lim ingest.Limits) *ingest.Workbook {
	t.Helper()
	wb, err := Reader{}.ReadWorkbook(mustBytes(t, b), lim)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return wb
}

// cellsOf lists the cells of a sheet as "A1=kind:text".
func cellsOf(s ingest.Sheet) []string {
	var out []string
	for _, r := range s.Rows {
		for _, c := range r.Cells {
			out = append(out, fmt.Sprintf("%s=%s:%s", ingest.CellRef(r.Number, c.Col), c.Kind, c.Text))
		}
	}
	return out
}

// toolBook writes one workbook the way a tool would. The content is the same for every tool: a Korean sheet name, text,
// a number stored as a number, a text with leading zeros, a date, a boolean, a merged range, a hidden column, a hidden
// row, a hidden sheet.
func toolBook(tool string) *Book {
	b := &Book{}
	switch tool {
	case "openpyxl": // inline strings, absolute targets, t="n", formulas without a stored value
		b.AbsoluteTargets, b.ExplicitN = true, true
	case "libreoffice": // shared strings in runs, relative targets, words for booleans, stored formula values
		b.SharedStrings, b.RichText, b.WordBool = true, true, true
	case "prefixed": // another library: every element has a prefix
		b.Prefix, b.SharedStrings = "x", true
	}
	p := b.Sheet("서버목록")
	date := Dated("46095", `yyyy\-mm\-dd`)
	if tool == "openpyxl" {
		date = Dated("46095", "yyyy-mm-dd")
	}
	p.Row(1, Str("자산번호"), Str("이름"), Str("분류"), Str("코드"), Str("도입일"), Bool(true))
	p.Row(2, Num("10001"), Str("웹-01"), Str("웹"), Str("00123"), date, Bool(false))
	p.Set("C3", Str("")) // merged below, no value
	p.Merges = []string{"C2:C3"}
	p.HiddenCols = []string{"D"}
	p.HiddenRows = []int{3}
	h := b.Sheet("Hidden")
	h.State = "hidden"
	h.Row(1, Str("a"))
	return b
}

// [REQ-284]
func TestSameWorkbookWrittenByEachToolReadsTheSame(t *testing.T) {
	want := []string{"A1=text:자산번호", "B1=text:이름", "C1=text:분류", "D1=text:코드", "E1=text:도입일", "F1=bool:true",
		"A2=number:10001", "B2=text:웹-01", "C2=text:웹", "D2=text:00123", "E2=date:2026-03-14", "F2=bool:false"}
	for _, tool := range []string{"openpyxl", "libreoffice", "prefixed"} {
		wb := read(t, toolBook(tool), ingest.Limits{})
		if len(wb.Sheets) != 2 || wb.Sheets[0].Name != "서버목록" || wb.Sheets[1].State != ingest.SheetHidden {
			t.Fatalf("%s: sheets %+v", tool, wb.Sheets)
		}
		s := wb.Sheets[0]
		if got := cellsOf(s); strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s:\n got %v\nwant %v", tool, got, want)
		}
		if len(s.Merges) != 1 || s.Merges[0] != (ingest.Merge{Row1: 2, Col1: 3, Row2: 3, Col2: 3}) {
			t.Errorf("%s: merges %v", tool, s.Merges)
		}
		if !s.IsColHidden(4) || s.IsColHidden(3) || s.HiddenRows != 1 {
			t.Errorf("%s: hidden %v %d", tool, s.HiddenCols, s.HiddenRows)
		}
		if wb.Sheets[0].Index != 1 || wb.Sheets[1].Index != 2 {
			t.Errorf("%s: indexes", tool)
		}
	}
}

// [REQ-284]
func TestNumbersAreWrittenInPlainDecimalForm(t *testing.T) {
	cases := map[string]string{
		"10001": "10001", "1.5": "1.5", "1.0": "1", "0.30000000000000004": "0.30000000000000004", "-0": "0", "-2.50": "-2.5",
		"1E+20": "100000000000000000000", "1.23456789012346E+019": "12345678901234600000", "1.234567890123457e+19": "12345678901234570000",
		"1e-07": "0.0000001", "1.5E-3": "0.0015", ".5": "0.5", "5.": "5", "+7": "7", "000123": "123", "1234567890123456789": "1234567890123456789",
	}
	for in, want := range cases {
		if got, ok := plainNumber(in); !ok || got != want {
			t.Errorf("%q: got %q %v, want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "abc", "1e999", "1.2.3", "--1", "e5", "NaN"} {
		if got, ok := plainNumber(bad); ok {
			t.Errorf("%q should not be a number, got %q", bad, got)
		}
	}
}

// [REQ-284]
func TestDatesFollowTheStyleAndTheDateSystem(t *testing.T) {
	type tc struct {
		cell     C
		date1904 bool
		want     string
	}
	cases := []tc{
		{Dated("46095", `yyyy\-mm\-dd`), false, "date:2026-03-14"},
		{Dated("46096.3958333333", `yyyy\-mm\-dd\ h:mm:ss`), false, "date:2026-03-15T09:30:00Z"},
		{Dated("46096.39583333334", "yyyy-mm-dd h:mm:ss"), false, "date:2026-03-15T09:30:00Z"},
		{Dated("44634", `yyyy"년" m"월" d"일"`), false, "date:2022-03-14"},
		{Dated("44634", "[$-412]yyyy-mm-dd"), false, "date:2022-03-14"},
		{Dated("44634.5", "dd/mm/yyyy"), false, "date:2022-03-14"},
		{Dated("0.3960069444444445", "h:mm:ss"), false, "date:09:30:15"},
		{Dated("1.253472222222222", "[h]:mm:ss"), false, "date:30:05:00"},
		{Dated("44634", "mmm-yy"), false, "date:2022-03-14"},
		{Dated("61", "yyyy-mm-dd"), false, "date:1900-03-01"},
		{Dated("60", "yyyy-mm-dd"), false, "date:1900-02-29"},
		{Dated("59", "yyyy-mm-dd"), false, "date:1900-02-28"},
		{Dated("1", "yyyy-mm-dd"), false, "date:1900-01-01"},
		{Dated("0", "yyyy-mm-dd"), false, "number:0"},
		{Dated("-5", "yyyy-mm-dd"), false, "number:-5"},
		{Dated("3000000", "yyyy-mm-dd"), false, "number:3000000"},
		{Dated("44634", "yyyy-mm-dd"), true, "date:2026-03-15"},
		{Dated("0", "yyyy-mm-dd"), true, "date:1904-01-01"},
		{DatedID("44634", 14), false, "date:2022-03-14"},
		{DatedID("44634.75", 22), false, "date:2022-03-14T18:00:00Z"},
		{DatedID("0.5", 18), false, "date:12:00:00"},
		{Dated("0.256", "0.00%"), false, "number:0.256"},
		{Dated("12345.678", "0.00E+00"), false, "number:12345.678"},
		{Dated("1500", `#,##0 "d"`), false, "number:1500"},
		{Dated("3", `0 "일"`), false, "number:3"},
		{Dated("3.25", "[Red]0.0"), false, "number:3.25"},
		{Dated("7", "General"), false, "number:7"},
		{ISODate("2026-03-14"), false, "date:2026-03-14"},
		{ISODate("2026-03-14T09:30:00"), false, "date:2026-03-14T09:30:00Z"},
		{ISODate("2026-03-14T09:30:00+09:00"), false, "date:2026-03-14T00:30:00Z"},
		{ISODate("garbage"), false, "error:#VALUE!"},
	}
	for _, c := range cases {
		b := &Book{Date1904: c.date1904}
		b.Sheet("s").Set("A1", c.cell)
		wb := read(t, b, ingest.Limits{})
		got := cellsOf(wb.Sheets[0])
		if len(got) != 1 || got[0] != "A1="+c.want {
			t.Errorf("%+v: got %v, want %s", c.cell, got, c.want)
		}
	}
}

// [REQ-284]
func TestStringsKeepTheirTextExactly(t *testing.T) {
	for _, shared := range []bool{false, true} {
		b := &Book{SharedStrings: shared, RichText: shared}
		p := b.Sheet("s")
		p.Row(1, Str("00123"), Str(" padded "), Str("line\none"), Str("a_x000D_b"), Str("_x005F_x0041_"), Str("<&>\"'"), Str("한글 😀"), Str("12345678901234567890"))
		wb := read(t, b, ingest.Limits{})
		want := []string{"A1=text:00123", "B1=text: padded ", "C1=text:line\none", "D1=text:a\rb", "E1=text:_x0041_", "F1=text:<&>\"'", "G1=text:한글 😀", "H1=text:12345678901234567890"}
		if got := cellsOf(wb.Sheets[0]); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("shared=%v:\n got %q\nwant %q", shared, got, want)
		}
	}
}

// [REQ-284]
func TestFormulaCellsGiveTheStoredValue(t *testing.T) {
	b := &Book{}
	p := b.Sheet("s")
	p.Set("A1", Formula("1+2", Num("3")))
	p.Set("A2", Uncalculated("SUM(A1:A1)"))
	p.Set("A3", Formula("1/0", Err("#DIV/0!")))
	p.Set("A4", Formula(`"a"&"b"`, Str("ab")))
	p.Set("A5", Formula("TRUE()", Bool(true)))
	p.Set("A6", Err("#N/A"))
	wb := read(t, b, ingest.Limits{})
	rows := wb.Sheets[0].Rows
	var kinds []string
	for _, r := range rows {
		c := r.Cells[0]
		kinds = append(kinds, fmt.Sprintf("%s:%s:%v:%v", c.Kind, c.Text, c.Formula, c.NoValue))
	}
	want := []string{"number:3:true:false", "text::true:true", "error:#DIV/0!:true:false", "text:ab:true:false", "bool:true:true:false", "error:#N/A:false:false"}
	if strings.Join(kinds, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v\nwant %v", kinds, want)
	}
}

// [REQ-284]
func TestHiddenSheetsRowsAndColumnsAreReadAndMarked(t *testing.T) {
	b := &Book{}
	vis := b.Sheet("visible")
	vis.Row(1, Str("a"), Str("b"), Str("c"), Str("d"))
	vis.HiddenRows = []int{1, 5}
	vis.HiddenCols = []string{"B", "C:D"}
	b.Sheet("hid").State = "hidden"
	b.Sheet("very").State = "veryHidden"
	b.Sheet("chart").Chart = true
	wb := read(t, b, ingest.Limits{})
	var states []string
	for _, s := range wb.Sheets {
		states = append(states, s.Name+":"+s.State+":"+s.Kind)
	}
	if strings.Join(states, " ") != "visible:visible:worksheet hid:hidden:worksheet very:very_hidden:worksheet chart:visible:other" {
		t.Fatalf("%v", states)
	}
	s := wb.Sheets[0]
	if !s.Rows[0].Hidden || s.HiddenRows != 2 || !s.IsColHidden(2) || !s.IsColHidden(4) || s.IsColHidden(1) || s.IsColHidden(5) {
		t.Fatalf("hidden: %+v", s)
	}
}

// [REQ-284]
func TestMacroPartsAndExternalLinksAreNotFollowed(t *testing.T) {
	b := &Book{Macro: true, Parts: map[string]string{
		"xl/externalLinks/externalLink1.xml":            "<externalLink>this is not even XML",
		"xl/externalLinks/_rels/externalLink1.xml.rels": `<Relationships><Relationship Id="rId1" Type="x/externalLinkPath" Target="https://example.invalid/data.xlsx" TargetMode="External"/></Relationships>`,
		"xl/connections.xml":                            "<connections>nor this",
	}}
	b.Sheet("s").Row(1, Str("kept"))
	wb := read(t, b, ingest.Limits{})
	if got := cellsOf(wb.Sheets[0]); len(got) != 1 || got[0] != "A1=text:kept" {
		t.Fatalf("%v", got)
	}
}

// [REQ-284]
func TestSheetsAreFoundByRelationshipNotByFileName(t *testing.T) {
	b := &Book{}
	b.Sheet("first").Row(1, Str("one"))
	b.Sheet("second").Row(1, Str("two"))
	data := mustBytes(t, b)
	// the first sheet in the workbook is the part sheet2.xml, as when sheets are reordered
	swapped := rewrite(t, data, func(name, body string) string {
		if name == "xl/_rels/workbook.xml.rels" {
			body = strings.Replace(body, `Id="rId1"`, `Id="rIdX"`, 1)
			body = strings.Replace(body, `Id="rId2"`, `Id="rId1"`, 1)
			body = strings.Replace(body, `Id="rIdX"`, `Id="rId2"`, 1)
		}
		return body
	})
	wb, err := Reader{}.ReadWorkbook(swapped, ingest.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got := cellsOf(wb.Sheets[0]); got[0] != "A1=text:two" {
		t.Fatalf("%v", got)
	}
}

// rewrite writes a workbook again with every part passed through a function.
func rewrite(t testing.TB, data []byte, fn func(name, body string) string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		rc, _ := f.Open()
		var body bytes.Buffer
		_, _ = body.ReadFrom(rc)
		rc.Close()
		w, _ := zw.Create(f.Name)
		_, _ = w.Write([]byte(fn(f.Name, body.String())))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func codeOf(err error) kernel.Code { return kernel.CodeOf(err) }
