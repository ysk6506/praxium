package pgingest

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// expWant is the set the expansion of the second run of expMakeWorld must make: the objects it adds (by what each is) and the records.
func expWant(w *expWorld) (objects map[string]int16, records []string) {
	return map[string]int16{w.old["SV1"]: pgschema.ObjectWhyRetire, w.old["2"]: pgschema.ObjectWhyRetire, w.old["3"]: pgschema.ObjectWhyRetire, w.old["SV2"]: pgschema.ObjectWhyClaim},
		[]string{"ds_servers/SV2", "ds_servers/SV3", "ds_servers/X1", "ds_servers/X2", "ds_servers/X3", "ds_servers/X4"}
}

// expAdded is the objects of the snapshot that the expansion added (the ones the identification made are not).
func expAdded(a expSnap) map[string]int16 {
	out := map[string]int16{}
	for ref, why := range a.Objects {
		if why != pgschema.ObjectWhyIdentified {
			out[ref] = why
		}
	}
	return out
}

// the rules that have a case of their own in expMakeWorld, and what each is the only one to add
var expOnlyOf = map[string]string{
	ruleOld: "object:SV1", ruleRetireOut: "record:ds_servers/X1", ruleRetireIn: "record:ds_servers/X2", ruleKeyOld: "record:ds_servers/SV2",
	ruleKeyNew: "record:ds_servers/SV3", ruleLimitOut: "record:ds_servers/X3", ruleLimitIn: "record:ds_servers/X4", ruleClaim: "object:SV2",
}

// [REQ-890] [REQ-891] [REQ-892] [REQ-893] [REQ-894] [REQ-895] [REQ-896]
func TestExpTheExpansionOfARunOfAWorldThatHasACaseForEveryRule(t *testing.T) {
	w := expMakeWorld(t, expRetiringMapping(t), 0, false)
	counts, full := w.expRun(t, 0, false)
	t.Run("adds what is wanted", func(t *testing.T) { expCheckAdds(t, w, counts, full) })
	t.Run("eight paths", func(t *testing.T) { expCheckSkips(t, w, full) })
	t.Run("sizes of a unit", func(t *testing.T) {
		for _, size := range []int{1, 3} {
			if _, got := w.expRun(t, size, false); !got.expSame(full) {
				t.Errorf("a unit of %d: %s, and of the default: %s", size, got.expText(), full.expText())
			}
		}
	})
	t.Run("retirable of the references", func(t *testing.T) { expCheckRetirable(t, w) })
	t.Run("everything", func(t *testing.T) { expCheckFull(t, w, full) })
	t.Run("cut and go on", func(t *testing.T) { expCheckResume(t, w, full) })
	t.Run("lease and order of calls", func(t *testing.T) { expCheckCalls(t, w) })
}

func expCheckAdds(t *testing.T, w *expWorld, counts ingest.ExpandCounts, snap expSnap) {
	objects, records := expWant(w)
	if got := expAdded(snap); !maps.Equal(got, objects) {
		t.Errorf("the objects the expansion added: %v, wanted %v", got, objects)
	}
	if !slices.Equal(snap.Expands, records) {
		t.Errorf("the records the expansion added: %v, wanted %v", snap.Expands, records)
	}
	if counts.OldObjects != 3 || counts.Retiring != 3 || counts.RetireRecords != 2 || counts.KeyRecords != 2 || counts.LimitRecords != 2 || counts.ClaimObjects != 1 || counts.Everything != 0 {
		t.Errorf("%+v", counts)
	}
	// what the counts say is what the tables hold
	if n := w.s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_record WHERE why = 2`, w.s.ref.KGNo)); n != counts.RetireRecords+counts.KeyRecords+counts.LimitRecords {
		t.Errorf("the counts say %+v and the tables hold %d records that were added", counts, n)
	}
	if n := w.s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_object WHERE why <> 1`, w.s.ref.KGNo)); n != counts.OldObjects+counts.ClaimObjects {
		t.Errorf("the counts say %+v and the tables hold %d objects that were added", counts, n)
	}
}

