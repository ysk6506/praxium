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

// The tests of the expansion (ADR-0114). Every helper, type and field they add starts with exp, so that none meets a name of another
// test file of this folder.

// expRetiringMapping is the mapping of the places, servers and virtual machines in which the feeds of the racks and of the servers
// retire the objects they make.
func expRetiringMapping(t *testing.T) string {
	t.Helper()
	m := strings.Replace(pgiMappingJSON, `"object_type":"Rack","creates_objects":true,`, `"object_type":"Rack","creates_objects":true,"retires_objects":true,`, 1)
	m = strings.Replace(m, `"object_type":"PhysicalServer","creates_objects":true,`, `"object_type":"PhysicalServer","creates_objects":true,"retires_objects":true,`, 1)
	if strings.Count(m, "retires_objects") != 2 {
		t.Fatal("the mapping of the test was not made")
	}
	return m
}

// The link types the fixtures of the tests use (numbers of no model type): a type that holds the start of its links to one, one that
// holds the end to one, and one that holds neither.
const (
	expTypeFromOne = 7001
	expTypeToOne   = 7002
	expTypeFree    = 7003
)

func expCards() []ingest.ExpandCard {
	return []ingest.ExpandCard{{LinkTypeNo: expTypeFromOne, FromOne: true}, {LinkTypeNo: expTypeToOne, ToOne: true}}
}

// expApply does to the registry, the index and the references what the later steps will do with what the run made, so that the next run
// finds it: the new objects and keys, the references of the records the run read (the ones it read again are written over), the keys of
// the ends of the links, and the retirement of the candidates and the return of the objects that are bound again.
func expApply(s *rdfSetup, t *testing.T) {
	t.Helper()
	k := s.ref.KGNo
	for _, q := range []string{
		`INSERT INTO "s_%[1]d".object (obj_no, ref, type_no, name, min_grade, seq_from) SELECT obj_no, ref, type_no, ref, 0, 1 FROM "s_%[1]d".calc_newobj`,
		`INSERT INTO "s_%[1]d".object_key (type_no, ctx_no, sig_hash, obj_no, bound_seq) SELECT type_no, ctx_no, sig_hash, obj_no, 1 FROM "s_%[1]d".calc_newkey`,
		`DELETE FROM "s_%[1]d".record_ref r USING "s_%[1]d".calc_record c WHERE c.why <> 2 AND c.src_no = r.src_no AND c.dataset_no = r.dataset_no AND c.key = r.key`,
		`INSERT INTO "s_%[1]d".record_ref (src_no, dataset_no, key, role, def_no, sub, lookup_hash, target_no, state, retirable)
			SELECT c.src_no, c.dataset_no, c.key, r.role, r.def_no, r.sub, r.lookup_hash, r.target_no, r.state, r.retirable FROM "s_%[1]d".calc_ref r JOIN "s_%[1]d".calc_record c ON c.rid = r.rid
			ON CONFLICT (src_no, dataset_no, key, role, def_no, sub) DO UPDATE SET target_no = excluded.target_no, state = excluded.state, retirable = excluded.retirable`,
		`INSERT INTO "s_%[1]d".record_ref (src_no, dataset_no, key, role, def_no, sub, lookup_hash, target_no, state, retirable)
			SELECT c.src_no, c.dataset_no, c.key, e.side, e.def_no, 0, e.lookup_hash, NULL, 0, false FROM "s_%[1]d".calc_end e JOIN "s_%[1]d".calc_record c ON c.rid = e.rid ON CONFLICT DO NOTHING`,
		`UPDATE "s_%[1]d".object SET retired_seq = 1 WHERE obj_no IN (SELECT obj_no FROM "s_%[1]d".calc_object WHERE why = 4)`,
		`UPDATE "s_%[1]d".object SET retired_seq = NULL WHERE obj_no IN (SELECT obj_no FROM "s_%[1]d".calc_object WHERE why = 1)`,
	} {
		s.e.exec(t, fmt.Sprintf(q, k))
	}
}

