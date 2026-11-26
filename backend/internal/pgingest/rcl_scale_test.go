package pgingest

import (
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// What the computation of the links reads when many records share one key (ADR-0122): m servers (the From side) that say one rack tag, m
// records of racks that stand under one object with that tag (half of them records the run reads, half the references that are kept), and
// many more references of other keys. A statement that read what the To side has for each From end would read m squared rows; the ends
// are read once by the index of the keys and what the To side has for a key is counted once, so the rows read go with m. The world is made
// in SQL, and the statements are asked how they went (EXPLAIN ANALYZE) with the plan as it is.

// rclRowsPerEnd is the most rows a statement of the computation of the links may read for an end of the From side of the world of
// rclShared (the end, its record, its binding, the To records of its key and the ends of them, counted once for a key).
const rclRowsPerEnd = 16

// rclShared makes the world of m From records and m To records of one key, in a run whose objects are computed.
func rclShared(t *testing.T, m int) (*rdfSetup, ingest.RunLease) {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	l := s.rdfComputing(t, "server-a", map[string]ingest.Coverage{"ds_sv": ingest.CoverageComplete, "ds_rk": ingest.CoverageComplete})
	k, src := s.ref.KGNo, s.ref.SrcNo
	def := ingest.RecomputeLinkDefsOf(rclPlans(s))[0]
	svNo, rkNo := s.rdfNo("ds_sv"), s.rdfNo("ds_rk")
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_owner (run_no) VALUES ($1)`, k), l.No)
	s.e.exec(t, `UPDATE sync_run SET calc_job = $2, counts = counts || '{"recompute":{"finished":true}}'::jsonb WHERE run_no = $1`, l.No, pgschema.CalcJobRecompute)
	// the servers are the objects 1 to m and the rack is m + 1
	ids := kernel.NewSequentialIDs()
	var refs []string
	for i := 0; i <= m; i++ {
		id, _ := kernel.NewID[graph.ObjectKind](ids, "obj")
		refs = append(refs, id.String())
	}
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".object (obj_no, ref, type_no, name, min_grade, seq_from) SELECT o.n, o.ref, 1, o.ref, 0, 1 FROM unnest($1::text[]) WITH ORDINALITY AS o(ref, n)`, k), textArray(refs))
	const hash = `decode('0123456789abcdef0123456789abcdef', 'hex')`
	zero := `decode('00000000000000000000000000000000', 'hex')`
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_record (src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, why)
		SELECT $1::int, $2::int, 'sv' || i, ARRAY['sv' || i, 'Zorvane', 'sn' || i, 'T', '1', 'on'], %s, d.decl_rev, 1, 0::smallint, 1::smallint
		FROM generate_series(1, %d) i CROSS JOIN source_dataset d WHERE d.dataset_no = $2
		UNION ALL SELECT $1::int, $3::int, 'rk' || i, ARRAY['rk' || i, 'T'], %s, d.decl_rev, 1, 0::smallint, 1::smallint
		FROM generate_series(1, %d) i CROSS JOIN source_dataset d WHERE d.dataset_no = $3`, k, zero, m, zero, m/2), src, svNo, rkNo)
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".calc_end (rid, def_no, side, lookup_hash, held)
		SELECT c.rid, %[2]d, CASE WHEN c.dataset_no = %[3]d THEN %[4]d ELSE %[5]d END::smallint, %[6]s, false FROM "s_%[1]d".calc_record c`, k, def.Def, svNo, pgschema.RefRoleLinkFrom, pgschema.RefRoleLinkTo, hash))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".calc_bind (rid, feed_no, key_ord, obj_no, is_new, round_no)
		SELECT c.rid, CASE WHEN c.dataset_no = %[2]d THEN %[3]d ELSE %[4]d END, 0, CASE WHEN c.dataset_no = %[2]d THEN substr(c.key, 3)::bigint ELSE %[5]d END, false, 0 FROM "s_%[1]d".calc_record c`,
		k, svNo, def.FromFeed, def.ToFeed, m+1))
	// the records of the racks that the run does not read, and the references of other keys, many more than these
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_ref (src_no, dataset_no, key, role, def_no, sub, lookup_hash, target_no, state, retirable)
		SELECT $1::int, $2::int, 'old' || i, %d, %d, 0, %s, %d, %d, false FROM generate_series(1, %d) i
		UNION ALL SELECT $1::int, $2::int, 'z' || i, %d, %d, 0, decode(lpad(to_hex(i), 32, '0'), 'hex'), %d, %d, false FROM generate_series(1, %d) i`,
		k, pgschema.RefRoleLinkTo, def.Def, hash, m+1, pgschema.RefResolved, m-m/2, pgschema.RefRoleLinkTo, def.Def, m+1, pgschema.RefResolved, 30*m), src, rkNo)
	for _, table := range []string{pgschema.TableRecordRef, pgschema.TableObject, pgschema.TableCalcRecord, pgschema.TableCalcEnd, pgschema.TableCalcBind} {
		s.e.exec(t, fmt.Sprintf(`ANALYZE "s_%d"."%s"`, k, table))
	}
	return s, l
}

