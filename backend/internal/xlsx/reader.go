package xlsx

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf16"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Reader reads the bytes of a workbook file. It is the ingest.WorkbookReader of the product.
type Reader struct{}

var _ ingest.WorkbookReader = Reader{}

var (
	zipSignature = []byte("PK\x03\x04")
	// The binary container of the old Excel files (.xls) and of the workbooks that have a password.
	oleSignature = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}
)

// ReadWorkbook reads every sheet of a workbook. The limits that are 0 are the defaults. It reads the bytes and nothing
// else: no external link is opened, and the macros of a macro-enabled file are not looked at.
func (Reader) ReadWorkbook(data []byte, limits ingest.Limits) (*ingest.Workbook, error) {
	if err := checkSignature(data); err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "the file is not a readable ZIP archive: it is damaged or cut short")
	}
	p := &pkg{files: map[string]*zip.File{}, lim: limits.WithDefaults()}
	for _, f := range zr.File {
		if f.Flags&0x1 != 0 {
			return nil, kernel.Errorf(kernel.CodeUnsupported, "the workbook is encrypted (the ZIP archive has a password), which this build cannot read: save a copy without a password")
		}
		p.files[strings.ReplaceAll(f.Name, "\\", "/")] = f
	}
	return p.workbook()
}

// checkSignature tells apart the files the reader cannot read before it tries: the old binary format, a workbook with
// a password, and a file that is no archive at all.
func checkSignature(data []byte) error {
	switch {
	case len(data) == 0:
		return kernel.Errorf(kernel.CodeInvalidValue, "the file is empty")
	case bytes.HasPrefix(data, oleSignature):
		if bytes.Contains(data, utf16Bytes("EncryptedPackage")) {
			return kernel.Errorf(kernel.CodeUnsupported, "the workbook has a password, which this build cannot read: save a copy without a password")
		}
		return kernel.Errorf(kernel.CodeUnsupported, "the file is in the old binary Excel format (.xls), which this build cannot read: save it again as .xlsx")
	case !bytes.HasPrefix(data, zipSignature) && !bytes.HasPrefix(data, []byte("PK\x05\x06")):
		return kernel.Errorf(kernel.CodeInvalidValue, "the file is not an .xlsx workbook: it does not start like a ZIP archive")
	}
	return nil
}

func utf16Bytes(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

// pkg is an opened archive and what is left of the budget of bytes to unpack.
type pkg struct {
	files map[string]*zip.File
	lim   ingest.Limits
	used  int64
}

// overLimit is the error of a file that passes a safety limit: it names the limit and says how to raise it.
func overLimit(name string, value any, where string) error {
	return kernel.Errorf(kernel.CodeInvalidValue, "the workbook is over the safety limit %s (%v) %s: if the file is meant to be this large, raise limits.%s in the source definition", name, value, where, name)
}

// budgetReader counts the bytes an archive part unpacks to and stops at the limit.
type budgetReader struct {
	r io.Reader
	p *pkg
}

func (b budgetReader) Read(buf []byte) (int, error) {
	n, err := b.r.Read(buf)
	b.p.used += int64(n)
	if b.p.used > b.p.lim.MaxUncompressedBytes {
		return n, overLimit("max_uncompressed_bytes", b.p.lim.MaxUncompressedBytes, "while unpacking the archive")
	}
	return n, err
}

// part finds a part of the archive by its name, which is matched without regard to case.
func (p *pkg) part(name string) *zip.File {
	name = strings.TrimPrefix(name, "/")
	if f, ok := p.files[name]; ok {
		return f
	}
	for n, f := range p.files {
		if strings.EqualFold(n, name) {
			return f
		}
	}
	return nil
}

// open gives a reader of the unpacked bytes of a part, counted against the budget. The size the archive declares is
// checked first, so an archive that is plainly too large is refused before anything is unpacked.
func (p *pkg) open(f *zip.File) (io.ReadCloser, error) {
	if int64(f.UncompressedSize64) > p.lim.MaxUncompressedBytes-p.used {
		return nil, overLimit("max_uncompressed_bytes", p.lim.MaxUncompressedBytes, "(the part "+f.Name+" alone declares "+fmt.Sprint(f.UncompressedSize64)+" bytes)")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, damaged(f.Name, err)
	}
	return struct {
		io.Reader
		io.Closer
	}{budgetReader{rc, p}, rc}, nil
}

// damaged is the error of a part that cannot be unpacked or parsed. A limit passes through unchanged.
func damaged(name string, err error) error {
	var ke *kernel.Error
	if errors.As(err, &ke) {
		return ke
	}
	return kernel.Wrap(kernel.CodeInvalidValue, err, "the workbook is damaged: the part "+name+" cannot be read")
}

// relationship is one entry of a .rels part.
type relationship struct {
	target   string
	typ      string
	external bool
}

// relsOf reads the relationships of a part, by id, and the first target of every type for the lookups by type.
func (p *pkg) relsOf(part string) (map[string]relationship, error) {
	dir, base := path.Split(part)
	f := p.part(dir + "_rels/" + base + ".rels")
	if f == nil {
		return map[string]relationship{}, nil
	}
	rc, err := p.open(f)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	rels, err := parseRels(rc)
	if err != nil {
		return nil, damaged(f.Name, err)
	}
	for id, r := range rels {
		if !r.external {
			r.target = resolve(dir, r.target)
			rels[id] = r
		}
	}
	return rels, nil
}

// resolve makes the target of a relationship a name in the archive: an absolute one starts at the root, a relative
// one at the folder of the part that has the relationship.
func resolve(dir, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(path.Clean(target), "/")
	}
	return path.Clean(path.Join(dir, target))
}

// byType finds the target of the first relationship of a type whose name ends as given.
func byType(rels map[string]relationship, suffix string) string {
	best := ""
	for _, r := range rels {
		if strings.HasSuffix(r.typ, suffix) && !r.external && (best == "" || r.target < best) {
			best = r.target
		}
	}
	return best
}
