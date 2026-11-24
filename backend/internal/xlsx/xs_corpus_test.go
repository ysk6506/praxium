package xlsx

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The workbooks that the reader reads as the code before the row parser read them (commit 8aecc37). The answer of each hand-written
// workbook is written down whole in testdata/xs_read_hand.json, and the digest of each workbook a seeded generator makes in
// testdata/xs_read_random.json. The workbooks that are read differently on purpose are apart (xs_changed_test.go).

// xsCase is one input of the reader: the bytes of a file and the limits to read them with.
type xsCase struct {
	name string
	data []byte
	lim  ingest.Limits
}

// xsAnswer is what the reader says of a file: the workbook it read, or the code and the message of the error it gave.
type xsAnswer struct {
	Workbook *ingest.Workbook `json:"workbook,omitempty"`
	Code     string           `json:"code,omitempty"`
	Error    string           `json:"error,omitempty"`
}

func xsRead(c xsCase) xsAnswer {
	wb, err := Reader{}.ReadWorkbook(c.data, c.lim)
	if err != nil {
		return xsAnswer{Code: string(kernel.CodeOf(err)), Error: err.Error()}
	}
	return xsAnswer{Workbook: wb}
}

// xsDigest is the SHA-256 of the JSON of an answer.
func xsDigest(a xsAnswer) string {
	b, err := json.Marshal(a)
	if err != nil {
		return "marshal: " + err.Error()
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// xsPart is one part of an archive, in the order the archive lists it.
type xsPart struct{ name, body string }

// xsArchive writes the parts as they are given, stored without compression.
func xsArchive(parts ...xsPart) []byte {
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, p := range parts {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Store})
		if err != nil {
			panic(err)
		}
		_, _ = w.Write([]byte(p.body))
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return out.Bytes()
}

const (
	xsDecl  = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
	xsMain  = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	xsRelNS = `xmlns="http://schemas.openxmlformats.org/package/2006/relationships"`
	xsRelTy = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
)

// xsRawBook is a workbook of one sheet whose XML is given as it is, with the shared strings and the styles when they are not empty.
func xsRawBook(sheet, shared, styles string) []byte {
	rels := `<Relationship Id="rId1" Type="` + xsRelTy + `worksheet" Target="worksheets/sheet1.xml"/>`
	parts := []xsPart{
		{"_rels/.rels", xsDecl + `<Relationships ` + xsRelNS + `><Relationship Id="rId1" Type="` + xsRelTy + `officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", xsDecl + `<workbook ` + xsMain + `><sheets><sheet name="Raw" sheetId="1" r:id="rId1"/></sheets></workbook>`},
	}
	if shared != "" {
		rels += `<Relationship Id="rId2" Type="` + xsRelTy + `sharedStrings" Target="sharedStrings.xml"/>`
		parts = append(parts, xsPart{"xl/sharedStrings.xml", xsDecl + `<sst ` + xsMain + `>` + shared + `</sst>`})
	}
	if styles != "" {
		rels += `<Relationship Id="rId3" Type="` + xsRelTy + `styles" Target="styles.xml"/>`
		parts = append(parts, xsPart{"xl/styles.xml", xsDecl + `<styleSheet ` + xsMain + `>` + styles + `</styleSheet>`})
	}
	parts = append(parts, xsPart{"xl/_rels/workbook.xml.rels", xsDecl + `<Relationships ` + xsRelNS + `>` + rels + `</Relationships>`},
		xsPart{"xl/worksheets/sheet1.xml", xsDecl + `<worksheet ` + xsMain + `>` + sheet + `</worksheet>`})
	return xsArchive(parts...)
}

func xsBookBytes(b *Book) []byte {
	data, err := b.Bytes()
	if err != nil {
		panic(err)
	}
	return data
}

// xsToolBook is a workbook with every kind of cell and every layout element, written the way a tool would.
func xsToolBook(set func(*Book)) []byte {
	b := &Book{}
	set(b)
	p := b.Sheet("서버 목록")
	p.Row(1, Str("Asset"), Str("Name"), Str("Count"), Str("Seen"), Str("OK"), Str("Note"))
	p.Row(2, Str("#1"), Str("a & b <c>"), Num("12"), Dated("46095", `yyyy\-mm\-dd`), Bool(true), Err("#N/A"))
	p.Row(3, Str("#2"), Str("  padded  "), Num("1.23456789012346E+019"), DatedID("46096", 14), Bool(false), Formula("A1&B1", Str("cached")))
	p.Row(5, Str("#3"), Str("가나다"), Num("0.1"), ISODate("2026-03-04"), Uncalculated("SUM(A1:A2)"), Str(""))
	p.Merges = []string{"A1:B1", "D5:E6"}
	p.HiddenRows, p.HiddenCols = []int{3, 9}, []string{"C", "E:F"}
	b.Sheet("hid").State = "hidden"
	b.Sheet("very").State = "veryHidden"
	b.Sheet("chart").Chart = true
	b.Sheet("empty")
	return xsBookBytes(b)
}

func xsHandCases() []xsCase {
	var out []xsCase
	add := func(name string, data []byte) { out = append(out, xsCase{name: name, data: data}) }
	add("inline strings", xsToolBook(func(b *Book) {}))
	add("inline strings with explicit n and absolute targets", xsToolBook(func(b *Book) { b.AbsoluteTargets, b.ExplicitN = true, true }))
	add("shared strings in runs and words for booleans", xsToolBook(func(b *Book) { b.SharedStrings, b.RichText, b.WordBool = true, true, true }))
	add("shared strings with a prefix on every element", xsToolBook(func(b *Book) { b.Prefix, b.SharedStrings = "x", true }))
	add("date system of 1904", xsToolBook(func(b *Book) { b.Date1904 = true }))
	add("macro and external parts that are not followed", xsToolBook(func(b *Book) {
		b.Macro = true
		b.Parts = map[string]string{"xl/externalLinks/externalLink1.xml": "<externalLink>this is not even XML", "xl/connections.xml": "<connections>nor this"}
	}))
	add("a bare number is a plain decimal", xsBookBytes(func() *Book {
		b := &Book{}
		b.Sheet("n").Row(1, Num("1E-07"), Num("-0"), Num("100"), Num("1.5E+3"), Num("abc"), Num("007"))
		return b
	}()))
	out = append(out, xsHandRaw()...)
	out = append(out, xsHandBroken()...)
	return out
}

// xsHandRaw are the sheets whose XML a writer would not make: no addresses, odd columns and merges, comments and CDATA.
func xsHandRaw() []xsCase {
	row := func(body string) []byte { return xsRawBook(`<sheetData>`+body+`</sheetData>`, "", "") }
	shared := `<si><t>one</t></si><si><r><t>tw</t></r><r><t>o</t></r><rPh><t>x</t></rPh></si><si><t xml:space="preserve">  three  </t></si>`
	cases := []xsCase{
		{name: "rows and cells without addresses", data: row(`<row><c t="inlineStr"><is><t>a</t></is></c><c t="inlineStr"><is><t>b</t></is></c></row><row><c r="C2" t="inlineStr"><is><t>c</t></is></c><c t="inlineStr"><is><t>d</t></is></c></row>`)},
		{name: "a row number that is not a number, and a row with no cell", data: row(`<row r="x"><c r="A1" t="inlineStr"><is><t>a</t></is></c></row><row r="7" hidden="1"/><row r="0"><c t="b"><v>1</v></c></row>`)},
		{name: "shared strings", data: xsRawBook(`<sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v> 1 </v></c><c r="C1" t="s"><v>2</v></c></row></sheetData>`, shared, "")},
		{name: "text of a formula and of a date", data: row(`<row r="1"><c r="A1" t="str"><f>A2</f><v>x_x0041_y</v></c><c r="B1" t="d"><v>2026-01-02T03:04:05</v></c><c r="C1" t="d"><v>nope</v></c></row>`)},
		{name: "a value in CDATA and a comment in the row", data: row(`<row r="1"><!-- a comment --><c r="A1"><v><![CDATA[12]]></v></c><c r="B1" t="inlineStr"><is><t><![CDATA[a<b]]></t></is></c></row>`)},
		{name: "columns and merges that are odd", data: xsRawBook(`<cols><col min="2" max="3" hidden="1"/><col min="0" max="1" hidden="1"/><col min="5" max="4" hidden="true"/><col min="7" max="7"/></cols><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>a</t></is></c></row></sheetData><mergeCells><mergeCell ref="A1:C1"/><mergeCell ref="C3:A1"/><mergeCell ref="x"/><mergeCell ref="B2:B2"/></mergeCells>`, "", "")},
		{name: "merges and columns before the data", data: xsRawBook(`<mergeCells><mergeCell ref="A1:B2"/></mergeCells><cols><col min="1" max="1" hidden="1"/></cols><sheetData><row r="2"><c r="B2" t="inlineStr"><is><t>b</t></is></c></row></sheetData>`, "", "")},
		{name: "styles that make a date and a formula that has no value", data: xsRawBook(`<sheetData><row r="1"><c r="A1" s="1"><v>46095.5</v></c><c r="B1" s="2"><v>46095</v></c><c r="C1" s="9"><v>5</v></c><c r="D1"><f>1+1</f></c><c r="E1"><v/></c><c r="F1" t="e"><v> #REF! </v></c></row></sheetData>`, "",
			`<numFmts><numFmt numFmtId="164" formatCode="yyyy\-mm\-dd hh:mm"/><numFmt numFmtId="165" formatCode="0.00"/></numFmts><cellXfs><xf numFmtId="0"/><xf numFmtId="164"/><xf numFmtId="165"/></cellXfs>`)},
	}
	return cases
}

