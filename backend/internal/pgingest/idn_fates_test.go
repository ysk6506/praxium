package pgingest

import (
	"fmt"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// idnPutClaimRaw keeps a raw record of the dataset of the claims, as a run before left it.
func idnPutClaimRaw(s *rdfSetup, t *testing.T, key string, fate int16, cells ...string) {
	t.Helper()
	s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate) VALUES ($1, $2, $3, $4, $5, 0, 1, $6)`,
		pgiTable(s.ref, pgschema.TableRawRecord)), s.ref.SrcNo, s.rdfNo("ds_s"), key, rdfHash(key), textArray(cells), fate)
}

// idnMadeBy counts the rows of a table of the computation that are of the record with the key.
func idnMadeBy(s *rdfSetup, t *testing.T, table, key string) int {
	t.Helper()
	k := s.ref.KGNo
	return s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".%s x JOIN "s_%d".calc_record c ON c.rid = x.rid WHERE c.key = '%s'`, k, table, k, key))
}

// [REQ-880] [REQ-883]
func TestIdnARecordTheComparisonClosedOrLetGoOfTakesNoPartAndMakesNoObjectKeyOrIdentifierWhileTheOtherRecordsAreIdentified(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnClaimSource, idnClaimMapping))
	// Z is not shown any more, and its wait for that is up; F is filtered by a rule, is not shown, and its wait for that is up
	idnPutClaimRaw(s, t, "Z", pgschema.FateUsed, "Z", "snZ", "Zorvane", "aa:aa:aa:aa:aa:0a", "on")
	idnPutClaimRaw(s, t, "F", pgschema.FateFiltered, "F", "snF", "Zorvane", "aa:aa:aa:aa:aa:0b", "off")
	s.rdfPutWaits(t,
		rdfWaitRow{"ds_s", "Z", pgschema.WaitUnseen, 50, rdfT0.Add(-1000 * time.Hour), false},
		rdfWaitRow{"ds_s", "F", pgschema.WaitFiltered, 2, rdfT0.Add(-1000 * time.Hour), false},
	)
	_, counts := idnPipeline(s, t, idnClaims("A,sn1,Zorvane,aa:aa:aa:aa:aa:01,on"), rdfT0, 0)
	why := map[string]int16{}
	for _, r := range s.rdfRecords(t) {
		why[r.Key] = r.Why
	}
	if why["Z"] != pgschema.CalcWhyClosed || why["F"] != pgschema.CalcWhyReleased || why["A"] != pgschema.CalcWhyNew {
		t.Fatalf("the comparison made %v", why)
	}
	for _, key := range []string{"Z", "F"} {
		for _, table := range []string{"calc_key", "calc_end", "calc_claim", "calc_bind", "calc_ref", "calc_problem", "calc_newobj"} {
			if n := idnMadeBy(s, t, table, key); n != 0 {
				t.Errorf("the record %s has %d rows in %s", key, n, table)
			}
		}
	}
	// the one that is shown makes its object, and the two that are left are told as records that were left out
	if counts.NewObjects != 1 || counts.Bound != 2 || counts.Skipped != 2 || counts.Rebuilt != 0 || counts.Unresolved != 0 {
		t.Errorf("%+v", counts)
	}
	// the keys that are bound are the ones of the record that is identified
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_newkey`, s.ref.KGNo)); n != 1 {
		t.Errorf("%d keys are bound, the record that is shown gave one", n)
	}
}

// [REQ-880] [REQ-885]
func TestIdnARecordThatRepeatsItsKeyIsIdentifiedAsTheRecordItIsAndMakesOneObjectNotTwo(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	// the later row of a key is the record, as in the code that was there; the earlier one is set aside
	l1, counts := idnPipeline(s, t, idnServers("A,sn1,Zorvane,u1", "D,snD1,Zorvane,uD", "D,snD2,Zorvane,uD"), rdfT0, 0)
	if counts.NewObjects != 2 || counts.Bound != 2 || counts.Unresolved != 0 {
		t.Fatalf("%+v", counts)
	}
	idnApply(s, t)
	s.rdfSettle(t, l1)
	// A begins to repeat: its content is the same, so nothing but the repeating is new, and the record is bound to its object again
	first := idnBounds(s, t)
	refA, refD := idnRefOf(first, "ds_s", "A"), idnRefOf(first, "ds_s", "D")
	_, counts = idnPipelineAfter(s, t, idnServers("A,sn1,Zorvane,u1", "A,sn1,Zorvane,u1", "D,snD2,Zorvane,uD"), 0)
	if counts.NewObjects != 0 || counts.Rebuilt < 1 || counts.Unresolved != 0 {
		t.Errorf("%+v", counts)
	}
	bs := idnBounds(s, t)
	if len(bs) < 1 || idnRefOf(bs, "ds_s", "A") != refA || idnRefOf(bs, "ds_s", "D") != "" && idnRefOf(bs, "ds_s", "D") != refD {
		t.Errorf("%+v, want A in %s and D in %s", bs, refA, refD)
	}
	for _, b := range bs {
		if b.IsNew {
			t.Errorf("a record that only repeats its key makes an object: %+v", b)
		}
	}
	for _, r := range s.rdfRecords(t) {
		if r.Key == "A" && r.Why != pgschema.CalcWhyDuplicates {
			t.Errorf("the comparison made A %d", r.Why)
		}
	}
}

// [REQ-880] [REQ-882]
func TestIdnTheRecordThatNamesAnObjectWhenTheRecordsGiveTheSameKeyIsTheOneWhoseDatasetAndKeyComeFirstWhateverTheOrderOfTheRows(t *testing.T) {
	var want string
	for n, rows := range [][]string{
		{"10,sn1,Zorvane,u1", "9,sn1,Zorvane,u1", "8,sn1,Zorvane,u1"},
		{"8,sn1,Zorvane,u1", "9,sn1,Zorvane,u1", "10,sn1,Zorvane,u1"},
		{"9,sn1,Zorvane,u1", "8,sn1,Zorvane,u1", "10,sn1,Zorvane,u1"},
	} {
		s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
		if _, counts := idnPipeline(s, t, idnServers(rows...), rdfT0, 0); counts.NewObjects != 1 {
			t.Fatalf("%+v", counts)
		}
		k := s.ref.KGNo
		got := s.e.text(t, fmt.Sprintf(`SELECT c.key FROM "s_%d".calc_newobj n JOIN "s_%d".calc_record c ON c.rid = n.rid`, k, k))
		if n == 0 {
			want = got
		}
		// the natural keys are compared by their bytes: "10" is before "8" and "9"
		if got != "10" || got != want {
			t.Errorf("order %d: the object is named by the record %q", n, got)
		}
	}
}

// [REQ-880] [REQ-883]
func TestIdnARecordThatGivesNoKeyKeepsItsClaimsOnceAndIsNotReadAgainFromItsCellsWhenItCameInTheRowsReceived(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnClaimSource, idnClaimMapping))
	_, counts := idnPipeline(s, t, idnClaims("A,sn1,Zorvane,aa:aa:aa:aa:aa:01,on", "E,,Zorvane,aa:aa:aa:aa:aa:03,on"), rdfT0, 0)
	if counts.Rebuilt != 0 || counts.NewObjects != 1 {
		t.Errorf("%+v", counts)
	}
	if n := idnMadeBy(s, t, "calc_claim", "E"); n != 1 {
		t.Errorf("a record with no key has %d claims, it gave one", n)
	}
	if n := idnMadeBy(s, t, "calc_key", "E"); n != 0 {
		t.Errorf("a record with no key has %d keys", n)
	}
}
