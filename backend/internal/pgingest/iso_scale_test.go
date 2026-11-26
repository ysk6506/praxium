package pgingest

import (
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// What the fifth step reads when many rows share one key (ADR-0123): an identifier that m objects of the run say, and one start of a
// binding held to one link that m links of the run have. A statement that read, for each row, the other rows of its key would read m
// squared rows; the step reads the rows of a key once (the identifiers of a page together, the places by the key of the table of the
// places), so the rows read go with m. The worlds are made in SQL, and the statements are asked how they went in the plan the server
// chooses (rclOperatingPlan).

// isoRowsPerLine is the most rows a statement of the step may read in a unit for a staged row of the worlds below (the row, its object
// or its record, its reference, and the few rows a key has).
const isoRowsPerLine = 16

// isoIdentScale is m objects of the run that say one identifier the KG has with another object, each with ten identifiers of its own.
func isoIdentScale(t *testing.T, m int) *isoFix {
	t.Helper()
	f := isoNewFix(t)
	k := f.s.ref.KGNo
	f.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".object (obj_no, ref, type_no, name, min_grade, seq_from) SELECT i, 'o-' || lpad(i::text, 6, '0'), $1, 'o', 0, 1 FROM generate_series(1, %[2]d + 1) i`, k, m), f.m.server)
	f.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".stage_object (obj_no, ref, type_no, name, min_grade, doc, content_hash, row_hash, is_new)
		SELECT i, 'o-' || lpad(i::text, 6, '0'), $1, 'o', 0, '{}', '\x00', '\x00', false FROM generate_series(1, %[2]d) i`, k, m), f.m.server)
	f.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".calc_object (obj_no, why) SELECT i, 1 FROM generate_series(1, %[2]d) i`, k, m))
	f.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".stage_ident (obj_no, scheme, value, scope) SELECT i, $1, 'shared', '' FROM generate_series(1, %[2]d) i
		UNION ALL SELECT i, $1, 'p-' || i || '-' || j, '' FROM generate_series(1, %[2]d) i CROSS JOIN generate_series(1, 10) j`, k, m), isoScheme)
	f.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".object_identifier (obj_no, scheme, value, scope, seq_from) VALUES (%[2]d + 1, $1, 'shared', '', 1)`, k, m), isoScheme)
	f.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".calc_record (src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, why)
		SELECT $1, $2, 'c' || i, ARRAY['c'], '\x00', d.decl_rev, 1, 0, 0 FROM generate_series(1, %[2]d) i CROSS JOIN source_dataset d WHERE d.dataset_no = $2`, k, m), f.s.ref.SrcNo, f.s.rdfNo("ds_sv"))
	f.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".calc_ref (rid, role, def_no, sub, lookup_hash, target_no, state, retirable)
		SELECT c.rid, %[2]d, %[3]d, 1, $1, substr(c.key, 2)::bigint, 0, false FROM "s_%[1]d".calc_record c`, k, pgschema.RefRoleClaim, isoSchemeNo), isoHash(t, "shared"))
	isoAnalyze(f, t)
	return f
}

// isoCardScale is m links of the run from one server to m racks, a binding that holds the start to one link.
func isoCardScale(t *testing.T, m int) *isoFix {
	t.Helper()
	f := isoNewFix(t)
	k := f.s.ref.KGNo
	f.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".object (obj_no, ref, type_no, name, min_grade, seq_from) SELECT i, 'o-' || lpad(i::text, 6, '0'), CASE WHEN i = 1 THEN $1::smallint ELSE $2::smallint END, 'o', 0, 1
		FROM generate_series(1, %[2]d + 1) i`, k, m), f.m.server, f.m.rack)
	f.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".stage_object (obj_no, ref, type_no, name, min_grade, doc, content_hash, row_hash, is_new) VALUES (1, 'o-000001', $1, 'o', 0, '{}', '\x00', '\x00', false)`, k), f.m.server)
	f.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".calc_record (src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, why)
		SELECT $1, $2, 'k' || i, ARRAY['k'], '\x00', d.decl_rev, 1, 0, 0 FROM generate_series(1, %[2]d) i CROSS JOIN source_dataset d WHERE d.dataset_no = $2`, k, m), f.s.ref.SrcNo, f.s.rdfNo("ds_sv"))
	f.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".stage_link (link_no, ref, def_no, type_no, src, dst, grade, basis, content_hash, row_hash)
		SELECT 100000 + i, 'l-' || lpad(i::text, 6, '0'), 0, $1, 1, i + 1, 'fact', 'declared', '\x00', '\x00' FROM generate_series(1, %[2]d) i`, k, m), f.m.mounted)
	f.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".calc_linkwin (ref, rid, def_no, src_no, dataset_no, key, held, sig, shown, link_name, def_id, dataset_id, link_no)
		SELECT 'l-' || lpad(substr(c.key, 2), 6, '0'), c.rid, 0, c.src_no, c.dataset_no, c.key, false, '', '{}', 'MOUNTED_IN', 'l_mounted', 'ds_sv', 100000 + substr(c.key, 2)::bigint
		FROM "s_%[1]d".calc_record c`, k))
	f.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".calc_ref (rid, role, def_no, sub, lookup_hash, target_no, state, retirable)
		SELECT c.rid, %[2]d, 0, 0, '\x01', 100000 + substr(c.key, 2)::bigint, 0, false FROM "s_%[1]d".calc_record c`, k, pgschema.RefRoleLinkFrom))
	isoAnalyze(f, t)
	return f
}

