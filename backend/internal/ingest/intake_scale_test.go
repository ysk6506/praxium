package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// sxWatched is a read port of one dataset, ds_racks, that watches how it is used: how many pages it gave, how many of them the
// sink has had, and whether its sequence was closed.
type sxWatched struct {
	src      io.Reader
	given    atomic.Int64
	staged   atomic.Int64
	closed   atomic.Bool
	overlaps atomic.Int64
}

func (w *sxWatched) ReadPages(ctx context.Context, _ string, ds Dataset, opts PageOptions) iter.Seq2[Page, error] {
	return func(yield func(Page, error) bool) {
		defer w.closed.Store(true)
		if ds.ID != "ds_racks" {
			yield(Page{}, kernel.Errorf(kernel.CodeNotFound, "not given"))
			return
		}
		for p, err := range StreamPages(ctx, w.src, ds, opts) {
			if w.given.Load() != w.staged.Load() {
				w.overlaps.Add(1)
			}
			w.given.Add(1)
			if !yield(p, err) || err != nil {
				return
			}
		}
	}
}

func (w *sxWatched) Check(ctx context.Context, source string, ds Dataset) CheckResult {
	return CheckFirstPage(ctx, w, source, ds)
}

// sxWatchSink is the sink of a watched reader. It looks, while it has a page, at how far the source has been read.
type sxWatchSink struct {
	w      *sxWatched
	taken  func() int64
	rows   int
	pages  int
	ahead  int64
	cancel func()
	until  int
}

func (s *sxWatchSink) Stage(_ context.Context, p StagePage) error {
	s.w.staged.Add(1)
	if s.w.given.Load()-s.w.staged.Load() != 0 {
		s.w.overlaps.Add(1)
	}
	s.rows += len(p.Records)
	s.pages++
	if over := s.taken() - int64(s.rows)*20; over > s.ahead {
		s.ahead = over
	}
	if s.cancel != nil && s.pages == s.until {
		s.cancel()
	}
	return nil
}

func sxRackRow(i int) string { return fmt.Sprintf("RK%07d,rack,42\n", i) }

func TestIntakeHoldsOnePageAtATimeWhateverTheSizeOfTheSource(t *testing.T) {
	const rows = 300_000
	src, taken, stop := sxEndless("asset_id,rack_name,total_u\n", sxRackRow, rows)
	defer stop()
	w := &sxWatched{src: src}
	sink := &sxWatchSink{w: w, taken: taken}
	rep, err := Intake{Reader: w, Sink: sink, Plan: sxScalePlan(t, sourceJSON, mappingJSON), Options: PageOptions{MaxRows: 1000}}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sink.rows != rows || sink.pages != 300 || len(rep.Datasets) != 1 || rep.Datasets[0].Pages != 300 || rep.Datasets[0].Records != rows || !rep.Datasets[0].Complete {
		t.Fatalf("%d rows in %d pages: %+v", sink.rows, sink.pages, rep)
	}
	if w.overlaps.Load() != 0 {
		t.Fatalf("the reader gave a page while the sink still had the one before it, %d times", w.overlaps.Load())
	}
	if sink.ahead > 1000*20+256<<10 {
		t.Fatalf("the source was read %d bytes ahead of the rows stored: a page is not all that is held", sink.ahead)
	}
	if !w.closed.Load() {
		t.Fatal("the reader was not closed")
	}
}

func TestIntakeStopsAtTheNextPageWhenTheContextIsCanceledAndClosesTheReader(t *testing.T) {
	src, _, stop := sxEndless("asset_id,rack_name,total_u\n", sxRackRow, 100_000)
	defer stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &sxWatched{src: src}
	sink := &sxWatchSink{w: w, taken: func() int64 { return 0 }, cancel: cancel, until: 3}
	_, err := Intake{Reader: w, Sink: sink, Plan: sxScalePlan(t, sourceJSON, mappingJSON), Options: PageOptions{MaxRows: 100}}.Run(ctx)
	if !errors.Is(err, context.Canceled) || sink.pages != 3 || !w.closed.Load() {
		t.Fatalf("%v after %d pages, closed %v", err, sink.pages, w.closed.Load())
	}
}

func TestIntakeStopsWhenTheSinkFailsWithoutReadingOn(t *testing.T) {
	src, _, stop := sxEndless("asset_id,rack_name,total_u\n", sxRackRow, 100_000)
	defer stop()
	w := &sxWatched{src: src}
	mem := &sxMemory{failOn: 2}
	_, err := Intake{Reader: w, Sink: mem, Plan: sxScalePlan(t, sourceJSON, mappingJSON), Options: PageOptions{MaxRows: 100}}.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "store is full") || len(mem.pages) != 1 || w.given.Load() != 2 || !w.closed.Load() {
		t.Fatalf("%v: %d stored, %d given, closed %v", err, len(mem.pages), w.given.Load(), w.closed.Load())
	}
}

