package pgingest

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the computation of the objects (ADR-0115). Every helper, type and constant they add starts with rcp, so that none meets a
// name of another test file of this folder, and none of them is a method of a type of another file.

var rcpSeen = rdfT0.Add(2 * time.Hour).UTC().Format(time.RFC3339)

// rcpHasher is the hasher of the contents the tests make the objects with.
func rcpHasher(t *testing.T, s *rdfSetup) *graph.ContentHasher {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	return graph.NewContentHasher(reg, graph.NewTestKeyProvider("rcp"), s.kg.ID)
}

// rcpStart is the start of the computation of the objects, with the sizes of a page given (0 is the default).
func rcpStart(s *rdfSetup, lines, objectMax int) ingest.RecomputeStart {
	return ingest.RecomputeStart{Lines: lines, ObjectMax: objectMax, Seen: rcpSeen, Schemes: s.plan.SchemeNumbers()}
}

// rcpMaker is the computer of the objects of the plan of the setup.
func rcpMaker(t *testing.T, s *rdfSetup, lines, objectMax int) *ingest.ObjectComputer {
	t.Helper()
	return ingest.NewObjectComputer(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan}, rcpHasher(t, s), rcpStart(s, lines, objectMax))
}

// rcpDo does the computation of the objects of a run whose expansion is done, from where it stands.
func rcpDo(s *rdfSetup, t *testing.T, l ingest.RunLease, lines, objectMax int) ingest.RecomputeCounts {
	t.Helper()
	counts, err := ingest.RunRecompute(s.e.ctx, s.e.st, l, rcpStart(s, lines, objectMax), rcpMaker(t, s, lines, objectMax))
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

// rcpExpand does the expansion of a run whose identification is done.
func rcpExpand(s *rdfSetup, t *testing.T, l ingest.RunLease, full bool) {
	t.Helper()
	if _, err := ingest.RunExpand(s.e.ctx, s.e.st, l, ingest.ExpandStart{Full: full, Cards: s.plan.ExpandCards(), Feeds: expFeeds(s)}); err != nil {
		t.Fatal(err)
	}
}

// rcpObject is a row of stage_object as the tests compare them: by the id, without the number the run gave the object.
type rcpObject struct {
	Ref, Name, Doc, SourceRefs, Content, Row, FirstKey string
	Grade                                              int16
	IsNew                                              bool
	Volatile                                           []string
	Idents                                             []string
}

func (o rcpObject) rcpText() string {
	return fmt.Sprintf("%s name=%q grade=%d doc=%s refs=%s content=%s row=%s first=%s new=%v volatile=%v idents=%v", o.Ref, o.Name, o.Grade, o.Doc, o.SourceRefs, o.Content, o.Row, o.FirstKey, o.IsNew, o.Volatile, o.Idents)
}

// rcpStaged reads what the computation made, by the id of the object.
func rcpStaged(s *rdfSetup, t *testing.T) map[string]rcpObject {
	t.Helper()
	k := s.ref.KGNo
	out := map[string]rcpObject{}
	rows, err := s.e.admin.QueryContext(s.e.ctx, fmt.Sprintf(`SELECT obj_no, ref, name, min_grade, doc::text, source_refs::text, encode(content_hash, 'hex'), encode(row_hash, 'hex'), COALESCE(first_key::text, ''), is_new FROM "s_%d".stage_object`, k))
	if err != nil {
		t.Fatal(err)
	}
	byNo := map[int64]string{}
	for rows.Next() {
		var no int64
		var o rcpObject
		if err := rows.Scan(&no, &o.Ref, &o.Name, &o.Grade, &o.Doc, &o.SourceRefs, &o.Content, &o.Row, &o.FirstKey, &o.IsNew); err != nil {
			t.Fatal(err)
		}
		byNo[no] = o.Ref
		out[o.Ref] = o
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	for _, q := range []struct{ query, kind string }{
		{fmt.Sprintf(`SELECT obj_no, attr_no || '=' || entry::text FROM "s_%d".stage_volatile ORDER BY 1, 2`, k), "v"},
		{fmt.Sprintf(`SELECT obj_no, scheme || '=' || value FROM "s_%d".stage_ident ORDER BY 1, 2`, k), "i"},
	} {
		rs, err := s.e.admin.QueryContext(s.e.ctx, q.query)
		if err != nil {
			t.Fatal(err)
		}
		for rs.Next() {
			var no int64
			var v string
			if err := rs.Scan(&no, &v); err != nil {
				t.Fatal(err)
			}
			o := out[byNo[no]]
			if q.kind == "v" {
				o.Volatile = append(o.Volatile, v)
			} else {
				o.Idents = append(o.Idents, v)
			}
			out[byNo[no]] = o
		}
		if err := rs.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rs.Close()
	}
	return out
}

// rcpText is the whole of what the computation made, in one text, in the order of the ids.
func rcpText(m map[string]rcpObject) string {
	var lines []string
	for _, o := range m {
		lines = append(lines, o.rcpText())
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// rcpProblems reads the problems of the records of the kinds the computation makes, as dataset/key/feed/kind/field.
func rcpProblems(s *rdfSetup, t *testing.T) []string {
	t.Helper()
	k := s.ref.KGNo
	got := expList(s.e, t, fmt.Sprintf(`SELECT d.dataset_id || '/' || c.key || '/' || p.def_no || '/' || p.kind || '/' || p.field FROM "s_%[1]d".calc_problem p
		JOIN "s_%[1]d".calc_record c ON c.rid = p.rid JOIN source_dataset d ON d.dataset_no = c.dataset_no WHERE p.kind IN ('%[2]s', '%[3]s') ORDER BY 1`,
		k, pgschema.ProblemSourceConflict, pgschema.ProblemInvalidValue))
	return got
}

// rcpWorld is a KG with one run done up to the end of its expansion: the second run of the world of the tests of the expansion.
func rcpWorld(t *testing.T, size int) *expWorld {
	t.Helper()
	w := expMakeWorld(t, pgiMappingJSON, size, false)
	rcpExpand(w.s, t, w.l, false)
	return w
}
