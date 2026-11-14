package xlsx

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// zipOf makes an archive of the parts, in a fixed order of names.
func zipOf(t testing.TB, parts map[string]string, flags uint16) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "mimetype", "xl/workbook.bin", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "word/document.xml", "xl/worksheets/sheet1.xml"} {
		body, ok := parts[name]
		if !ok {
			continue
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Flags: flags})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// [REQ-285]
func TestFilesThatCannotBeReadAreRefusedWithAClearError(t *testing.T) {
	good := mustBytes(t, func() *Book { b := &Book{}; b.Sheet("s").Row(1, Str("a")); return b }())
	ole := append(append([]byte{}, oleSignature...), make([]byte, 600)...)
	encrypted := append(append([]byte{}, ole...), utf16Bytes("EncryptedPackage")...)
	cases := []struct {
		name string
		data []byte
		code kernel.Code
		want string
	}{
		{"empty", nil, kernel.CodeInvalidValue, "the file is empty"},
		{"old binary format", ole, kernel.CodeUnsupported, "old binary Excel format (.xls)"},
		{"password", encrypted, kernel.CodeUnsupported, "has a password"},
		{"text", []byte("a,b\n1,2\n"), kernel.CodeInvalidValue, "does not start like a ZIP archive"},
		{"cut short", good[:len(good)/2], kernel.CodeInvalidValue, "damaged or cut short"},
		{"encrypted archive", zipOf(t, map[string]string{"xl/workbook.xml": "<workbook/>"}, 0x1), kernel.CodeUnsupported, "encrypted"},
		{"binary workbook", zipOf(t, map[string]string{"xl/workbook.bin": "x"}, 0), kernel.CodeUnsupported, ".xlsb"},
		{"OpenDocument", zipOf(t, map[string]string{"mimetype": "application/vnd.oasis.opendocument.spreadsheet"}, 0), kernel.CodeUnsupported, ".ods"},
		{"another document", zipOf(t, map[string]string{"word/document.xml": "<w/>"}, 0), kernel.CodeInvalidValue, "no xl/workbook.xml"},
		{"sheet part missing", zipOf(t, map[string]string{
			"xl/workbook.xml":            `<workbook xmlns="m" xmlns:r="r"><sheets><sheet name="s" sheetId="1" r:id="rId1"/></sheets></workbook>`,
			"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Type="x/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`}, 0), kernel.CodeInvalidValue, "has no part xl/worksheets/sheet1.xml"},
		{"broken XML", rewrite(t, good, func(name, body string) string {
			if name == "xl/worksheets/sheet1.xml" {
				return body[:len(body)/2]
			}
			return body
		}), kernel.CodeInvalidValue, "part xl/worksheets/sheet1.xml cannot be read"},
		{"missing shared string", rewrite(t, mustBytes(t, func() *Book {
			b := &Book{SharedStrings: true}
			b.Sheet("s").Row(1, Str("a"))
			return b
		}()), func(name, body string) string {
			if name == "xl/worksheets/sheet1.xml" {
				return strings.Replace(body, "<v>0</v>", "<v>9</v>", 1)
			}
			return body
		}), kernel.CodeInvalidValue, "shared string"},
	}
	for _, c := range cases {
		_, err := Reader{}.ReadWorkbook(c.data, ingest.Limits{})
		if err == nil || kernel.CodeOf(err) != c.code || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v (%s), want %s containing %q", c.name, err, kernel.CodeOf(err), c.code, c.want)
		}
	}
}

// [REQ-285]
func TestFilesOverALimitAreRefusedAndTheErrorNamesTheLimit(t *testing.T) {
	book := func(fill func(p *Page), opts ...func(*Book)) *Book {
		b := &Book{}
		for _, o := range opts {
			o(b)
		}
		fill(b.Sheet("Servers"))
		return b
	}
	shared := func(b *Book) { b.SharedStrings = true }
	cases := []struct {
		name  string
		b     *Book
		lim   ingest.Limits
		limit string
		where string
	}{
		{"rows", book(func(p *Page) { p.Set("A5", Str("x")) }), ingest.Limits{MaxRowsPerSheet: 4}, "max_rows_per_sheet", `"Servers"`},
		{"columns", book(func(p *Page) { p.Set("E1", Str("x")) }), ingest.Limits{MaxColumnsPerSheet: 3}, "max_columns_per_sheet", "column E"},
		{"cell characters", book(func(p *Page) { p.Set("A1", Str(strings.Repeat("가", 11))) }), ingest.Limits{MaxCellChars: 10}, "max_cell_chars", ""},
		{"cell characters of a shared string", book(func(p *Page) { p.Set("A1", Str(strings.Repeat("a", 11))) }, shared), ingest.Limits{MaxCellChars: 10}, "max_cell_chars", ""},
		{"shared strings", book(func(p *Page) { p.Row(1, Str("a"), Str("b"), Str("c")) }, shared), ingest.Limits{MaxSharedStrings: 2}, "max_shared_strings", ""},
		{"size of what is unpacked", book(func(p *Page) { p.Set("A1", Str(strings.Repeat("a", 300_000))) }), ingest.Limits{MaxCellChars: 1 << 20, MaxUncompressedBytes: 100_000}, "max_uncompressed_bytes", ""},
	}
	for _, c := range cases {
		_, err := Reader{}.ReadWorkbook(mustBytes(t, c.b), c.lim)
		if err == nil || kernel.CodeOf(err) != kernel.CodeInvalidValue || !strings.Contains(err.Error(), c.limit) || !strings.Contains(err.Error(), c.where) || !strings.Contains(err.Error(), "raise limits."+c.limit) {
			t.Errorf("%s: got %v", c.name, err)
		}
	}
	// the same files are fine under their default limits
	for _, c := range cases {
		if _, err := (Reader{}).ReadWorkbook(mustBytes(t, c.b), ingest.Limits{MaxCellChars: 1 << 20}); err != nil {
			t.Errorf("%s under default limits: %v", c.name, err)
		}
	}
}

// [REQ-285]
func TestAnArchiveThatLiesAboutItsSizeIsRefused(t *testing.T) {
	b := &Book{}
	b.Sheet("s").Set("A1", Str(strings.Repeat("a", 300_000)))
	data := mustBytes(t, b)
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	p := &pkg{files: map[string]*zip.File{}, lim: ingest.Limits{MaxCellChars: 1 << 20}.WithDefaults()}
	for _, f := range zr.File {
		p.files[f.Name] = f
		if f.Name == "xl/worksheets/sheet1.xml" {
			f.UncompressedSize64 = 10 // what the header says; the bytes are many more
		}
	}
	if _, err = p.workbook(); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("got %v", err)
	}
}

// [REQ-285]
func TestTheBudgetOfUnpackedBytesCountsWhatIsRead(t *testing.T) {
	p := &pkg{lim: ingest.Limits{MaxUncompressedBytes: 1000}.WithDefaults()}
	r := budgetReader{strings.NewReader(strings.Repeat("a", 5000)), p}
	buf := make([]byte, 400)
	var err error
	for i := 0; i < 5 && err == nil; i++ {
		_, err = r.Read(buf)
	}
	if err == nil || !strings.Contains(err.Error(), "max_uncompressed_bytes") {
		t.Fatalf("got %v", err)
	}
}

// [REQ-285]
func TestDefaultLimits(t *testing.T) {
	l := ingest.Limits{MaxRowsPerSheet: 7}.WithDefaults()
	if l.MaxRowsPerSheet != 7 || l.MaxUncompressedBytes != ingest.DefaultMaxUncompressedBytes || l.MaxColumnsPerSheet != ingest.DefaultMaxColumnsPerSheet ||
		l.MaxCellChars != ingest.DefaultMaxCellChars || l.MaxSharedStrings != ingest.DefaultMaxSharedStrings {
		t.Fatalf("%+v", l)
	}
}
