package pgingest

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the computation of the links (ADR-0122). Every helper, type and constant they add starts with rcl, so that none meets a
// name of another test file of this folder, and none of them is a method of a type of another file.

// The world: the first source has racks, servers (a server that is held by a rule, and two records that stand for one server), and virtual
// machines; the second source finds the racks and the servers the first one made and says where more servers are mounted.
// The key a mount finds its rack by (tag) is not the identity of the rack (rack_id), so that two racks can have one tag.
const rclSourceA = `{"id":"src_rcl_a","name":"rcl a","adapter":"file","datasets":[
 {"id":"ds_rk","parser":"csv","natural_key":["rack_id"],"columns":[{"name":"rack_id"},{"name":"tag"}]},
 {"id":"ds_sv","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"mfr"},{"name":"serial"},{"name":"rack_tag"},{"name":"pos"},{"name":"flag"}]},
 {"id":"ds_vm","parser":"csv","natural_key":["uuid"],"columns":[{"name":"uuid"}]}]}`

const rclMappingA = `{"refinements":[{"id":"r_sv","input":"ds_sv","ops":[{"op":"filter","column":"flag","in":["on"]}]}],
"feeds":[
 {"id":"f_rk","dataset":"ds_rk","object_type":"Rack","creates_objects":true,"basis":"declared",
  "attributes":[{"attribute":"name","column":"rack_id"},{"attribute":"asset_tag","column":"rack_id"}],
  "identity":[{"components":[{"property":"asset_tag","column":"rack_id"}],"priority":1}]},
 {"id":"f_sv","dataset":"r_sv","object_type":"PhysicalServer","creates_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"asset_id"}],
  "identity":[{"components":[{"property":"manufacturer","column":"mfr"},{"property":"serial_number","column":"serial"}],"priority":1}]},
 {"id":"f_vm","dataset":"ds_vm","object_type":"VirtualMachine","creates_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"uuid"}],
  "identity":[{"scheme":"vm_instance_uuid","column":"uuid","priority":1}]}],
"links":[{"id":"l_mounted","link_type":"MOUNTED_IN","from":{"feed":"f_sv","columns":["rack_tag"]},"to":{"feed":"f_rk","columns":["tag"]},"condition":"same_key","basis":"declared",
  "attributes":[{"attribute":"position_u","column":"pos"},{"attribute":"face","value":"front"}]}]}`

const rclSourceB = `{"id":"src_rcl_b","name":"rcl b","adapter":"file","datasets":[
 {"id":"ds_rkb","parser":"csv","natural_key":["rack_id"],"columns":[{"name":"rack_id"},{"name":"tag"}]},
 {"id":"ds_mt","parser":"csv","natural_key":["mount_id"],"columns":[{"name":"mount_id"},{"name":"mfr"},{"name":"serial"},{"name":"rack_tag"},{"name":"pos"}]}]}`

// rclMappingBOwn is the second source with racks and servers of its own (its feeds make objects): the old pipeline keeps the identities of
// a source apart from the other sources', so this is the second source the old pipeline can be held to.
const rclMappingBOwn = `{"feeds":[
 {"id":"f_rkb","dataset":"ds_rkb","object_type":"Rack","creates_objects":true,"basis":"declared",
  "attributes":[{"attribute":"name","column":"rack_id"},{"attribute":"asset_tag","column":"rack_id"}],
  "identity":[{"components":[{"property":"asset_tag","column":"rack_id"}],"priority":1}]},
 {"id":"f_mt","dataset":"ds_mt","object_type":"PhysicalServer","creates_objects":true,"basis":"observed","attributes":[{"attribute":"name","column":"mount_id"}],
  "identity":[{"components":[{"property":"manufacturer","column":"mfr"},{"property":"serial_number","column":"serial"}],"priority":1}]}],
"links":[{"id":"l_mount_b","link_type":"MOUNTED_IN","from":{"feed":"f_mt","columns":["rack_tag"]},"to":{"feed":"f_rkb","columns":["tag"]},"condition":"same_key","basis":"declared",
  "attributes":[{"attribute":"position_u","column":"pos"}]}]}`

// the second source finds the racks and the servers the first one made (its feeds make no object) and says where more servers are mounted
const rclMappingB = `{"feeds":[
 {"id":"f_rkb","dataset":"ds_rkb","object_type":"Rack","basis":"declared","attributes":[],
  "identity":[{"components":[{"property":"asset_tag","column":"rack_id"}],"priority":1}]},
 {"id":"f_mt","dataset":"ds_mt","object_type":"PhysicalServer","basis":"observed","attributes":[],
  "identity":[{"components":[{"property":"manufacturer","column":"mfr"},{"property":"serial_number","column":"serial"}],"priority":1}]}],
"links":[{"id":"l_mount_b","link_type":"MOUNTED_IN","from":{"feed":"f_mt","columns":["rack_tag"]},"to":{"feed":"f_rkb","columns":["tag"]},"condition":"same_key","basis":"declared",
  "attributes":[{"attribute":"position_u","column":"pos"}]}]}`

