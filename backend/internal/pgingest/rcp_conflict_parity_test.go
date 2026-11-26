package pgingest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// The source conflicts (a value that lost to a value of another text) of the computation of the objects are the ones the old pipeline
// reports from the same captures: ADR-0119 closes the gap the review found in the comparison of ADR-0115, which compared the objects.

// rcpConflictsOf is the conflicts the new flow wrote for the run, as dataset/key.
func rcpConflictsOf(s *rdfSetup, t *testing.T) []string {
	t.Helper()
	return expList(s.e, t, fmt.Sprintf(`SELECT d.dataset_id || '/' || c.key FROM "s_%[1]d".calc_problem p JOIN "s_%[1]d".calc_record c ON c.rid = p.rid
		JOIN source_dataset d ON d.dataset_no = c.dataset_no WHERE p.kind = 'source_conflict' ORDER BY 1`, s.ref.KGNo))
}

// [REQ-974]
func TestRcpTheSourceConflictsAreTheOnesTheOldPipelineReportsFromTheSameCaptures(t *testing.T) {
	o := rcpOldNew(t, idnMoreSource, idnMoreMapping)
	s := rdfNew(t, pgiPlan(t, idnMoreSource, idnMoreMapping))
	total := 0
	for n, st := range idnMoreSteps() {
		at := rdfT0.Add(time.Duration(n) * 13 * time.Hour)
		texts := pgiTexts{"ds_a": idnMoreTexts(st.a, true), "ds_b": idnMoreTexts(st.b, false)}
		for name, text := range texts {
			o.fetch.text[name] = text
		}
		o.clock.Set(at)
		report, err := o.runner.Run(context.Background(), o.plan, o.kg.ID)
		if err != nil {
			t.Fatalf("the old run: %v", err)
		}
		var old []string
		for _, p := range report.Problems {
			if p.Kind == ingest.ProblemSourceConflict {
				old = append(old, p.Dataset+"/"+p.Key)
			}
		}
		sort.Strings(old)
		l, _ := idnPipeline(s, t, texts, at, 0)
		rcpExpand(s, t, l, true)
		start := rcpStart(s, 0, 0)
		start.Seen = at.UTC().Format(time.RFC3339)
		if _, err := ingest.RunRecompute(s.e.ctx, s.e.st, l, start, ingest.NewObjectComputer(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan}, rcpHasher(t, s), start)); err != nil {
			t.Fatal(err)
		}
		got := rcpConflictsOf(s, t)
		total += len(got)
		if strings.Join(old, " ") != strings.Join(got, " ") {
			t.Errorf("step %d: the old pipeline reports %v, the new flow %v", n, old, got)
		}
		rcpApply(s, t)
		s.rdfSettle(t, l)
	}
	if total == 0 {
		t.Fatal("the case has no conflict: it does not compare anything")
	}
}
