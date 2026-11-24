package pgingest

import (
	"context"
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// rdfFault is a step of the comparison that the database refuses: what is broken, whether the comparison has begun when it is, and the
// call that must then fail (with an error, and without moving the run).
type rdfFault struct {
	name   string
	broken string
	begun  bool
	call   string
}

func rdfFaults(kg int64) []rdfFault {
	k := fmt.Sprintf(`"s_%d"`, kg)
	return []rdfFault{
		{"the datasets of the source cannot be read", `ALTER TABLE source_dataset RENAME COLUMN dataset_id TO dataset_id_x`, false, "begin"},
		{"the tables of the computation cannot be emptied", `DROP TABLE ` + k + `.calc_wait`, false, "begin"},
		{"the place of the comparison cannot be written", `CREATE FUNCTION rdf_refuse() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'refused'; END $$;
			CREATE TRIGGER rdf_refuse BEFORE UPDATE ON sync_run FOR EACH ROW EXECUTE FUNCTION rdf_refuse()`, false, "begin"},
		{"the counts of the receiving are not what they were written as", `UPDATE sync_run SET counts = '{"receive":"x"}'`, false, "begin"},
		{"the KG of the run has no number", `ALTER TABLE kg_catalog RENAME COLUMN kg_no TO kg_no_x`, false, "begin"},
		{"the KG of a bucket has no number", `ALTER TABLE kg_catalog RENAME COLUMN kg_no TO kg_no_x`, true, "bucket"},
		{"the counts of the comparison are not what they were written as", `UPDATE sync_run SET counts = counts || '{"raw_diff":"x"}'`, true, "bucket"},
		{"the counts of the comparison are not what they were written as, for the place", `UPDATE sync_run SET counts = counts || '{"raw_diff":"x"}'`, true, "place"},
		{"the counts of the comparison are not what they were written as, for the report", `UPDATE sync_run SET counts = counts || '{"raw_diff":"x"}'`, true, "report"},
		{"the time of the comparison is not a time", `UPDATE sync_run SET counts = jsonb_set(counts, '{raw_diff,at}', '"x"')`, true, "bucket"},
		{"the end of a bucket cannot be found", `ALTER TABLE ` + k + `.record_wait RENAME COLUMN key TO key_x`, true, "bucket"},
		{"the statement of a bucket cannot be run", `DROP TABLE ` + k + `.calc_problem`, true, "bucket"},
		{"the place after a bucket cannot be written", `CREATE FUNCTION rdf_refuse() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'refused'; END $$;
			CREATE TRIGGER rdf_refuse BEFORE UPDATE ON sync_run FOR EACH ROW EXECUTE FUNCTION rdf_refuse()`, true, "bucket"},
	}
}

// [REQ-782]
func TestRdfAStepThatTheDatabaseRefusesIsToldAndTheBucketLeavesNothing(t *testing.T) {
	for i := 0; i < len(rdfFaults(0)); i++ {
		s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
		rdfSeedFates(t, s)
		f := rdfFaults(s.ref.KGNo)[i]
		l := s.rdfComputing(t, "server-a", rdfCover())
		if f.begun {
			if _, err := s.e.st.BeginRawDiff(s.e.ctx, l, s.rdfStart(rdfNow, 0)); err != nil {
				t.Fatal(err)
			}
		}
		s.e.exec(t, f.broken)
		var err error
		switch f.call {
		case "begin":
			_, err = s.e.st.BeginRawDiff(s.e.ctx, l, s.rdfStart(rdfNow, 0))
		case "bucket":
			_, err = s.e.st.DiffRawBucket(s.e.ctx, l, 0)
		case "place":
			_, err = s.e.st.RawDiffPlace(s.e.ctx, l.No)
		default:
			_, err = s.e.st.RawDiffReport(s.e.ctx, l.No)
		}
		if err == nil {
			t.Errorf("%s: no error", f.name)
		}
		if f.call == "bucket" {
			if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_record`, s.ref.KGNo)); n != 0 {
				t.Errorf("%s: a bucket that failed left %d records", f.name, n)
			}
		}
	}
}

// [REQ-780]
func TestRdfACallWithACanceledContextIsRefusedByEveryStepOfTheComparison(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	l := s.rdfComputing(t, "server-a", rdfCover())
	ctx, cancel := context.WithCancel(s.e.ctx)
	cancel()
	if _, err := s.e.st.BeginRawDiff(ctx, l, s.rdfStart(rdfNow, 0)); err == nil {
		t.Error("begin")
	}
	if _, err := s.e.st.DiffRawBucket(ctx, l, 0); err == nil {
		t.Error("bucket")
	}
	if _, err := s.e.st.RawDiffPlace(ctx, l.No); err == nil {
		t.Error("place")
	}
	if _, err := s.e.st.RawDiffReport(ctx, l.No); err == nil {
		t.Error("report")
	}
	if _, err := s.e.st.RawDiffPlace(s.e.ctx, l.No+1000); err == nil {
		t.Error("a run that is not there")
	}
	if _, err := s.e.st.RawDiffReport(s.e.ctx, l.No+1000); err == nil {
		t.Error("the report of a run that is not there")
	}
	var _ ingest.RawDiffer = s.e.st
}