// rclTextsA are the rows of the datasets of the first source, as "rack_id,tag" and "asset_id,mfr,serial,rack_tag,pos,flag" and "uuid".
func rclTextsA(racks, servers, vms []string) pgiTexts {
	return pgiTexts{
		"ds_rk": "rack_id,tag\n" + idnJoin(racks...),
		"ds_sv": "asset_id,mfr,serial,rack_tag,pos,flag\n" + idnJoin(servers...),
		"ds_vm": "uuid\n" + idnJoin(vms...),
	}
}

func rclTextsB(racks, mounts []string) pgiTexts {
	return pgiTexts{"ds_rkb": "rack_id,tag\n" + idnJoin(racks...), "ds_mt": "mount_id,mfr,serial,rack_tag,pos\n" + idnJoin(mounts...)}
}

// rclMaker is the computer of the links of the plans.
func rclMaker(t *testing.T, s *rdfSetup, plans map[int64]*ingest.Plan) *ingest.LinkComputer {
	t.Helper()
	return ingest.NewLinkComputer(plans, rcpHasher(t, s))
}

// rclStart is the start of the computation of the links of the plans, with the size of a page given (0 is the default).
func rclStart(plans map[int64]*ingest.Plan, lines int) ingest.RecomputeLinksStart {
	return ingest.RecomputeLinksStart{Lines: lines, Defs: ingest.RecomputeLinkDefsOf(plans)}
}

// rclObjects does the computation of the objects of a run whose expansion is done.
func rclObjects(s *rdfSetup, t *testing.T, l ingest.RunLease, plans map[int64]*ingest.Plan, at string, objectMax int) {
	t.Helper()
	start := ingest.RecomputeStart{Seen: at, Schemes: s.plan.SchemeNumbers(), ObjectMax: objectMax}
	if _, err := ingest.RunRecompute(s.e.ctx, s.e.st, l, start, ingest.NewObjectComputer(plans, rcpHasher(t, s), start)); err != nil {
		t.Fatal(err)
	}
}

