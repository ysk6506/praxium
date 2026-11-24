package xlsx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// The files that a few kilobytes can make a reader hold a great deal of, read by the reader: each must be refused with the right
// error, and the heap must stay under the limit of a read of a workbook (ADR-0103: 160 MiB at the setting of the measure, which is 256 MiB at
// the default one). The measure is the one of the reads of a source elsewhere (hsfPeakHeap).

const xsHeapLimit = 160 << 20

// xsPeakHeap runs f and gives the most the heap held above what it held before, sampled while f runs, with the collector made to
// run when the heap is a quarter bigger than what is alive.
func xsPeakHeap(f func()) uint64 {
	old := debug.SetGCPercent(25)
	defer debug.SetGCPercent(old)
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	base := m.HeapAlloc
	var mu sync.Mutex
	peak := base
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Millisecond):
				var s runtime.MemStats
				runtime.ReadMemStats(&s)
				mu.Lock()
				peak = max(peak, s.HeapAlloc)
				mu.Unlock()
			}
		}
	}()
	f()
	close(stop)
	<-done
	mu.Lock()
	defer mu.Unlock()
	return max(peak, base) - base
}

// xsBig is a part that is written as it is made: a function that writes its bytes, so that a part of many megabytes is never held whole.
type xsBig struct {
	name  string
	write func(w io.Writer)
}

// xsBigArchive writes the parts to an archive, compressed, so that the file is small.
func xsBigArchive(parts ...xsBig) []byte {
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, p := range parts {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Deflate})
		if err != nil {
			panic(err)
		}
		p.write(w)
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return out.Bytes()
}

func xsText(s string) func(io.Writer) { return func(w io.Writer) { _, _ = io.WriteString(w, s) } }

// xsRepeat writes the head, the unit n times, and the tail.
func xsRepeat(head, unit string, n int, tail string) func(io.Writer) {
	return func(w io.Writer) {
		_, _ = io.WriteString(w, head)
		chunk := strings.Repeat(unit, max(1, (1<<16)/max(1, len(unit))))
		per := max(1, (1<<16)/max(1, len(unit)))
		for ; n >= per; n -= per {
			_, _ = io.WriteString(w, chunk)
		}
		_, _ = io.WriteString(w, strings.Repeat(unit, n))
		_, _ = io.WriteString(w, tail)
	}
}

