package filesource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// psTrack is a file that tells when it was closed.
type psTrack struct {
	io.Reader
	closed *int
}

func (p psTrack) Close() error { *p.closed++; return nil }

func psDataset(parser string) ingest.Dataset {
	return ingest.Dataset{ID: "ds", Parser: parser, NaturalKey: []string{"id"}, Columns: []ingest.Column{{Name: "id", Paths: []string{"id"}}, {Name: "name", Paths: []string{"name"}}}}
}

func psSource(data string, closed *int) PageSource {
	return PageSource{
		Open: func(name string) (io.ReadCloser, error) {
			if name != "a" {
				return nil, errors.New("no such file")
			}
			return psTrack{strings.NewReader(data), closed}, nil
		},
		Files: map[string]string{"ds": "a"},
	}
}

func psRows(n int) string {
	var b strings.Builder
	b.WriteString("id,name\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%d,n%d\n", i, i)
	}
	return b.String()
}

func TestPageSourceGivesTheRowsOfTheOldReadOfTheSameFile(t *testing.T) {
	data := psRows(25) + "9\n"
	var closed int
	ds := psDataset(ingest.ParserCSV)
	var rows []ingest.Row
	var unreadable int
	var pages int
	for p, err := range psSource(data, &closed).ReadPages(context.Background(), "src", ds, ingest.PageOptions{MaxRows: 10}) {
		if err != nil {
			t.Fatal(err)
		}
		pages++
		rows = append(rows, p.Rows...)
		unreadable += len(p.Unreadable)
	}
	old := ingest.ParseLimited(ds, []byte(data), 0)
	if pages != 3 || unreadable != len(old.Unreadable) || !reflect.DeepEqual(rows, old.Rows) || closed != 1 {
		t.Fatalf("%d pages, %d unreadable, %d rows of %d, closed %d", pages, unreadable, len(rows), len(old.Rows), closed)
	}
}

func TestPageSourceClosesTheFileWhenThePagesAreNotWantedAnyMore(t *testing.T) {
	var closed int
	for range psSource(psRows(50), &closed).ReadPages(context.Background(), "src", psDataset(ingest.ParserCSV), ingest.PageOptions{MaxRows: 10}) {
		break
	}
	if closed != 1 {
		t.Fatalf("closed %d times", closed)
	}
}

func TestPageSourceRefusesWhatItCannotReadAndSaysSo(t *testing.T) {
	var closed int
	ctx := context.Background()
	first := func(s PageSource, ds ingest.Dataset) error {
		for _, err := range s.ReadPages(ctx, "src", ds, ingest.PageOptions{}) {
			return err
		}
		return nil
	}
	if err := first(PageSource{Files: map[string]string{}}, psDataset(ingest.ParserCSV)); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Fatalf("a dataset with no file: %v", err)
	}
	s := psSource("x", &closed)
	s.Files["ds"] = "missing"
	if err := first(s, psDataset(ingest.ParserCSV)); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("a file that cannot be opened: %v", err)
	}
	if err := first(psSource("PK", &closed), psDataset(ingest.ParserXLSX)); kernel.CodeOf(err) != kernel.CodeUnsupported || closed != 1 {
		t.Fatalf("a workbook is not streamed: %v, closed %d", err, closed)
	}
}

func TestPageSourceEndsADatasetOverTheRowLimitAsNotComplete(t *testing.T) {
	var closed int
	var last ingest.Page
	for p, err := range psSource(psRows(10), &closed).ReadPages(context.Background(), "src", psDataset(ingest.ParserCSV), ingest.PageOptions{MaxRows: 4, MaxDatasetRows: 6}) {
		if err != nil {
			t.Fatal(err)
		}
		last = p
	}
	if !last.Last || last.End.Complete || last.End.Reason == "" {
		t.Fatalf("%+v", last.End)
	}
}

func TestPageSourceChecksTheFirstPage(t *testing.T) {
	var closed int
	ds := psDataset(ingest.ParserCSV)
	if got := psSource(psRows(3), &closed).Check(context.Background(), "src", ds); got.Kind != ingest.CheckOK || got.Rows != 3 {
		t.Fatalf("%+v", got)
	}
	if got := psSource("id\n1\n", &closed).Check(context.Background(), "src", ds); got.Kind != ingest.CheckConfig {
		t.Fatalf("%+v", got)
	}
}