// rclDo does the computation of the links of a run whose objects are done, from where it stands.
func rclDo(s *rdfSetup, t *testing.T, l ingest.RunLease, plans map[int64]*ingest.Plan, lines int) ingest.RecomputeLinksCounts {
	t.Helper()
	counts, err := ingest.RunRecomputeLinks(s.e.ctx, s.e.st, l, rclStart(plans, lines), rclMaker(t, s, plans))
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

// rclPlans is the plans of a setup.
func rclPlans(s *rdfSetup) map[int64]*ingest.Plan { return map[int64]*ingest.Plan{s.ref.SrcNo: s.plan} }

// rclLink is a row of stage_link as the tests compare them: by the id, without the number the run gave the link.
type rclLink struct {
	Ref, Src, Dst, Grade, Basis, Doc, SourceRefs, Content, Row string
	Type                                                       int
	No                                                         int64
}

func (l rclLink) rclText() string {
	return fmt.Sprintf("%s type=%d %s>%s grade=%s basis=%s doc=%s refs=%s content=%s row=%s", l.Ref, l.Type, l.Src, l.Dst, l.Grade, l.Basis, l.Doc, l.SourceRefs, l.Content, l.Row)
}

// rclStaged reads what the computation of the links made, by the id of the link.
func rclStaged(s *rdfSetup, t *testing.T) map[string]rclLink {
	t.Helper()
	k := s.ref.KGNo
	rows, err := s.e.admin.QueryContext(s.e.ctx, fmt.Sprintf(`SELECT l.link_no, l.ref, l.type_no, COALESCE(so.ref, sn.ref, ''), COALESCE(d.ref, dn.ref, ''), l.grade, l.basis, l.doc::text, l.source_refs::text,
		encode(l.content_hash, 'hex'), encode(l.row_hash, 'hex') FROM "s_%[1]d".stage_link l
		LEFT JOIN "s_%[1]d".object so ON so.obj_no = l.src LEFT JOIN "s_%[1]d".calc_newobj sn ON sn.obj_no = l.src
		LEFT JOIN "s_%[1]d".object d ON d.obj_no = l.dst LEFT JOIN "s_%[1]d".calc_newobj dn ON dn.obj_no = l.dst`, k))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]rclLink{}
	for rows.Next() {
		var l rclLink
		if err := rows.Scan(&l.No, &l.Ref, &l.Type, &l.Src, &l.Dst, &l.Grade, &l.Basis, &l.Doc, &l.SourceRefs, &l.Content, &l.Row); err != nil {
			t.Fatal(err)
		}
		out[l.Ref] = l
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// rclText is the whole of what the computation made, without the numbers, in the order of the ids.
func rclText(m map[string]rclLink) string {
	var lines []string
	for _, l := range m {
		lines = append(lines, l.rclText())
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// rclKinds are the problems of the links the old pipeline reports, by its names: the new flow gives the third one as source_conflict, a
// reason that begins with "the link " (ADR-0122).
var rclKinds = []string{pgschema.ProblemLinkTarget, pgschema.ProblemLinkAmbiguous, ingest.ProblemLinkConflict}

// rclConflictReason is how the reason of a conflict of a link begins.
const rclConflictReason = "the link "

// rclProblems reads the problems of the records of the kinds the computation of the links makes, as kind/dataset/key, with the name the
// old pipeline gives the conflict of a link.
func rclProblems(s *rdfSetup, t *testing.T) []string {
	t.Helper()
	k := s.ref.KGNo
	return expList(s.e, t, fmt.Sprintf(`SELECT CASE WHEN p.kind = '%[3]s' THEN '%[4]s' ELSE p.kind END || '/' || d.dataset_id || '/' || c.key FROM "s_%[1]d".calc_problem p
		JOIN "s_%[1]d".calc_record c ON c.rid = p.rid JOIN source_dataset d ON d.dataset_no = c.dataset_no
		WHERE p.kind IN ('%[2]s', '%[5]s') OR (p.kind = '%[3]s' AND p.reason LIKE '%[6]s%%') ORDER BY 1`,
		k, rclKinds[0], pgschema.ProblemSourceConflict, rclKinds[2], rclKinds[1], rclConflictReason))
}

// rclRefs reads the references of the ends of the links of the run, as dataset/key/role/state/target, where the target of a link is its id.
func rclRefs(s *rdfSetup, t *testing.T) []string {
	t.Helper()
	k := s.ref.KGNo
	return expList(s.e, t, fmt.Sprintf(`SELECT d.dataset_id || '/' || c.key || '/' || r.role || '/' || r.state || '/' || COALESCE(l.ref, o.ref, n.ref, '-') FROM "s_%[1]d".calc_ref r
		JOIN "s_%[1]d".calc_record c ON c.rid = r.rid JOIN source_dataset d ON d.dataset_no = c.dataset_no
		LEFT JOIN "s_%[1]d".stage_link l ON r.role = %[2]d AND l.link_no = r.target_no
		LEFT JOIN "s_%[1]d".object o ON r.role = %[3]d AND o.obj_no = r.target_no LEFT JOIN "s_%[1]d".calc_newobj n ON r.role = %[3]d AND n.obj_no = r.target_no
		WHERE r.role IN (%[2]d, %[3]d) ORDER BY 1`, k, pgschema.RefRoleLinkFrom, pgschema.RefRoleLinkTo))
}

// rclKnown is the first source and the second one put into a KG, with the setup of each and the plans of both.
type rclKnown struct {
	a, b  *rdfSetup
	plans map[int64]*ingest.Plan
}

// rclTwo is a KG with the two sources attached, the second one with the mapping given.
func rclTwo(t *testing.T, mappingB string) rclKnown {
	t.Helper()
	a := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	planB := pgiPlan(t, rclSourceB, mappingB)
	b := &rdfSetup{e: a.e, kg: a.kg, plan: planB, ref: a.e.attach(t, a.kg, planB)}
	return rclKnown{a: a, b: b, plans: map[int64]*ingest.Plan{a.ref.SrcNo: a.plan, b.ref.SrcNo: b.plan}}
}

// rclPrepare does the first steps of a run of a source whose texts are given, up to the end of the computation of the objects, and says
// whether the whole of the source is expanded (full).
func rclPrepare(s *rdfSetup, t *testing.T, texts pgiTexts, at time.Time, full bool) ingest.RunLease {
	t.Helper()
	return rclPrepareMax(s, t, texts, at, full, 0)
}

// rclPrepareMax is rclPrepare with the most lines an object may have (0 is the default).
func rclPrepareMax(s *rdfSetup, t *testing.T, texts pgiTexts, at time.Time, full bool, objectMax int) ingest.RunLease {
	t.Helper()
	l, _ := idnPipeline(s, t, texts, at, 0)
	rcpExpand(s, t, l, full)
	rclObjects(s, t, l, rclPlans(s), at.UTC().Format(time.RFC3339), objectMax)
	return l
}

// rclTagIdentity is the mapping of the first source with a rack that is known by its tag, so that two records of one tag are one object.
func rclTagIdentity() string {
	m := strings.Replace(rclMappingA, `{"attribute":"asset_tag","column":"rack_id"}`, `{"attribute":"asset_tag","column":"tag"}`, 1)
	return strings.Replace(m, `{"property":"asset_tag","column":"rack_id"}`, `{"property":"asset_tag","column":"tag"}`, 1)
}