func TestIntakeReportsWhatEachDatasetGaveAndSkipsTheOnesTheReaderDoesNotGive(t *testing.T) {
	texts := sxTexts{
		"ds_racks":   "asset_id,rack_name,total_u\nRK1,a,1\nRK2,b,2\n\n,x,3\n",
		"ds_servers": "asset_id,serial_no\nSV1,sn1\n",
	}
	mem, rep := sxReceive(t, sxScalePlan(t, sourceJSON, mappingJSON), texts, PageOptions{MaxRows: 1})
	if len(rep.Datasets) != 2 {
		t.Fatalf("ds_vms is not given: %+v", rep.Datasets)
	}
	racks, servers := rep.Datasets[0], rep.Datasets[1]
	if racks.Dataset != "ds_racks" || racks.Pages != 3 || racks.Records != 2 || racks.Unreadable != 1 || !racks.Complete || racks.NotRead != "" || !racks.Collected {
		t.Fatalf("%+v", racks)
	}
	if servers.Dataset != "ds_servers" || servers.Records != 0 || !strings.Contains(servers.NotRead, "the file lacks the columns [manufacturer rack_asset_id power_state]") || !servers.Complete {
		t.Fatalf("a header that lacks columns gives no row and says so: %+v", servers)
	}
	last := mem.pages[2]
	if last.Page != 2 || !last.Last || !last.End.Complete || mem.pages[0].Last {
		t.Fatalf("the pages are numbered and only the last one says it is the last: %+v", mem.pages)
	}
}

func TestIntakeTellsADatasetThatWentOverTheLimitIsNotCompleteAndGivesTheProblem(t *testing.T) {
	mem, rep := sxReceive(t, sxScalePlan(t, sourceJSON, mappingJSON), sxTexts{"ds_racks": racksV1 + "RK3,rack-c,40\nRK4,rack-d,41\n"}, PageOptions{MaxDatasetRows: 3})
	if len(rep.Datasets) != 1 || rep.Datasets[0].Complete || !strings.Contains(rep.Datasets[0].Reason, "3 data rows") {
		t.Fatalf("%+v", rep)
	}
	probs := mem.sxProblemsOf("ds_racks")
	if len(probs) != 1 || probs[0].Kind != ProblemIncomplete || probs[0].No != -1 || !mem.pages[len(mem.pages)-1].Last {
		t.Fatalf("%+v", probs)
	}
}

// sxScript is a read port that gives the pages it was given, to try the receiving on a reader that does what a reader should not.
type sxScript struct {
	pages []Page
	err   error
}

func (s sxScript) ReadPages(_ context.Context, _ string, ds Dataset, _ PageOptions) iter.Seq2[Page, error] {
	return func(yield func(Page, error) bool) {
		if ds.ID != "ds_racks" {
			yield(Page{}, kernel.Errorf(kernel.CodeNotFound, "not given"))
			return
		}
		for _, p := range s.pages {
			if !yield(p, nil) {
				return
			}
		}
		if s.err != nil {
			yield(Page{}, s.err)
		}
	}
}

func (s sxScript) Check(ctx context.Context, source string, ds Dataset) CheckResult {
	return CheckFirstPage(ctx, s, source, ds)
}

func TestIntakeKeepsThePageNumberAndTheTokenOfEveryPage(t *testing.T) {
	row := Row{Key: "RK1", Line: 2, Fields: map[string]string{"asset_id": "RK1", "rack_name": "a"}}
	script := sxScript{pages: []Page{
		{Dataset: "ds_racks", No: 7, Rows: []Row{row}, Token: []byte("after-7")},
		{Dataset: "ds_racks", No: 8, Token: []byte("after-8"), Last: true, End: Completeness{Complete: true}},
	}}
	mem := &sxMemory{}
	if _, err := (Intake{Reader: script, Sink: mem, Plan: sxScalePlan(t, sourceJSON, mappingJSON)}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(mem.pages) != 2 || mem.pages[0].Page != 7 || string(mem.pages[0].Token) != "after-7" || mem.pages[1].Page != 8 || string(mem.pages[1].Token) != "after-8" || !mem.pages[1].Last {
		t.Fatalf("%+v", mem.pages)
	}
	if got := mem.pages[0].Records[0].Fields; got[0] != "RK1" || got[1] != "a" || got[2] != "" {
		t.Fatalf("a column the row lacks is an empty cell: %q", got)
	}
}

func TestIntakeRefusesAReaderThatEndsWithoutALastPageAndPassesOnItsErrors(t *testing.T) {
	plan := sxScalePlan(t, sourceJSON, mappingJSON)
	endless := sxScript{pages: []Page{{Dataset: "ds_racks"}}}
	if _, err := (Intake{Reader: endless, Sink: &sxMemory{}, Plan: plan}).Run(context.Background()); kernel.CodeOf(err) != kernel.CodeInternal {
		t.Fatalf("%v", err)
	}
	boom := errors.New("the source went away")
	if _, err := (Intake{Reader: sxScript{err: boom}, Sink: &sxMemory{}, Plan: plan}).Run(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Intake{Reader: sxScript{pages: []Page{{Dataset: "ds_racks", Last: true}}}, Sink: &sxMemory{}, Plan: plan}).Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled context stops the receiving: %v", err)
	}
	if _, err := (Intake{Reader: endless, Sink: &sxMemory{}, Plan: &Plan{Source: Source{Datasets: []Dataset{{ID: "ds_none"}}}}}).Run(context.Background()); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Logf("a plan whose dataset the preparer does not know: %v", err)
	}
}

