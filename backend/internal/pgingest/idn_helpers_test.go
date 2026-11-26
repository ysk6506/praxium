package pgingest

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the identification (ADR-0108). Every helper, type and field they add starts with idn, so that none meets a name of
// another test file of this folder.

// idnStart is the start of the identification of the plan, with the sizes of a unit given (0 is the default).
func idnStart(s *rdfSetup, size, pages int) ingest.IdentifyStart {
	return ingest.IdentifyStart{Feeds: s.plan.IdentifyFeeds(s.ref.SrcNo), Schemes: s.plan.SchemeNumbers(), BucketRows: size, Pages: pages}
}

// idnPipeline receives the datasets, compares them with the raw records and identifies the records of the run.
func idnPipeline(s *rdfSetup, t *testing.T, texts pgiTexts, at time.Time, size int) (ingest.RunLease, ingest.IdentifyCounts) {
	t.Helper()
	l := s.e.open(t, s.kg, s.plan.Source.ID, "server-a")
	if _, err := s.e.st.Receive(s.e.ctx, l, ingest.Intake{Reader: texts, Plan: s.plan, Options: ingest.PageOptions{MaxRows: 2}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.RunRawDiff(s.e.ctx, s.e.st, l, s.rdfStart(at, size)); err != nil {
		t.Fatal(err)
	}
	keys := ingest.NewPlanKeys(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan})
	counts, err := ingest.RunIdentify(s.e.ctx, s.e.st, l, idnStart(s, size, 1), keys)
	if err != nil {
		t.Fatal(err)
	}
	return l, counts
}

// idnObject is an object of the computation: what a record is bound to.
type idnBound struct {
	Dataset string
	Key     string
	Feed    int
	Obj     int64
	Ref     string
	IsNew   bool
}

func idnBounds(s *rdfSetup, t *testing.T) []idnBound {
	t.Helper()
	k := s.ref.KGNo
	rows, err := s.e.admin.QueryContext(s.e.ctx, fmt.Sprintf(`SELECT d.dataset_id, c.key, b.feed_no, b.obj_no, COALESCE(o.ref, n.ref), b.is_new
		FROM "s_%d".calc_bind b JOIN "s_%d".calc_record c ON c.rid = b.rid JOIN source_dataset d ON d.dataset_no = c.dataset_no
		LEFT JOIN "s_%d".object o ON o.obj_no = b.obj_no LEFT JOIN "s_%d".calc_newobj n ON n.obj_no = b.obj_no ORDER BY d.dataset_id, c.key, b.feed_no`, k, k, k, k))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []idnBound
	for rows.Next() {
		var b idnBound
		if err := rows.Scan(&b.Dataset, &b.Key, &b.Feed, &b.Obj, &b.Ref, &b.IsNew); err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// idnRefOf is the object a record of a dataset is bound to, by the key of the record.
func idnRefOf(bs []idnBound, dataset, key string) string {
	for _, b := range bs {
		if b.Dataset == dataset && b.Key == key {
			return b.Ref
		}
	}
	return ""
}

// idnApply does to the registry and the identification index what the apply will do with what the identification made, so that the
// next run finds the objects of this one: the new objects, the keys, and the references of the records (it is not the apply: it is what
// a test of the identification needs of it).
func idnApply(s *rdfSetup, t *testing.T) {
	t.Helper()
	k := s.ref.KGNo
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".object (obj_no, ref, type_no, name, min_grade, seq_from) SELECT obj_no, ref, type_no, ref, 0, 1 FROM "s_%d".calc_newobj`, k, k))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".object_key (type_no, ctx_no, sig_hash, obj_no, bound_seq) SELECT type_no, ctx_no, sig_hash, obj_no, 1 FROM "s_%d".calc_newkey`, k, k))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_ref (src_no, dataset_no, key, role, def_no, sub, lookup_hash, target_no, state, retirable)
		SELECT c.src_no, c.dataset_no, c.key, r.role, r.def_no, r.sub, r.lookup_hash, r.target_no, r.state, false FROM "s_%d".calc_ref r JOIN "s_%d".calc_record c ON c.rid = r.rid
		ON CONFLICT (src_no, dataset_no, key, role, def_no, sub) DO UPDATE SET target_no = excluded.target_no, state = excluded.state`, k, k, k))
}

func idnJoin(rows ...string) string { return strings.Join(rows, "\n") + "\n" }

// idnPipelineAfter is idnPipeline for a run that goes on from the state the last run left: the run before it is ended.
func idnPipelineAfter(s *rdfSetup, t *testing.T, texts pgiTexts, n int) (ingest.RunLease, ingest.IdentifyCounts) {
	t.Helper()
	return idnPipeline(s, t, texts, rdfT0.Add(time.Duration(n+1)*time.Hour), 0)
}

// idnUndo ends the run and empties what it received, and puts back the registry and the index of what the run before had, so that
// the same run can be done again in another order.
func idnUndo(s *rdfSetup, t *testing.T, l ingest.RunLease) {
	t.Helper()
	if err := s.e.st.EndRun(s.e.ctx, l, ingest.RunEnd{Stage: ingest.RunFailed, Class: ingest.EndProduct, Code: "test", Reason: "a test does it again"}); err != nil {
		t.Fatal(err)
	}
	trunc, err := pgschema.TruncateReceiveSQL(s.ref.KGNo, s.ref.SrcNo)
	if err != nil {
		t.Fatal(err)
	}
	s.e.exec(t, trunc)
}
