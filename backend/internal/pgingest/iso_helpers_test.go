package pgingest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the fifth step, its first part (ADR-0123). Every helper, type and constant they add starts with iso, so that none meets a
// name of another test file of this folder, and none of them is a method of a type of another file.

// isoDo does the fifth step of a run from where it stands, with pages of the size given (0 is the default).
func isoDo(s *rdfSetup, t *testing.T, l ingest.RunLease, plans map[int64]*ingest.Plan, lines int) ingest.IsolateCounts {
	t.Helper()
	m, err := ingest.NewIsolationMakerOf(plans)
	if err != nil {
		t.Fatal(err)
	}
	counts, err := ingest.RunIsolate(s.e.ctx, s.e.st, l, ingest.IsolateStart{Lines: lines, Cards: ingest.IsolateCardsOf(plans)}, m)
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

// isoTaken is what the step took out, as phase/why/what, where what is the id of a link or the id of an object and the identifier.
func isoTaken(s *rdfSetup, t *testing.T) []string {
	t.Helper()
	return expList(s.e, t, fmt.Sprintf(`SELECT i.phase || '/' || i.why || '/' || CASE WHEN i.kind = %[2]d THEN i.ref ELSE COALESCE(o.ref, so.ref, '?') || ' ' || i.scheme || '=' || i.value END
		FROM "s_%[1]d".calc_isolated i LEFT JOIN "s_%[1]d".object o ON i.kind = %[3]d AND o.obj_no = i.entity_no LEFT JOIN "s_%[1]d".stage_object so ON i.kind = %[3]d AND so.obj_no = i.entity_no ORDER BY 1`,
		s.ref.KGNo, pgschema.DiffKindLink, pgschema.DiffKindIdentifier))
}

// isoProblems is the problems of the kinds the step writes, as kind/dataset/key.
func isoProblems(s *rdfSetup, t *testing.T) []string {
	t.Helper()
	k := s.ref.KGNo
	return expList(s.e, t, fmt.Sprintf(`SELECT p.kind || '/' || d.dataset_id || '/' || c.key FROM "s_%[1]d".calc_problem p JOIN "s_%[1]d".calc_record c ON c.rid = p.rid
		JOIN source_dataset d ON d.dataset_no = c.dataset_no WHERE p.kind IN ('%[2]s', '%[3]s') OR (p.kind = '%[4]s' AND p.reason NOT LIKE 'no key%%') ORDER BY 1`,
		k, pgschema.ProblemCardinality, pgschema.ProblemLinkTarget, pgschema.ProblemIdentityConflict))
}

// isoBroken is what breaks the rules the step keeps after it (SQL, apart from the step's statements): a staged link whose end is neither a
// staged object nor one the KG has alive, or retires; two links at one start of the types given that hold the start to one (the staged
// ones and the ones the KG has that the run does not stage or take out); and an identifier that two objects have after the run (the
// staged ones, and the ones the KG has of the objects the run does not compute or retire).
func isoBroken(s *rdfSetup, t *testing.T, fromOne ...int) []string {
	t.Helper()
	k := s.ref.KGNo
	var types []string
	for _, ty := range fromOne {
		types = append(types, fmt.Sprint(ty))
	}
	if len(types) == 0 {
		types = []string{"0"}
	}
	return expList(s.e, t, fmt.Sprintf(`
		SELECT 'end ' || l.ref FROM "s_%[1]d".stage_link l CROSS JOIN LATERAL unnest(ARRAY[l.src, l.dst]) AS e(obj)
		WHERE NOT EXISTS (SELECT 1 FROM "s_%[1]d".stage_object o WHERE o.obj_no = e.obj)
		  AND (NOT EXISTS (SELECT 1 FROM "s_%[1]d".object o WHERE o.obj_no = e.obj AND o.retired_seq IS NULL) OR EXISTS (SELECT 1 FROM "s_%[1]d".calc_object c WHERE c.obj_no = e.obj AND c.why = %[2]d))
		UNION ALL SELECT 'place ' || type_no || '/' || src FROM (
		  SELECT type_no, src FROM "s_%[1]d".stage_link WHERE type_no IN (%[3]s)
		  UNION ALL SELECT k.type_no, k.src FROM "s_%[1]d".link k WHERE k.type_no IN (%[3]s) AND NOT EXISTS (SELECT 1 FROM "s_%[1]d".stage_link l WHERE l.link_no = k.link_no)
		    AND NOT EXISTS (SELECT 1 FROM "s_%[1]d".calc_isolated i WHERE i.kind = %[4]d AND i.entity_no = k.link_no)
		    AND NOT EXISTS (SELECT 1 FROM "s_%[1]d".calc_object c WHERE c.obj_no IN (k.src, k.dst) AND c.why = %[2]d AND NOT EXISTS (SELECT 1 FROM "s_%[1]d".stage_object x WHERE x.obj_no = c.obj_no))
		    AND NOT EXISTS (SELECT 1 FROM "s_%[1]d".record_ref f WHERE f.role = %[5]d AND f.target_no = k.link_no AND f.state = 0
		      AND EXISTS (SELECT 1 FROM "s_%[1]d".calc_record c WHERE c.src_no = f.src_no AND c.dataset_no = f.dataset_no AND c.key = f.key)
		      AND NOT EXISTS (SELECT 1 FROM "s_%[1]d".record_ref g WHERE g.role = %[5]d AND g.target_no = k.link_no AND g.state = 0
		        AND NOT EXISTS (SELECT 1 FROM "s_%[1]d".calc_record c WHERE c.src_no = g.src_no AND c.dataset_no = g.dataset_no AND c.key = g.key)))) z GROUP BY type_no, src HAVING count(*) > 1
		UNION ALL SELECT 'identifier ' || scheme || '=' || value FROM (
		  SELECT obj_no, scheme, value, scope FROM "s_%[1]d".stage_ident
		  UNION SELECT i.obj_no, i.scheme, i.value, i.scope FROM "s_%[1]d".object_identifier i JOIN "s_%[1]d".object o ON o.obj_no = i.obj_no AND o.retired_seq IS NULL
		  WHERE i.seq_to IS NULL AND NOT EXISTS (SELECT 1 FROM "s_%[1]d".stage_object x WHERE x.obj_no = i.obj_no)
		    AND NOT EXISTS (SELECT 1 FROM "s_%[1]d".calc_object c WHERE c.obj_no = i.obj_no AND c.why = %[2]d)) z GROUP BY scheme, value, scope HAVING count(*) > 1 ORDER BY 1`,
		k, pgschema.ObjectWhyRetire, strings.Join(types, ","), pgschema.DiffKindLink, pgschema.RefRoleLinkFrom))
}

// isoModel is the numbers of the model the cases use.
type isoModel struct {
	reg                                     *model.Registry
	server, rack, host, vm, mounted, runsOn int
}

func isoModelOf(t *testing.T) isoModel {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	no := func(name string) int {
		ot, ok := reg.ObjectTypeByName(name)
		if !ok {
			t.Fatalf("no type %s", name)
		}
		return ot.TypeNo
	}
	link := func(name string) int {
		lt, ok := reg.LinkTypeByName(name)
		if !ok {
			t.Fatalf("no link type %s", name)
		}
		return lt.LinkTypeNo
	}
	return isoModel{reg: reg, server: no("PhysicalServer"), rack: no("Rack"), host: no("HypervisorHost"), vm: no("VirtualMachine"), mounted: link("MOUNTED_IN"), runsOn: link("RUNS_ON")}
}

// isoScheme is the identifier scheme the fixtures claim, and its number in the claims of the references.
const (
	isoScheme   = "is.serial"
	isoSchemeNo = 3
)

// isoFix is a KG whose run has done its fourth step, made in SQL: the objects, the links and the identifiers the cases need, each where a
// case puts it (the KG, the staged rows of the run, or both), and the records and references that stand under them.
type isoFix struct {
	s  *rdfSetup
	l  ingest.RunLease
	m  isoModel
	no map[string]int64 // the number of an object or a link by its id
}

func isoNewFix(t *testing.T) *isoFix {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	l := s.rdfComputing(t, "server-a", map[string]ingest.Coverage{"ds_sv": ingest.CoverageComplete})
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_owner (run_no) VALUES ($1)`, s.ref.KGNo), l.No)
	s.e.exec(t, `UPDATE sync_run SET calc_job = $2, counts = counts || $3::jsonb WHERE run_no = $1`, l.No, pgschema.CalcJobRecompute,
		fmt.Sprintf(`{"recompute":{"finished":true,"schemes":{%q:%d}},"recompute_links":{"finished":true}}`, isoScheme, isoSchemeNo))
	return &isoFix{s: s, l: l, m: isoModelOf(t), no: map[string]int64{}}
}

func (f *isoFix) isoQ(query string) string {
	return strings.ReplaceAll(query, "{k}", fmt.Sprintf(`"s_%d"`, f.s.ref.KGNo))
}

// isoObj puts an object: where is one of kg (alive in the KG and untouched), new (staged only), both (in the KG and staged), retired (in
// the KG retired), retiring (in the KG, a candidate the run retires), left (in the KG, left by the computation).
func (f *isoFix) isoObj(t *testing.T, ref string, typ int, where string) int64 {
	t.Helper()
	no := int64(len(f.no) + 1)
	f.no[ref] = no
	if where != "new" {
		retired := "NULL"
		if where == "retired" {
			retired = "1"
		}
		f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.object (obj_no, ref, type_no, name, min_grade, seq_from, retired_seq) VALUES ($1, $2, $3, $2, 0, 1, `+retired+`)`), no, ref, typ)
	}
	switch where {
	case "new", "both":
		f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.stage_object (obj_no, ref, type_no, name, min_grade, doc, content_hash, row_hash, is_new) VALUES ($1, $2, $3, $2, 0, '{}', '\x00', '\x00', $4)`),
			no, ref, typ, where == "new")
		f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.calc_object (obj_no, why) VALUES ($1, 1)`), no)
	case "retiring":
		f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.calc_object (obj_no, why) VALUES ($1, $2)`), no, pgschema.ObjectWhyRetire)
	case "left":
		f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.calc_object (obj_no, why) VALUES ($1, 1)`), no)
		f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.calc_left (obj_no, why, lines, is_new, reason) VALUES ($1, $2, 9, false, 'too many')`), no, pgschema.LeftTooMany)
	}
	return no
}

// isoRecord puts a record of the run of the servers' dataset and gives its rid; why is the reason it is in the run.
func (f *isoFix) isoRecord(t *testing.T, key string, why int16) int64 {
	t.Helper()
	return int64(f.s.e.count(t, f.isoQ(`INSERT INTO {k}.calc_record (src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, why)
		SELECT $1, $2, $3, ARRAY[$3], '\x00', d.decl_rev, 1, 0, $4 FROM source_dataset d WHERE d.dataset_no = $2 RETURNING rid`), f.s.ref.SrcNo, f.s.rdfNo("ds_sv"), key, why))
}

// isoLink puts a link: staged (the run staged it, made by the record key, held when the record is one a rule filtered out), and in the KG
// (kg: the record keyFrom is the one the KG says made it; an empty one is a link no ingestion made).
func (f *isoFix) isoLink(t *testing.T, ref string, typ int, src, dst, key string, staged, kg bool, held bool) int64 {
	t.Helper()
	no := int64(1000 + len(f.no))
	f.no[ref] = no
	if staged {
		f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.stage_link (link_no, ref, def_no, type_no, src, dst, grade, basis, content_hash, row_hash) VALUES ($1, $2, 0, $3, $4, $5, 'fact', 'declared', '\x00', '\x00')`),
			no, ref, typ, f.no[src], f.no[dst])
		rid := f.isoRecord(t, key, pgschema.CalcWhyChanged)
		f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.calc_linkwin (ref, rid, def_no, src_no, dataset_no, key, held, sig, shown, link_name, def_id, dataset_id, link_no)
			VALUES ($1, $2, 0, $3, $4, $5, $6, '', '{}', 'MOUNTED_IN', 'l_mounted', 'ds_sv', $7)`), ref, rid, f.s.ref.SrcNo, f.s.rdfNo("ds_sv"), key, held, no)
		f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.calc_ref (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) VALUES ($1, $2, 0, 0, '\x01', $3, $4, false)`),
			rid, pgschema.RefRoleLinkFrom, no, pgschema.RefResolved)
	}
	if kg {
		f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.link (link_no, ref, type_no, src, dst, grade, basis, content_hash, row_hash, seq_from) VALUES ($1, $2, $3, $4, $5, 'fact', 'declared', '\x00', '\x00', 1)`),
			no, ref, typ, f.no[src], f.no[dst])
	}
	return no
}

// isoKeptFrom says the KG's link was made by the record key, which the run does not read (inRun false) or reads.
func (f *isoFix) isoKeptFrom(t *testing.T, ref, key string, inRun bool) {
	t.Helper()
	f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.record_ref (src_no, dataset_no, key, role, def_no, sub, lookup_hash, target_no, state, retirable) VALUES ($1, $2, $3, $4, 0, 0, '\x01', $5, $6, false)`),
		f.s.ref.SrcNo, f.s.rdfNo("ds_sv"), key, pgschema.RefRoleLinkFrom, f.no[ref], pgschema.RefResolved)
	if inRun {
		f.isoRecord(t, key, pgschema.CalcWhyChanged)
	}
}

