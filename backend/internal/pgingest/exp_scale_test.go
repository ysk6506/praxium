package pgingest

import (
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// What a statement of the expansion reads when records of the run share what they look for: m records that give the same end key, that
// are bound to the same object, or that claim the same identifier, against m records that read that key, make the links of that object,
// or claim it too. A statement that read for each record all that the others have in common would read m squared rows; the one that
// reads what is shared once for its unit reads a number of rows that goes with m. The world is made in SQL (no pipeline), the statements
// are asked how they went (EXPLAIN ANALYZE) as the measurement of the identification does it (idn_plan_test.go).

// expShared makes the records and references of m of each of four kinds of sharing in a run that has finished its identification.
func expShared(t *testing.T, m int) (*rdfSetup, ingest.RunLease) {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	l := s.rdfComputing(t, "server-a", map[string]ingest.Coverage{"ds_s": ingest.CoverageComplete})
	k, src, ds := s.ref.KGNo, s.ref.SrcNo, s.rdfNo("ds_s")
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_owner (run_no) VALUES ($1)`, k), l.No)
	s.e.exec(t, `UPDATE sync_run SET calc_job = $2, counts = counts || '{"identify":{"phase":"done"}}'::jsonb WHERE run_no = $1`, l.No, pgschema.CalcJobIdentify)
	rec := func(prefix string, why int16) {
		s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_record (src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, why)
			SELECT $1, $2, $3 || i, '{x}', '\x00', 0, 1, 0, %d FROM generate_series(1, %d) i`, k, why, m), src, ds, prefix)
	}
	raw := func(prefix string) {
		s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate)
			SELECT $1, $2, $3 || i, '\x00', '{x}', 0, 1, 0 FROM generate_series(1, %d) i`, pgiTable(s.ref, pgschema.TableRawRecord), m), src, ds, prefix)
	}
	ref := func(prefix string, role int16, hash, target string, retirable bool) {
		s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_ref (src_no, dataset_no, key, role, def_no, sub, lookup_hash, target_no, state, retirable)
			SELECT $1, $2, $3 || i, %d, 0, 0, %s, %s, 0, %t FROM generate_series(1, %d) i`, k, role, hash, target, retirable, m), src, ds, prefix)
	}
	// an end key that m records give, and m records read
	rec("T", pgschema.CalcWhyChanged)
	ref("T", pgschema.RefRoleLinkTo, `'\x01'::bytea`, "NULL::bigint", false)
	raw("F")
	ref("F", pgschema.RefRoleLinkFrom, `'\x01'::bytea`, "NULL::bigint", false)
	// an object that m records are bound to, that has a link of a type that holds its start to one, made by m records
	rec("R", pgschema.CalcWhyChanged)
	ref("R", pgschema.RefRoleIdentity, "NULL::bytea", "5::bigint", false)
	// an object that m records were bound to, that the feeds that made it may retire, and that none of them is bound to now
	rec("Q", pgschema.CalcWhyChanged)
	ref("Q", pgschema.RefRoleIdentity, "NULL::bytea", "7::bigint", true)
	expPutLink(s, t, 9100, expTypeFromOne, 5, 6)
	raw("P")
	ref("P", pgschema.RefRoleLinkFrom, "NULL::bytea", "9100::bigint", false)
	// an identifier that m records claim, each for an object of its own
	rec("C", pgschema.CalcWhyChanged)
	ref("C", pgschema.RefRoleClaim, `'\x02'::bytea`, "100 + i::bigint", false)
	for _, table := range []string{pgschema.TableCalcRecord, pgschema.TableRecordRef, pgschema.TableLink} {
		s.e.exec(t, fmt.Sprintf(`ANALYZE "s_%d"."%s"`, k, table))
	}
	s.e.exec(t, fmt.Sprintf(`ANALYZE %s`, pgiTable(s.ref, pgschema.TableRawRecord)))
	return s, l
}

// the statements that read what the records share, by what they say they do
var expSharedWhats = []string{
	"the objects the records were bound to", "the records that read an end key the records had", "the records of the links that compete for a start",
	"the objects that claim the same identifiers",
}

// expRowsRead is the rows each statement of the expansion read, the most that a unit of it did.
func expRowsRead(t *testing.T, m int) (map[string]float64, ingest.ExpandCounts) {
	t.Helper()
	s, l := expShared(t, m)
	p := &idnPlans{most: map[string]*idnRead{}}
	s.e.st.expTest = &expHook{Seen: p.idnHook}
	counts, err := ingest.RunExpand(s.e.ctx, s.e.st, l, ingest.ExpandStart{Rows: 10 * m, Cards: expCards()})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for what, read := range p.most {
		for _, n := range read.rows {
			out[what] += n
		}
	}
	return out, counts
}

// [REQ-899]
func TestExpWhatManyRecordsOfARunShareIsReadOnceForAUnitSoTheRowsReadGoWithTheNumberOfRecords(t *testing.T) {
	const m = 150
	small, counts := expRowsRead(t, m)
	big, bigCounts := expRowsRead(t, 3*m)
	if counts.KeyRecords != m || counts.LimitRecords != m || counts.ClaimObjects != m || counts.Retiring != 1 {
		t.Errorf("%+v", counts)
	}
	if bigCounts.KeyRecords != 3*m || bigCounts.LimitRecords != 3*m || bigCounts.ClaimObjects != 3*m || bigCounts.Retiring != 1 {
		t.Errorf("%+v", bigCounts)
	}
	for _, what := range expSharedWhats {
		a, b := small[what], big[what]
		t.Logf("%s: %d records %.0f rows, %d records %.0f rows", what, m, a, 3*m, b)
		if a < m || b < 3*m {
			t.Errorf("%s: read %.0f and %.0f rows, fewer than the records that share what is read", what, a, b)
		}
		if b > 3.4*a {
			t.Errorf("%s: %.0f rows for %d records and %.0f for %d: more than three times as many", what, a, m, b, 3*m)
		}
	}
}