const xsRelsXML = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`

// xsAttackBook is a workbook whose sheet part, workbook part or rels part is the one that is given; the others are fine.
func xsAttackBook(workbook, rels, sheet func(io.Writer)) []byte {
	if workbook == nil {
		workbook = xsText(`<workbook ` + xsMain + `><sheets><sheet name="s" r:id="rId1"/></sheets></workbook>`)
	}
	if rels == nil {
		rels = xsText(xsRelsXML)
	}
	if sheet == nil {
		sheet = xsText(`<worksheet ` + xsMain + `><sheetData/></worksheet>`)
	}
	return xsBigArchive(xsBig{"xl/workbook.xml", workbook}, xsBig{"xl/_rels/workbook.xml.rels", rels}, xsBig{"xl/worksheets/sheet1.xml", sheet})
}

func xsWorksheet(body func(io.Writer)) func(io.Writer) {
	return func(w io.Writer) {
		_, _ = io.WriteString(w, `<worksheet `+xsMain+`>`)
		body(w)
		_, _ = io.WriteString(w, `</worksheet>`)
	}
}

// xsWorstPeak reads the file three times and gives the largest of the peaks and the last error.
func xsWorstPeak(data []byte) (uint64, error) {
	var worst uint64
	var err error
	for range 3 {
		worst = max(worst, xsPeakHeap(func() { _, err = Reader{}.ReadWorkbook(data, ingest.Limits{}) }))
	}
	return worst, err
}

// [REQ-807] [REQ-808]
func TestXsFilesThatMakeTheDecoderGatherWithoutABoundAreRefusedInSmallMemory(t *testing.T) {
	const mib = 1 << 20
	cdataPiece := strings.Repeat("a", 512<<10) + "<"
	for _, c := range []struct {
		name, want string
		data       []byte
	}{
		{"a tag of 60 MiB", "a tag is larger", xsAttackBook(nil, nil, xsWorksheet(xsRepeat(`<sheetData x="`, "a", 60*mib, `"/>`)))},
		{"a comment of 60 MiB between rows", "a comment is larger", xsAttackBook(nil, nil, xsWorksheet(xsRepeat(`<sheetData><!--`, "<row>", 12*mib, `--></sheetData>`)))},
		{"a CDATA section of 60 MiB with a < in it every 512 KiB", "a CDATA section is larger", xsAttackBook(nil, nil, xsWorksheet(xsRepeat(`<sheetData><row r="1"><c r="A1"><v><![CDATA[`, cdataPiece, 120, `]]></v></c></row></sheetData>`)))},
		{"a text of 60 MiB in a cell", "max_cell_chars", xsAttackBook(nil, nil, xsWorksheet(xsRepeat(`<sheetData><row r="1"><c r="A1"><v>`, "a", 60*mib, `</v></c></row></sheetData>`)))},
		{"nesting three million deep", "nested deeper than 64 levels", xsAttackBook(nil, nil, xsWorksheet(xsRepeat(`<sheetData>`, "<a>", 3_000_000, `</sheetData>`)))},
		{"nesting three million deep in the workbook part", "nested deeper than 64 levels", xsAttackBook(xsRepeat(`<workbook>`, "<a>", 3_000_000, ``), nil, nil)},
		{"a document type of 60 MiB", "a document type declaration", xsAttackBook(nil, xsRepeat(`<!DOCTYPE a [`, "<!ENTITY x 'y'>", 4_000_000, `]><Relationships/>`), nil)},
		{"a text of many CDATA pieces in one cell", "max_cell_chars", xsAttackBook(nil, nil, xsWorksheet(xsRepeat(`<sheetData><row r="1"><c r="A1" t="str"><v>`, `<![CDATA[`+strings.Repeat("a", 256<<10)+`]]>`, 200, `</v></c></row></sheetData>`)))},
	} {
		peak, err := xsWorstPeak(c.data)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want one that says %q", c.name, err, c.want)
		}
		if peak > xsHeapLimit {
			t.Errorf("%s: the peak of the heap is %d MiB, more than %d MiB", c.name, peak>>20, xsHeapLimit>>20)
		}
		t.Logf("%s: the peak of the heap above its start is %d MiB", c.name, peak>>20)
	}
}

// [REQ-808]
func TestXsFilesThatMakeALongListAreRefusedInSmallMemory(t *testing.T) {
	long := strings.Repeat("a", 900<<10)
	for _, c := range []struct {
		name, want string
		data       []byte
	}{
		{"400,000 sheets", "max_sheets", xsAttackBook(xsRepeat(`<workbook `+xsMain+`><sheets>`, `<sheet name="s" r:id="rId1"/>`, 400_000, `</sheets></workbook>`), nil, nil)},
		{"1,500,000 relationships", "max_relationships", xsAttackBook(nil, xsRepeat(`<Relationships>`, `<Relationship Id="rId1" Type="t" Target="x"/>`, 1_500_000, `</Relationships>`), nil)},
		{"2,000,000 number formats", "max_number_formats", xsStylesBook(xsRepeat(`<styleSheet><numFmts>`, `<numFmt numFmtId="200" formatCode="0"/>`, 2_000_000, `</numFmts></styleSheet>`))},
		{"6,000,000 column entries", "max_col_elements", xsAttackBook(nil, nil, xsWorksheet(xsRepeat(`<cols>`, `<col min="1" max="1" hidden="1"/>`, 6_000_000, `</cols><sheetData/>`)))},
		{"3,000,000 merged ranges", "max_merge_ranges", xsAttackBook(nil, nil, xsWorksheet(xsRepeat(`<sheetData/><mergeCells>`, `<mergeCell ref="A1:B2"/>`, 3_000_000, `</mergeCells>`)))},
		{"a sheet name of 900 KiB", "max_sheet_name_bytes", xsAttackBook(xsText(`<workbook `+xsMain+`><sheets><sheet name="`+long+`" r:id="rId1"/></sheets></workbook>`), nil, nil)},
		{"a relationship type of 900 KiB", "max_relationship_type_bytes", xsAttackBook(nil, xsText(`<Relationships><Relationship Id="rId1" Type="`+long+`" Target="x"/></Relationships>`), nil)},
		{"a number format of 900 KiB", "max_number_format_bytes", xsStylesBook(xsText(`<styleSheet><numFmts><numFmt numFmtId="200" formatCode="` + long + `"/></numFmts></styleSheet>`))},
	} {
		peak, err := xsWorstPeak(c.data)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want one that says %q", c.name, err, c.want)
		}
		if peak > xsHeapLimit {
			t.Errorf("%s: the peak of the heap is %d MiB, more than %d MiB", c.name, peak>>20, xsHeapLimit>>20)
		}
		t.Logf("%s: the peak of the heap above its start is %d MiB", c.name, peak>>20)
	}
}

// xsStylesBook is a workbook that has the styles part that is given.
func xsStylesBook(styles func(io.Writer)) []byte {
	rels := `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="` + xsRelTy + `worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="` + xsRelTy + `styles" Target="styles.xml"/></Relationships>`
	return xsBigArchive(
		xsBig{"xl/workbook.xml", xsText(fmt.Sprintf(`<workbook %s><sheets><sheet name="s" r:id="rId1"/></sheets></workbook>`, xsMain))},
		xsBig{"xl/_rels/workbook.xml.rels", xsText(rels)}, xsBig{"xl/styles.xml", styles},
		xsBig{"xl/worksheets/sheet1.xml", xsText(`<worksheet ` + xsMain + `><sheetData/></worksheet>`)})
}
