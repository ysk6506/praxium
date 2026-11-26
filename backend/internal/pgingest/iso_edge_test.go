package pgingest

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The cases of the three checks of the fifth step (ADR-0123), each made in SQL so that what each case needs is where the case says.

// isoIdentWorld puts the cases of the identifiers, the objects in the order given or the reverse one.
func isoIdentWorld(t *testing.T, reverse bool) *isoFix {
	t.Helper()
	f := isoNewFix(t)
	objs := []struct{ ref, where string }{
		{"obj-a", "both"}, {"obj-b", "new"}, {"obj-c", "kg"}, {"obj-d", "both"}, {"obj-e", "retiring"}, {"obj-f", "both"}, {"obj-g", "left"},
		{"obj-h", "both"}, {"obj-r", "retired"},
	}
	for i := range objs {
		o := objs[i]
		if reverse {
			o = objs[len(objs)-1-i]
		}
		f.isoObj(t, o.ref, f.m.server, o.where)
	}
	// v1: two objects of the run, and no object of the KG: the smaller id keeps it
	f.isoIdent(t, "obj-a", "v1", "ka1", true, false, "")
	f.isoIdent(t, "obj-b", "v1", "kb1", true, false, "")
	// v2: an object the run does not touch has it in the KG
	f.isoIdent(t, "obj-c", "v2", "", false, true, "")
	f.isoIdent(t, "obj-d", "v2", "kd2", true, false, "")
	// v3: the object the KG has it with retires, so it is free
	f.isoIdent(t, "obj-e", "v3", "", false, true, "")
	f.isoIdent(t, "obj-f", "v3", "kf3", true, false, "")
	// v4: the object the KG has it with is computed without it (its value changed), so it is free
	f.isoIdent(t, "obj-d", "v4", "", false, true, "")
	f.isoIdent(t, "obj-a", "v4", "ka4", true, false, "")
	// v5: one object, two records that claim it: no other object has it
	f.isoIdent(t, "obj-f", "v5", "kf5", true, false, "")
	f.isoIdent(t, "obj-f", "v5x", "kf5x", true, false, "")
	// v6: an object the computation left keeps what it has
	f.isoIdent(t, "obj-g", "v6", "", false, true, "")
	f.isoIdent(t, "obj-b", "v6", "kb6", true, false, "")
	// v7: the object the KG has it with keeps it and is of the run, and an object of a smaller id says it too
	f.isoIdent(t, "obj-h", "v7", "kh7", true, true, "")
	f.isoIdent(t, "obj-a", "v7", "ka7", true, false, "")
	// v8: the line of an object that a record the run reads again only claims (its reference is kept), against the KG's object
	f.isoIdent(t, "obj-c", "v8", "", false, true, "")
	f.isoIdent(t, "obj-b", "v8", "", true, false, "kb8")
	// v9: the object the KG has it with is retired (its line is still open): it is no object any more
	f.isoIdent(t, "obj-r", "v9", "", false, true, "")
	f.isoIdent(t, "obj-h", "v9", "kh9", true, false, "")
	// v5 and v5x are one object's: the same claims twice are one line
	f.s.e.exec(t, f.isoQ(`DELETE FROM {k}.stage_ident WHERE value = 'v5x'`))
	f.s.e.exec(t, f.isoQ(`UPDATE {k}.calc_ref SET lookup_hash = $1 WHERE lookup_hash = $2`), isoHash(t, "v5"), isoHash(t, "v5x"))
	f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.stage_ident (obj_no, scheme, value, scope) VALUES ($1, $2, 'v5', '') ON CONFLICT DO NOTHING`), f.no["obj-f"], isoScheme)
	return f
}

// [REQ-847] [REQ-855]
func TestIsoAnIdentifierIsKeptByTheObjectTheKGHasItWithElseTheSmallestIDAndTheOthersLoseTheLineOnly(t *testing.T) {
	f := isoIdentWorld(t, false)
	counts := f.isoRun(t, 0)
	taken := strings.Join(isoTaken(f.s, t), "\n")
	want := strings.Join([]string{
		"1/1/obj-b is.serial=v1", "1/2/obj-a is.serial=v7", "1/2/obj-b is.serial=v6", "1/2/obj-b is.serial=v8", "1/2/obj-d is.serial=v2",
	}, "\n")
	if taken != want {
		t.Errorf("taken out:\n%s\nwant\n%s", taken, want)
	}
	if got := strings.Join(isoProblems(f.s, t), " "); got != "identity_conflict/ds_sv/ka7 identity_conflict/ds_sv/kb1 identity_conflict/ds_sv/kb6 identity_conflict/ds_sv/kb8 identity_conflict/ds_sv/kd2" {
		t.Errorf("the problems: %s", got)
	}
	refs := strings.Join(f.isoRefs(t), " ")
	for _, r := range []string{"kb1/4/2/obj-b", "kd2/4/2/obj-d", "ka7/4/2/obj-a", "kb8/4/2/obj-b", "kh7/4/0/obj-h", "kf3/4/0/obj-f", "ka4/4/0/obj-a", "kh9/4/0/obj-h", "kf5/4/0/obj-f", "kf5x/4/0/obj-f"} {
		if !strings.Contains(refs, r) {
			t.Errorf("no reference %s in %s", r, refs)
		}
	}
	if counts.Idents != 5 || counts.Refs != 5 || counts.Problems != 5 || counts.Keys != 5 {
		t.Errorf("the counts: %+v", counts)
	}
	// the objects stay: only the lines are taken out
	if n := f.s.e.count(t, f.isoQ(`SELECT count(*) FROM {k}.stage_object`)); n != 5 {
		t.Errorf("%d objects are staged", n)
	}
	if broken := isoBroken(f.s, t); len(broken) != 0 {
		t.Errorf("after the step: %v", broken)
	}
}

// isoEndsWorld puts the cases of the ends of the links.
func isoEndsWorld(t *testing.T) *isoFix {
	t.Helper()
	f := isoNewFix(t)
	sv, rk := f.m.server, f.m.rack
	for _, o := range []struct {
		ref, where string
		typ        int
	}{
		{"s1", "both", sv}, {"s2", "new", sv}, {"s3", "both", sv}, {"s4", "both", sv}, {"s5", "both", sv}, {"s6", "retired", sv}, {"s7", "both", sv}, {"s8", "both", sv},
		{"s9", "retired", sv}, {"r1", "kg", rk}, {"r2", "retiring", rk}, {"r3", "retired", rk}, {"r4", "left", rk},
	} {
		f.isoObj(t, o.ref, o.typ, o.where)
	}
	f.no["gone"] = 999 // an object the run made and took back: no table has it
	// s9 is retired and is made again by the run: it is there
	f.s.e.exec(t, f.isoQ(`INSERT INTO {k}.stage_object (obj_no, ref, type_no, name, min_grade, doc, content_hash, row_hash, is_new) VALUES ($1, 's9', $2, 's9', 0, '{}', '\x00', '\x00', false)`), f.no["s9"], sv)
	mt := f.m.mounted
	f.isoLink(t, "l1", mt, "s1", "r1", "k1", true, false, false) // both ends there
	f.isoLink(t, "l2", mt, "s2", "r1", "k2", true, false, false) // the start is a new object of the run
	f.isoLink(t, "l3", mt, "s3", "r2", "k3", true, true, false)  // the end retires (the KG has the link too)
	f.isoLink(t, "l4", mt, "s4", "r3", "k4", true, false, false) // the end is retired
	f.isoLink(t, "l5", mt, "s5", "r4", "k5", true, false, false) // the end is an object the computation left: it stays
	f.isoLink(t, "l6", mt, "s6", "s6", "k6", true, false, false) // both ends are one object, which is retired
	f.isoLink(t, "l7", mt, "s7", "gone", "k7", true, false, false)
	f.isoLink(t, "l8", mt, "s8", "r3", "k8", true, false, true) // a record a rule filtered out: no problem
	f.isoLink(t, "l9", mt, "s9", "r1", "k9", true, false, false)
	return f
}

// [REQ-848] [REQ-855]
func TestIsoALinkWhoseEndIsNotThereIsTakenOutAndOneWhoseEndRetiresGoesWithItAndIsNoProblem(t *testing.T) {
	f := isoEndsWorld(t)
	mt := f.m.mounted
	counts := f.isoRun(t, 2)
	if got := strings.Join(isoTaken(f.s, t), " "); got != "2/3/l4 2/3/l6 2/3/l7 2/3/l8 2/4/l3" {
		t.Errorf("taken out: %s", got)
	}
	if got := strings.Join(isoProblems(f.s, t), " "); got != "link_target_missing/ds_sv/k4 link_target_missing/ds_sv/k6 link_target_missing/ds_sv/k7" {
		t.Errorf("the problems: %s", got)
	}
	refs := strings.Join(f.isoRefs(t), " ")
	for _, r := range []string{"k1/2/0/", "k3/2/1/-", "k4/2/1/-", "k6/2/1/-", "k7/2/1/-", "k8/2/3/-", "k9/2/0/"} {
		if !strings.Contains(refs, r) {
			t.Errorf("no reference %s in %s", r, refs)
		}
	}
	if counts.Missing != 4 || counts.Retiring != 1 || counts.Problems != 3 || counts.Refs != 5 {
		t.Errorf("the counts: %+v", counts)
	}
	if got := strings.Join(expList(f.s.e, t, f.isoQ(`SELECT ref FROM {k}.stage_link ORDER BY ref`)), " "); got != "l1 l2 l5 l9" {
		t.Errorf("the staged links: %s", got)
	}
	if broken := isoBroken(f.s, t, mt); len(broken) != 0 {
		t.Errorf("after the step: %v", broken)
	}
}

// isoCardWorld puts the cases of the cardinality, in the order given or the reverse one (the numbers of the objects and the links are
// given in that order).
func isoCardWorld(t *testing.T, reverse bool) *isoFix {
	t.Helper()
	f := isoNewFix(t)
	m := f.m
	objs := []struct {
		ref, where string
		typ        int
	}{
		{"s1", "both", m.server}, {"s2", "both", m.server}, {"s3", "both", m.server}, {"s4", "both", m.server}, {"s5", "both", m.server},
		{"s6", "both", m.server}, {"s7", "both", m.server}, {"s8", "both", m.server}, {"s9", "both", m.server}, {"s10", "both", m.server},
		{"r1", "kg", m.rack}, {"r2", "kg", m.rack}, {"r9", "retiring", m.rack}, {"h1", "both", m.host}, {"h2", "both", m.host}, {"h3", "both", m.host},
		{"v1", "both", m.vm}, {"v2", "both", m.vm}, {"v3", "both", m.vm},
	}
	type link struct {
		ref, src, dst, key string
		typ                int
		staged, kg, held   bool
	}
	links := []link{
		// two new links at one start: the smaller id keeps it
		{"l-10", "s1", "r1", "k10", m.mounted, true, false, false}, {"l-11", "s1", "r2", "k11", m.mounted, true, false, false},
		// the KG has one and the run stages it, and a new one of a smaller id: the KG's keeps its place
		{"l-20", "s2", "r1", "k20", m.mounted, true, true, false}, {"a-21", "s2", "r2", "k21", m.mounted, true, false, false},
		// the KG has one the run does not touch (its record is not read): it keeps its place
		{"l-30", "s3", "r1", "", m.mounted, false, true, false}, {"l-31", "s3", "r2", "k31", m.mounted, true, false, false},
		// the KG has one whose record the run reads and which the run does not make again: it goes, and the new one has the place
		{"l-40", "s4", "r1", "", m.mounted, false, true, false}, {"l-41", "s4", "r2", "k41", m.mounted, true, false, false},
		// the KG has one whose end retires: it goes with it, and the new one has the place
		{"l-50", "s5", "r9", "", m.mounted, false, true, false}, {"l-51", "s5", "r2", "k51", m.mounted, true, false, false},
		// the KG has one no record of an ingestion made: it keeps its place
		{"l-60", "s6", "r1", "", m.mounted, false, true, false}, {"l-61", "s6", "r2", "k61", m.mounted, true, false, false},
		// the KG has the link the run stages: one link, no contest
		{"l-70", "s7", "r1", "k70", m.mounted, true, true, false},
		// one to one: a host runs on one server and a server has one host
		{"r-1", "h1", "s8", "kr1", m.runsOn, true, false, false}, {"r-2", "h1", "s9", "kr2", m.runsOn, true, false, false},
		{"r-3", "h2", "s9", "kr3", m.runsOn, true, false, false}, {"r-4", "h3", "s8", "kr4", m.runsOn, true, false, false},
		// a pair of types the binding of the link type does not have: no check
		{"v-1", "v1", "v2", "kv1", m.runsOn, true, false, false}, {"v-2", "v1", "v3", "kv2", m.runsOn, true, false, false},
		// the one that loses is of a record a rule filtered out: no problem
		{"l-a0", "s10", "r1", "ka0", m.mounted, true, false, false}, {"l-a1", "s10", "r2", "ka1", m.mounted, true, false, true},
	}
	if reverse {
		for i, j := 0, len(objs)-1; i < j; i, j = i+1, j-1 {
			objs[i], objs[j] = objs[j], objs[i]
		}
		for i, j := 0, len(links)-1; i < j; i, j = i+1, j-1 {
			links[i], links[j] = links[j], links[i]
		}
	}
	for _, o := range objs {
		f.isoObj(t, o.ref, o.typ, o.where)
	}
	for _, l := range links {
		f.isoLink(t, l.ref, l.typ, l.src, l.dst, l.key, l.staged, l.kg, l.held)
	}
	f.isoKeptFrom(t, "l-30", "kk30", false)
	f.isoKeptFrom(t, "l-40", "kk40", true)
	f.isoKeptFrom(t, "l-50", "kk50", false)
	return f
}

// [REQ-849] [REQ-855]
func TestIsoAPlaceHeldToOneLinkIsKeptByTheLinkTheKGHasElseBySmallestIDAndTheOthersAreTakenOut(t *testing.T) {
	f := isoCardWorld(t, false)
	counts := f.isoRun(t, 0)
	if got := strings.Join(isoTaken(f.s, t), " "); got != "3/5/a-21 3/5/l-11 3/5/l-31 3/5/l-61 3/5/l-a1 3/5/r-2 3/5/r-4" {
		t.Errorf("taken out: %s", got)
	}
	if got := strings.Join(isoProblems(f.s, t), " "); got != "cardinality/ds_sv/k11 cardinality/ds_sv/k21 cardinality/ds_sv/k31 cardinality/ds_sv/k61 cardinality/ds_sv/kr2 cardinality/ds_sv/kr4" {
		t.Errorf("the problems: %s", got)
	}
	refs := strings.Join(f.isoRefs(t), " ")
	for _, r := range []string{"k11/2/2/-", "k21/2/2/-", "ka1/2/3/-", "kr3/2/0/r-3", "k41/2/0/l-41", "k51/2/0/l-51", "kv2/2/0/v-2", "k20/2/0/l-20"} {
		if !strings.Contains(refs, r) {
			t.Errorf("no reference %s in %s", r, refs)
		}
	}
	reason := f.s.e.text(t, f.isoQ(`SELECT reason FROM {k}.calc_problem WHERE kind = 'cardinality' ORDER BY reason LIMIT 1`))
	if !strings.HasPrefix(reason, "the link MOUNTED_IN from s1 to r2 would break the cardinality N:1, so it was not made") {
		t.Errorf("the reason: %s", reason)
	}
	if counts.Cardinality != 7 || counts.Problems != 6 || counts.Refs != 7 {
		t.Errorf("the counts: %+v", counts)
	}
	if broken := isoBroken(f.s, t, f.m.mounted); len(broken) != 0 {
		t.Errorf("after the step: %v", broken)
	}
	// a place has one link: the key of the table of the places
	if n := f.s.e.count(t, f.isoQ(`SELECT count(*) FROM {k}.calc_seat WHERE side = $1`), 2); n != 2 {
		t.Errorf("%d ends of the one to one links have a link", n)
	}
	if n := f.s.e.count(t, f.isoQ(`SELECT count(*) FROM {k}.calc_isolated WHERE phase = $1`), pgschema.IsolatePhaseCard); n != 7 {
		t.Errorf("%d rows of the check of the cardinality", n)
	}
}
