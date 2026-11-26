package pgingest

import (
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// What the computation of the objects refuses when the run is not where the call needs it, and what it refuses of a page that is not the
// next one (ADR-0115).

// [REQ-937]
func TestRcpTheComputationIsRefusedBeforeTheExpansionIsDoneAndAPageThatIsNotTheNextOneIsRefused(t *testing.T) {
	w := rcpWorld(t, 0)
	st, ctx, s := w.s.e.st, w.s.e.ctx, w.s
	start := rcpStart(s, 3, 0)
	// the expansion forgotten: the run is at its identification
	rcpReset(s, t, w.l)
	if err := st.BeginRecompute(ctx, w.l, start); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("begun before the expansion: %v", err)
	}
	// the expansion begun and not done: it is at a rule
	// the unit size is the one BeginExpand writes when the start gives no rows (as the expansion below does)
	s.e.exec(t, fmt.Sprintf(`UPDATE sync_run SET calc_job = 3, counts = counts || '{"expand":{"rule":"claim","size":%d}}'::jsonb WHERE run_no = $1`, ingest.DefaultExpandRows), w.l.No)
	if err := st.BeginRecompute(ctx, w.l, start); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("begun at a rule of the expansion: %v", err)
	}
	rcpExpand(s, t, w.l, true)
	if err := st.BeginRecompute(ctx, w.l, start); err != nil {
		t.Fatal(err)
	}
	page, err := st.RecomputePage(ctx, w.l)
	if err != nil {
		t.Fatal(err)
	}
	made, err := rcpMaker(t, s, 0, 0).Make(page)
	if err != nil {
		t.Fatal(err)
	}
	if page.Last {
		t.Fatal("the case has one page: it does not tell the pages apart")
	}
	other := made
	other.Unit++
	if err := st.PutRecomputed(ctx, w.l, other); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a page of another unit: %v", err)
	}
	stuck := made
	stuck.Through, stuck.Last = 0, false
	if err := st.PutRecomputed(ctx, w.l, stuck); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a page that does not go past the cursor: %v", err)
	}
	if n := rcpCountOf(s, t, "stage_object"); n != 0 {
		t.Errorf("refused pages staged %d objects", n)
	}
	rcpDo(s, t, w.l, 3, 0)
	if _, err := st.RecomputePage(ctx, w.l); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a page after the end: %v", err)
	}
	if err := st.PutRecomputed(ctx, w.l, made); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a page put after the end: %v", err)
	}
	if p, err := st.RecomputePlace(ctx, w.l); err != nil || !p.Finished {
		t.Errorf("%+v %v", p, err)
	}
}

// rcpWithout renames a table of the KG for as long as the call runs, so a statement that reads or writes it fails.
func rcpWithout(s *rdfSetup, t *testing.T, table string, call func()) {
	t.Helper()
	k := s.ref.KGNo
	s.e.exec(t, fmt.Sprintf(`ALTER TABLE "s_%d"."%s" RENAME TO "%s_gone"`, k, table, table))
	defer s.e.exec(t, fmt.Sprintf(`ALTER TABLE "s_%d"."%s_gone" RENAME TO "%s"`, k, table, table))
	call()
}

// the tables the reading of a page reads, and the ones the writing of a page writes or reads
var (
	rcpReadTables = []string{pgschema.TableRawRecord, pgschema.TableRecordRef, pgschema.TableObject, pgschema.TableCalcRecord, pgschema.TableCalcRef,
		pgschema.TableCalcBind, pgschema.TableCalcObject, pgschema.TableCalcNewObj, pgschema.TableCalcAsk}
	rcpWriteTables = []string{pgschema.TableCalcProblem, pgschema.TableStageObject, pgschema.TableStageVolat}
)

// [REQ-937]
func TestRcpAStatementThatFailsLeavesTheRunWhereItWasAndWhatIsWrittenIsNotKept(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, rcpVolatileMapping(t)))
	servers := "asset_id,serial,manufacturer,rack_tag,power_state\nSV1,sn1,Zorvane,RK1,maybe\nSV2,sn2,Zorvane,RK2,off\n"
	l, _ := idnPipeline(s, t, pgiTexts{"ds_places": pgiPlaces, "ds_servers": servers, "ds_vms": pgiVMs}, rdfT0, 0)
	rcpExpand(s, t, l, true)
	w := struct{ l ingest.RunLease }{l}
	st, ctx := s.e.st, s.e.ctx
	start := rcpStart(s, 0, 0)
	rcpWithout(s, t, pgschema.TableCalcObject, func() {
		if err := st.BeginRecompute(ctx, w.l, start); err == nil {
			t.Error("the computation begins without the objects of the run")
		}
	})
	if p, err := st.RecomputePlace(ctx, w.l); err != nil || p.Begun {
		t.Fatalf("a computation that failed to begin has begun: %+v %v", p, err)
	}
	if err := st.BeginRecompute(ctx, w.l, start); err != nil {
		t.Fatal(err)
	}
	page, err := st.RecomputePage(ctx, w.l)
	if err != nil {
		t.Fatal(err)
	}
	made, err := rcpMaker(t, s, 0, 0).Make(page)
	if err != nil {
		t.Fatal(err)
	}
	place, _ := st.RecomputePlace(ctx, w.l)
	reads := 0
	for _, table := range rcpReadTables {
		rcpWithout(s, t, table, func() {
			if _, err := st.RecomputePage(ctx, w.l); err != nil {
				reads++
			}
		})
	}
	if reads < len(rcpReadTables)/2 {
		t.Errorf("only %d of the %d tables a page reads make the reading fail", reads, len(rcpReadTables))
	}
	for _, table := range rcpWriteTables {
		rcpWithout(s, t, table, func() {
			if err := st.PutRecomputed(ctx, w.l, made); err == nil {
				t.Errorf("%s: a page is put without it", table)
			}
		})
		if after, _ := st.RecomputePlace(ctx, w.l); after != place {
			t.Fatalf("%s: a page that failed moved the run: %+v and %+v", table, place, after)
		}
		for _, staged := range []string{pgschema.TableStageObject, pgschema.TableStageVolat, pgschema.TableStageIdent} {
			if staged != table && rcpCountOf(s, t, staged) != 0 {
				t.Fatalf("%s: a page that failed left rows in %s", table, staged)
			}
		}
	}
	if err := st.PutRecomputed(ctx, w.l, made); err != nil {
		t.Errorf("the page cannot be put after the tables are back: %v", err)
	}
}
