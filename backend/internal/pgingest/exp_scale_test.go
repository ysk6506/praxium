package pgingest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// What a statement of the expansion reads when records of the run share what they look for: m records that give the same end key, that
// are bound to the same object, or that claim the same identifier, against m records that read that key, make the links of that object,
// or claim it too. A statement that read for each record all that the others have in common would read m squared rows, and one that
// reads what is shared once for each unit that has a record of it reads m squared over the size of a unit. What is measured is the rows
// the statements read in all the units, for units that are smaller than m and for one unit that holds everything: both go with m
// (ADR-0118). The world is made in SQL (no pipeline), the statements are asked how they went (EXPLAIN ANALYZE) as the measurement of the
// identification does it (idn_plan_test.go).

// expShared makes the records and references of m of each of seven kinds of sharing in a run that has finished its identification.
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
	// an end key that m records give now (their ends are in calc_end), and m records read it
	rec("K", pgschema.CalcWhyChanged)
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_end (rid, def_no, side, lookup_hash, held) SELECT rid, 0, %d, '\x03', false FROM "s_%d".calc_record WHERE key LIKE 'K%%'`, k, pgschema.RefRoleLinkTo, k))
	raw("G")
	ref("G", pgschema.RefRoleLinkFrom, `'\x03'::bytea`, "NULL::bigint", false)
	// an object that m records are bound to, at which a link of a type that holds its end to one ends, made by m records
	rec("S", pgschema.CalcWhyChanged)
	ref("S", pgschema.RefRoleIdentity, "NULL::bytea", "11::bigint", false)
	expPutLink(s, t, 9300, expTypeToOne, 12, 11)
	raw("U")
	ref("U", pgschema.RefRoleLinkFrom, "NULL::bytea", "9300::bigint", false)
	// m links that start at the object to retire (each made by a record of its own), and m that end at it
	for i, kind := range []struct {
		prefix string
		from   string
		to     string
	}{{"V", "7", "1000 + i"}, {"W", "2000 + i", "7"}} {
		s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".link (link_no, ref, type_no, src, dst, grade, basis, content_hash, row_hash, seq_from)
			SELECT %d + i, 'link-%s' || i, %d, %s, %s, 'declared', 'declared', '\x00', '\x00', 1 FROM generate_series(1, %d) i`, k, 100_000*(i+1), kind.prefix, expTypeFree, kind.from, kind.to, m))
		raw(kind.prefix)
		ref(kind.prefix, pgschema.RefRoleLinkFrom, "NULL::bytea", fmt.Sprintf("%d + i::bigint", 100_000*(i+1)), false)
	}
	for _, table := range []string{pgschema.TableCalcRecord, pgschema.TableRecordRef, pgschema.TableLink, pgschema.TableCalcEnd} {
		s.e.exec(t, fmt.Sprintf(`ANALYZE "s_%d"."%s"`, k, table))
	}
	s.e.exec(t, fmt.Sprintf(`ANALYZE %s`, pgiTable(s.ref, pgschema.TableRawRecord)))
	return s, l
}

// the statements that read what the records share, by what they say they do: all eight of the rules that read
var expSharedWhats = []string{
	"the objects the records were bound to", "the records that made the links that start at an object to retire", "the records that made the links that end at an object to retire",
	"the records that read an end key the records had", "the records that read an end key the records give", "the records of the links that compete for a start",
	"the records of the links that compete for an end", "the objects that claim the same identifiers",
}

// expReadsInto adds up, for each statement of the expansion (by what it says it does), the rows it read in every unit: the rows each
// relation gave, with the loops counted. The statement is run in a savepoint that is rolled back, and then runs for good.
func expReadsInto(total map[string]float64) func(tx *sql.Tx, what, query string, args []any) {
	return func(tx *sql.Tx, what, query string, args []any) {
		if _, err := tx.Exec("SAVEPOINT exp_plan"); err != nil {
			panic(err)
		}
		defer func() {
			if _, err := tx.Exec("ROLLBACK TO SAVEPOINT exp_plan"); err != nil {
				panic(err)
			}
		}()
		// the tables of the test are a few pages, which a scan reads whole each time; the tables of a run of millions of records are read by
		// their indexes, so the plan is asked for the indexes where there are some (a table without the index is still scanned)
		if _, err := tx.Exec("SET LOCAL enable_seqscan = off"); err != nil {
			panic(err)
		}
		var doc []byte
		if err := tx.QueryRow("EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, FORMAT JSON) "+query, args...).Scan(&doc); err != nil {
			panic(fmt.Sprintf("%s: %v", what, err))
		}
		var top []struct{ Plan map[string]any }
		if err := json.Unmarshal(doc, &top); err != nil {
			panic(err)
		}
		var walk func(map[string]any)
		walk = func(node map[string]any) {
			if _, ok := node["Relation Name"].(string); ok {
				actual, _ := node["Actual Rows"].(float64)
				loops, _ := node["Actual Loops"].(float64)
				removed, _ := node["Rows Removed by Filter"].(float64)
				total[what] += (actual + removed) * loops
			}
			if kids, ok := node["Plans"].([]any); ok {
				for _, k := range kids {
					if m, ok := k.(map[string]any); ok {
						walk(m)
					}
				}
			}
		}
		for _, p := range top {
			walk(p.Plan)
		}
	}
}

