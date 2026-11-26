package pgingest

import (
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// What a unit of the difference reads when rows are many (ADR-0127): an object that retires with m links of the KG at it, and a type that
// has m rows of the difference. The worlds are made in SQL, and the statements are asked how they went (EXPLAIN ANALYZE) in the plan the
// server chooses (rclOperatingPlan), after the statistics are taken.

// dfiRowsPerRow is the most rows a statement of the difference may read for a row of the range of its unit (the row, what the KG has of it
// by an index, and, for a link of the KG, its ends and the references of its From end).
const dfiRowsPerRow = 16

// dfiAnalyze takes the statistics of the tables the difference reads.
func dfiAnalyze(f *dfiFix, t *testing.T) {
	t.Helper()
	for _, table := range []string{pgschema.TableObject, pgschema.TableLink, pgschema.TableRecordRef, pgschema.TableStageObject, pgschema.TableStageLink,
		pgschema.TableCalcObject, pgschema.TableCalcRecord, pgschema.TableCalcIsolated, pgschema.TableCalcDiffRow, pgschema.TableCalcDiffPlan, pgschema.TypeTable(f.m.server)} {
		f.s.e.exec(t, fmt.Sprintf(`ANALYZE "s_%d"."%s"`, f.s.ref.KGNo, table))
	}
}

// dfiHub is a world of one rack the run retires and m servers of the KG mounted in it, each link made by a record the run does not read,
// and as many more links between other objects that the run does not touch.
func dfiHub(t *testing.T, m int) *dfiFix {
	t.Helper()
	f := dfiNewFix(t, false)
	f.dfiObj(t, "r-hub", f.m.rack, "retiring")
	f.dfiObj(t, "r-other", f.m.rack, "kg")
	e, k := f.s.e, f.s.ref.KGNo
	e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".object (obj_no, ref, type_no, name, min_grade, seq_from) SELECT 1000 + i, 's-' || i, %[2]d, 's-' || i, 0, 1 FROM generate_series(1, $1) i`, k, f.m.server), 2*m)
	e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".t_%[2]d (obj_no, ref, name, min_grade, doc, content_hash, row_hash, seq_from)
		SELECT 1000 + i, 's-' || i, 's-' || i, 0, '{}', sha256(convert_to('s-' || i, 'UTF8')), sha256(convert_to('row s-' || i, 'UTF8')), 1 FROM generate_series(1, $1) i`, k, f.m.server), 2*m)
	e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".link (link_no, ref, type_no, src, dst, grade, basis, content_hash, row_hash, seq_from)
		SELECT 200000 + i, 'l-' || i, %[2]d, 1000 + i, CASE WHEN i <= $1 THEN $2::bigint ELSE $3::bigint END, 'fact', 'declared', sha256(convert_to('l-' || i, 'UTF8')), sha256(convert_to('row l-' || i, 'UTF8')), 1
		FROM generate_series(1, 2 * $1) i`, k, f.m.mounted), m, f.no["r-hub"], f.no["r-other"])
	e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".record_ref (src_no, dataset_no, key, role, def_no, sub, lookup_hash, target_no, state, retirable)
		SELECT $1, $2, 'rec ' || i, %[2]d, 0, 0, '\x01', 200000 + i, %[3]d, false FROM generate_series(1, 2 * $3) i`, k, pgschema.RefRoleLinkFrom, pgschema.RefResolved),
		f.s.ref.SrcNo, f.s.rdfNo("ds_sv"), m)
	dfiAnalyze(f, t)
	return f
}

// dfiMany is a world of m new servers the run staged, each with an identifier and a key.
func dfiMany(t *testing.T, m int) *dfiFix {
	t.Helper()
	f := dfiNewFix(t, false)
	e, k := f.s.e, f.s.ref.KGNo
	e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".stage_object (obj_no, ref, type_no, name, min_grade, doc, content_hash, row_hash, is_new)
		SELECT i, 's-' || lpad(((i * 7919) %% $1)::text, 6, '0'), %[2]d, 's-' || i, 0, '{}', sha256(convert_to('s-' || i, 'UTF8')), sha256(convert_to('row s-' || i, 'UTF8')), true
		FROM generate_series(1, $1) i`, k, f.m.server), m)
	e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".calc_object (obj_no, why) SELECT i, 1 FROM generate_series(1, $1) i`, k), m)
	e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".stage_ident (obj_no, scheme, value, scope) SELECT i, '%[2]s', 'sn-' || i, '' FROM generate_series(1, $1) i`, k, isoScheme), m)
	e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".calc_newkey (type_no, ctx_no, sig_hash, obj_no, round_no)
		SELECT %[2]d, 0, substring(sha256(convert_to('key ' || i, 'UTF8')) from 1 for 16), i, 0 FROM generate_series(1, $1) i`, k, f.m.server), m)
	dfiAnalyze(f, t)
	return f
}

// dfiRowsRead does the difference of a world with units of the size given and gives the most rows each statement read in a unit.
func dfiRowsRead(t *testing.T, f *dfiFix, rows, chunk int) (map[string]float64, ingest.DiffCounts) {
	t.Helper()
	p := &idnPlans{most: map[string]*idnRead{}}
	f.s.e.st.diffSeen = rclOperatingPlan(p)
	counts := dfiDo(f.s, t, f.l, rows, chunk)
	f.s.e.st.diffSeen = nil
	out := map[string]float64{}
	for what, read := range p.most {
		for _, n := range read.rows {
			out[what] += n
		}
	}
	return out, counts
}

// dfiLinear holds the most rows a unit of each statement read for m and for 3m to the rows of a unit (size and bigSize): no more than three
// times as many, and no more than dfiRowsPerRow for a row of the unit.
func dfiLinear(t *testing.T, small, big map[string]float64, size, bigSize int, must ...string) {
	t.Helper()
	for what, a := range small {
		b := big[what]
		t.Logf("%s: %.0f rows for %d, %.0f for %d", what, a, size, b, bigSize)
		if b > 3.4*max(a, float64(size)) {
			t.Errorf("%s: %.0f rows for %d and %.0f for %d: more than three times as many", what, a, size, b, bigSize)
		}
		if a > dfiRowsPerRow*float64(size) || b > dfiRowsPerRow*float64(bigSize) {
			t.Errorf("%s: %.0f rows for %d and %.0f for %d: more than %d rows for a row", what, a, size, b, bigSize, dfiRowsPerRow)
		}
	}
	for _, what := range must {
		if small[what] == 0 {
			t.Errorf("%s read no rows", what)
		}
	}
}

// [REQ-969]
func TestDfiAnObjectThatRetiresWithMLinksIsReadWithRowsThatGoWithM(t *testing.T) {
	const m = 600
	small, counts := dfiRowsRead(t, dfiHub(t, m), 0, 0)
	big, bigCounts := dfiRowsRead(t, dfiHub(t, 3*m), 0, 0)
	// the rack and its m links: the links of the other rack stay, made by records the run does not read
	if counts.Of(ingest.DiffLink, ingest.DiffRetired) != m || bigCounts.Of(ingest.DiffLink, ingest.DiffRetired) != 3*m || counts.Of(ingest.DiffObject, ingest.DiffRetired) != 1 {
		t.Fatalf("%+v %+v", counts, bigCounts)
	}
	// a unit of the retirement has the one object and its m links: the rows read go with the links, and the links of the other rack are not
	// read
	dfiLinear(t, small, big, m, 3*m, "close the links of the objects that retire")
}

// [REQ-969]
func TestDfiAUnitReadsTheRangeOfItsSourceOrOfItsTypeAndNotTheRowsOfTheType(t *testing.T) {
	const m, size, chunk = 600, 50, 400
	small, counts := dfiRowsRead(t, dfiMany(t, m), size, chunk)
	big, bigCounts := dfiRowsRead(t, dfiMany(t, 3*m), size, chunk)
	if counts.Rows != 3*m || bigCounts.Rows != 9*m || counts.Chunks != 6 || bigCounts.Chunks != 15 {
		t.Fatalf("%+v %+v", counts, bigCounts)
	}
	// a unit of the print is a chunk: it reads the rows of the chunk, as the apply does
	const digest = "make the digest of a chunk"
	for _, n := range []float64{small[digest], big[digest]} {
		if n == 0 || n > chunk {
			t.Errorf("a digest read %.0f rows of a chunk of %d", n, chunk)
		}
	}
	delete(small, digest)
	delete(big, digest)
	// every other unit, of every phase, reads the rows of its range (size): the same at m and at 3m
	dfiLinear(t, small, big, size, size, "number the rows of the difference", "find the end of the range of the order", "compare the staged objects",
		"compare the identifiers of the staged objects", "compare the keys the run binds", "find the end of the range of retire")
}
