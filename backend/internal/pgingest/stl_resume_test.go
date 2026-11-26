package pgingest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The cleaning taken over, done again and asked in the wrong order (ADR-0128).

// stlCut is a Settler that stops (as a server that dies) at the bucket after the first n: the bucket is not done.
type stlCut struct {
	ingest.Settler
	n int
}

func (c *stlCut) SettleBucket(ctx context.Context, l ingest.RunLease, job ingest.SettleJob, part int) (ingest.SettleStep, error) {
	if c.n == 0 {
		return ingest.SettleStep{}, kernel.Errorf(kernel.CodeConflict, "the server stops")
	}
	c.n--
	return c.Settler.SettleBucket(ctx, l, job, part)
}

// stlReversed is the texts with the rows of each dataset in the reverse order.
func stlReversed(texts pgiTexts) pgiTexts {
	out := pgiTexts{}
	for name, text := range texts {
		lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		for i, j := 1, len(lines)-1; i < j; i, j = i+1, j-1 {
			lines[i], lines[j] = lines[j], lines[i]
		}
		out[name] = strings.Join(lines, "\n") + "\n"
	}
	return out
}

// [REQ-986]
func TestStlACleaningCutOffIsTakenOverFromItsBucketAndEndsAsOneThatWasNotAndABucketIsDoneOnce(t *testing.T) {
	steps := stlSteps()
	ref := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	for n := range steps[:2] {
		stlWhole(ref, t, steps[n], stlAt(n), 2)
	}
	want := stlDump(ref, t, true)
	for _, cut := range []int{0, 9} {
		s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
		stlWhole(s, t, steps[0], stlAt(0), 2)
		l := stlCompute(s, t, steps[1], stlAt(1), false)
		stlSettling(s, t, l)
		stlGraph(s, t)
		confirms := &stlConfirms{}
		if _, err := ingest.RunSettle(s.e.ctx, &stlCut{Settler: s.e.st, n: cut}, l, stlStart(s, 2), confirms); kernel.CodeOf(err) != kernel.CodeConflict {
			t.Fatalf("cut at %d: %v", cut, err)
		}
		s.e.expire(t, l.No)
		taken, err := s.e.st.AcquireRun(s.e.ctx, l.No, "server-b", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		// the server that lost the lease is refused, reads too
		if _, err := s.e.st.SettlePlace(s.e.ctx, l); !pgiCodeIs(err, ingest.CodeRunLeaseLost) {
			t.Errorf("cut at %d: the old lease reads the place: %v", cut, err)
		}
		place, err := s.e.st.SettlePlace(s.e.ctx, taken)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.e.st.SettleBucket(s.e.ctx, l, place.Job, place.Part); !pgiCodeIs(err, ingest.CodeRunLeaseLost) {
			t.Errorf("cut at %d: the old lease does a bucket: %v", cut, err)
		}
		if cut > 0 {
			// the bucket before the place was done: asking for it again is refused, and so is a bucket after the place
			prev := ingest.SettleJob(place.Job)
			for _, p := range []int{place.Part - 1, place.Part + 1} {
				if p < 0 {
					continue
				}
				if _, err := s.e.st.SettleBucket(s.e.ctx, taken, prev, p); !pgiCodeIs(err, ingest.CodeRunStage) {
					t.Errorf("cut at %d: the bucket %d of the job %d when the place is %+v: %v", cut, p, prev, place, err)
				}
			}
		}
		if _, err := ingest.RunSettle(s.e.ctx, s.e.st, taken, stlStart(s, 2), confirms); err != nil {
			t.Fatal(err)
		}
		// a cleaning that was cut before the confirmation was written as done confirms again (a seal that changes nothing)
		if cut == 0 && confirms.n != 2 || cut > 0 && confirms.n != 1 {
			t.Errorf("cut at %d: the graph was confirmed %d times", cut, confirms.n)
		}
		stlSame(t, fmt.Sprintf("cut at %d", cut), want, stlDump(s, t, true), "raw", "wait", "ref", "problem")
		if _, err := s.e.st.SettlePlace(s.e.ctx, taken); !pgiCodeIs(err, ingest.CodeRunStage) {
			t.Errorf("cut at %d: a run that ended reads the place of its cleaning: %v", cut, err)
		}
	}
}

// [REQ-986]
func TestStlTheSizeOfABucketAndTheOrderOfTheRowsDoNotChangeWhatTheCleaningLeaves(t *testing.T) {
	// the captures whose links the fifth step takes out: the references that are not solved and their problems
	steps := isoSteps()
	var first map[string][]string
	for i, c := range []struct {
		rows     int
		reversed bool
	}{{1, false}, {1000, false}, {3, true}} {
		s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
		for n, texts := range steps {
			if c.reversed {
				texts = stlReversed(texts)
			}
			stlWhole(s, t, texts, stlAt(n), c.rows)
		}
		got := stlDump(s, t, true)
		if i == 0 {
			first = got
			if len(got["ref"]) == 0 || len(got["problem"]) == 0 {
				t.Fatalf("the case has no references or problems: %v", got)
			}
			continue
		}
		stlSame(t, fmt.Sprintf("buckets of %d, reversed %t", c.rows, c.reversed), first, got, "raw", "wait", "ref", "problem")
	}
}

// [REQ-986]
func TestStlTheCleaningBeginsOnceForARunThatIsSettlingWhoseComputingIsDoneAndWhoseTablesAreItsOwn(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	start := stlStart(s, 2)
	l := rclPrepare(s, t, stlSteps()[0], stlAt(0), false)
	if _, err := ingest.RunSettle(s.e.ctx, s.e.st, l, start, &stlConfirms{}); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Errorf("a run that is computing is cleaned: %v", err)
	}
	stlSettling(s, t, l)
	if err := s.e.st.BeginSettle(s.e.ctx, l, start); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Errorf("a run whose fifth step is not done begins its cleaning: %v", err)
	}
	for _, bad := range []ingest.SettleStart{{Rows: -1}, {Feeds: []ingest.SettleFeed{{Feed: 0, ID: "a"}, {Feed: 0, ID: "b"}}}, {Feeds: []ingest.SettleFeed{{Feed: 0}}}} {
		if err := s.e.st.BeginSettle(s.e.ctx, l, bad); !pgiCodeIs(err, kernel.CodeInvalidValue) {
			t.Errorf("the start %+v: %v", bad, err)
		}
	}
	if _, err := ingest.RunSettle(s.e.ctx, s.e.st, l, start, nil); !pgiCodeIs(err, kernel.CodeInvalidValue) {
		t.Errorf("a cleaning with no confirmer: %v", err)
	}
	if _, err := s.e.st.SettleBucket(s.e.ctx, l, ingest.SettleJobConfirm, 0); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Errorf("a bucket of a cleaning that has not begun: %v", err)
	}
	s.e.exec(t, `UPDATE sync_run SET counts = counts || '{"isolate":{"finished":true}}' WHERE run_no = $1`, l.No)
	s.e.exec(t, fmt.Sprintf(`DELETE FROM "s_%d".calc_owner`, s.ref.KGNo))
	if err := s.e.st.BeginSettle(s.e.ctx, l, start); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Errorf("a run whose tables of the computation are not its own begins its cleaning: %v", err)
	}
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_owner (run_no) VALUES ($1)`, s.ref.KGNo), l.No)
	if err := s.e.st.BeginSettle(s.e.ctx, l, start); err != nil {
		t.Fatal(err)
	}
	if err := s.e.st.BeginSettle(s.e.ctx, l, start); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Errorf("the cleaning begins twice: %v", err)
	}
	s.e.exec(t, fmt.Sprintf(`DELETE FROM "s_%d".calc_owner`, s.ref.KGNo))
	if _, err := s.e.st.SettleBucket(s.e.ctx, l, ingest.SettleJobConfirm, 0); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Errorf("a bucket of a run whose tables are not its own: %v", err)
	}
	if n := s.e.count(t, `SELECT count(*) FROM sync_run WHERE run_no = $1 AND settle_job = $2 AND settle_part = 0`, l.No, int(ingest.SettleJobConfirm)); n != 1 {
		t.Error("the place of the cleaning moved")
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".raw_record`, s.ref.KGNo)); n != 0 {
		t.Errorf("%d raw records were written", n)
	}
}
