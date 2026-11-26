package pgingest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// A KG whose run has done its isolation, made in SQL (ADR-0127): the objects, the links, the identifiers and the keys each case needs, each
// where the case puts it (the KG, the rows the run staged, or both), so that every source the difference reads has rows of every kind of
// change. The contents are hashes of words, and the rows of the KG are the ones the apply reads back (the registry, the tables of the
// types, the links), so the staged apply can write the difference.

// dfiFix is the world: the setup and the lease, the numbers of the model, the numbers given to the objects and links by their ids, and
// whether the numbers go down (so that the order they are given in is another one).
type dfiFix struct {
	s       *rdfSetup
	l       ingest.RunLease
	m       isoModel
	no      map[string]int64
	reverse bool
	objs    int64
	links   int64
}

func dfiNewFix(t *testing.T, reverse bool) *dfiFix {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	l := s.rdfComputing(t, "server-a", map[string]ingest.Coverage{"ds_sv": ingest.CoverageComplete})
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_owner (run_no) VALUES ($1)`, s.ref.KGNo), l.No)
	s.e.exec(t, `UPDATE sync_run SET calc_job = $2, counts = counts || '{"isolate":{"finished":true}}'::jsonb WHERE run_no = $1`, l.No, pgschema.CalcJobIsolate)
	return &dfiFix{s: s, l: l, m: isoModelOf(t), no: map[string]int64{}, reverse: reverse}
}

func (f *dfiFix) dfiQ(query string) string {
	return strings.ReplaceAll(query, "{k}", fmt.Sprintf(`"s_%d"`, f.s.ref.KGNo))
}

// dfiNext gives the next number of an object or a link, up from 1 (100001 for a link), or down from 99999 (199999) when the fixture goes down.
func (f *dfiFix) dfiNext(ref string, link bool) int64 {
	n := &f.objs
	base := int64(0)
	if link {
		n, base = &f.links, 100000
	}
	*n++
	no := base + *n
	if f.reverse {
		no = base + 100000 - *n
	}
	f.no[ref] = no
	return no
}

// dfiHash is the hash of a word, as SQL.
const dfiHash = "sha256(convert_to($%d, 'UTF8'))"

func dfiH(n int) string { return fmt.Sprintf(dfiHash, n) }

// dfiObj puts an object: where is one of kg (alive in the KG and untouched), same (in the KG and staged with the same content), changed (in the
// KG and staged with another), new (staged only), revived (in the KG retired and staged), retiring (in the KG, a candidate the run retires),
// gone (in the KG retired, and a candidate again), left (in the KG, left by the computation of the objects).
func (f *dfiFix) dfiObj(t *testing.T, ref string, typ int, where string) int64 {
	t.Helper()
	no := f.dfiNext(ref, false)
	e := f.s.e
	if where != "new" {
		retired := "NULL"
		if where == "revived" || where == "gone" {
			retired = "1"
		}
		e.exec(t, f.dfiQ(`INSERT INTO {k}.object (obj_no, ref, type_no, name, min_grade, seq_from, retired_seq) VALUES ($1, $2, $3, $2, 0, 1, `+retired+`)`), no, ref, typ)
		if retired == "NULL" {
			e.exec(t, f.dfiQ(fmt.Sprintf(`INSERT INTO {k}.t_%d (obj_no, ref, name, min_grade, doc, content_hash, row_hash, seq_from) VALUES ($1, $2, $2, 0, '{}', %s, %s, 1)`,
				typ, dfiH(3), dfiH(4))), no, ref, "kg "+ref, "row "+ref)
		}
	}
	switch where {
	case "same", "changed", "new", "revived":
		content := "kg " + ref
		if where != "same" {
			content = "run " + ref
		}
		e.exec(t, f.dfiQ(fmt.Sprintf(`INSERT INTO {k}.stage_object (obj_no, ref, type_no, name, min_grade, doc, content_hash, row_hash, is_new) VALUES ($1, $2, $3, $2, 0, '{}', %s, %s, $6)`,
			dfiH(4), dfiH(5))), no, ref, typ, content, "row run "+ref, where == "new")
		e.exec(t, f.dfiQ(`INSERT INTO {k}.calc_object (obj_no, why) VALUES ($1, 1)`), no)
	case "retiring", "gone":
		e.exec(t, f.dfiQ(`INSERT INTO {k}.calc_object (obj_no, why) VALUES ($1, $2)`), no, pgschema.ObjectWhyRetire)
	case "left":
		e.exec(t, f.dfiQ(`INSERT INTO {k}.calc_object (obj_no, why) VALUES ($1, 1)`), no)
		e.exec(t, f.dfiQ(`INSERT INTO {k}.calc_left (obj_no, why, lines, is_new, reason) VALUES ($1, $2, 9, false, 'too many')`), no, pgschema.LeftTooMany)
	}
	return no
}

// dfiRecord puts a record of the run of the servers' dataset and gives its rid.
func (f *dfiFix) dfiRecord(t *testing.T, key string) int64 {
	t.Helper()
	return int64(f.s.e.count(t, f.dfiQ(`INSERT INTO {k}.calc_record (src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, why)
		SELECT $1, $2, $3, ARRAY[$3], '\x00', d.decl_rev, 1, 0, 0 FROM source_dataset d WHERE d.dataset_no = $2 RETURNING rid`), f.s.ref.SrcNo, f.s.rdfNo("ds_sv"), key))
}

// dfiFrom says the KG's link was made by the record key (a resolved reference of its From end), which the run reads (inRun) or not.
func (f *dfiFix) dfiFrom(t *testing.T, ref, key string, inRun bool) {
	t.Helper()
	f.s.e.exec(t, f.dfiQ(`INSERT INTO {k}.record_ref (src_no, dataset_no, key, role, def_no, sub, lookup_hash, target_no, state, retirable) VALUES ($1, $2, $3, $4, 0, 0, '\x01', $5, $6, false)`),
		f.s.ref.SrcNo, f.s.rdfNo("ds_sv"), key, pgschema.RefRoleLinkFrom, f.no[ref], pgschema.RefResolved)
	if inRun {
		f.dfiRecord(t, key)
	}
}

// dfiLink puts a link: where is one of same, changed, new (as for an object), and for a link of the KG the run did not stage: run (made by a
// record the run reads), kept (made by a record the run does not read), bare (made by no record of an ingestion), iso (taken out by the
// isolation, made by a record the run reads).
func (f *dfiFix) dfiLink(t *testing.T, ref string, typ int, src, dst, where string) int64 {
	t.Helper()
	no := f.dfiNext(ref, true)
	e := f.s.e
	if where != "new" {
		e.exec(t, f.dfiQ(fmt.Sprintf(`INSERT INTO {k}.link (link_no, ref, type_no, src, dst, grade, basis, content_hash, row_hash, seq_from) VALUES ($1, $2, $3, $4, $5, 'fact', 'declared', %s, %s, 1)`,
			dfiH(6), dfiH(7))), no, ref, typ, f.no[src], f.no[dst], "kg "+ref, "row "+ref)
	}
	switch where {
	case "same", "changed", "new":
		content := "kg " + ref
		if where != "same" {
			content = "run " + ref
		}
		e.exec(t, f.dfiQ(fmt.Sprintf(`INSERT INTO {k}.stage_link (link_no, ref, def_no, type_no, src, dst, grade, basis, content_hash, row_hash) VALUES ($1, $2, 0, $3, $4, $5, 'fact', 'declared', %s, %s)`,
			dfiH(6), dfiH(7))), no, ref, typ, f.no[src], f.no[dst], content, "row run "+ref)
	case "run":
		f.dfiFrom(t, ref, "rec "+ref, true)
	case "kept":
		f.dfiFrom(t, ref, "rec "+ref, false)
	case "iso":
		f.dfiFrom(t, ref, "rec "+ref, true)
		e.exec(t, f.dfiQ(`INSERT INTO {k}.calc_isolated (kind, entity_no, ref, phase, why, reason) VALUES ($1, $2, $3, $4, $5, 'taken out')`),
			pgschema.DiffKindLink, no, ref, pgschema.IsolatePhaseCard, pgschema.IsolatedCardinality)
	}
	return no
}

// dfiIdent puts an identifier of an object: staged (its staged identifiers have it) and kg (the KG has it open).
func (f *dfiFix) dfiIdent(t *testing.T, obj, value string, staged, kg bool) {
	t.Helper()
	if staged {
		f.s.e.exec(t, f.dfiQ(`INSERT INTO {k}.stage_ident (obj_no, scheme, value, scope) VALUES ($1, $2, $3, '')`), f.no[obj], isoScheme, value)
	}
	if kg {
		f.s.e.exec(t, f.dfiQ(`INSERT INTO {k}.object_identifier (obj_no, scheme, value, scope, seq_from) VALUES ($1, $2, $3, '', 1)`), f.no[obj], isoScheme, value)
	}
}

// dfiKey puts a key the run binds to an object (calc_newkey), which the index of the KG has already when bound is true.
func (f *dfiFix) dfiKey(t *testing.T, obj, sig string, typ int, bound bool) {
	t.Helper()
	f.s.e.exec(t, f.dfiQ(`INSERT INTO {k}.calc_newkey (type_no, ctx_no, sig_hash, obj_no, round_no) VALUES ($1, 0, substring(sha256(convert_to($2, 'UTF8')) from 1 for 16), $3, 0)`), typ, sig, f.no[obj])
	if bound {
		f.s.e.exec(t, f.dfiQ(`INSERT INTO {k}.object_key (type_no, ctx_no, sig_hash, obj_no, bound_seq) VALUES ($1, 0, substring(sha256(convert_to($2, 'UTF8')) from 1 for 16), $3, 1)`), typ, sig, f.no[obj])
	}
}

// dfiAll puts a world that has a row of every source and every change, and rows of each that make none.
func dfiAll(t *testing.T, reverse bool) *dfiFix {
	t.Helper()
	f := dfiNewFix(t, reverse)
	sv, rk, vm, mt := f.m.server, f.m.rack, f.m.vm, f.m.mounted
	for _, o := range []struct{ ref, where string }{{"s-kg", "kg"}, {"s-same", "same"}, {"s-changed", "changed"}, {"s-new", "new"}, {"s-revived", "revived"},
		{"s-retiring", "retiring"}, {"s-gone", "gone"}, {"s-left", "left"}, {"s-new2", "new"}} {
		f.dfiObj(t, o.ref, sv, o.where)
	}
	for _, o := range []struct{ ref, where string }{{"r-kg", "kg"}, {"r-changed", "changed"}, {"r-retiring", "retiring"}, {"r-new", "new"}} {
		f.dfiObj(t, o.ref, rk, o.where)
	}
	f.dfiObj(t, "v-new", vm, "new")
	for _, l := range []struct{ ref, src, dst, where string }{
		{"l-same", "s-same", "r-kg", "same"}, {"l-changed", "s-changed", "r-kg", "changed"}, {"l-new", "s-new", "r-new", "new"},
		{"l-run", "s-kg", "r-changed", "run"}, {"l-kept", "s-kg", "r-kg", "kept"}, {"l-bare", "s-left", "r-kg", "bare"},
		{"l-iso", "s-same", "r-changed", "iso"}, {"l-retiring", "s-kg", "r-retiring", "kept"}, {"l-retiring2", "s-retiring", "r-kg", "bare"},
	} {
		f.dfiLink(t, l.ref, mt, l.src, l.dst, l.where)
	}
	f.dfiIdent(t, "s-same", "sn-same", true, true)
	f.dfiIdent(t, "s-changed", "sn-changed-new", true, false)
	f.dfiIdent(t, "s-changed", "sn-changed-old", false, true)
	f.dfiIdent(t, "s-new", "sn-new", true, false)
	f.dfiIdent(t, "s-new", "sn-new-b", true, false)
	f.dfiIdent(t, "s-retiring", "sn-retiring", false, true)
	f.dfiIdent(t, "s-kg", "sn-kg", false, true)
	f.dfiKey(t, "s-new", "key s-new", sv, false)
	f.dfiKey(t, "s-new", "key s-new alias", sv, false)
	f.dfiKey(t, "s-kg", "key s-kg alias", sv, false)
	f.dfiKey(t, "s-same", "key s-same", sv, true)
	f.dfiKey(t, "r-new", "key r-new", rk, false)
	return f
}

// dfiAllRows is the difference of dfiAll as dfiRows gives it without the hashes: the chunk, the kind, the type, the id and the change.
func dfiAllRows(f *dfiFix, t *testing.T) []string {
	t.Helper()
	var out []string
	for _, r := range dfiRows(f.s, t) {
		parts := strings.Fields(r)
		out = append(out, strings.Join(parts[:5], " "))
	}
	return out
}
