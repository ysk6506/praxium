package pgingest

import (
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

const idnOtherSource = `{"id":"src_other","name":"other","adapter":"file","datasets":[
 {"id":"ds_b","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"serial"},{"name":"manufacturer"}]}]}`

// the feed of the other source only finds the servers there are: its feed 0 does not make objects, while feed 0 of the source of the run does
const idnOtherMapping = `{"feeds":[
 {"id":"f_b","dataset":"ds_b","object_type":"PhysicalServer","creates_objects":false,"basis":"observed",
  "attributes":[{"attribute":"name","column":"asset_id"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]}]}`

// idnWithOtherSource gives the KG a second source that has one record whose references are not solved, and receives and compares one
// run of the first source whose rows are given.
func idnWithOtherSource(t *testing.T, rows ...string) (*rdfSetup, SourceRef, *ingest.Plan, ingest.RunLease) {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	other := pgiPlan(t, idnOtherSource, idnOtherMapping)
	ref := s.e.attach(t, s.kg, other)
	s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate) VALUES ($1, $2, 'B1', $3, $4, 0, 1, 0)`, pgiTable(ref, pgschema.TableRawRecord)),
		ref.SrcNo, ref.Datasets["ds_b"], rdfHash("B1"), textArray([]string{"B1", "snB", "Zorvane"}))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_ref (src_no, dataset_no, key, role, def_no, sub, state) VALUES ($1, $2, 'B1', 1, 0, 0, %d)`, s.ref.KGNo, pgschema.RefUnresolved),
		ref.SrcNo, ref.Datasets["ds_b"])
	return s, ref, other, idnReceived(s, t, idnServers(rows...), 0)
}

// [REQ-880] [REQ-884]
func TestIdnAnUnresolvedRecordOfASourceThatHasNoPlanInTheRunIsLeftAloneAndCountedAndDoesNotStopTheRunOfAnotherSource(t *testing.T) {
	s, _, _, l := idnWithOtherSource(t, "A,sn1,Zorvane,u1")
	keys := ingest.NewPlanKeys(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan})
	counts, err := ingest.RunIdentify(s.e.ctx, s.e.st, l, idnStart(s, 0, 0), keys)
	if err != nil {
		t.Fatal(err)
	}
	if counts.NewObjects != 1 || counts.Seeded != 0 || counts.Left != 1 || counts.Unresolved != 0 {
		t.Errorf("%+v", counts)
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_record WHERE key = 'B1'`, s.ref.KGNo)); n != 0 {
		t.Errorf("a record of a source that has no plan is in the run %d times", n)
	}
}

// [REQ-880] [REQ-884]
func TestIdnAnUnresolvedRecordOfAnotherSourceIsIdentifiedByTheFeedsOfItsOwnSourceAndFindsTheObjectOfThisRun(t *testing.T) {
	// B1 gives the key of a server of this run, and its feed only finds: it is bound to that object and makes none
	s, ref, other, l := idnWithOtherSource(t, "A,snB,Zorvane,u1")
	plans := map[int64]*ingest.Plan{s.ref.SrcNo: s.plan, ref.SrcNo: other}
	start := idnStart(s, 0, 0)
	start.Feeds = ingest.IdentifyFeedsOf(plans)
	counts, err := ingest.RunIdentify(s.e.ctx, s.e.st, l, start, ingest.NewPlanKeys(plans))
	if err != nil {
		t.Fatal(err)
	}
	if counts.NewObjects != 1 || counts.Seeded != 1 || counts.Rebuilt != 1 || counts.Left != 0 || counts.Unresolved != 0 {
		t.Fatalf("%+v", counts)
	}
	bs := idnBounds(s, t)
	if len(bs) != 2 || idnRefOf(bs, "ds_s", "A") == "" || idnRefOf(bs, "ds_s", "A") != idnRefOf(bs, "ds_b", "B1") {
		t.Errorf("%+v", bs)
	}
}

// [REQ-880] [REQ-884]
func TestIdnAnUnresolvedRecordOfAnotherSourceWhoseFeedMakesNoObjectAndFindsNoneStaysUnresolvedWhileTheFeedOfThisSourceThatMakesObjectsDoesNotRead(t *testing.T) {
	s, ref, other, l := idnWithOtherSource(t, "A,sn1,Zorvane,u1")
	plans := map[int64]*ingest.Plan{s.ref.SrcNo: s.plan, ref.SrcNo: other}
	start := idnStart(s, 0, 0)
	start.Feeds = ingest.IdentifyFeedsOf(plans)
	counts, err := ingest.RunIdentify(s.e.ctx, s.e.st, l, start, ingest.NewPlanKeys(plans))
	if err != nil {
		t.Fatal(err)
	}
	// the feed 0 of the other source makes no object, as the feed 0 of this one does: B1 is read by its own feed
	if counts.NewObjects != 1 || counts.Unresolved != 1 {
		t.Fatalf("%+v", counts)
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_problem p JOIN "s_%d".calc_record c ON c.rid = p.rid WHERE c.key = 'B1' AND p.kind = '%s'`,
		s.ref.KGNo, s.ref.KGNo, pgschema.ProblemOrphanValue)); n != 1 {
		t.Errorf("B1 has %d problems that no object has its identity", n)
	}
}

// [REQ-880]
func TestIdnTheFeedsOfTheIdentificationAreNamedBySourceAndNumberAndTheRunsOwnSourceMustHaveSome(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	l := idnReceived(s, t, idnServers("A,sn1,Zorvane,u1"), 0)
	none := idnStart(s, 0, 0)
	none.Feeds = nil
	if err := s.e.st.BeginIdentify(s.e.ctx, l, none); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a run given no feed of its own source: %v", err)
	}
	other := idnStart(s, 0, 0)
	for i := range other.Feeds {
		other.Feeds[i].SrcNo += 100
	}
	if err := s.e.st.BeginIdentify(s.e.ctx, l, other); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a run given the feeds of another source only: %v", err)
	}
	if p, err := s.e.st.IdentifyPlace(s.e.ctx, l.No); err != nil || p.Begun {
		t.Errorf("a begin that is refused begins the identification: %+v %v", p, err)
	}
}
