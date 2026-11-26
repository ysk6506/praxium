package pgingest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// idnCut gives the port to the run until it has done n units, and then fails as a server that dies does.
type idnCut struct {
	ingest.Identifier
	left int
}

var idnDied = kernel.Errorf(kernel.CodeInternal, "the server died")

func (c *idnCut) idnTick() error {
	if c.left == 0 {
		return idnDied
	}
	c.left--
	return nil
}

func (c *idnCut) IdentifyStep(ctx context.Context, l ingest.RunLease) error {
	if err := c.idnTick(); err != nil {
		return err
	}
	return c.Identifier.IdentifyStep(ctx, l)
}

func (c *idnCut) PutRebuilt(ctx context.Context, l ingest.RunLease, unit int, rows []ingest.RebuiltRow) error {
	if err := c.idnTick(); err != nil {
		return err
	}
	return c.Identifier.PutRebuilt(ctx, l, unit, rows)
}

func (c *idnCut) PutAnswers(ctx context.Context, l ingest.RunLease, a []ingest.Answer) error {
	if err := c.idnTick(); err != nil {
		return err
	}
	return c.Identifier.PutAnswers(ctx, l, a)
}

// idnState is what a run made, in a form two runs are held to each other by: what each record is bound to, and the number of the rows
// of the tables of the identification.
func idnState(s *rdfSetup, t *testing.T) string {
	t.Helper()
	out := ""
	for _, b := range idnBounds(s, t) {
		out += fmt.Sprintf("%s/%s/%d=%s;", b.Dataset, b.Key, b.Feed, b.Ref)
	}
	for _, table := range []string{"calc_newobj", "calc_newkey", "calc_dropped", "calc_ref", "calc_problem", "calc_object", "calc_bind"} {
		out += fmt.Sprintf("%s=%d;", table, s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".%s`, s.ref.KGNo, table)))
	}
	return out
}

// [REQ-883]
func TestIdnARunThatStopsGoesOnFromTheNextUnitAndMakesWhatARunThatDoesNotStopMakes(t *testing.T) {
	texts := idnPlaces("1,HQ,B1,F1,R1,rack a,RK1", "2,HQ,B1,F1,R1,rack b,RK2", "3,HQ,B1,F2,R2,rack c,RK3", "4,HQ,B2,F1,R1,rack d,RK4")
	whole := rdfNew(t, pgiPlan(t, pgiSourceJSON, idnMapping))
	idnPipeline(whole, t, texts, rdfT0, 1)
	want := idnState(whole, t)

	for _, cut := range []int{0, 1, 5, 17, 40, 80} {
		s := rdfNew(t, pgiPlan(t, pgiSourceJSON, idnMapping))
		l := s.e.open(t, s.kg, s.plan.Source.ID, "server-a")
		if _, err := s.e.st.Receive(s.e.ctx, l, ingest.Intake{Reader: texts, Plan: s.plan, Options: ingest.PageOptions{MaxRows: 2}}); err != nil {
			t.Fatal(err)
		}
		if _, err := ingest.RunRawDiff(s.e.ctx, s.e.st, l, s.rdfStart(rdfT0, 1)); err != nil {
			t.Fatal(err)
		}
		keys := ingest.NewPlanKeys(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan})
		_, err := ingest.RunIdentify(s.e.ctx, &idnCut{Identifier: s.e.st, left: cut}, l, idnStart(s, 1, 1), keys)
		died := err != nil
		if died && err.Error() != idnDied.Error() {
			t.Fatalf("cut %d: %v", cut, err)
		}
		// another server takes the run over once the lease has run out; the first one, woken late, changes nothing
		s.e.expire(t, l.No)
		l2, err := s.e.st.AcquireRun(s.e.ctx, l.No, "server-b", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if died {
			before := idnState(s, t)
			if err := s.e.st.IdentifyStep(s.e.ctx, l); kernel.CodeOf(err) != ingest.CodeRunLeaseLost {
				t.Errorf("cut %d: a unit of the attempt that lost the run: %v", cut, err)
			}
			if after := idnState(s, t); after != before {
				t.Errorf("cut %d: a unit of the attempt that lost the run changed what the run made", cut)
			}
		}
		if _, err := ingest.RunIdentify(s.e.ctx, s.e.st, l2, idnStart(s, 1, 1), keys); err != nil {
			t.Fatalf("cut %d: %v", cut, err)
		}
		if got := idnState(s, t); got != want {
			t.Errorf("cut %d: the run made\n%s\nand the whole one\n%s", cut, got, want)
		}
	}
}