// isoHash is the hash a claim of a value names it by (the one the receiving gives, a content hash of the text).
func isoHash(t *testing.T, value string) []byte {
	t.Helper()
	h, err := kernel.ContentHash(value)
	if err != nil {
		t.Fatal(err)
	}
	return h[:16]
}

// isoIdent puts an identifier of an object: staged (its staged identifiers have it, claimed by the record key of the run), kg (the KG has
// it open), and kept (a record the run reads again only, keptKey, claims it by a reference that is kept).
func (f *isoFix) isoIdent(t *testing.T, obj, value, key string, staged, kg bool, keptKey string) {
	t.Helper()
	no := f.no[obj]
	if staged {
		f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.stage_ident (obj_no, scheme, value, scope) VALUES ($1, $2, $3, '')`), no, isoScheme, value)
		if key != "" {
			rid := f.isoRecord(t, key, pgschema.CalcWhyChanged)
			f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.calc_ref (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) VALUES ($1, $2, $3, 1, $4, $5, $6, false)`),
				rid, pgschema.RefRoleClaim, isoSchemeNo, isoHash(t, value), no, pgschema.RefResolved)
		}
	}
	if kg {
		f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.object_identifier (obj_no, scheme, value, scope, seq_from) VALUES ($1, $2, $3, '', 1)`), no, isoScheme, value)
	}
	if keptKey != "" {
		f.isoRecord(t, keptKey, pgschema.CalcWhyExpanded)
		f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.record_ref (src_no, dataset_no, key, role, def_no, sub, lookup_hash, target_no, state, retirable) VALUES ($1, $2, $3, $4, $5, 1, $6, $7, $8, false)`),
			f.s.ref.SrcNo, f.s.rdfNo("ds_sv"), keptKey, pgschema.RefRoleClaim, isoSchemeNo, isoHash(t, value), no, pgschema.RefResolved)
	}
}

