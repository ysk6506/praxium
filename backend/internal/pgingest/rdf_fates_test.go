package pgingest

import (
	"strconv"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// rdfNow is the time of the run of the fates test: the first waits below are counted at it.
var rdfNow = rdfT0.Add(48*time.Hour + 30*time.Minute + 500*time.Millisecond)

// rdfSeedFates keeps and receives the rows of one dataset so that every fate of a key is in it once.
func rdfSeedFates(t *testing.T, s *rdfSetup) {
	t.Helper()
	const ds = "ds_places"
	s.rdfPutRaw(t,
		rdfRaw{ds, "same", "v1", pgschema.FateUsed}, rdfRaw{ds, "changed", "v1", pgschema.FateUsed},
		rdfRaw{ds, "gone1", "v1", pgschema.FateUsed},          // not shown for the first time
		rdfRaw{ds, "gone2", "v1", pgschema.FateUsed},          // not shown for the third time, and the first was long ago
		rdfRaw{ds, "gone3", "v1", pgschema.FateUsed},          // not shown for the third time, and the first was not long ago
		rdfRaw{ds, "back", "v1", pgschema.FateUsed},           // was not shown and is shown again
		rdfRaw{ds, "held", "v1", pgschema.FateFiltered},       // filtered, for the second run
		rdfRaw{ds, "freed", "v1", pgschema.FateFiltered},      // filtered, for the third run and long enough
		rdfRaw{ds, "unheld", "v1", pgschema.FateFiltered},     // was filtered and is shown whole again
		rdfRaw{ds, "dup", "v1", pgschema.FateUsed},            // has two rows now
		rdfRaw{ds, "undup", "v1", pgschema.FateUsed},          // had two rows and has one
		rdfRaw{ds, "gonef", "v1", pgschema.FateFiltered},      // filtered, not shown now, and the third run of it that is long enough
		rdfRaw{ds, "goneclosed", "v1", pgschema.FateFiltered}, // filtered, and not shown for the third time, which closes it
	)
	s.rdfPutWaits(t,
		rdfWaitRow{ds, "gone2", pgschema.WaitUnseen, 2, rdfT0.Add(2 * time.Hour), false},
		rdfWaitRow{ds, "gone3", pgschema.WaitUnseen, 2, rdfT0.Add(40 * time.Hour), false},
		rdfWaitRow{ds, "back", pgschema.WaitUnseen, 1, rdfT0.Add(40 * time.Hour), false},
		rdfWaitRow{ds, "held", pgschema.WaitFiltered, 1, rdfT0.Add(47 * time.Hour), false},
		rdfWaitRow{ds, "freed", pgschema.WaitFiltered, 2, rdfT0.Add(2 * time.Hour), false},
		rdfWaitRow{ds, "unheld", pgschema.WaitFiltered, 1, rdfT0.Add(2 * time.Hour), false},
		rdfWaitRow{ds, "gone1", pgschema.WaitFiltered, 1, rdfT0.Add(2 * time.Hour), false},
		rdfWaitRow{ds, "gonef", pgschema.WaitFiltered, 2, rdfT0.Add(2 * time.Hour), false},
		rdfWaitRow{ds, "goneclosed", pgschema.WaitFiltered, 1, rdfT0.Add(2 * time.Hour), false},
		rdfWaitRow{ds, "goneclosed", pgschema.WaitUnseen, 2, rdfT0.Add(2 * time.Hour), false},
	)
	s.e.exec(t, `INSERT INTO "s_`+rdfItoa(s.ref.KGNo)+`".record_problem (src_no, dataset_no, key, def_no, field, kind, reason) VALUES ($1, $2, 'undup', NULL, '', 'duplicate_key', 'an old one')`, s.ref.SrcNo, s.rdfNo(ds))
	s.rdfPutStage(t,
		rdfStaged{ds, 0, 0, "same", "v1", pgschema.ReceiveSame, false},
		rdfStaged{ds, 0, 1, "changed", "v2", pgschema.ReceiveChanged, false},
		rdfStaged{ds, 0, 2, "new", "v1", pgschema.ReceiveNew, false},
		rdfStaged{ds, 0, 3, "back", "v1", pgschema.ReceiveSame, false},
		rdfStaged{ds, 0, 4, "held", "v1", pgschema.ReceiveSame, true},
		rdfStaged{ds, 0, 5, "freed", "v1", pgschema.ReceiveSame, true},
		rdfStaged{ds, 0, 6, "unheld", "v1", pgschema.ReceiveChanged, false},
		rdfStaged{ds, 0, 7, "dup", "v1", pgschema.ReceiveSame, false},
		rdfStaged{ds, 1, 0, "dup", "v1", pgschema.ReceiveSame, false},
		rdfStaged{ds, 1, 1, "undup", "v1", pgschema.ReceiveSame, false},
		rdfStaged{ds, 1, 2, "twice", "v1", pgschema.ReceiveNew, false},
		rdfStaged{ds, 1, 3, "twice", "v2", pgschema.ReceiveNew, false},
		rdfStaged{ds, 2, 4, "twice", "v3", pgschema.ReceiveNew, false},
	)
	// the row of the problems of a page is no record
	s.e.exec(t, `INSERT INTO `+pgiTable(s.ref, pgschema.TableStageRecord)+` (src_no, dataset_no, page, row_no, key, change_kind, problems) VALUES ($1, $2, 0, -1, '', 2, '[]')`, s.ref.SrcNo, s.rdfNo(ds))
}

func rdfItoa(n int64) string { return strconv.FormatInt(n, 10) }

// [REQ-780]
func TestRdfEveryFateOfAKeyIsToldAndTheWaitsAreCountedAtTheTimeOfTheRun(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	rdfSeedFates(t, s)
	l := s.rdfComputing(t, "server-a", map[string]ingest.Coverage{"ds_places": ingest.CoverageComplete, "ds_servers": ingest.CoverageComplete})
	rep := s.rdfRun(t, l, s.rdfStart(rdfNow, 0))
	recs := rdfByKey(s.rdfRecords(t), "ds_places")
	want := map[string]struct {
		why  int16
		page string
		row  string
	}{
		"new":        {pgschema.CalcWhyNew, "0", "2"},
		"changed":    {pgschema.CalcWhyChanged, "0", "1"},
		"back":       {pgschema.CalcWhyReturned, "-", "-"},
		"freed":      {pgschema.CalcWhyReleased, "-", "-"},
		"unheld":     {pgschema.CalcWhyChanged, "0", "6"},
		"dup":        {pgschema.CalcWhyDuplicates, "-", "-"},
		"undup":      {pgschema.CalcWhyDuplicates, "-", "-"},
		"twice":      {pgschema.CalcWhyNew, "2", "4"},
		"gone2":      {pgschema.CalcWhyClosed, "-", "-"},
		"gonef":      {pgschema.CalcWhyReleased, "-", "-"},
		"goneclosed": {pgschema.CalcWhyClosed, "-", "-"},
	}
	if len(recs) != len(want) {
		t.Fatalf("%d records: %+v", len(recs), recs)
	}
	for key, w := range want {
		r, ok := recs[key]
		if !ok || r.Why != w.why || r.Page != w.page || r.Row != w.row {
			t.Errorf("%s: %+v, want %+v", key, r, w)
		}
	}
	if r := recs["twice"]; r.Fields != `{v3}` || r.Opened != l.No {
		t.Errorf("the last row of a key that came three times is used, and the run opens it: %+v", r)
	}
	if r := recs["back"]; r.Fields != `{v1}` || r.Opened != 100 || r.Fate != pgschema.FateUsed {
		t.Errorf("a record that is only read again has what the raw record had: %+v", r)
	}
	if r := recs["changed"]; r.Fate != pgschema.FateUsed || recs["unheld"].Fate != pgschema.FateUsed {
		t.Errorf("a changed row that is not filtered is used until the computation says more: %+v", r)
	}
	if r := recs["gonef"]; r.Fields != `{v1}` || r.Opened != 100 || r.Fate != pgschema.FateFiltered {
		t.Errorf("a filtered record that is not shown and whose time is up is let go of with what the raw record had: %+v", r)
	}
	counts := rep.Counts[0]
	if counts.Dataset != "ds_places" || counts.Added != 2 || counts.Modified != 2 || counts.Unchanged != 6 || counts.Deleted != 2 || counts.Unseen != 3 ||
		counts.Returned != 2 || counts.Duplicates != 3 || counts.Released != 2 || counts.Starting != 11 {
		t.Errorf("%+v", counts)
	}
	rdfCheckFateWaits(t, s)
	probs := s.e.count(t, `SELECT count(*) FROM "s_`+rdfItoa(s.ref.KGNo)+`".calc_problem WHERE def_no IS NULL AND kind = 'duplicate_key'`)
	if probs != 2 {
		t.Errorf("a key that is repeated in the batch has the problem once: %d", probs)
	}
}

func rdfCheckFateWaits(t *testing.T, s *rdfSetup) {
	t.Helper()
	got := rdfWaitsByKey(s.rdfCalcWaits(t), "ds_places")
	truncated := rdfNow.Truncate(time.Second)
	type w struct {
		kind    int16
		runs    int
		first   time.Time
		expired bool
	}
	want := map[string][]w{
		"gone1": {{pgschema.WaitUnseen, 1, truncated, false}, {pgschema.WaitFiltered, 2, rdfT0.Add(2 * time.Hour), false}}, // not shown, and still a record a rule filters
		"gone2": {{pgschema.WaitUnseen, 3, rdfT0.Add(2 * time.Hour), true}},
		"gone3": {{pgschema.WaitUnseen, 3, rdfT0.Add(40 * time.Hour), false}},
		"held":  {{pgschema.WaitFiltered, 2, rdfT0.Add(47 * time.Hour), false}},
		"freed": {{pgschema.WaitFiltered, 3, rdfT0.Add(2 * time.Hour), true}},
		"gonef": {{pgschema.WaitUnseen, 1, truncated, false}, {pgschema.WaitFiltered, 3, rdfT0.Add(2 * time.Hour), true}},
		// the record is closed by its absence, so what a rule filters of it is no more counted
		"goneclosed": {{pgschema.WaitUnseen, 3, rdfT0.Add(2 * time.Hour), true}},
	}
	if len(got) != len(want) {
		t.Fatalf("the waits of the run: %+v", got)
	}
	for key, ws := range want {
		if len(got[key]) != len(ws) {
			t.Errorf("%s: %+v", key, got[key])
			continue
		}
		for i, x := range ws {
			g := got[key][i]
			if g.Kind != x.kind || g.Runs != x.runs || !g.First.Equal(x.first) || g.Expired != x.expired {
				t.Errorf("%s: %+v, want %+v", key, g, x)
			}
		}
	}
}

// [REQ-780]
func TestRdfAKeyHasOneReasonByOrderOfWeightSoWhatIsLetGoOfIsToldByTheWaitAndWhatIsShownAgainByTheCounts(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	const ds = "ds_places"
	s.rdfPutRaw(t,
		rdfRaw{ds, "chg_exp", "v1", pgschema.FateFiltered}, // changed and let go of in the same run
		rdfRaw{ds, "chg_ret", "v1", pgschema.FateUsed},     // changed and shown again in the same run
		rdfRaw{ds, "ret_exp", "v1", pgschema.FateFiltered}, // shown again and let go of in the same run
		rdfRaw{ds, "exp_next", "v1", pgschema.FateFiltered},
	)
	s.rdfPutWaits(t,
		rdfWaitRow{ds, "chg_exp", pgschema.WaitFiltered, 2, rdfT0, false},
		rdfWaitRow{ds, "chg_ret", pgschema.WaitUnseen, 1, rdfT0, false},
		rdfWaitRow{ds, "ret_exp", pgschema.WaitUnseen, 1, rdfT0, false},
		rdfWaitRow{ds, "ret_exp", pgschema.WaitFiltered, 2, rdfT0, false},
		rdfWaitRow{ds, "exp_next", pgschema.WaitFiltered, 5, rdfT0, true}, // let go of by an earlier run
	)
	s.rdfPutStage(t,
		rdfStaged{ds, 0, 0, "chg_exp", "v2", pgschema.ReceiveChanged, true},
		rdfStaged{ds, 0, 1, "chg_ret", "v2", pgschema.ReceiveChanged, false},
		rdfStaged{ds, 0, 2, "ret_exp", "v1", pgschema.ReceiveSame, true},
		rdfStaged{ds, 0, 3, "exp_next", "v1", pgschema.ReceiveSame, true},
	)
	l := s.rdfComputing(t, "server-a", map[string]ingest.Coverage{ds: ingest.CoverageComplete})
	rep := s.rdfRun(t, l, s.rdfStart(rdfNow, 0))
	recs := rdfByKey(s.rdfRecords(t), ds)
	want := map[string]int16{"chg_exp": pgschema.CalcWhyChanged, "chg_ret": pgschema.CalcWhyChanged, "ret_exp": pgschema.CalcWhyReturned}
	if len(recs) != len(want) {
		t.Fatalf("the records: %+v", recs)
	}
	for key, why := range want {
		if recs[key].Why != why {
			t.Errorf("%s: the reason is %d, not %d", key, recs[key].Why, why)
		}
	}
	// a key that a wait lets go of is told by the wait, whatever its reason is; the one that was let go of before is not told again
	expired := map[string]bool{}
	for _, w := range s.rdfCalcWaits(t) {
		if w.Kind == pgschema.WaitFiltered {
			expired[w.Key] = w.Expired
		}
	}
	if !expired["chg_exp"] || !expired["ret_exp"] || !expired["exp_next"] {
		t.Errorf("the waits that are up: %v", expired)
	}
	if c := rep.Counts[0]; c.Released != 2 || c.Returned != 2 || c.Modified != 2 || c.Starting != 3 {
		t.Errorf("what is let go of and what is shown again is counted whatever the reason of the record is: %+v", c)
	}
}