// rclRowsRead is the rows each statement of the computation of the links read for m, and the report.
func rclRowsRead(t *testing.T, m int) (map[string]float64, ingest.RecomputeLinksCounts) {
	t.Helper()
	s, l := rclShared(t, m)
	p := &idnPlans{most: map[string]*idnRead{}}
	s.e.st.rclSeen = p.idnHook
	counts := rclDo(s, t, l, rclPlans(s), 100*m)
	out := map[string]float64{}
	for what, read := range p.most {
		for _, n := range read.rows {
			out[what] += n
		}
		t.Logf("m %d, %s: %v by %v", m, what, read.rows, read.scans)
	}
	return out, counts
}

// [REQ-865]
func TestRclWhatManyEndsOfOneKeyShareIsReadOnceSoTheRowsReadGoWithTheNumberOfEnds(t *testing.T) {
	// m is big enough that the planner reads the bindings of a record by their index at both sizes (with a table of a few pages it reads the
	// table from the start for each row, which is the square of the ends; the bound of each size below fails then)
	const m = 300
	small, counts := rclRowsRead(t, m)
	big, bigCounts := rclRowsRead(t, 3*m)
	// every server says the rack of the tag, one link each (m servers, m links, one more for each record that wins none: the servers are
	// different objects, so no link is the same)
	if counts.Links != m || bigCounts.Links != 3*m || counts.Keys != 1 || bigCounts.Keys != 1 || counts.Problems != 0 || counts.Ends != m || bigCounts.Ends != 3*m {
		t.Fatalf("%+v %+v", counts, bigCounts)
	}
	checked := 0
	for what, a := range small {
		b := big[what]
		t.Logf("%s: m %.0f rows, 3m %.0f rows", what, a, b)
		if b > 3.4*a {
			t.Errorf("%s: %.0f rows for %d ends and %.0f for %d: more than three times as many", what, a, m, b, 3*m)
		}
		// and a bound of its own for each size, so that a plan that reads the square at the small size and not at the big one (the
		// planner reads a small table from the start for each row) is not taken for one that goes with the ends: a statement reads a
		// few rows for each end, and the references of the other keys (thirty times the ends) are not read
		if a > rclRowsPerEnd*float64(m) || b > rclRowsPerEnd*float64(3*m) {
			t.Errorf("%s: %.0f rows for %d ends and %.0f for %d: more than %d rows for an end", what, a, m, b, 3*m, rclRowsPerEnd)
		}
		checked++
	}
	if checked < 5 {
		t.Errorf("only %d statements were looked at", checked)
	}
	for _, what := range []string{"read the ends of the From side", "count the objects of the To side of the keys"} {
		if small[what] == 0 {
			t.Errorf("%s read no rows", what)
		}
	}
}
