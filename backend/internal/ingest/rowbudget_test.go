package ingest

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// rbRead reads a file of JSON lines through StreamPages and gives the rows it made and the completeness of its last page.
func rbRead(t *testing.T, ds Dataset, body string) (int, Completeness) {
	t.Helper()
	rows := 0
	var end Completeness
	for p, err := range StreamPages(context.Background(), strings.NewReader(body), ds, PageOptions{}) {
		if err != nil {
			t.Fatal(err)
		}
		rows += len(p.Rows)
		if p.Last {
			end = p.End
		}
	}
	return rows, end
}

// rbDataset is a file of JSON lines whose records_path opens an array, with a column ^.blob that reads the outer record in every row.
func rbDataset(t *testing.T) Dataset {
	t.Helper()
	src, err := ParseAdapterSource(strings.NewReader(hxdFile(
		`{"id":"d","parser":"json_lines","records_path":"[*].ifs","natural_key":["host","id"],"columns":[{"name":"host","paths":["^.hostid"]},{"name":"id"},{"name":"blob","paths":["^.blob"]}]}`)))
	if err != nil {
		t.Fatal(err)
	}
	return src.Datasets[0]
}

// N1: a line of 200 elements that each repeat a 64 KiB value of the outer record makes about 13 MB of cells from a file of about
// 64 KiB, which is more than 4 times the bytes read plus DefaultPageBytes. The read stops there and is not complete.
func TestRowBudgetFileStopsRowsBeyondFourTimesTheBytesRead(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"hostid":"h1","blob":"` + strings.Repeat("x", 64<<10) + `","ifs":[`)
	for i := 0; i < 200; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":"%x"}`, i)
	}
	b.WriteString("]}\n")
	_, end := rbRead(t, rbDataset(t), b.String())
	if end.Complete {
		t.Fatal("the rows made beyond the bound were read as complete")
	}
	if !strings.Contains(end.Reason, "more than 4 times the bytes read") {
		t.Fatalf("the reason does not say why the read stopped: %q", end.Reason)
	}
}

// N1: a line whose outer value is small and whose elements are many is read whole: the rows are about the size of the bytes read.
func TestRowBudgetFileExpansionWithASmallOuterValueIsRead(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"hostid":"h1","blob":"b","ifs":[`)
	const n = 20000
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":"%x"}`, i)
	}
	b.WriteString("]}\n")
	rows, end := rbRead(t, rbDataset(t), b.String())
	if !end.Complete || rows != n {
		t.Fatalf("rows %d, complete %v; want %d rows and complete", rows, end.Complete, n)
	}
}

// N1: the bound is exactly RowBytesFactor times the base plus the fixed part: more than that is over, not the bound itself.
func TestRowBudgetBoundIsExactlyTheFactor(t *testing.T) {
	if RowsOver(4*100, 100, 0) || !RowsOver(4*100+1, 100, 0) {
		t.Fatal("RowsOver is not the factor times the base")
	}
	if RowsOver(4*100+50, 100, 50) || !RowsOver(4*100+51, 100, 50) {
		t.Fatal("RowsOver does not add the fixed part")
	}
}