func isoAnalyze(f *isoFix, t *testing.T) {
	t.Helper()
	for _, table := range []string{pgschema.TableObject, pgschema.TableStageObject, pgschema.TableStageIdent, pgschema.TableStageLink, pgschema.TableCalcRecord,
		pgschema.TableCalcRef, pgschema.TableCalcLinkWin, pgschema.TableObjectIdentifier, pgschema.TableCalcObject} {
		f.s.e.exec(t, fmt.Sprintf(`ANALYZE "s_%d"."%s"`, f.s.ref.KGNo, table))
	}
}

// isoRowsRead does the step of a world with pages of the size given and gives the most rows each statement read in a unit.
func isoRowsRead(t *testing.T, f *isoFix, lines int) map[string]float64 {
	t.Helper()
	p := &idnPlans{most: map[string]*idnRead{}}
	f.s.e.st.isoSeen = rclOperatingPlan(p)
	f.isoRun(t, lines)
	f.s.e.st.isoSeen = nil
	out := map[string]float64{}
	for what, read := range p.most {
		for _, n := range read.rows {
			out[what] += n
		}
		t.Logf("pages of %d, %s: %v", lines, what, read.rows)
	}
	return out
}

// isoLinear holds the most rows a unit of each statement read for m and for 3m rows of the world to the staged rows a unit has (lines and
// bigLines): no more than three times as many, and no more than isoRowsPerLine for a row of the unit.
func isoLinear(t *testing.T, small, big map[string]float64, lines, bigLines int, must ...string) {
	t.Helper()
	for what, a := range small {
		b := big[what]
		t.Logf("%s: %.0f rows for %d, %.0f for %d", what, a, lines, b, bigLines)
		// (a statement that reads a few rows at one size, the rows of one identifier, is held to the rows of the unit)
		if b > 3.4*max(a, float64(lines)) {
			t.Errorf("%s: %.0f rows for %d and %.0f for %d: more than three times as many", what, a, lines, b, bigLines)
		}
		if a > isoRowsPerLine*float64(lines) || b > isoRowsPerLine*float64(bigLines) {
			t.Errorf("%s: %.0f rows for %d and %.0f for %d: more than %d rows for a row", what, a, lines, b, bigLines, isoRowsPerLine)
		}
	}
	for _, what := range must {
		if small[what] == 0 {
			t.Errorf("%s read no rows", what)
		}
	}
}

// [REQ-853]
func TestIsoAnIdentifierManyObjectsSayIsReadOnceSoTheRowsReadGoWithTheObjects(t *testing.T) {
	// m is big enough that the planner reads the tables of the objects and the references by their index at both sizes (with a table of a
	// few pages it reads the table from the start for each row, which is the square of the rows; the bound of each size below fails then)
	const m = 600
	a, b := isoIdentScale(t, m), isoIdentScale(t, 3*m)
	small, big := isoRowsRead(t, a, 1000), isoRowsRead(t, b, 1000)
	for _, f := range []*isoFix{a, b} {
		if n := f.s.e.count(t, f.isoQ(`SELECT count(*) FROM {k}.stage_ident WHERE value = 'shared'`)); n != 0 {
			t.Errorf("%d lines of the identifier the KG has with another object are staged", n)
		}
	}
	// a unit reads a page of 1000 lines and the whole of the identifier that is on it (m lines)
	isoLinear(t, small, big, 1000+m, 1000+3*m, "read the identifiers that more than one object has", "make the claims of the run that lost lost")
}

// [REQ-853]
func TestIsoAStartManyLinksHaveIsReadOnceForAPageSoTheRowsReadGoWithTheLinks(t *testing.T) {
	const m = 1000 // big enough for the indexes, as above
	a, b := isoCardScale(t, m), isoCardScale(t, 3*m)
	small, big := isoRowsRead(t, a, 50), isoRowsRead(t, b, 50)
	for _, f := range []*isoFix{a, b} {
		if n := f.s.e.count(t, f.isoQ(`SELECT count(*) FROM {k}.stage_link`)); n != 1 {
			t.Errorf("%d links of the server are staged", n)
		}
	}
	// a unit reads a page of 50 links, at m and at 3m alike
	isoLinear(t, small, big, 50, 50, "read the links of a page", "read the places earlier pages took", "take the links out")
}