// expRowsRead is the rows each statement of the expansion read in all its units, for a unit of size records.
func expRowsRead(t *testing.T, m, size int) (map[string]float64, ingest.ExpandCounts) {
	t.Helper()
	s, l := expShared(t, m)
	total := map[string]float64{}
	s.e.st.expTest = &expHook{Seen: expReadsInto(total)}
	counts, err := ingest.RunExpand(s.e.ctx, s.e.st, l, ingest.ExpandStart{Rows: size, Cards: expCards(), Feeds: expFeeds(s)})
	if err != nil {
		t.Fatal(err)
	}
	return total, counts
}

// [REQ-899]
func TestExpWhatManyRecordsOfARunShareIsReadOnceForAUnitSoTheRowsReadGoWithTheNumberOfRecords(t *testing.T) {
	const m = 100
	// a unit of fifty records is smaller than the records that share what is read, and one unit holds everything
	for _, size := range []int{50, 100 * m} {
		small, counts := expRowsRead(t, m, size)
		big, bigCounts := expRowsRead(t, 3*m, size)
		if counts.KeyRecords != 2*m || counts.LimitRecords != 2*m || counts.RetireRecords != 2*m || counts.ClaimObjects != m || counts.Retiring != 1 || counts.OldObjects != 3 {
			t.Errorf("a unit of %d, %d records: %+v", size, m, counts)
		}
		if bigCounts.KeyRecords != 6*m || bigCounts.LimitRecords != 6*m || bigCounts.RetireRecords != 6*m || bigCounts.ClaimObjects != 3*m || bigCounts.Retiring != 1 || bigCounts.OldObjects != 3 {
			t.Errorf("a unit of %d, %d records: %+v", size, 3*m, bigCounts)
		}
		for _, what := range expSharedWhats {
			a, b := small[what], big[what]
			t.Logf("a unit of %d, %s: %d records %.0f rows, %d records %.0f rows", size, what, m, a, 3*m, b)
			if a < m || b < 3*m {
				t.Errorf("a unit of %d, %s: read %.0f and %.0f rows, fewer than the records that share what is read", size, what, a, b)
			}
			if b > 3.4*a {
				t.Errorf("a unit of %d, %s: %.0f rows for %d records and %.0f for %d: more than three times as many", size, what, a, m, b, 3*m)
			}
		}
	}
}

// the numbers of the records follow the records that were made and not what the rules looked at: a rule that was turned away from a
// record that was there has not used a number of it
// [REQ-899] [REQ-957]
func TestExpTheNumbersOfTheRecordsOfARunAreAsManyAsTheRecordsWhateverTheRulesLookedAt(t *testing.T) {
	var last [2]int
	for i, m := range []int{40, 120} {
		s, l := expShared(t, m)
		if _, err := ingest.RunExpand(s.e.ctx, s.e.st, l, ingest.ExpandStart{Rows: 20, Cards: expCards(), Feeds: expFeeds(s)}); err != nil {
			t.Fatal(err)
		}
		n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_record`, s.ref.KGNo))
		hi := s.e.count(t, fmt.Sprintf(`SELECT max(rid) FROM "s_%d".calc_record`, s.ref.KGNo))
		if hi != n {
			t.Errorf("%d records, and the last number is %d", n, hi)
		}
		last[i] = hi
		if place, err := s.e.st.ExpandPlace(s.e.ctx, l); err != nil || !place.Done() {
			t.Errorf("%+v %v", place, err)
		} else if place.Unit > 8*(n/20+2) {
			t.Errorf("%d units for %d records and a unit of twenty", place.Unit, n)
		}
	}
	if last[1] > 4*last[0] {
		t.Errorf("the last number of a record is %d for 40 and %d for 120: more than three times as many", last[0], last[1])
	}
}

// the keys the expansion has read are kept by the run and not by the statement: a rule that was left out leaves its keys unread (its own
// kind) and a key that two units have is read by the first of them
// [REQ-899] [REQ-957]
func TestExpAKeyThatTwoUnitsHaveIsReadByTheFirstOfThemAndByNoOtherAndTheKeysAreOfTheRun(t *testing.T) {
	const m = 40
	keysOf := func(size int) ([]string, ingest.ExpandCounts) {
		s, l := expShared(t, m)
		counts, err := ingest.RunExpand(s.e.ctx, s.e.st, l, ingest.ExpandStart{Rows: size, Cards: expCards(), Feeds: expFeeds(s)})
		if err != nil {
			t.Fatal(err)
		}
		return expList(s.e, t, fmt.Sprintf(`SELECT %s::text || ':' || count(*)::text FROM "s_%d".%s GROUP BY %s ORDER BY %s`, pgschema.ColKind, s.ref.KGNo, pgschema.TableCalcXKey, pgschema.ColKind, pgschema.ColKind)), counts
	}
	// one key of each kind of sharing that the records have: the end key m records had, the one m records give, the three objects the records
	// are bound to, the m + 4 objects that links end at (the ends of the m links, and the ends of the objects the records are bound to),
	// and the identifier m records claim
	want := []string{"1:1", "2:1", "3:3", fmt.Sprintf("5:%d", m+4), "6:1"}
	small, counts := keysOf(7)
	if !slices.Equal(small, want) || counts.KeyRecords != 2*m {
		t.Errorf("the keys that were read by units of seven records, by kind: %v, wanted %v (%+v)", small, want, counts)
	}
	// the same keys when one unit holds every record: a key is read once, whatever the units are
	if big, _ := keysOf(100 * m); !slices.Equal(big, small) {
		t.Errorf("the keys that one unit read, by kind: %v, and units of seven: %v", big, small)
	}
	if !strings.Contains(strings.Join(pgschema.ComputeTables(), ","), pgschema.TableCalcXKey) {
		t.Errorf("the keys are not emptied with the tables of the computation")
	}
}
