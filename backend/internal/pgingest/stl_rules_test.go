package pgingest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The rules of the cleaning of the references and the problems (④) and of the fate (②), on records written by hand (ADR-0128, the table
// of decision 4): which lines of which record the run's lines take the place of, and which stay.

// stlFix is a run that is settling with the tables of its computation written by the test.
type stlFix struct {
	s *rdfSetup
	l ingest.RunLease
}

// stlNewFix is a run of the first source whose computing is done, moved to settling, with nothing computed yet.
func stlNewFix(t *testing.T) *stlFix {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	l := s.rdfComputing(t, "server-a", map[string]ingest.Coverage{"ds_sv": ingest.CoverageComplete})
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_owner (run_no) VALUES ($1)`, s.ref.KGNo), l.No)
	s.e.exec(t, `UPDATE sync_run SET calc_job = $2, counts = counts || $3::jsonb WHERE run_no = $1`, l.No, pgschema.CalcJobIsolate,
		fmt.Sprintf(`{"isolate":{"finished":true},"raw_diff":{"at":"2031-01-01T00:00:00Z","datasets":[{"dataset":"ds_sv","dataset_no":%d,"coverage":"complete"}]}}`, s.rdfNo("ds_sv")))
	stlSettling(s, t, l)
	return &stlFix{s: s, l: l}
}

// stlQ writes the schema of the KG ({k}) into a statement.
func (f *stlFix) stlQ(query string) string {
	return strings.ReplaceAll(query, "{k}", fmt.Sprintf(`"s_%d"`, f.s.ref.KGNo))
}

// stlRecord writes a record of the run (and, unless it is new, its raw record) and gives its number. page is the place of the row the
// comparison used, for a record received now (-1 for none).
func (f *stlFix) stlRecord(t *testing.T, key string, why, fate int16, page int) int64 {
	t.Helper()
	ds := f.s.rdfNo("ds_sv")
	var p any
	if page >= 0 {
		p = page
	}
	if why != pgschema.CalcWhyNew {
		f.s.e.exec(t, f.stlQ(`INSERT INTO {k}.raw_record (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate) VALUES ($1, $2, $3, '\x00', ARRAY['old'], 0, 1, 0)`), f.s.ref.SrcNo, ds, key)
	}
	f.s.e.exec(t, f.stlQ(`INSERT INTO {k}.calc_record (src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, why, page, row_no) VALUES ($1, $2, $3, ARRAY['new'], '\x01', 0, 7, $4, $5, $6, $6)`),
		f.s.ref.SrcNo, ds, key, fate, why, p)
	return int64(f.s.e.count(t, f.stlQ(`SELECT rid FROM {k}.calc_record WHERE key = $1`), key))
}

// stlRef writes a reference of a record (role, definition, target, state) to a table of references; stlProblem a problem (definition, kind,
// reason); stlCalcProblem and stlCalcRef what the run computed of a record.
func (f *stlFix) stlRef(t *testing.T, table, key string, role, def int, target int64, state int16) {
	t.Helper()
	cols := "src_no, dataset_no, key"
	vals := "$1, $2, $3"
	f.s.e.exec(t, f.stlQ(`INSERT INTO {k}.`+table+` (`+cols+`, role, def_no, sub, lookup_hash, target_no, state, retirable) VALUES (`+vals+`, $4, $5, 0, '\x01', NULLIF($6, 0), $7, false)`),
		f.s.ref.SrcNo, f.s.rdfNo("ds_sv"), key, role, def, target, state)
}

func (f *stlFix) stlProblem(t *testing.T, key string, def any, kind, reason string) {
	t.Helper()
	f.s.e.exec(t, f.stlQ(`INSERT INTO {k}.record_problem (src_no, dataset_no, key, def_no, field, kind, reason) VALUES ($1, $2, $3, $4, '', $5, $6)`),
		f.s.ref.SrcNo, f.s.rdfNo("ds_sv"), key, def, kind, reason)
}

func (f *stlFix) stlCalcProblem(t *testing.T, rid int64, def any, kind, reason string) {
	t.Helper()
	f.s.e.exec(t, f.stlQ(`INSERT INTO {k}.calc_problem (rid, def_no, field, kind, reason) VALUES ($1, $2, '', $3, $4)`), rid, def, kind, reason)
}

func (f *stlFix) stlCalcRef(t *testing.T, rid int64, role, def int, target int64, state int16) {
	t.Helper()
	f.s.e.exec(t, f.stlQ(`INSERT INTO {k}.calc_ref (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) VALUES ($1, $2, $3, 0, '\x02', NULLIF($4, 0), $5, false)`),
		rid, role, def, target, state)
}

// stlLines is the lines of the records of the fixture: the problems as key/definition/kind/reason, the references as key/role/definition/state/target.
func (f *stlFix) stlLines(t *testing.T) (problems, refs string) {
	t.Helper()
	p := expList(f.s.e, t, f.stlQ(`SELECT key || '/' || COALESCE(def_no::text, '-') || '/' || kind || '/' || reason FROM {k}.record_problem ORDER BY 1`))
	r := expList(f.s.e, t, f.stlQ(`SELECT key || '/' || role || '/' || def_no || '/' || state || '/' || COALESCE(target_no::text, '-') FROM {k}.record_ref ORDER BY 1`))
	return strings.Join(p, " "), strings.Join(r, " ")
}

// stlSettle cleans the fixture with the feeds of the plan, the one of the machines taken as a feed that only adds values.
func (f *stlFix) stlSettle(t *testing.T) {
	t.Helper()
	start := stlStart(f.s, 2)
	for i := range start.Feeds {
		if start.Feeds[i].ID == "f_vm" {
			start.Feeds[i].Creates = false
		}
	}
	if _, err := ingest.RunSettle(f.s.e.ctx, f.s.e.st, f.l, start, &stlConfirms{}); err != nil {
		t.Fatal(err)
	}
}

// [REQ-984]
func TestStlARecordUnderTwoObjectsOfWhichOneIsComputedKeepsTheLinesOfTheOtherAndAFeedAndALinkDefinitionOfOneNumberAreApart(t *testing.T) {
	for _, linkToo := range []bool{false, true} {
		f := stlNewFix(t)
		k := f.s.ref.KGNo
		f.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".stage_object (obj_no, ref, type_no, name, min_grade, doc, content_hash, row_hash, is_new) VALUES (1, 'o-1', 1, 'o', 0, '{}', '\x00', '\x00', false)`, k))
		// R1 stands under the object 1 by the feed 0 (computed) and under the object 2 by the feed 1 (not computed), and made the link 10 by
		// the link definition 0, whose problems keep -1: the feed 0 and the link definition 0 are one number apart
		rid := f.stlRecord(t, "R1", pgschema.CalcWhyExpanded, pgschema.FateUsed, -1)
		f.stlRef(t, "record_ref", "R1", int(pgschema.RefRoleIdentity), 0, 1, pgschema.RefResolved)
		f.stlRef(t, "record_ref", "R1", int(pgschema.RefRoleIdentity), 1, 2, pgschema.RefResolved)
		f.stlRef(t, "record_ref", "R1", int(pgschema.RefRoleLinkFrom), 0, 10, pgschema.RefResolved)
		link := pgschema.ProblemDefOfLink(0)
		f.stlProblem(t, "R1", 0, pgschema.ProblemSourceConflict, "old A")
		f.stlProblem(t, "R1", 1, pgschema.ProblemSourceConflict, "old B")
		f.stlProblem(t, "R1", 1, pgschema.ProblemInvalidValue, "cell")
		f.stlProblem(t, "R1", link, pgschema.ProblemSourceConflict, "old link")
		f.stlProblem(t, "R1", nil, pgschema.ProblemDuplicateKey, "dup")
		f.stlCalcProblem(t, rid, 0, pgschema.ProblemSourceConflict, "new A")
		if linkToo {
			// the computation of the links read the end of R1 again
			f.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_end (rid, def_no, side, lookup_hash) VALUES ($1, 0, 2, '\x01')`, k), rid)
			f.stlCalcRef(t, rid, int(pgschema.RefRoleLinkFrom), 0, 11, pgschema.RefResolved)
			f.stlCalcProblem(t, rid, link, pgschema.ProblemSourceConflict, "new link")
		}
		f.stlSettle(t)
		problems, refs := f.stlLines(t)
		wantP := "R1/-/duplicate_key/dup R1/-1/source_conflict/old link R1/0/source_conflict/new A R1/1/invalid_value/cell R1/1/source_conflict/old B"
		wantR := "R1/1/0/0/1 R1/1/1/0/2 R1/2/0/0/10"
		if linkToo {
			wantP = strings.Replace(wantP, "old link", "new link", 1)
			wantR = "R1/1/0/0/1 R1/1/1/0/2 R1/2/0/0/11"
		}
		if problems != wantP || refs != wantR {
			t.Errorf("link computed %t:\n got %s\n     %s\nwant %s\n     %s", linkToo, problems, refs, wantP, wantR)
		}
	}
}

// [REQ-984] [REQ-985]
func TestStlEachKindOfRecordOfTheRunHasTheLinesTheTableOfTheCleaningSaysAndItsFate(t *testing.T) {
	f := stlNewFix(t)
	k := f.s.ref.KGNo
	ident, from := int(pgschema.RefRoleIdentity), int(pgschema.RefRoleLinkFrom)
	// R2 is new: every line is the run's, with the problem the receiving found of its row
	r2 := f.stlRecord(t, "R2", pgschema.CalcWhyNew, pgschema.FateUsed, 0)
	f.s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, page, row_no, key, place, filtered, change_kind, raw_hash, fields, problems) VALUES ($1, $2, 0, 0, 'R2', '', false, 0, '\x01', ARRAY['new'], $3)`,
		pgiTable(f.s.ref, pgschema.TableStageRecord)), f.s.ref.SrcNo, f.s.rdfNo("ds_sv"), `[{"kind":"invalid_value","feed":"f_sv","field":"pos","reason":"not a number"}]`)
	f.stlCalcRef(t, r2, ident, 1, 5, pgschema.RefResolved)
	f.stlCalcProblem(t, r2, 1, pgschema.ProblemIdentityConflict, "conflict")
	// R3 is closed: nothing of it stays
	f.stlRecord(t, "R3", pgschema.CalcWhyClosed, pgschema.FateUsed, -1)
	f.stlRef(t, "record_ref", "R3", ident, 1, 5, pgschema.RefResolved)
	f.stlProblem(t, "R3", 1, pgschema.ProblemSourceConflict, "gone")
	// R4 is not solved and read again whole: the lines of the identification and of the links are the run's, the receiving's stay
	r4 := f.stlRecord(t, "R4", pgschema.CalcWhyUnresolved, pgschema.FateProblem, -1)
	f.stlRef(t, "record_ref", "R4", ident, 1, 0, pgschema.RefUnresolved)
	f.stlRef(t, "record_ref", "R4", from, 0, 0, pgschema.RefUnresolved)
	f.stlProblem(t, "R4", 1, pgschema.ProblemOrphanValue, "old orphan")
	f.stlProblem(t, "R4", 1, pgschema.ProblemMissingIdentity, "received before")
	f.stlProblem(t, "R4", pgschema.ProblemDefOfLink(0), pgschema.ProblemLinkTarget, "old end")
	f.stlProblem(t, "R4", nil, pgschema.ProblemDuplicateKey, "still repeats")
	f.stlCalcRef(t, r4, ident, 1, 0, pgschema.RefUnresolved)
	f.stlCalcProblem(t, r4, 1, pgschema.ProblemMissingName, "no name")
	// R5 is shown again and a rule filters it: the cells of the feed that only adds values are the run's, the receiving's stay, and the
	// repeated key is judged again
	r5 := f.stlRecord(t, "R5", pgschema.CalcWhyReturned, pgschema.FateFiltered, -1)
	f.stlRef(t, "record_ref", "R5", ident, 1, 6, pgschema.RefResolved)
	f.stlProblem(t, "R5", 2, pgschema.ProblemInvalidValue, "value cell")
	f.stlProblem(t, "R5", 1, pgschema.ProblemInvalidValue, "object cell")
	f.stlProblem(t, "R5", nil, pgschema.ProblemDuplicateKey, "repeated")
	f.stlCalcRef(t, r5, ident, 1, 6, pgschema.RefHeld)
	// R6 changed and nothing of it is used or wrong: unused
	f.stlRecord(t, "R6", pgschema.CalcWhyChanged, pgschema.FateUsed, 1)
	f.s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, page, row_no, key, place, filtered, change_kind, raw_hash, fields) VALUES ($1, $2, 1, 1, 'R6', '', false, 1, '\x01', ARRAY['new'])`,
		pgiTable(f.s.ref, pgschema.TableStageRecord)), f.s.ref.SrcNo, f.s.rdfNo("ds_sv"))
	f.stlProblem(t, "R6", 1, pgschema.ProblemSourceConflict, "was wrong")
	f.stlSettle(t)
	problems, refs := f.stlLines(t)
	wantP := strings.Join([]string{"R2/1/identity_conflict/conflict", "R2/1/invalid_value/not a number", "R4/-/duplicate_key/still repeats", "R4/1/missing_identity/received before",
		"R4/1/missing_name/no name", "R5/1/invalid_value/object cell"}, " ")
	wantR := "R2/1/1/0/5 R4/1/1/1/- R5/1/1/3/6"
	if problems != wantP || refs != wantR {
		t.Errorf("\n got %s\n     %s\nwant %s\n     %s", problems, refs, wantP, wantR)
	}
	fates := expList(f.s.e, t, fmt.Sprintf(`SELECT key || '=' || fate || ':' || opened_seq FROM "s_%d".raw_record ORDER BY key`, k))
	want := fmt.Sprintf("R2=%d:7 R4=%d:1 R5=%d:1 R6=%d:7", pgschema.FateUsed, pgschema.FateProblem, pgschema.FateFiltered, pgschema.FateUnused)
	if strings.Join(fates, " ") != want {
		t.Errorf("the raw records: %v, want %s", fates, want)
	}
}
