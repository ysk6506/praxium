package pgingest

import (
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The fifth step on a server whose tables were damaged (ADR-0123): a step that needs a table that is gone fails with an error, and a run
// whose tables of the computation are another run's is refused.

// isoSmallWorld has a page of work in each phase: an identifier two objects say, one of which the KG has it with (and a record the run
// reads again claims it), a link whose end is gone, and two links at one start.
func isoSmallWorld(t *testing.T) *isoFix {
	t.Helper()
	f := isoLinksWorld(t)
	f.isoIdent(t, "a", "v", "", true, true, "")
	f.isoIdent(t, "b", "v", "kb", true, false, "kbx")
	return f
}

// isoLinksWorld is isoSmallWorld without the identifier: the links of the run are the first thing a damaged table of the problems or of
// the references stops.
func isoLinksWorld(t *testing.T) *isoFix {
	t.Helper()
	f := isoNewFix(t)
	m := f.m
	for _, o := range []struct {
		ref, where string
		typ        int
	}{{"a", "both", m.server}, {"b", "both", m.server}, {"r1", "kg", m.rack}, {"r2", "kg", m.rack}} {
		f.isoObj(t, o.ref, o.typ, o.where)
	}
	f.no["gone"] = 999
	f.isoLink(t, "l-1", m.mounted, "a", "gone", "k1", true, false, false)
	f.isoLink(t, "l-2", m.mounted, "b", "r1", "k2", true, false, false)
	f.isoLink(t, "l-3", m.mounted, "b", "r2", "k3", true, false, false)
	return f
}

// isoDamaged does the step of a world with a table renamed, and holds that it fails.
func isoDamaged(t *testing.T, world func(*testing.T) *isoFix, table string) {
	t.Run(table, func(t *testing.T) {
		f := world(t)
		f.s.e.exec(t, fmt.Sprintf(`ALTER TABLE "s_%d"."%s" RENAME TO "%s_gone"`, f.s.ref.KGNo, table, table))
		if _, err := ingest.RunIsolate(f.s.e.ctx, f.s.e.st, f.l, ingest.IsolateStart{Cards: ingest.ExpandCards(f.m.reg)}, ingest.NewIsolationMaker(f.m.reg)); err == nil {
			t.Fatalf("the step went on without %s", table)
		}
	})
}

// [REQ-846]
func TestIsoAStepOfTheLinksThatNeedsATableThatIsDamagedFailsWithAnError(t *testing.T) {
	for _, table := range []string{pgschema.TableStageLink, pgschema.TableCalcLinkWin, pgschema.TableCalcSeat, pgschema.TableLink, pgschema.TableCalcProblem, pgschema.TableCalcRef} {
		isoDamaged(t, isoLinksWorld, table)
	}
}

// [REQ-846]
func TestIsoAStepOfTheIdentifiersThatNeedsATableThatIsDamagedFailsWithAnError(t *testing.T) {
	for _, table := range []string{pgschema.TableCalcIsolated, pgschema.TableStageIdent, pgschema.TableObjectIdentifier, pgschema.TableRecordRef} {
		isoDamaged(t, isoSmallWorld, table)
	}
	t.Run("the computation is another run's", func(t *testing.T) {
		f := isoSmallWorld(t)
		f.s.e.exec(t, f.isoQ(`DELETE FROM {k}.calc_owner`))
		if _, err := ingest.RunIsolate(f.s.e.ctx, f.s.e.st, f.l, ingest.IsolateStart{}, ingest.NewIsolationMaker(f.m.reg)); kernel.CodeOf(err) != ingest.CodeRunStage {
			t.Errorf("%v", err)
		}
	})
	t.Run("the whole of it", func(t *testing.T) {
		f := isoSmallWorld(t)
		counts := f.isoRun(t, 1)
		if counts.Idents != 1 || counts.Missing != 1 || counts.Cardinality != 1 || counts.Refs != 4 || counts.Problems != 4 {
			t.Errorf("%+v\n%s", counts, f.isoState(t))
		}
	})
}