func expCheckSkips(t *testing.T, w *expWorld, full expSnap) {
	// the three that the model check left out when the rules were taken away one by one (the competitor of an identifier, the links of an
	// object that is retired, the objects the run is sent) come first
	for _, rule := range []string{ruleClaim, ruleRetireOut, ruleRetireIn, ruleOld, ruleKeyOld, ruleKeyNew, ruleLimitOut, ruleLimitIn} {
		_, got := w.expRun(t, 0, false, rule)
		if got.expSame(full) {
			t.Errorf("without %s the expansion is the same: %s", rule, got.expText())
			continue
		}
		if name, ok := strings.CutPrefix(expOnlyOf[rule], "object:"); ok {
			if why, in := got.Objects[w.old[name]]; in && why != pgschema.ObjectWhyIdentified {
				t.Errorf("without %s the object %s is added: %s", rule, name, got.expText())
			}
		} else if rec := strings.TrimPrefix(expOnlyOf[rule], "record:"); slices.Contains(got.Expands, rec) {
			t.Errorf("without %s the record %s is added: %s", rule, rec, got.expText())
		}
		// all that is lost is its own case, except for the rule that finds the objects the others go through
		if lost := len(full.Expands) + len(expAdded(full)) - len(got.Expands) - len(expAdded(got)); rule != ruleOld && lost != 1 {
			t.Errorf("without %s %d things are lost, not one: %s (with it: %s)", rule, lost, got.expText(), full.expText())
		}
	}
}

// the identity references of the records are retirable where the feed of the record retires what it makes
func expCheckRetirable(t *testing.T, w *expWorld) {
	k := w.s.ref.KGNo
	rows, err := w.s.e.admin.QueryContext(w.s.e.ctx, fmt.Sprintf(`SELECT d.dataset_id, bool_and(r.retirable), bool_or(r.retirable) FROM "s_%[1]d".calc_ref r
		JOIN "s_%[1]d".calc_record c ON c.rid = r.rid JOIN source_dataset d ON d.dataset_no = c.dataset_no WHERE r.role = %[2]d AND r.state = %[3]d GROUP BY 1`,
		k, pgschema.RefRoleIdentity, pgschema.RefResolved))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	seen := 0
	for rows.Next() {
		var ds string
		var all, some bool
		if err := rows.Scan(&ds, &all, &some); err != nil {
			t.Fatal(err)
		}
		seen++
		if want := ds != "ds_vms"; all != want || some != want {
			t.Errorf("the references of %s are retirable: all %t, some %t", ds, all, some)
		}
	}
	if seen != 3 {
		t.Errorf("%d datasets have references that are solved", seen)
	}
}

