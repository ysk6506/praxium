package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// This file is a writer of workbooks for tests. It is not a general writer: it makes the files the readers of the
// real tools have to cope with, and its options are the differences between the tools that were found by writing the
// same workbook with openpyxl and with LibreOffice (ADR-0040). The product never calls it.

const (
	nsMain = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	nsRel  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsPkg  = "http://schemas.openxmlformats.org/package/2006/relationships"
	relTyp = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
)

// C is one cell to write.
type C struct {
	kind    string // "s" text, "n" number, "b" bool, "e" error, "str" formula text, "" empty
	raw     string
	formula string
	// NoCache writes a formula without a stored value.
	noCache bool
	// FmtCode is a custom number format; FmtID a built-in one (when FmtCode is empty).
	fmtCode string
	fmtID   int
	dateISO bool
}

// Str is a text cell.
func Str(s string) C { return C{kind: "s", raw: s} }

// Num is a number cell whose text is written as it is, so a test can write "1.23456789012346E+019" the way
// LibreOffice does.
func Num(text string) C { return C{kind: "n", raw: text} }

// Bool is a boolean cell.
func Bool(b bool) C {
	if b {
		return C{kind: "b", raw: "1"}
	}
	return C{kind: "b", raw: "0"}
}

// Err is an error cell such as #N/A.
func Err(code string) C { return C{kind: "e", raw: code} }

// Dated is a number cell shown with a custom number format code, such as yyyy\-mm\-dd.
func Dated(serial, code string) C { return C{kind: "n", raw: serial, fmtCode: code} }

// DatedID is a number cell shown with a built-in number format.
func DatedID(serial string, id int) C { return C{kind: "n", raw: serial, fmtID: id} }

// ISODate is a cell of the date type (t="d").
func ISODate(s string) C { return C{kind: "d", raw: s} }

// Formula is a formula cell with the value the file stored for it.
func Formula(f string, cached C) C {
	cached.formula = f
	return cached
}

// Uncalculated is a formula cell that has no stored value, as openpyxl writes it.
func Uncalculated(f string) C { return C{kind: "n", formula: f, noCache: true} }

// Page is one sheet to write.
type Page struct {
	Name       string
	State      string // "", "hidden" or "veryHidden"
	cells      map[[2]int]C
	Merges     []string
	HiddenRows []int
	HiddenCols []string // "H" or "C:E"
	// Chart makes the sheet a chart sheet that holds no cells.
	Chart bool
}

// Set writes a cell by its address.
func (p *Page) Set(ref string, c C) *Page {
	r, col, _ := ingest.ParseCellRef(ref)
	if p.cells == nil {
		p.cells = map[[2]int]C{}
	}
	p.cells[[2]int{r, col}] = c
	return p
}

// Row writes the cells of one row from column A on; an empty C leaves a gap.
func (p *Page) Row(row int, cells ...C) *Page {
	for i, c := range cells {
		if c.kind != "" || c.formula != "" {
			p.Set(ingest.CellRef(row, i+1), c)
		}
	}
	return p
}

// Book is a workbook to write, with the options that differ between the tools.
type Book struct {
	Pages    []*Page
	Date1904 bool
	// SharedStrings writes text as shared strings, as LibreOffice and Excel do; otherwise it is inline text, as this
	// openpyxl does.
	SharedStrings bool
	// RichText writes shared strings as runs with fonts and a phonetic hint.
	RichText bool
	// Prefix writes every element of the main namespace with a prefix (x:worksheet), as some libraries do.
	Prefix string
	// AbsoluteTargets writes the targets of the relationships as /xl/worksheets/sheet1.xml.
	AbsoluteTargets bool
	// ExplicitN writes t="n" on number cells; WordBool writes true instead of 1 on boolean attributes.
	ExplicitN, WordBool bool
	// Macro adds a macro part and the content type of a macro-enabled workbook.
	Macro bool
	// Parts are further parts to put in the archive (external links, connections, anything).
	Parts map[string]string
}

// Sheet adds a sheet.
func (b *Book) Sheet(name string) *Page {
	p := &Page{Name: name}
	b.Pages = append(b.Pages, p)
	return p
}