// expList reads a column of text of a query.
func expList(e *pgiEnv, t *testing.T, query string) []string {
	t.Helper()
	rows, err := e.admin.QueryContext(e.ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// expObjOf is the number of the object a record is bound to by the references that are kept.
func expObjOf(s *rdfSetup, t *testing.T, dataset, key string) int64 {
	t.Helper()
	return int64(s.e.count(t, fmt.Sprintf(`SELECT target_no FROM "s_%d".record_ref WHERE dataset_no = $1 AND key = $2 AND role = 1`, s.ref.KGNo), s.rdfNo(dataset), key))
}

// expRefOf is the id of the object of a record.
func expRefOf(s *rdfSetup, t *testing.T, dataset, key string) string {
	t.Helper()
	return s.e.text(t, fmt.Sprintf(`SELECT o.ref FROM "s_%d".object o WHERE o.obj_no = $1`, s.ref.KGNo), expObjOf(s, t, dataset, key))
}

// expPutLink puts a link to the KG, and expPutFrom the reference of a record to the link it made.
func expPutLink(s *rdfSetup, t *testing.T, no int64, typ int, src, dst int64) {
	t.Helper()
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".link (link_no, ref, type_no, src, dst, grade, basis, content_hash, row_hash, seq_from) VALUES ($1, $2, $3, $4, $5, 'declared', 'declared', '\x00', '\x00', 1)`, s.ref.KGNo),
		no, fmt.Sprintf("link-%d", no), typ, src, dst)
}

func expPutFrom(s *rdfSetup, t *testing.T, dataset, key string, link int64) {
	t.Helper()
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_ref (src_no, dataset_no, key, role, def_no, sub, state, target_no, retirable) VALUES ($1, $2, $3, %d, 0, 0, %d, $4, false)`,
		s.ref.KGNo, pgschema.RefRoleLinkFrom, pgschema.RefResolved), s.ref.SrcNo, s.rdfNo(dataset), key, link)
}

// expSnap is what the expansion made of the set of the run: the objects with the reason each is in it, by id, and the records that it
// made records of the run (the ones with the reason it gives), by dataset and key.
type expSnap struct {
	Objects map[string]int16
	Expands []string
}

func expSnapOf(s *rdfSetup, t *testing.T) expSnap {
	t.Helper()
	out := expSnap{Objects: map[string]int16{}}
	k := s.ref.KGNo
	rows, err := s.e.admin.QueryContext(s.e.ctx, fmt.Sprintf(`SELECT COALESCE(o.ref, n.ref), c.why FROM "s_%[1]d".calc_object c LEFT JOIN "s_%[1]d".object o ON o.obj_no = c.obj_no
		LEFT JOIN "s_%[1]d".calc_newobj n ON n.obj_no = c.obj_no`, k))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var ref string
		var why int16
		if err := rows.Scan(&ref, &why); err != nil {
			t.Fatal(err)
		}
		out.Objects[ref] = why
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, r := range s.rdfRecords(t) {
		if r.Why == pgschema.CalcWhyExpanded {
			out.Expands = append(out.Expands, r.Dataset+"/"+r.Key)
		}
	}
	sort.Strings(out.Expands)
	return out
}

func (a expSnap) expText() string {
	var objs []string
	for ref, why := range a.Objects {
		objs = append(objs, fmt.Sprintf("%s:%d", ref, why))
	}
	sort.Strings(objs)
	return fmt.Sprintf("objects %v, records %v", objs, a.Expands)
}

func (a expSnap) expSame(b expSnap) bool { return a.expText() == b.expText() }

// expReset puts the run back to the end of the identification: the objects and the records the expansion added are taken away and the
// place of the expansion is forgotten (a test does what the code that runs never does, to run the expansion again on the same run).
func expReset(s *rdfSetup, t *testing.T, l ingest.RunLease) {
	t.Helper()
	k := s.ref.KGNo
	s.e.exec(t, fmt.Sprintf(`DELETE FROM "s_%d".calc_object WHERE why <> %d`, k, pgschema.ObjectWhyIdentified))
	s.e.exec(t, fmt.Sprintf(`DELETE FROM "s_%d".%s`, k, pgschema.TableCalcXKey))
	s.e.exec(t, fmt.Sprintf(`DELETE FROM "s_%d".calc_record WHERE why = %d`, k, pgschema.CalcWhyExpanded))
	s.e.exec(t, `UPDATE sync_run SET calc_job = $2, calc_part = 0, counts = counts - 'expand' WHERE run_no = $1`, l.No, pgschema.CalcJobIdentify)
}

// expWorld is a KG that has had a run (the places, the servers and the virtual machines) and has a second run whose records are
// received, compared and identified, and the fixtures the rules of the expansion each have a case in.
type expWorld struct {
	s   *rdfSetup
	l   ingest.RunLease
	old map[string]string
	// ids of the objects the cases are made of
	obj map[string]int64
}

const (
	expServers1 = "asset_id,serial,manufacturer,rack_tag,power_state\nSV1,sn1,Zorvane,RK1,on\nSV2,sn2,Zorvane,RK2,off\nSV3,sn3,Zorvane,RK7,on\n"
	expServers2 = "asset_id,serial,manufacturer,rack_tag,power_state\nSV1,sn1x,Zorvane,RK1,on\nSV2,sn2,Zorvane,RK2,off\nSV3,sn3,Zorvane,RK7,on\n"
	expPlaces2  = "row_id,site,building,floor,room,rack,tag\n1,HQ,B1,F1,R1,rack a,RK1\n2,HQ,B1,F1,R1,rack b,RK7\n3,HQ,B1,F2,R2,rack z,RK9\n"
	expVMs2     = "{\"uuid\":\"u-1\",\"name\":\"vm uno\"}\n{\"uuid\":\"u-2\",\"name\":\"vm two\"}\n"
)

// expTexts are the datasets of the second run, with the rows of each in the order given (reversed, or as they are).
func expTexts(reverse bool) pgiTexts {
	texts := pgiTexts{"ds_places": expPlaces2, "ds_servers": expServers2, "ds_vms": expVMs2}
	if !reverse {
		return texts
	}
	for name, text := range texts {
		lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		head := []string{}
		if name != "ds_vms" {
			head, lines = lines[:1], lines[1:]
		}
		for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
			lines[i], lines[j] = lines[j], lines[i]
		}
		texts[name] = strings.Join(append(head, lines...), "\n") + "\n"
	}
	return texts
}

// expMakeWorld does the first run, puts the fixtures, and does the second run up to the end of the identification. The cases are:
//
//	retire_out: a link of the type that holds nothing, that starts at the server whose serial changes (so its object is left), made by X1
//	retire_in:  such a link that ends at the rack c, whose tag changes (a rack is identified by its tag here, so a new object is made), made by X2
//	key_old:    the server SV2 reads the tag the rack b had; key_new: the server SV3 reads the tag the rack b has now (the objects of the
//	            racks b and c are left, and the servers the links of whose ends are not touched are read again for the keys)
//	limit_out:  a link of the type that holds the start to one, starting at the object of the first virtual machine, made by X3
//	limit_in:   a link of the type that holds the end to one, ending at the object of the first virtual machine, made by X4
//	claim:      the record X5 claims the identifier the first virtual machine claims, for the object of the server SV2
func expMakeWorld(t *testing.T, mapping string, size int, reverse bool) *expWorld {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, mapping))
	l1, _ := idnPipeline(s, t, pgiTexts{"ds_places": pgiPlaces, "ds_servers": expServers1, "ds_vms": pgiVMs}, rdfT0, 0)
	expApply(s, t)
	s.rdfSettle(t, l1)
	w := &expWorld{s: s, old: map[string]string{}, obj: map[string]int64{}}
	for _, r := range []struct{ ds, key string }{{"ds_servers", "SV1"}, {"ds_servers", "SV2"}, {"ds_places", "2"}, {"ds_places", "3"}, {"ds_vms", "u-1"}} {
		w.old[r.key] = expRefOf(s, t, r.ds, r.key)
		w.obj[r.key] = expObjOf(s, t, r.ds, r.key)
	}
	for _, x := range []string{"X1", "X2", "X3", "X4", "X5"} {
		s.rdfPutRaw(t, rdfRaw{dataset: "ds_servers", key: x, content: x})
	}
	// the records are kept under the declaration the dataset has, as the records of a run are (the computation of everything reads only those)
	s.e.exec(t, fmt.Sprintf(`UPDATE %s r SET decl_rev = d.decl_rev FROM source_dataset d WHERE d.dataset_no = r.dataset_no AND r.key IN ('X1', 'X2', 'X3', 'X4', 'X5')`, pgiTable(s.ref, pgschema.TableRawRecord)))
	expPutLink(s, t, 9003, expTypeFree, w.obj["SV1"], w.obj["SV2"])
	expPutFrom(s, t, "ds_servers", "X1", 9003)
	expPutLink(s, t, 9005, expTypeFree, w.obj["SV2"], w.obj["3"])
	expPutFrom(s, t, "ds_servers", "X2", 9005)
	expPutLink(s, t, 9006, expTypeFromOne, w.obj["u-1"], w.obj["SV2"])
	expPutFrom(s, t, "ds_servers", "X3", 9006)
	expPutLink(s, t, 9008, expTypeToOne, w.obj["SV2"], w.obj["u-1"])
	expPutFrom(s, t, "ds_servers", "X4", 9008)
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".record_ref (src_no, dataset_no, key, role, def_no, sub, lookup_hash, target_no, state, retirable)
		SELECT src_no, dataset_no, 'X5', role, def_no, sub, lookup_hash, $2, state, false FROM "s_%[1]d".record_ref WHERE dataset_no = $1 AND key = 'u-1' AND role = %[2]d`, s.ref.KGNo, pgschema.RefRoleClaim),
		s.rdfNo("ds_vms"), w.obj["SV2"])
	w.l, _ = idnPipeline(s, t, expTexts(reverse), rdfT0.Add(time.Hour), size)
	return w
}

// expRun does the expansion of the second run from its start, leaving the rules named out.
func (w *expWorld) expRun(t *testing.T, size int, full bool, skip ...string) (ingest.ExpandCounts, expSnap) {
	t.Helper()
	expReset(w.s, t, w.l)
	w.s.e.st.expTest = &expHook{Skip: func(rule string) bool {
		for _, r := range skip {
			if r == rule {
				return true
			}
		}
		return false
	}}
	counts, err := ingest.RunExpand(w.s.e.ctx, w.s.e.st, w.l, ingest.ExpandStart{Full: full, Rows: size, Cards: expCards(), Feeds: expFeeds(w.s)})
	if err != nil {
		t.Fatal(err)
	}
	return counts, expSnapOf(w.s, t)
}