// the expansion of everything: every open record is a record of the run, the objects are the ones the references point at, and what is
// a candidate is what it is when only what changed is read (the state is one the earlier runs left, so what is not read is not in doubt)
func expCheckFull(t *testing.T, w *expWorld, inc expSnap) {
	counts, all := w.expRun(t, 0, true)
	k := w.s.ref.KGNo
	if counts.Everything == 0 {
		t.Errorf("%+v", counts)
	}
	if n, m := w.s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".raw_record`, k)), w.s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%[1]d".raw_record r WHERE NOT EXISTS
		(SELECT 1 FROM "s_%[1]d".calc_record c WHERE (c.src_no, c.dataset_no, c.key) = (r.src_no, r.dataset_no, r.key))`, k)); n == 0 || m != 0 {
		t.Errorf("%d raw records, %d of them not records of the run", n, m)
	}
	// an object has the first reason it is found for, so a reason that is not "retire" may differ; the candidates may not
	for ref, why := range inc.Objects {
		if _, in := all.Objects[ref]; !in || (why == pgschema.ObjectWhyRetire) != (all.Objects[ref] == pgschema.ObjectWhyRetire) {
			t.Errorf("the object %s is %d when only what changed is read and %d when everything is", ref, why, all.Objects[ref])
		}
	}
	for ref, why := range all.Objects {
		if _, in := inc.Objects[ref]; !in && why == pgschema.ObjectWhyRetire {
			t.Errorf("the object %s is a candidate only when everything is read: %s", ref, all.expText())
		}
	}
	if n := w.s.e.count(t, fmt.Sprintf(`SELECT count(DISTINCT target_no) FROM "s_%d".record_ref WHERE role = 1 AND target_no IS NOT NULL`, k)); len(all.Objects) < n {
		t.Errorf("%d objects are pointed at, and %d are in the run", n, len(all.Objects))
	}
	for _, rec := range inc.Expands {
		if !slices.Contains(all.Expands, rec) {
			t.Errorf("the record %s is read again when only what changed is read, and not when everything is", rec)
		}
	}
}

func expSteps(t *testing.T, w *expWorld, size, cut int, then *Store) expSnap {
	t.Helper()
	expReset(w.s, t, w.l)
	st := w.s.e.st
	st.expTest = nil
	start := ingest.ExpandStart{Rows: size, Cards: expCards(), Feeds: expFeeds(w.s)}
	if err := st.BeginExpand(w.s.e.ctx, w.l, start); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < cut; i++ {
		place, err := st.ExpandPlace(w.s.e.ctx, w.l)
		if err != nil || place.Done() {
			t.Fatalf("at the unit %d: %+v %v", i, place, err)
		}
		if err := st.ExpandStep(w.s.e.ctx, w.l); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ingest.RunExpand(w.s.e.ctx, then, w.l, start); err != nil {
		t.Fatal(err)
	}
	return expSnapOf(w.s, t)
}

// the units can be cut anywhere and gone on from by another server, and the run is not done before its last rule
func expCheckResume(t *testing.T, w *expWorld, full expSnap) {
	expReset(w.s, t, w.l)
	total := 0
	last := ""
	order := map[string]int{}
	for i, r := range expRules() {
		order[r] = i
	}
	if err := w.s.e.st.BeginExpand(w.s.e.ctx, w.l, ingest.ExpandStart{Rows: 4, Cards: expCards(), Feeds: expFeeds(w.s)}); err != nil {
		t.Fatal(err)
	}
	for {
		place, err := w.s.e.st.ExpandPlace(w.s.e.ctx, w.l)
		if err != nil {
			t.Fatal(err)
		}
		if place.Done() {
			break
		}
		if order[place.Rule] < order[last] {
			t.Errorf("the rule %s comes after %s", place.Rule, last)
		}
		last = place.Rule
		if err := w.s.e.st.ExpandStep(w.s.e.ctx, w.l); err != nil {
			t.Fatal(err)
		}
		total++
	}
	if last != ruleClaim {
		t.Errorf("the last unit is of %s: the run was done before its last rule", last)
	}
	other := New(w.s.e.pool)
	for _, cut := range []int{0, 1, total / 2, total - 1} {
		if got := expSteps(t, w, 4, cut, other); !got.expSame(full) {
			t.Errorf("cut after %d of %d units: %s, straight through: %s", cut, total, got.expText(), full.expText())
		}
	}
}

// the calls of a server that lost its lease change nothing and read nothing, the calls out of order are refused
func expCheckCalls(t *testing.T, w *expWorld) {
	ctx, st := w.s.e.ctx, w.s.e.st
	expReset(w.s, t, w.l)
	if _, err := st.ExpandPlace(ctx, w.l); err != nil {
		t.Errorf("the place of a run that has not begun the expansion: %v", err)
	}
	if err := st.ExpandStep(ctx, w.l); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a unit before the expansion begins: %v", err)
	}
	start := ingest.ExpandStart{Rows: 4, Cards: expCards(), Feeds: expFeeds(w.s)}
	if err := st.BeginExpand(ctx, w.l, start); err != nil {
		t.Fatal(err)
	}
	if err := st.BeginExpand(ctx, w.l, start); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("the expansion begun twice: %v", err)
	}
	if err := st.BeginExpand(ctx, w.l, ingest.ExpandStart{Rows: -1, Feeds: expFeeds(w.s)}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a size below nothing: %v", err)
	}
	if err := st.BeginExpand(ctx, w.l, ingest.ExpandStart{Cards: []ingest.ExpandCard{{LinkTypeNo: 1}, {LinkTypeNo: 1}}, Feeds: expFeeds(w.s)}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a link type given twice: %v", err)
	}
	if err := st.ExpandStep(ctx, w.l); err != nil {
		t.Fatal(err)
	}
	// another server takes the run over: the calls of the first, reads too, are refused and nothing is changed
	before, _ := st.ExpandReport(ctx, w.l)
	w.s.e.expire(t, w.l.No)
	next, err := st.AcquireRun(ctx, w.l.No, "server-b", w.l.Term)
	if err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"step":   func() error { return st.ExpandStep(ctx, w.l) },
		"begin":  func() error { return st.BeginExpand(ctx, w.l, start) },
		"place":  func() error { _, err := st.ExpandPlace(ctx, w.l); return err },
		"report": func() error { _, err := st.ExpandReport(ctx, w.l); return err },
	} {
		if err := call(); kernel.CodeOf(err) != ingest.CodeRunLeaseLost {
			t.Errorf("the %s of the lease that was lost: %v", name, err)
		}
	}
	if after, _ := st.ExpandReport(ctx, next); after != before {
		t.Errorf("the calls that were refused changed the counts: %+v, then %+v", before, after)
	}
	if _, err := ingest.RunExpand(ctx, st, next, start); err != nil {
		t.Fatal(err)
	}
	if err := st.ExpandStep(ctx, next); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a unit of an expansion that is done: %v", err)
	}
	w.l = next
	// the tables of the computation are the run's: when they are not, nothing is read
	w.s.e.exec(t, fmt.Sprintf(`DELETE FROM "s_%d".calc_owner`, w.s.ref.KGNo))
	if err := st.ExpandStep(ctx, next); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a unit on the tables of another run: %v", err)
	}
	if _, err := st.ExpandReport(ctx, next); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("the report on the tables of another run: %v", err)
	}
}

// [REQ-890] [REQ-895]
func TestExpTheSetIsTheSameForAnyOrderOfTheRowsReceivedAndOfTheNumbersOfTheRecords(t *testing.T) {
	a := expMakeWorld(t, expRetiringMapping(t), 0, false)
	_, want := a.expRun(t, 0, false)
	b := expMakeWorld(t, expRetiringMapping(t), 1, true)
	if _, got := b.expRun(t, 1, false); !got.expSame(want) {
		t.Errorf("the rows in the other order: %s, and in this one: %s", got.expText(), want.expText())
	}
}

// [REQ-890] [REQ-891]
func TestExpAnObjectIsOnlyACandidateForRetirementWhenAFeedThatRetiresMadeIt(t *testing.T) {
	w := expMakeWorld(t, pgiMappingJSON, 0, false)
	counts, snap := w.expRun(t, 0, false)
	if counts.Retiring != 0 || counts.OldObjects != 3 {
		t.Errorf("%+v", counts)
	}
	if why := snap.Objects[w.old["SV1"]]; why != pgschema.ObjectWhyOldRef {
		t.Errorf("an object no feed that retires made is %d, not an object that was bound before", why)
	}
	for _, rec := range snap.Expands {
		if rec == "ds_servers/X1" || rec == "ds_servers/X2" {
			t.Errorf("the links of an object that is not retired are looked at: %s", snap.expText())
		}
	}
}
