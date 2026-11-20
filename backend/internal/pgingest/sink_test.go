package pgingest

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// pgiHash is a raw hash of 64 hex characters.
func pgiHash(seed string) string { return strings.Repeat(seed, 64/len(seed)) }

// pgiPage is a page of one dataset of the fixture with the rows given, for the tests that put a page together by hand.
func pgiPage(dataset string, no int, keys ...string) ingest.StagePage {
	p := ingest.StagePage{Dataset: dataset, Page: no, Token: []byte("tok")}
	for i, k := range keys {
		p.Records = append(p.Records, ingest.StageRecord{No: i, Key: k, RawHash: pgiHash("ab"), Fields: []string{k}})
	}
	return p
}

func pgiPlaceOf(t *testing.T, e *pgiEnv, run int64) string {
	t.Helper()
	return e.text(t, `SELECT coalesce(recv_dataset_no::text, '-') || ':' || recv_page || ':' || coalesce(recv_token, '-') FROM sync_run WHERE run_no = $1`, run)
}

// [REQ-701]
func TestPgiAPageThatFailsHalfWayLeavesNothingNotEvenItsPlaceAndNoRowIsHalfWritten(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	ref := e.attach(t, kg, plan)
	lease := e.open(t, kg, plan.Source.ID, "server-a")
	sink, err := e.st.newSink(e.ctx, lease, plan)
	if err != nil {
		t.Fatal(err)
	}
	good := pgiPage("ds_places", 0, "a", "b")
	good.Keys = []ingest.StageKey{{No: 0, TypeNo: 5, SigHash: ingest.KeyHash{1}}}
	if err := sink.Stage(e.ctx, good); err != nil {
		t.Fatal(err)
	}
	place := pgiPlaceOf(t, e, lease.No)
	// the records go in first and the key is what the server refuses: a type number that does not fit a smallint.
	bad := pgiPage("ds_places", 1, "c", "d")
	bad.Keys = []ingest.StageKey{{No: 1, TypeNo: 70000, SigHash: ingest.KeyHash{2}}}
	if err := sink.Stage(e.ctx, bad); err == nil {
		t.Fatal("a key the table cannot hold was accepted")
	}
	if got := pgiPlaceOf(t, e, lease.No); got != place {
		t.Errorf("the place moved to %s although the page failed (it was %s)", got, place)
	}
	rec := pgiTable(ref, pgschema.TableStageRecord)
	if n := e.count(t, `SELECT count(*) FROM `+rec+` WHERE page = 1`); n != 0 {
		t.Errorf("%d rows of the page that failed stayed", n)
	}
	if n := e.count(t, `SELECT count(*) FROM `+rec+` WHERE page = 0`); n != 2 {
		t.Errorf("the page before is %d rows", n)
	}
	// a claim of a scheme the model does not have, and a problem of a kind that is not in the list, are refused the same way.
	odd := pgiPage("ds_vms", 0, "x")
	odd.Claims = []ingest.StageClaim{{No: 0, Scheme: "not_a_scheme", Value: "v"}}
	if err := sink.Stage(e.ctx, odd); !pgiCodeIs(err, kernel.CodeInvalidValue) {
		t.Errorf("a claim of a scheme the model does not have: %v", err)
	}
	odd = pgiPage("ds_vms", 0, "x")
	odd.Problems = []ingest.StageProblem{{No: 0, Kind: "made_up"}}
	if err := sink.Stage(e.ctx, odd); !pgiCodeIs(err, kernel.CodeInternal) {
		t.Errorf("a problem of a kind that is not in the list: %v", err)
	}
	odd = pgiPage("ds_vms", 0, "x")
	odd.Records[0].No = 5
	if err := sink.Stage(e.ctx, odd); !pgiCodeIs(err, kernel.CodeInternal) {
		t.Errorf("a record that is not numbered by its place in the page: %v", err)
	}
	odd = pgiPage("ds_vms", 0, "x")
	odd.Records[0].RawHash = "xyz"
	if err := sink.Stage(e.ctx, odd); err == nil {
		t.Error("a hash that is not a hash")
	}
	if err := sink.Stage(e.ctx, pgiPage("ds_unknown", 0, "x")); !pgiCodeIs(err, kernel.CodeInvalidValue) {
		t.Errorf("a dataset that was not put to the source: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM `+rec); n != 2 {
		t.Errorf("%d rows are staged, want the 2 of the page that went in", n)
	}
}

// [REQ-701]
func TestPgiAWriteOfAnOldAttemptAfterAnotherServerTookTheRunOverWritesNothing(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	ref := e.attach(t, kg, plan)
	lease := e.open(t, kg, plan.Source.ID, "server-a")
	sink, err := e.st.newSink(e.ctx, lease, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Stage(e.ctx, pgiPage("ds_places", 0, "a")); err != nil {
		t.Fatal(err)
	}
	e.expire(t, lease.No)
	if _, err := e.st.AcquireRun(e.ctx, lease.No, "server-b", lease.Term); err != nil {
		t.Fatal(err)
	}
	place := pgiPlaceOf(t, e, lease.No)
	if err := sink.Stage(e.ctx, pgiPage("ds_places", 1, "b")); !pgiCodeIs(err, ingest.CodeRunLeaseLost) {
		t.Fatalf("the page of the server that woke up late: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM `+pgiTable(ref, pgschema.TableStageRecord)); n != 1 {
		t.Errorf("%d rows are staged: the page of the old attempt wrote", n)
	}
	if pgiPlaceOf(t, e, lease.No) != place {
		t.Error("the place of the run moved")
	}
	if _, err := e.st.newSink(e.ctx, lease, plan); !pgiCodeIs(err, ingest.CodeRunLeaseLost) {
		t.Errorf("a receiving that begins with the old attempt empties nothing: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM `+pgiTable(ref, pgschema.TableStageRecord)); n != 1 {
		t.Errorf("the old attempt emptied the table: %d rows", n)
	}
	// a run that is no longer receiving does not receive, and a plan of another source is no plan of the run.
	again, _ := e.st.AcquireRun(e.ctx, lease.No, "server-b", lease.Term)
	other := pgiPlan(t, strings.Replace(pgiSourceJSON, "src_pgi", "src_else", 1), pgiMappingJSON)
	if _, err := e.st.newSink(e.ctx, again, other); !pgiCodeIs(err, kernel.CodeInvalidValue) {
		t.Errorf("a plan of another source: %v", err)
	}
	if err := e.st.AdvanceRun(e.ctx, again, ingest.RunReceiving, ingest.RunComputing); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.newSink(e.ctx, again, plan); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Errorf("a run that is computing: %v", err)
	}
	if err := sink.Stage(e.ctx, pgiPage("ds_places", 2, "c")); !pgiCodeIs(err, ingest.CodeRunLeaseLost) {
		t.Errorf("%v", err)
	}
	late := ingest.RunLease{No: again.No, Attempt: again.Attempt, Owner: again.Owner, Term: again.Term}
	sink2 := &Sink{st: e.st, lease: late, kgNo: ref.KGNo, srcNo: ref.SrcNo, datasets: ref.Datasets, skipped: map[string]map[string]int{}}
	if err := sink2.Stage(e.ctx, pgiPage("ds_places", 2, "c")); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Errorf("a page for a run that is not receiving any more: %v", err)
	}
}

// [REQ-702]
func TestPgiReceivingAgainEmptiesTheSourcesPartitionAndBeginsAtTheFirstPageAndTheOtherSourcesAreLeftAlone(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	other := pgiPlan(t, `{"id":"src_two","adapter":"file","datasets":[{"id":"ds_one","parser":"csv","natural_key":["k"],"columns":[{"name":"k"}]}]}`,
		`{"feeds":[{"id":"f_one","dataset":"ds_one","object_type":"Rack","creates_objects":true,"basis":"declared","attributes":[{"attribute":"name","column":"k"}],"identity":[{"components":[{"property":"asset_tag","column":"k"}]}]}]}`)
	kg := e.kg(t, 1)
	ref := e.attach(t, kg, plan)
	refOther := e.attach(t, kg, other)
	a := e.open(t, kg, plan.Source.ID, "server-a")
	b := e.open(t, kg, other.Source.ID, "server-a")
	if _, err := e.st.Receive(e.ctx, b, ingest.Intake{Reader: pgiTexts{"ds_one": "k\nx\ny\n"}, Plan: other}); err != nil {
		t.Fatal(err)
	}
	// the first receiving of the source stops at the third page of its places: the reader fails.
	failing := pgiFailAfter{pgiAll(), 2}
	rep, err := e.st.Receive(e.ctx, a, ingest.Intake{Reader: failing, Plan: plan, Options: ingest.PageOptions{MaxRows: 1}})
	if err == nil || len(rep.Datasets) != 0 {
		t.Fatalf("the reader failed: %+v %v", rep, err)
	}
	rec := pgiTable(ref, pgschema.TableStageRecord)
	if n := e.count(t, `SELECT count(*) FROM `+rec); n != 2 {
		t.Fatalf("%d rows are staged after two pages", n)
	}
	if got := e.text(t, `SELECT stage FROM sync_run WHERE run_no = $1`, a.No); got != "receiving" {
		t.Fatalf("a run whose reading failed stays receiving: %s", got)
	}
	place, err := e.st.ReceivePlace(e.ctx, a.No)
	if err != nil || place.Dataset != "ds_places" || place.Page != 1 || place.Last {
		t.Fatalf("the place after two pages: %+v %v", place, err)
	}
	// the run goes on by receiving again: from the first page, with the partition emptied and nothing of the first try left. The
	// run of the other source is computing and holds the gate of the KG, so this one is not moved on: it stays receiving, with
	// its rows, and the error says why.
	rep, err = e.st.Receive(e.ctx, a, ingest.Intake{Reader: pgiAll(), Plan: plan, Options: ingest.PageOptions{MaxRows: 2}})
	if !pgiCodeIs(err, ingest.CodeKGGateTaken) || len(rep.Datasets) != 3 {
		t.Fatalf("the gate of the KG is taken: %+v %v", rep, err)
	}
	if got := e.text(t, `SELECT stage FROM sync_run WHERE run_no = $1`, a.No); got != "receiving" {
		t.Fatalf("a run that did not get the gate stays receiving: %s", got)
	}
	if n := e.count(t, `SELECT count(*) FROM `+rec+` WHERE dataset_no = $1`, ref.Datasets["ds_places"]); n != 3 {
		t.Errorf("%d rows of the places", n)
	}
	if n := e.count(t, `SELECT count(*) FROM `+rec+` WHERE dataset_no = $1 AND page = 1`, ref.Datasets["ds_places"]); n != 1 {
		t.Errorf("the second page of the places has %d rows: the rows of the first try stayed", n)
	}
	if n := e.count(t, `SELECT count(*) FROM `+pgiTable(refOther, pgschema.TableStageRecord)); n != 2 {
		t.Errorf("the other source has %d rows: receiving again touched its partition", n)
	}
	// the gate is let go, and the caller moves the run on without receiving it again.
	if err := e.st.EndRun(e.ctx, b, ingest.RunEnd{Stage: ingest.RunCanceled}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AdvanceRun(e.ctx, a, ingest.RunReceiving, ingest.RunComputing); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.Receive(e.ctx, a, ingest.Intake{Reader: pgiAll(), Plan: plan}); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Errorf("a run that moved on does not receive again: %v", err)
	}
}

// pgiFailAfter is a reader that gives a number of pages of the first dataset and then fails.
type pgiFailAfter struct {
	pgiTexts
	after int
}

func (f pgiFailAfter) ReadPages(ctx context.Context, source string, ds ingest.Dataset, opts ingest.PageOptions) iter.Seq2[ingest.Page, error] {
	return func(yield func(ingest.Page, error) bool) {
		n := 0
		for p, err := range f.pgiTexts.ReadPages(ctx, source, ds, opts) {
			if n == f.after {
				yield(ingest.Page{}, kernel.Errorf(kernel.CodeInternal, "the source went away"))
				return
			}
			n++
			if !yield(p, err) {
				return
			}
		}
	}
}

// [REQ-701]
func TestPgiAPageOfTenThousandRowsGoesInWithOneCopyOfEachTable(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	ref := e.attach(t, kg, plan)
	lease := e.open(t, kg, plan.Source.ID, "server-a")
	var b strings.Builder
	b.WriteString("asset_id,serial,manufacturer,rack_tag,power_state\n")
	for i := 0; i < 10_000; i++ {
		fmt.Fprintf(&b, "SV%05d,sn%05d,Zorvane,RK%d,on\n", i, i, i%50)
	}
	texts := pgiTexts{"ds_servers": b.String()}
	rep, pages := pgiReceive(t, e, plan, ref, lease, texts, ingest.PageOptions{MaxRows: 10_000})
	if len(pages) != 1 || len(pages[0].Records) != 10_000 || rep.Datasets[0].Pages != 1 {
		t.Fatalf("%d pages", len(pages))
	}
	servers := ref.Datasets["ds_servers"]
	for table, want := range map[string]int{pgschema.TableStageRecord: 10_000, pgschema.TableStageKey: 10_000, pgschema.TableStageEnd: 10_000} {
		if n := e.count(t, `SELECT count(*) FROM `+pgiTable(ref, table)+` WHERE dataset_no = $1 AND page = 0`, servers); n != want {
			t.Errorf("%s has %d rows, want %d", table, n, want)
		}
	}
	// all of it is one transaction: one row of the run says where the page was, and the rows have one transaction id.
	if n := e.count(t, `SELECT count(DISTINCT xmin::text) FROM `+pgiTable(ref, pgschema.TableStageRecord)); n != 1 {
		t.Errorf("the rows of the page were written by %d transactions", n)
	}
}

// [REQ-700]
func TestPgiTheColumnsOfEachCopyAreTheColumnsOfItsTable(t *testing.T) {
	for table, cols := range map[string][]string{pgschema.TableStageRecord: recordCols, pgschema.TableStageKey: keyCols,
		pgschema.TableStageEnd: endCols, pgschema.TableStageClaim: claimCols} {
		if got := strings.Join(pgschema.ColumnsOf(table), ","); got != strings.Join(cols, ",") {
			t.Errorf("%s: the table has %s, the COPY gives %s", table, got, strings.Join(cols, ","))
		}
	}
	if got := copyStatement(7, 12, pgschema.TableStageEnd, endCols); got != `COPY "s_7"."stage_end_p12" ("src_no", "dataset_no", "page", "row_no", "def_no", "side", "lookup_hash", "held") FROM STDIN` {
		t.Errorf("%s", got)
	}
}

func TestPgiArraysAreWrittenInTheTextFormPostgreSQLReads(t *testing.T) {
	if got := textArray([]string{"a", `b"c`, `d\e`, "", "f,g {h}"}); got != `{"a","b\"c","d\\e","","f,g {h}"}` {
		t.Errorf("%s", got)
	}
	if got := textArray(nil); got != "{}" {
		t.Errorf("%s", got)
	}
	if got := smallArray([]int{4, 12}); got != "{4,12}" || smallArray(nil) != "{}" {
		t.Errorf("%s", got)
	}
	if got := bytesArray([][]byte{{0, 255, 16}, {}}); got != `{"\\x00ff10","\\x"}` {
		t.Errorf("%s", got)
	}
}