// xsHandBroken are the files the reader refuses, with the limits it was given.
func xsHandBroken() []xsCase {
	good := xsBookBytes(func() *Book { b := &Book{}; b.Sheet("s").Row(1, Str("a")); return b }())
	var out []xsCase
	add := func(name string, data []byte, lim ingest.Limits) {
		out = append(out, xsCase{name: name, data: data, lim: lim})
	}
	add("empty", nil, ingest.Limits{})
	add("text", []byte("a,b\n1,2\n"), ingest.Limits{})
	add("cut short", good[:len(good)/2], ingest.Limits{})
	add("a sheet that is no part", xsArchive(xsPart{"xl/workbook.xml", xsDecl + `<workbook ` + xsMain + `><sheets><sheet name="s" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		xsPart{"xl/_rels/workbook.xml.rels", `<Relationships><Relationship Id="rId1" Type="x/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`}), ingest.Limits{})
	add("a sheet that has no relationship, an external one and a chart", xsArchive(xsPart{"xl/workbook.xml", xsDecl + `<workbook ` + xsMain + `><sheets><sheet name="a" r:id="rId9"/><sheet name="b" r:id="rId2"/><sheet name="c" r:id="rId3"/></sheets></workbook>`},
		xsPart{"xl/_rels/workbook.xml.rels", `<Relationships><Relationship Id="rId2" Type="x/worksheet" Target="https://example.invalid/s.xml" TargetMode="External"/><Relationship Id="rId3" Type="x/chartsheet" Target="c.xml"/></Relationships>`}), ingest.Limits{})
	add("a shared string that does not exist", xsRawBook(`<sheetData><row r="1"><c r="A1" t="s"><v>9</v></c></row></sheetData>`, `<si><t>x</t></si>`, ""), ingest.Limits{})
	add("a broken sheet", xsRawBook(`<sheetData><row r="1"><c r="A1" t="b"><v>1</v></c>`, "", ""), ingest.Limits{})
	add("rows over the limit", xsRawBook(`<sheetData><row r="5"><c r="A5" t="b"><v>1</v></c></row></sheetData>`, "", ""), ingest.Limits{MaxRowsPerSheet: 4})
	add("columns over the limit", xsRawBook(`<sheetData><row r="1"><c r="E1" t="b"><v>1</v></c></row></sheetData>`, "", ""), ingest.Limits{MaxColumnsPerSheet: 3})
	add("a cell of too many characters", xsRawBook(`<sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>`+strings.Repeat("가", 11)+`</t></is></c></row></sheetData>`, "", ""), ingest.Limits{MaxCellChars: 10})
	add("too many shared strings", xsRawBook(`<sheetData/>`, `<si><t>a</t></si><si><t>b</t></si><si><t>c</t></si>`, ""), ingest.Limits{MaxSharedStrings: 2})
	add("more unpacked bytes than the limit", xsRawBook(`<sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>`+strings.Repeat("a", 300_000)+`</t></is></c></row></sheetData>`, "", ""), ingest.Limits{MaxCellChars: 1 << 20, MaxUncompressedBytes: 100_000})
	return out
}
