package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// sxDataset is a dataset of the given parser whose columns are read by their own names, the first being the natural key.
func sxDataset(parser string, cols ...string) Dataset {
	d := Dataset{ID: "ds_sx", Parser: parser, NaturalKey: cols[:1]}
	for _, c := range cols {
		d.Columns = append(d.Columns, Column{Name: c, Paths: []string{c}})
	}
	return d
}

// sxCollect reads every page of a stream; it fails the test on an error.
func sxCollect(t *testing.T, src io.Reader, ds Dataset, opts PageOptions) []Page {
	t.Helper()
	pages, err := sxPages(src, ds, opts)
	if err != nil {
		t.Fatalf("the stream failed: %v", err)
	}
	return pages
}

func sxPages(src io.Reader, ds Dataset, opts PageOptions) ([]Page, error) {
	var pages []Page
	for p, err := range StreamPages(context.Background(), src, ds, opts) {
		if err != nil {
			return pages, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

// sxJoin puts the pages together the way a table has them.
func sxJoin(pages []Page) (t Table, shape *Shape) {
	for _, p := range pages {
		t.Rows = append(t.Rows, p.Rows...)
		t.Unreadable = append(t.Unreadable, p.Unreadable...)
		for k, n := range p.Skipped {
			if t.Skipped == nil {
				t.Skipped = map[string]int{}
			}
			t.Skipped[k] += n
		}
		if p.Shape != nil {
			shape = p.Shape
		}
	}
	return t, shape
}

func sxNumbered(n int) string {
	var b strings.Builder
	b.WriteString("id,name\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "k%03d,name-%03d\n", i, i)
	}
	return b.String()
}

func TestStreamPagesCutsPagesByRowsAndHasTheLastOneSayItIsComplete(t *testing.T) {
	pages := sxCollect(t, strings.NewReader(sxNumbered(25)), sxDataset(ParserCSV, "id", "name"), PageOptions{MaxRows: 10})
	var sizes []int
	for i, p := range pages {
		sizes = append(sizes, len(p.Rows))
		if p.No != i || p.Last != (i == len(pages)-1) || p.Dataset != "ds_sx" {
			t.Errorf("page %d: no %d, last %v, dataset %q", i, p.No, p.Last, p.Dataset)
		}
		if (p.Shape != nil) != (i == 0) {
			t.Errorf("page %d: the shape of the header is on the first page only: %+v", i, p.Shape)
		}
	}
	if !reflect.DeepEqual(sizes, []int{10, 10, 5}) {
		t.Fatalf("the pages have %v rows", sizes)
	}
	if last := pages[2]; !last.End.Complete || last.End.Reason != "" || pages[0].End.Complete {
		t.Fatalf("only the last page tells it is complete: %+v", pages)
	}
	if pages[0].Rows[0].Key != "k000" || pages[2].Rows[4].Key != "k024" || pages[0].Rows[0].Line != 2 {
		t.Fatalf("the rows are in the order of the file with their lines: %+v", pages[0].Rows[0])
	}
}

func TestStreamPagesCutsPagesByBytesAndKeepsAHugeRowOnAPageOfItsOwn(t *testing.T) {
	data := "id,name\na,12345\nb,12345\nc,12345\nd," + strings.Repeat("x", 40) + "\ne,12345\n"
	pages := sxCollect(t, strings.NewReader(data), sxDataset(ParserCSV, "id", "name"), PageOptions{MaxBytes: 14})
	var keys []string
	for _, p := range pages {
		var ks []string
		for _, r := range p.Rows {
			ks = append(ks, r.Key)
		}
		keys = append(keys, strings.Join(ks, ""))
		if p.Bytes > 14 && len(p.Rows) != 1 {
			t.Errorf("a page over the size limit holds one row only: %+v", p)
		}
	}
	if !reflect.DeepEqual(keys, []string{"ab", "c", "d", "e"}) {
		t.Fatalf("the pages hold %q", keys)
	}
	if pages[2].Bytes != 41 {
		t.Fatalf("the bytes of a page are the size of its cells: %d", pages[2].Bytes)
	}
}

func TestStreamPagesOfAnEmptyDatasetIsOneLastPage(t *testing.T) {
	pages := sxCollect(t, strings.NewReader("id,name\n"), sxDataset(ParserCSV, "id", "name"), PageOptions{})
	if len(pages) != 1 || !pages[0].Last || !pages[0].End.Complete || len(pages[0].Rows) != 0 || pages[0].Shape == nil {
		t.Fatalf("%+v", pages)
	}
	pages = sxCollect(t, strings.NewReader(""), sxDataset(ParserJSONLines, "id"), PageOptions{})
	if len(pages) != 1 || !pages[0].Last || !pages[0].End.Complete {
		t.Fatalf("%+v", pages)
	}
}

func TestStreamPagesGoingOverTheRowLimitEndsIncompleteAndStopsReading(t *testing.T) {
	opts := PageOptions{MaxRows: 4, MaxDatasetRows: 10}
	pages := sxCollect(t, strings.NewReader(sxNumbered(50)), sxDataset(ParserCSV, "id", "name"), opts)
	last := pages[len(pages)-1]
	if !last.Last || last.End.Complete || !strings.Contains(last.End.Reason, "10 data rows") {
		t.Fatalf("the dataset is cut off and says why: %+v", last.End)
	}
	rows := 0
	for _, p := range pages {
		rows += len(p.Rows)
	}
	if rows != 10 {
		t.Fatalf("the rows given are the ones within the limit: %d", rows)
	}
	exact := sxCollect(t, strings.NewReader(sxNumbered(10)), sxDataset(ParserCSV, "id", "name"), opts)
	if end := exact[len(exact)-1].End; !end.Complete {
		t.Fatalf("a dataset with as many rows as the limit is complete: %+v", end)
	}
	lines := strings.Repeat(`{"id":"x"}`+"\n", 6)
	pages = sxCollect(t, strings.NewReader(lines), sxDataset(ParserJSONLines, "id"), PageOptions{MaxDatasetRows: 5})
	if end := pages[len(pages)-1].End; end.Complete {
		t.Fatalf("the limit of the dataset counts for JSON lines too: %+v", end)
	}
}

// pagesCase is an input both the current parser and the stream read.
type sxEqualCase struct {
	name string
	ds   Dataset
	text string
}

func sxEqualCases() []sxEqualCase {
	csv3 := sxDataset(ParserCSV, "id", "name", "note")
	opt := sxDataset(ParserCSV, "id", "name")
	opt.Columns = append(opt.Columns, Column{Name: "extra", Paths: []string{"extra"}, Optional: true})
	skipTotals := sxDataset(ParserCSV, "id", "name")
	skipTotals.IgnoreRowsWithoutKey = true
	ignoreExtra := sxDataset(ParserCSV, "id")
	semi := sxDataset(ParserCSV, "id", "name")
	semi.Delimiter = ";"
	jl := sxDataset(ParserJSONLines, "id", "name")
	jl.Columns[1].Paths = []string{"info.name", "name"}
	return []sxEqualCase{
		{"clean", csv3, "id,name,note\n1,a,x\n2,b,y\n"},
		{"byte order mark and quotes", csv3, "\xef\xbb\xbfid,name,note\n1,\"a, b\",\"line\nbreak\"\n2,\"say \"\"hi\"\"\",\n"},
		{"blank lines and a line without end", csv3, "id,name,note\n\n1,a,x\n   \n2,b,y"},
		{"wrong field count", csv3, "id,name,note\n1,a\n2,b,y,z\n3,c,z\n"},
		{"empty natural key", csv3, "id,name,note\n,a,x\n 3 ,b,y\n"},
		{"rows without a key are skipped", skipTotals, "id,name\n1,a\n,\n,\n2,b\n,x\n"},
		{"a column the file lacks", csv3, "id,name\n1,a\n"},
		{"a header of one column that holds semicolons", csv3, "id;name;note\n1;a;x\n"},
		{"an optional column", opt, "id,name\n1,a\n"},
		{"undeclared columns", ignoreExtra, "id,other,more\n1,a,b\n"},
		{"another delimiter", semi, "id;name\n1;a\n"},
		{"an empty file", csv3, ""},
		{"a header only", csv3, "id,name,note\n"},
		{"json lines", jl, `{"id":"1","info":{"name":"a"}}` + "\n" + `{"id":2,"name":"b"}` + "\n\n" + `{"id":"3"}`},
		{"json lines that are broken or not scalar", jl, "{bad\n" + `{"id":"4","name":["x"]}` + "\n" + `{"id":"5","name":null}` + "\n" + `{"id":"6"} {"id":"7"}` + "\n"},
		{"json lines with no key", jl, `{"name":"a"}` + "\n" + `{"id":""}` + "\n"},
	}
}

func TestStreamPagesGivesTheRowsTheCurrentParserGives(t *testing.T) {
	for _, c := range sxEqualCases() {
		want := Parse(c.ds, []byte(c.text))
		for _, size := range []int{1, 2, 3, DefaultPageRows} {
			got, shape := sxJoin(sxCollect(t, strings.NewReader(c.text), c.ds, PageOptions{MaxRows: size}))
			if !reflect.DeepEqual(got.Rows, want.Rows) || !reflect.DeepEqual(got.Unreadable, want.Unreadable) || !reflect.DeepEqual(got.Skipped, want.Skipped) {
				t.Errorf("%s (pages of %d): rows %+v unreadable %+v skipped %v, want rows %+v unreadable %+v skipped %v", c.name, size, got.Rows, got.Unreadable, got.Skipped, want.Rows, want.Unreadable, want.Skipped)
			}
			if c.ds.Parser == ParserCSV {
				if shape == nil || !reflect.DeepEqual(shape.Missing, want.Missing) || !reflect.DeepEqual(shape.Extra, want.Extra) || !reflect.DeepEqual(shape.Header, want.Header) || shape.Hint != want.Hint {
					t.Errorf("%s: shape %+v, want missing %v extra %v header %v hint %q", c.name, shape, want.Missing, want.Extra, want.Header, want.Hint)
				}
			}
		}
	}
}

func TestStreamPagesStopsAfterAHeaderThatLacksAColumn(t *testing.T) {
	pages := sxCollect(t, strings.NewReader("id\n1\n2\n"), sxDataset(ParserCSV, "id", "name"), PageOptions{})
	if len(pages) != 1 || len(pages[0].Rows) != 0 || !pages[0].Last || !pages[0].End.Complete || !reflect.DeepEqual(pages[0].Shape.Missing, []string{"name"}) {
		t.Fatalf("a header that lacks a column gives one page with no row: %+v", pages)
	}
}

// sxBreaking is a reader that gives its text and then fails with err.
func sxBreaking(text string, err error) io.Reader {
	return io.MultiReader(strings.NewReader(text), iotest.ErrReader(err))
}

func TestStreamPagesWhoseSourceBreaksEndsIncompleteAfterTheRowsRead(t *testing.T) {
	boom := errors.New("connection lost")
	for name, text := range map[string]string{
		"in the header":                          "id,na",
		"after the start":                        sxNumbered(5),
		"after a sample":                         sxNumbered(400),
		"in the header of a file with no sample": "",
	} {
		pages := sxCollect(t, sxBreaking(text, boom), sxDataset(ParserCSV, "id", "name"), PageOptions{})
		last := pages[len(pages)-1]
		if !last.Last || last.End.Complete || last.End.Reason == "" {
			t.Errorf("%s: %+v", name, last)
		}
	}
	pages := sxCollect(t, sxBreaking(`{"id":"1"}`+"\n"+`{"id":"2"}`, boom), sxDataset(ParserJSONLines, "id"), PageOptions{})
	rows, last := 0, pages[len(pages)-1]
	for _, p := range pages {
		rows += len(p.Rows)
	}
	if rows != 2 || last.End.Complete {
		t.Fatalf("JSON lines: %d rows, %+v", rows, last.End)
	}
	pages = sxCollect(t, iotest.ErrReader(boom), sxDataset(ParserJSONLines, "id"), PageOptions{})
	if end := pages[len(pages)-1].End; end.Complete {
		t.Fatalf("%+v", end)
	}
}

func TestStreamPagesSeesCancelBetweenPages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var got int
	var last error
	for p, err := range StreamPages(ctx, strings.NewReader(sxNumbered(30)), sxDataset(ParserCSV, "id", "name"), PageOptions{MaxRows: 5}) {
		if err != nil {
			last = err
			break
		}
		got++
		cancel()
		if p.Last {
			t.Fatal("the stream did not stop")
		}
	}
	if got != 1 || !errors.Is(last, context.Canceled) {
		t.Fatalf("a canceled context stops the stream at the next page: %d pages, %v", got, last)
	}
	for _, err := range StreamPages(ctx, strings.NewReader("id\n"), sxDataset(ParserCSV, "id"), PageOptions{}) {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("a stream that starts with a canceled context fails: %v", err)
		}
	}
}

// sxEndless is a CSV text of many rows that is made as it is read, through a pipe, and the number of bytes the reader has
// taken: a write to a pipe returns only when the reader took its bytes, so what the writer counted is what was read.
func sxEndless(header string, row func(i int) string, rows int) (src io.Reader, taken func() int64, stop func()) {
	pr, pw := io.Pipe()
	var n atomic.Int64
	put := func(text string) error {
		k, err := io.WriteString(pw, text)
		n.Add(int64(k))
		return err
	}
	go func() {
		err := put(header)
		for i := 1; err == nil && i <= rows; i++ {
			err = put(row(i))
		}
		pw.CloseWithError(err)
	}()
	return pr, n.Load, func() { pr.Close() }
}

func sxPairRow(i int) string { return fmt.Sprintf("k%07d,name\n", i) }

func TestStreamPagesReadsOnlyAsFarAsThePageInHand(t *testing.T) {
	src, taken, stop := sxEndless("id,name\n", sxPairRow, 1_000_000)
	defer stop()
	for p, err := range StreamPages(context.Background(), src, sxDataset(ParserCSV, "id", "name"), PageOptions{MaxRows: 100}) {
		if err != nil || len(p.Rows) != 100 {
			t.Fatalf("%v %d", err, len(p.Rows))
		}
		break
	}
	if n := taken(); n > 100*19+64<<10 {
		t.Fatalf("the stream read %d bytes for a page of 100 rows of 19 bytes: it reads ahead of the page", n)
	}
}

func TestStreamPagesRefusesWhatIsNotUTF8TheWayTheCheckOfAFileDoes(t *testing.T) {
	ds := sxDataset(ParserCSV, "id", "name")
	long := sxNumbered(500)
	for name, text := range map[string]string{
		"a UTF-16 byte order mark":           "\xff\xfei\x00d\x00\n\x00",
		"UTF-16 without a mark":              "i\x00d\x00,\x00n\x00a\x00m\x00e\x00\n\x001\x00,\x00a\x00\n\x00",
		"a bad byte near the start":          "id,name\n1,\xe9t\xe9\n",
		"a bad byte after the sample":        long + "9,caf\xe9\n",
		"a character cut at the end of file": long + "9,caf\xc3",
		"a bad byte in a short file":         "id,name\n1,\xff\n",
	} {
		want := CheckEncoding([]byte(text), ParserCSV)
		if want == nil {
			t.Fatalf("%s: the file is fine", name)
		}
		for _, src := range []io.Reader{strings.NewReader(text), iotest.OneByteReader(strings.NewReader(text)), iotest.DataErrReader(strings.NewReader(text))} {
			_, err := sxPages(src, ds, PageOptions{})
			if err == nil || kernel.CodeOf(err) != kernel.CodeInvalidValue || !strings.HasSuffix(err.Error(), plainMessage(want)) || !strings.Contains(err.Error(), "dataset ds_sx") {
				t.Errorf("%s: got %v, want %v", name, err, want)
			}
		}
	}
	jl := sxDataset(ParserJSONLines, "id")
	if _, err := sxPages(strings.NewReader("{\"id\":\"\xff\"}\n"), jl, PageOptions{}); err == nil || !strings.Contains(err.Error(), "Save the file again as UTF-8") {
		t.Fatalf("%v", err)
	}
}

func TestStreamPagesReadsCharactersThatAreCutBetweenReads(t *testing.T) {
	text := "id,name\n" + strings.Repeat("가나다,라마바\n", 600)
	for _, src := range []io.Reader{strings.NewReader(text), iotest.OneByteReader(strings.NewReader(text)), iotest.HalfReader(strings.NewReader(text))} {
		pages := sxCollect(t, src, sxDataset(ParserCSV, "id", "name"), PageOptions{})
		rows := 0
		for _, p := range pages {
			rows += len(p.Rows)
		}
		if rows != 600 || pages[len(pages)-1].End.Reason != "" || pages[0].Rows[0].Fields["name"] != "라마바" {
			t.Fatalf("%d rows, %+v", rows, pages[len(pages)-1].End)
		}
	}
}

func TestStreamPagesDoesNotReadAWorkbookOrAnUnknownParser(t *testing.T) {
	for _, parser := range []string{ParserXLSX, "xml"} {
		_, err := sxPages(strings.NewReader("x"), Dataset{ID: "ds", Parser: parser}, PageOptions{})
		if kernel.CodeOf(err) != kernel.CodeUnsupported {
			t.Errorf("%s: %v", parser, err)
		}
	}
}

func TestPageOptionsDefaultsAreTheStartingValues(t *testing.T) {
	o := PageOptions{}.WithDefaults()
	if o.MaxRows != 10_000 || o.MaxBytes != 8<<20 || o.MaxDatasetRows != 50_000_000 {
		t.Fatalf("%+v", o)
	}
	o = PageOptions{MaxRows: 3, MaxBytes: 4, MaxDatasetRows: 5}.WithDefaults()
	if o.MaxRows != 3 || o.MaxBytes != 4 || o.MaxDatasetRows != 5 {
		t.Fatalf("a value that is set is kept: %+v", o)
	}
}