func TestCheckFirstPageReadsThePageAndSaysHowItWentOut(t *testing.T) {
	ds := sxDataset(ParserCSV, "id", "name")
	check := func(r PageReader) CheckResult { return r.Check(context.Background(), "src", ds) }
	if got := check(sxTexts{"ds_sx": sxNumbered(30)}); got.Kind != CheckOK || got.Rows != 30 || !strings.Contains(got.Message, "30 rows") {
		t.Fatalf("%+v", got)
	}
	if got := check(sxTexts{"ds_sx": "id\n1\n"}); got.Kind != CheckConfig || !strings.Contains(got.Message, "the file lacks the columns [name]") {
		t.Fatalf("a header that lacks a column is the setting's: %+v", got)
	}
	if got := check(sxTexts{}); got.Kind != CheckConfig || !strings.Contains(got.Message, "no text") {
		t.Fatalf("a dataset that is not there is the setting's: %+v", got)
	}
	if got := check(sxTexts{"ds_sx": "id,name\n1,\xff\n"}); got.Kind != CheckConfig || !strings.Contains(got.Message, "not UTF-8 text") {
		t.Fatalf("a file that is not UTF-8 is the setting's: %+v", got)
	}
	ds.ID = "ds_racks"
	cut := sxScript{pages: []Page{{Dataset: "ds_racks", Last: true, Rows: []Row{{Key: "1"}}, End: Completeness{Reason: "the read stopped"}}}}
	if got := check(cut); got.Kind != CheckTransient || got.Message != "the read stopped" || got.Rows != 1 {
		t.Fatalf("a first page that is cut short is a passing trouble: %+v", got)
	}
	if got := check(sxScript{}); got.Kind != CheckProduct || !strings.Contains(got.Message, "no page") {
		t.Fatalf("a reader that gives no page is a fault of the product: %+v", got)
	}
}

// sxPassing is an error that says it is passing, to try how the check sorts the errors of a source.
type sxPassing struct{ error }

func (sxPassing) Temporary() bool { return true }

type sxSlow struct{ error }

func (sxSlow) Timeout() bool { return true }

func TestClassifyCheckErrorSortsTheTroubleIntoTheFourKinds(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		err  error
		want CheckKind
	}{
		{kernel.Errorf(kernel.CodeInvalidValue, "bad"), CheckConfig},
		{kernel.Errorf(kernel.CodeNotFound, "no"), CheckConfig},
		{kernel.Errorf(kernel.CodeUnsupported, "no"), CheckConfig},
		{kernel.Errorf(kernel.CodeReadOnlyViolation, "no"), CheckConfig},
		{kernel.Errorf(kernel.CodeConflict, "no"), CheckConfig},
		{kernel.Errorf(kernel.CodeInternal, "bug"), CheckProduct},
		{boom, CheckProduct},
		{context.DeadlineExceeded, CheckTransient},
		{fmt.Errorf("while reading: %w", context.DeadlineExceeded), CheckTransient},
		{sxPassing{boom}, CheckTransient},
		{sxSlow{boom}, CheckTransient},
		{kernel.Wrap(kernel.CodeInvalidValue, sxSlow{boom}, "cannot open"), CheckTransient},
	}
	for _, c := range cases {
		if got := ClassifyCheckError(c.err); got != c.want {
			t.Errorf("%v: %s, want %s", c.err, got, c.want)
		}
	}
}

func TestPageReceivedCountsTheRowsAndTheLinesThatCouldNotBeRead(t *testing.T) {
	p := Page{Rows: make([]Row, 3), Unreadable: make([]Unreadable, 2)}
	if p.Received() != 5 {
		t.Fatal(p.Received())
	}
}