// el writes the name of an element of the main namespace with the prefix.
func (b *Book) el(name string) string {
	if b.Prefix == "" {
		return name
	}
	return b.Prefix + ":" + name
}

func (b *Book) ns() string {
	if b.Prefix == "" {
		return ` xmlns="` + nsMain + `" xmlns:r="` + nsRel + `"`
	}
	return ` xmlns:` + b.Prefix + `="` + nsMain + `" xmlns:r="` + nsRel + `"`
}

func (b *Book) flag(on bool) string {
	switch {
	case !on:
		return "0"
	case b.WordBool:
		return "true"
	}
	return "1"
}

func esc(s string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(s))
	return out.String()
}

// Bytes writes the workbook file.
func (b *Book) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	put := func(name, body string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(body))
		return err
	}
	st := newStyles()
	sst := &sharedTable{index: map[string]int{}}
	sheets := make([]string, len(b.Pages))
	for i, p := range b.Pages {
		sheets[i] = b.sheetXML(p, st, sst)
	}
	parts := [][2]string{
		{"[Content_Types].xml", b.contentTypes()},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="` + nsPkg + `"><Relationship Id="rId1" Type="` + relTyp + `officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", b.workbookXML()},
		{"xl/_rels/workbook.xml.rels", b.relsXML()},
		{"xl/styles.xml", b.stylesXML(st)},
	}
	for i, s := range sheets {
		parts = append(parts, [2]string{fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), s})
	}
	if b.SharedStrings {
		parts = append(parts, [2]string{"xl/sharedStrings.xml", b.sstXML(sst)})
	}
	if b.Macro {
		parts = append(parts, [2]string{"xl/vbaProject.bin", "not a real macro project"})
	}
	names := make([]string, 0, len(b.Parts))
	for n := range b.Parts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		parts = append(parts, [2]string{n, b.Parts[n]})
	}
	for _, pt := range parts {
		if err := put(pt[0], pt[1]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (b *Book) contentTypes() string {
	main := "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"
	if b.Macro {
		main = "application/vnd.ms-excel.sheet.macroEnabled.main+xml"
	}
	var s strings.Builder
	s.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	s.WriteString(`<Override PartName="/xl/workbook.xml" ContentType="` + main + `"/></Types>`)
	return s.String()
}

func (b *Book) workbookXML() string {
	var s strings.Builder
	s.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><` + b.el("workbook") + b.ns() + `>`)
	if b.Date1904 {
		s.WriteString(`<` + b.el("workbookPr") + ` date1904="` + b.flag(true) + `"/>`)
	}
	s.WriteString(`<` + b.el("sheets") + `>`)
	for i, p := range b.Pages {
		state := p.State
		if state == "" {
			state = "visible"
		}
		s.WriteString(fmt.Sprintf(`<%s name="%s" sheetId="%d" state="%s" r:id="rId%d"/>`, b.el("sheet"), esc(p.Name), i+1, state, i+1))
	}
	s.WriteString(`</` + b.el("sheets") + `></` + b.el("workbook") + `>`)
	return s.String()
}

func (b *Book) relsXML() string {
	var s strings.Builder
	s.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="` + nsPkg + `">`)
	target := func(n string) string {
		if b.AbsoluteTargets {
			return "/xl/" + n
		}
		return n
	}
	for i, p := range b.Pages {
		typ, file := "worksheet", fmt.Sprintf("worksheets/sheet%d.xml", i+1)
		if p.Chart {
			typ = "chartsheet"
		}
		s.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" Type="%s%s" Target="%s"/>`, i+1, relTyp, typ, target(file)))
	}
	n := len(b.Pages)
	s.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" Type="%sstyles" Target="%s"/>`, n+1, relTyp, target("styles.xml")))
	if b.SharedStrings {
		s.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" Type="%ssharedStrings" Target="%s"/>`, n+2, relTyp, target("sharedStrings.xml")))
	}
	s.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" Type="%sexternalLink" Target="file:///C:/other/book.xlsx" TargetMode="External"/>`, n+3, relTyp))
	s.WriteString(`</Relationships>`)
	return s.String()
}
