package pgingest

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The computation of everything and the computation of what the run touched make the same objects (ADR-0115 decision 2): from a state
// in which the run before was computed in full and applied, a run that changes a few records is computed twice, by the expansion that
// follows the rules and by the one that takes every open record, and the objects the first touched are the same in the second, and the
// objects it did not touch are what the run before made of them.

// rcpWorldFull is expMakeWorld with the first run computed as well: the objects of the first run, by id, are what the second run finds
// as the permanent values.
func rcpWorldFull(t *testing.T, size int) (*expWorld, map[string]rcpObject) {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, expRetiringMapping(t)))
	l1, _ := idnPipeline(s, t, pgiTexts{"ds_places": pgiPlaces, "ds_servers": expServers1, "ds_vms": pgiVMs}, rdfT0, 0)
	rcpExpand(s, t, l1, true)
	rcpDo(s, t, l1, 0, 0)
	first := rcpStaged(s, t)
	rcpApply(s, t)
	s.rdfSettle(t, l1)
	w := &expWorld{s: s, old: map[string]string{}, obj: map[string]int64{}}
	for _, r := range []struct{ ds, key string }{{"ds_servers", "SV1"}, {"ds_servers", "SV2"}, {"ds_places", "2"}, {"ds_places", "3"}, {"ds_vms", "u-1"}} {
		w.old[r.key] = expRefOf(s, t, r.ds, r.key)
		w.obj[r.key] = expObjOf(s, t, r.ds, r.key)
	}
	for _, x := range []string{"X1", "X2", "X3", "X4", "X5"} {
		s.rdfPutRaw(t, rdfRaw{dataset: "ds_servers", key: x, content: x})
	}
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
	w.l, _ = idnPipeline(s, t, expTexts(false), rdfT0.Add(time.Hour), size)
	return w, first
}

// rcpReset puts the run back to the end of its identification: the expansion and the computation of the objects are forgotten.
func rcpReset(s *rdfSetup, t *testing.T, l ingest.RunLease) {
	t.Helper()
	expReset(s, t, l)
	for _, table := range []string{pgschema.TableStageObject, pgschema.TableStageVolat, pgschema.TableStageIdent, pgschema.TableCalcLeft} {
		s.e.exec(t, fmt.Sprintf(`DELETE FROM "s_%d"."%s"`, s.ref.KGNo, table))
	}
	s.e.exec(t, `UPDATE sync_run SET counts = counts - 'recompute' WHERE run_no = $1`, l.No)
}

// rcpSameRow is the text of a row without what depends on the run it was made in: the object was new in the run before, and is not.
func rcpSameRow(o rcpObject) string {
	o.IsNew, o.FirstKey = false, ""
	return o.rcpText()
}

func rcpRefs(m map[string]rcpObject) []string {
	var out []string
	for ref := range m {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

// [REQ-935]
func TestRcpTheObjectsTheRunTouchedAreTheSameWhenEveryRecordIsComputedAndTheOthersAreWhatTheRunBeforeMade(t *testing.T) {
	w, first := rcpWorldFull(t, 0)
	s := w.s
	rcpExpand(s, t, w.l, false)
	incCounts := rcpDo(s, t, w.l, 0, 0)
	inc := rcpStaged(s, t)
	rcpReset(s, t, w.l)
	rcpExpand(s, t, w.l, true)
	fullCounts := rcpDo(s, t, w.l, 0, 0)
	full := rcpStaged(s, t)
	t.Logf("touched %v of %v", rcpRefs(inc), rcpRefs(full))
	if len(inc) == 0 || len(inc) >= len(full) {
		t.Fatalf("the run touched %d of the %d objects: the case does not tell the two apart", len(inc), len(full))
	}
	for ref, got := range inc {
		if want, ok := full[ref]; !ok || want.rcpText() != got.rcpText() {
			t.Errorf("%s: touched: %s; every record: %s", ref, got.rcpText(), want.rcpText())
		}
	}
	for ref, got := range full {
		if _, touched := inc[ref]; touched {
			continue
		}
		before, ok := first[ref]
		if !ok {
			t.Errorf("%s: an object that is new and that the run did not touch: %s", ref, got.rcpText())
			continue
		}
		if rcpSameRow(before) != rcpSameRow(got) {
			t.Errorf("%s: not touched, and it is not what the run before made of it:\n before %s\n now    %s", ref, rcpSameRow(before), rcpSameRow(got))
		}
	}
	if incCounts.Objects != len(inc) || fullCounts.Objects != len(full) {
		t.Errorf("the reports say %d and %d objects, %d and %d are staged", incCounts.Objects, fullCounts.Objects, len(inc), len(full))
	}
}

// The claim rule (h) of the expansion is what puts the other object that has an identifier a record claims into the run: without it the
// objects the claim changes are not computed, so the objects of the run are not the same as when every record is computed.
// [REQ-935]
func TestRcpTheClaimRuleOfTheExpansionIsNeededForTheObjectsThatHoldAClaimToBeComputed(t *testing.T) {
	w, _ := rcpWorldFull(t, 0)
	s := w.s
	rcpExpand(s, t, w.l, false)
	rcpDo(s, t, w.l, 0, 0)
	with := rcpStaged(s, t)
	rcpReset(s, t, w.l)
	s.e.st.expTest = &expHook{Skip: func(r string) bool { return r == ruleClaim }}
	rcpExpand(s, t, w.l, false)
	rcpDo(s, t, w.l, 0, 0)
	without := rcpStaged(s, t)
	s.e.st.expTest = nil
	if len(without) >= len(with) {
		t.Fatalf("%d objects without the rule and %d with it", len(without), len(with))
	}
	for _, key := range []string{"SV2", "u-1"} {
		if _, ok := with[w.old[key]]; !ok {
			t.Errorf("%s: not computed by the expansion that follows every rule", key)
		}
	}
}
