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
func (s *rdfSetup) idnStart(size, pages int) ingest.IdentifyStart {
	return ingest.IdentifyStart{Feeds: s.plan.IdentifyFeeds(), Schemes: s.plan.SchemeNumbers(), BucketRows: size, Pages: pages}
}

// idnPipeline receives the datasets, compares them with the raw records and identifies the records of the run.
func (s *rdfSetup) idnPipeline(t *testing.T, texts pgiTexts, at time.Time, size int) (ingest.RunLease, ingest.IdentifyCounts) {
	t.Helper()
	l := s.e.open(t, s.kg, s.plan.Source.ID, "server-a")
	if _, err := s.e.st.Receive(s.e.ctx, l, ingest.Intake{Reader: texts, Plan: s.plan, Options: ingest.PageOptions{MaxRows: 2}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.RunRawDiff(s.e.ctx, s.e.st, l, s.rdfStart(at, size)); err != nil {
		t.Fatal(err)
	}
	keys := ingest.NewPlanKeys(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan})
	counts, err := ingest.RunIdentify(s.e.ctx, s.e.st, l, s.idnStart(size, 1), keys)
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

func (s *rdfSetup) idnBound(t *testing.T) []idnBound {
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
func (s *rdfSetup) idnApply(t *testing.T) {
	t.Helper()
	k := s.ref.KGNo
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".object (obj_no, ref, type_no, name, min_grade, seq_from) SELECT obj_no, ref, type_no, ref, 0, 1 FROM "s_%d".calc_newobj`, k, k))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".object_key (type_no, ctx_no, sig_hash, obj_no, bound_seq) SELECT type_no, ctx_no, sig_hash, obj_no, 1 FROM "s_%d".calc_newkey`, k, k))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_ref (src_no, dataset_no, key, role, def_no, sub, lookup_hash, target_no, state, retirable)
		SELECT c.src_no, c.dataset_no, c.key, r.role, r.def_no, r.sub, r.lookup_hash, r.target_no, r.state, false FROM "s_%d".calc_ref r JOIN "s_%d".calc_record c ON c.rid = r.rid
		ON CONFLICT (src_no, dataset_no, key, role, def_no, sub) DO UPDATE SET target_no = excluded.target_no, state = excluded.state`, k, k, k))
	s.e.exec(t, fmt.Sprintf(`DELETE FROM "s_%d".raw_record r USING "s_%d".calc_record c WHERE c.why = %d AND c.dataset_no = r.dataset_no AND c.key = r.key`, k, k, pgschema.CalcWhyClosed))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".raw_record (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate)
		SELECT src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate FROM "s_%d".calc_record WHERE why IN (%d, %d) AND fields IS NOT NULL
		ON CONFLICT (src_no, dataset_no, key) DO UPDATE SET raw_hash = excluded.raw_hash, fields = excluded.fields, decl_rev = excluded.decl_rev, fate = excluded.fate`,
		k, k, pgschema.CalcWhyNew, pgschema.CalcWhyChanged))
}

func idnJoin(rows ...string) string { return strings.Join(rows, "\n") + "\n" }
