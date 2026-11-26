package pgingest

import (
	"fmt"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The end of a search for the groups and the groups of several objects (ADR-0116): where and by whom Go reads and writes, what a read
// does to the lease, and the two conditions of the statement that binds a record of a group of several objects. Every helper starts
// with idnf, as in idn_follow_test.go.

// idnfLeaseFor says whether the lease of the run is held by the attempt and runs out more than half a term from now.
func idnfLeaseFor(s *rdfSetup, t *testing.T, l ingest.RunLease) bool {
	t.Helper()
	return s.e.count(t, `SELECT count(*) FROM sync_run WHERE run_no = $1 AND attempt = $2 AND lease_until > now() + make_interval(secs => $3::float8)`,
		l.No, l.Attempt, l.Term.Seconds()/2) == 1
}

// idnfLabelRows is how many labels the run holds.
func idnfLabelRows(s *rdfSetup, t *testing.T) int {
	t.Helper()
	return s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_label`, s.ref.KGNo))
}

// [REQ-959]
func TestIdnGoReadsAndWritesTheEndOfASearchOnlyWhereTheRunIsAtItAndEachReadExtendsTheLeaseOfTheAttemptThatHoldsIt(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	l := s.e.open(t, s.kg, s.plan.Source.ID, "server-a")
	if _, err := s.e.st.Receive(s.e.ctx, l, ingest.Intake{Reader: idnServers(idnShuffled(300, 11)...), Plan: s.plan, Options: ingest.PageOptions{MaxRows: 100}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.RunRawDiff(s.e.ctx, s.e.st, l, s.rdfStart(rdfT0, 100)); err != nil {
		t.Fatal(err)
	}
	start := idnStart(s, 100, 1)
	start.Turns = 2
	if err := s.e.st.BeginIdentify(s.e.ctx, l, start); err != nil {
		t.Fatal(err)
	}
	// where the run is not at the end of a search, Go's read and write are refused and change nothing
	refused := func(when string, lease ingest.RunLease, code kernel.Code) {
		t.Helper()
		before := idnState(s, t) + fmt.Sprint(idnfLabelRows(s, t))
		_, rerr := s.e.st.SettleRead(s.e.ctx, lease, 0)
		perr := s.e.st.PutSettled(s.e.ctx, lease, []ingest.SettledPair{{Pid: 1025, Label: 1025}}, ingest.SettleEnd{Last: true})
		if kernel.CodeOf(rerr) != code || kernel.CodeOf(perr) != code {
			t.Errorf("%s: the read %v and the write %v, not %s", when, rerr, perr, code)
		}
		if after := idnState(s, t) + fmt.Sprint(idnfLabelRows(s, t)); after != before {
			t.Errorf("%s: a call that was refused changed what the run holds", when)
		}
	}
	refused("before the rounds", l, ingest.CodeRunStage)
	for {
		place, err := s.e.st.IdentifyPlace(s.e.ctx, l.No)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case place.Need == ingest.NeedSettle:
		case place.Need == ingest.NeedRebuild:
			// every record of the run was received, so there is no key to make again
			page, err := s.e.st.RebuildPage(s.e.ctx, l)
			if err != nil || len(page.Rows) != 0 {
				t.Fatalf("%d records to make again: %v", len(page.Rows), err)
			}
			if err := s.e.st.PutRebuilt(s.e.ctx, l, page.Unit, nil); err != nil {
				t.Fatal(err)
			}
			continue
		case place.Need != ingest.NeedNothing || place.Done():
			t.Fatalf("the run is at %+v, not at the end of a search", place)
		default:
			if err := s.e.st.IdentifyStep(s.e.ctx, l); err != nil {
				t.Fatal(err)
			}
			continue
		}
		break
	}
	// each page Go reads moves the end of the lease a term on, and the attempt is the same
	pages := 0
	for after := int64(0); ; pages++ {
		s.e.exec(t, `UPDATE sync_run SET lease_until = now() + interval '1 second' WHERE run_no = $1`, l.No)
		page, err := s.e.st.SettleRead(s.e.ctx, l, after)
		if err != nil {
			t.Fatal(err)
		}
		if !idnfLeaseFor(s, t, l) {
			t.Errorf("page %d: the read did not extend the lease of the attempt %d", pages, l.Attempt)
		}
		if page.Done {
			break
		}
		after = page.Next
	}
	if pages < 2 {
		t.Fatalf("Go read %d pages: the test is about a read of several", pages+1)
	}
	// a server that lost the run reads and writes nothing, and does not move the lease of the one that took it over
	s.e.expire(t, l.No)
	l2, err := s.e.st.AcquireRun(s.e.ctx, l.No, "server-b", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	lease := s.e.text(t, `SELECT lease_until::text FROM sync_run WHERE run_no = $1`, l.No)
	refused("by the attempt that lost the run", l, ingest.CodeRunLeaseLost)
	if now := s.e.text(t, `SELECT lease_until::text FROM sync_run WHERE run_no = $1`, l.No); now != lease {
		t.Errorf("the attempt that lost the run moved the lease from %s to %s", lease, now)
	}
	if _, err := ingest.RunIdentify(s.e.ctx, s.e.st, l2, start, ingest.NewPlanKeys(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan})); err != nil {
		t.Fatal(err)
	}
	if objs := idnObjectsOf(s, t); len(objs) != 1 {
		t.Fatalf("a chain of 300 records is %d objects", len(objs))
	}
	refused("after the run is done", l2, ingest.CodeRunStage)
}

// idnfAmbiguous runs the servers A (sn1, u1) and B (sn2, u2), and then the records given beside them, and gives the counts of the
// second run, what each record of it is bound to and the records that have a problem of identity.
func idnfAmbiguous(t *testing.T, second ...string) (ingest.IdentifyCounts, map[string]string, map[string]bool) {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	first := []string{"A,sn1,Zorvane,u1", "B,sn2,Zorvane,u2"}
	l1, _ := idnPipelineWith(s, t, idnServers(first...), rdfT0, 3, 0, 0, 0, 0)
	idnApply(s, t)
	s.rdfSettle(t, l1)
	_, counts := idnPipelineWith(s, t, idnServers(append(first, second...)...), rdfT0.Add(time.Hour), 4, 7, 0, 0, 0)
	bound := map[string]string{}
	for _, b := range idnBounds(s, t) {
		bound[b.Key] = b.Ref
	}
	k := s.ref.KGNo
	rows, err := s.e.admin.QueryContext(s.e.ctx, fmt.Sprintf(`SELECT c.key FROM "s_%d".calc_problem p JOIN "s_%d".calc_record c ON c.rid = p.rid WHERE p.kind = 'identity_conflict'`, k, k))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	conflicts := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatal(err)
		}
		conflicts[key] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return counts, bound, conflicts
}

// [REQ-882]
func TestIdnARecordOfAGroupOfSeveralObjectsWhoseKeyThePairsOfTwoObjectsHaveIsNotBoundToEitherAndIsAConflict(t *testing.T) {
	// Q is A (its serial), R is B (its serial), and P has a new serial and the uuid PH that Q and R have: the key PH is of two objects
	counts, bound, conflicts := idnfAmbiguous(t, "Q,sn1,Zorvane,PH", "R,sn2,Zorvane,PH", "P,snP,Zorvane,PH")
	if bound["Q"] == "" || bound["R"] == "" || bound["Q"] == bound["R"] {
		t.Fatalf("Q and R are %q and %q", bound["Q"], bound["R"])
	}
	if bound["P"] != "" || counts.Unresolved != 1 || !conflicts["P"] {
		t.Errorf("P is %q (%+v, conflicts %v): a key of two objects tells none", bound["P"], counts, conflicts)
	}
}

// [REQ-882]
func TestIdnARecordOfAGroupOfSeveralObjectsWhoseKeysAreEachOfOneObjectButOfTwoIsNotBoundToEither(t *testing.T) {
	// Q is A (its serial) and R is B (its uuid); P shares its serial with R and its uuid PH with Q: each key is of one object, and the two
	// are two
	counts, bound, conflicts := idnfAmbiguous(t, "Q,sn1,Zorvane,PH", "R,snP,Zorvane,u2", "P,snP,Zorvane,PH")
	if bound["Q"] == "" || bound["R"] == "" || bound["Q"] == bound["R"] {
		t.Fatalf("Q and R are %q and %q", bound["Q"], bound["R"])
	}
	if bound["P"] != "" || counts.Unresolved != 1 || !conflicts["P"] {
		t.Errorf("P is %q (%+v, conflicts %v): keys of two objects tell none", bound["P"], counts, conflicts)
	}
}
