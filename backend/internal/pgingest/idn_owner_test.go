package pgingest

import (
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-886]
func TestIdnARunThatDoesNotOwnTheTablesOfTheComputationNeitherBeginsNorGoesOnWithTheIdentification(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	l := s.e.open(t, s.kg, s.plan.Source.ID, "server-a")
	if _, err := s.e.st.Receive(s.e.ctx, l, ingest.Intake{Reader: pgiAll(), Plan: s.plan, Options: ingest.PageOptions{MaxRows: 2}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.RunRawDiff(s.e.ctx, s.e.st, l, s.rdfStart(rdfT0, 0)); err != nil {
		t.Fatal(err)
	}
	owner := fmt.Sprintf(`"s_%d".calc_owner`, s.ref.KGNo)
	s.e.exec(t, "UPDATE "+owner+" SET run_no = run_no + 1000")
	err := s.e.st.BeginIdentify(s.e.ctx, l, idnStart(s, 0, 0))
	if kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Fatalf("a run that does not own the tables began: %v", err)
	}
	s.e.exec(t, "UPDATE "+owner+" SET run_no = run_no - 1000")
	if err := s.e.st.BeginIdentify(s.e.ctx, l, idnStart(s, 0, 0)); err != nil {
		t.Fatal(err)
	}
	s.e.exec(t, "UPDATE "+owner+" SET run_no = run_no + 1000")
	err = s.e.st.IdentifyStep(s.e.ctx, l)
	if kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Fatalf("a run that does not own the tables went on: %v", err)
	}
}