// isoRefs is the references of the records of the run that the step made or changed, as key/role/state/target (the target by its id).
func (f *isoFix) isoRefs(t *testing.T) []string {
	t.Helper()
	return expList(f.s.e, t, f.isoQ(`SELECT c.key || '/' || r.role || '/' || r.state || '/' || COALESCE(l.ref, sl.ref, o.ref, so.ref, '-') FROM {k}.calc_ref r JOIN {k}.calc_record c ON c.rid = r.rid
		LEFT JOIN {k}.link l ON r.role = 2 AND l.link_no = r.target_no LEFT JOIN {k}.stage_link sl ON r.role = 2 AND sl.link_no = r.target_no
		LEFT JOIN {k}.object o ON r.role = 4 AND o.obj_no = r.target_no LEFT JOIN {k}.stage_object so ON r.role = 4 AND so.obj_no = r.target_no ORDER BY 1`))
}

// isoMaker is the maker of the model of the fixture.
func (f *isoFix) isoRun(t *testing.T, lines int) ingest.IsolateCounts {
	t.Helper()
	counts, err := ingest.RunIsolate(f.s.e.ctx, f.s.e.st, f.l, ingest.IsolateStart{Lines: lines, Cards: ingest.ExpandCards(f.m.reg)}, ingest.NewIsolationMaker(f.m.reg))
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

// isoState is the whole of what the step left, as text: what it took out, the staged links and identifiers, the references and problems.
func (f *isoFix) isoState(t *testing.T) string {
	t.Helper()
	links := expList(f.s.e, t, f.isoQ(`SELECT ref FROM {k}.stage_link ORDER BY ref`))
	idents := expList(f.s.e, t, f.isoQ(`SELECT so.ref || '=' || i.value FROM {k}.stage_ident i JOIN {k}.stage_object so ON so.obj_no = i.obj_no ORDER BY 1`))
	seats := expList(f.s.e, t, f.isoQ(`SELECT st.side || '/' || COALESCE(o.ref, so.ref) || '/' || l.ref FROM {k}.calc_seat st JOIN {k}.stage_link l ON l.link_no = st.link_no
		LEFT JOIN {k}.object o ON o.obj_no = st.end_no LEFT JOIN {k}.stage_object so ON so.obj_no = st.end_no ORDER BY 1`))
	parts := []string{strings.Join(isoTaken(f.s, t), ","), strings.Join(links, ","), strings.Join(idents, ","), strings.Join(seats, ","),
		strings.Join(f.isoRefs(t), ","), strings.Join(isoProblems(f.s, t), ",")}
	return strings.Join(parts, "\n")
}
