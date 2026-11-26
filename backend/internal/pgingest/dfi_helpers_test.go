package pgingest

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the difference of the fifth step (ADR-0127). Every helper, type and constant they add starts with dfi, so that none meets a
// name of another test file of this folder, and none of them is a method of a type of another file.

// dfiDo does the difference of a run whose isolation is done, from where it stands.
func dfiDo(s *rdfSetup, t *testing.T, l ingest.RunLease, rows, chunk int) ingest.DiffCounts {
	t.Helper()
	counts, err := ingest.RunDiff(s.e.ctx, s.e.st, l, ingest.DiffStart{Rows: rows, ChunkRows: chunk})
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

// dfiRunTo does a run of the texts up to the end of its difference: the first steps, the links, the isolation and the difference, with the
// sizes of a unit of the difference and of a chunk given (0 is the default of each).
func dfiRunTo(s *rdfSetup, t *testing.T, texts pgiTexts, at time.Time, full bool, rows, chunk int) (ingest.RunLease, ingest.DiffCounts) {
	t.Helper()
	plans := rclPlans(s)
	l := rclPrepare(s, t, texts, at, full)
	rclDo(s, t, l, plans, 0)
	isoDo(s, t, l, plans, 0)
	return l, dfiDo(s, t, l, rows, chunk)
}

// dfiRows is the difference as text, in its stored order: chunk, kind, type, ref, change and the two hashes (- for none, NULL being told
// apart from an empty value), and for a key its context.
func dfiRows(s *rdfSetup, t *testing.T) []string {
	t.Helper()
	return expList(s.e, t, fmt.Sprintf(`SELECT chunk_no || ' ' || kind || ' ' || type_no || ' ' || ref || ' ' || change_kind || ' ' ||
		COALESCE(encode(old_hash, 'hex'), '-') || ' ' || COALESCE(encode(new_hash, 'hex'), '-') || CASE WHEN kind = %d THEN ' ' || ctx_no ELSE '' END
		FROM "s_%d".ingest_diff ORDER BY ord`, pgschema.DiffKindKey, s.ref.KGNo))
}

// dfiKG is the apply of a KG of the tests: the applier the KG is checked with and the lease of it.
type dfiKG struct {
	app   *graph.Applier
	lease graph.Lease
	clock *kernel.ManualClock
}

// dfiApplierOf is the applier of the KG of the setup, with the key provider the contents were hashed with (rcpHasher).
func dfiApplierOf(s *rdfSetup, t *testing.T) *dfiKG {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	clock := kernel.NewManualClock(rdfT0)
	app, err := graph.NewApplier(s.e.pst, reg, graph.NewTestKeyProvider("rcp"), clock, kernel.NewSequentialIDs(), graph.Options{Semantic: 1, LeaseTTL: 1000 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := app.Acquire(s.e.ctx, s.kg.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &dfiKG{app: app, lease: lease, clock: clock}
}

// dfiApply applies the stored difference of the run with the staged apply, chunk by chunk, and gives where it ended.
func dfiApply(s *rdfSetup, t *testing.T, k *dfiKG, l ingest.RunLease) graph.StagedProgress {
	t.Helper()
	ap := graph.StagedApply{KG: s.kg.ID, Generation: s.kg.Generation, Lease: k.lease, Now: k.clock.Now(), ApplyID: fmt.Sprintf("run-%d", l.No), Kind: graph.BatchSync, SourceID: s.plan.Source.ID}
	p, err := s.e.pst.StagedProgress(s.e.ctx, ap)
	if err != nil {
		t.Fatal(err)
	}
	for p.State == graph.StagedPending || p.State == graph.StagedOpen {
		if p, err = s.e.pst.ApplyStagedChunk(s.e.ctx, ap, p); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// dfiSettle does what the cleaning will do with the references of the records of the run (they are written over by the run's), and what
// rdfSettle does with the raw records and the waits, and ends the run.
func dfiSettle(s *rdfSetup, t *testing.T, l ingest.RunLease) {
	t.Helper()
	k := s.ref.KGNo
	for _, q := range []string{
		`DELETE FROM "s_%[1]d".record_ref r USING "s_%[1]d".calc_record c WHERE c.why <> 2 AND c.src_no = r.src_no AND c.dataset_no = r.dataset_no AND c.key = r.key`,
		`INSERT INTO "s_%[1]d".record_ref (src_no, dataset_no, key, role, def_no, sub, lookup_hash, target_no, state, retirable)
			SELECT c.src_no, c.dataset_no, c.key, r.role, r.def_no, r.sub, r.lookup_hash, r.target_no, r.state, r.retirable FROM "s_%[1]d".calc_ref r JOIN "s_%[1]d".calc_record c ON c.rid = r.rid
			ON CONFLICT (src_no, dataset_no, key, role, def_no, sub) DO UPDATE SET target_no = excluded.target_no, state = excluded.state, retirable = excluded.retirable`,
	} {
		s.e.exec(t, fmt.Sprintf(q, k))
	}
	s.rdfSettle(t, l)
}

// dfiStep does a run of the texts to the end: up to its difference, the staged apply and the cleaning.
func dfiStep(s *rdfSetup, t *testing.T, k *dfiKG, texts pgiTexts, at time.Time, full bool) ingest.DiffCounts {
	t.Helper()
	l, counts := dfiRunTo(s, t, texts, at, full, 0, 0)
	p := dfiApply(s, t, k, l)
	if counts.Rows > 0 && (p.State != graph.StagedDone || p.Chain != counts.Fingerprint) || counts.Rows == 0 && p.State != graph.StagedEmpty {
		t.Fatalf("the apply ended at %+v and the difference has %d rows and the fingerprint %s", p, counts.Rows, counts.Fingerprint)
	}
	dfiSettle(s, t, l)
	return counts
}

// dfiHead is the state hash of the KG, and the KG is checked against the rows it has (Verify).
func dfiHead(s *rdfSetup, t *testing.T, k *dfiKG) graph.HashState {
	t.Helper()
	h, err := s.e.pst.Head(s.e.ctx, s.kg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.app.Verify(s.e.ctx, s.kg.ID); err != nil {
		t.Fatalf("the KG is not what its head says: %v", err)
	}
	return h.Hash
}

// dfiKGText is what the KG has open, by the ids: the objects (type, name, content hash), the links and the identifiers.
func dfiKGText(s *rdfSetup, t *testing.T) string {
	t.Helper()
	k := s.ref.KGNo
	parts := [][]string{
		expList(s.e, t, fmt.Sprintf(`SELECT 'o ' || ref || ' ' || type_no || ' ' || name || CASE WHEN retired_seq IS NULL THEN '' ELSE ' retired' END FROM "s_%d".object ORDER BY ref`, k)),
		expList(s.e, t, fmt.Sprintf(`SELECT 'l ' || ref || ' ' || encode(content_hash, 'hex') FROM "s_%d".link ORDER BY ref`, k)),
		expList(s.e, t, fmt.Sprintf(`SELECT 'i ' || o.ref || ' ' || i.scheme || '=' || i.value FROM "s_%[1]d".object_identifier i JOIN "s_%[1]d".object o ON o.obj_no = i.obj_no WHERE i.seq_to IS NULL ORDER BY 1`, k)),
	}
	var lines []string
	for _, p := range parts {
		lines = append(lines, p...)
	}
	return strings.Join(lines, "\n")
}
