package pgingest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the follow-up of the expansion (ADR-0118): the retirement of the objects held to the old pipeline in the cases the first
// tests did not have, what the identification leaves, the computation of everything, and the competitors of the ends held to one. Every
// helper, type and field starts with exp, so that none meets a name of another test file of this folder.

// expFeeds is the feeds of the plan of the source of the setup, as the expansion is given them.
func expFeeds(s *rdfSetup) []ingest.ExpandFeed {
	return ingest.ExpandFeedsOf(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan})
}

// expApplyMarked is expApply and the mark of the registry that the step that applies a run writes (MarkRetirableObjects).
func expApplyMarked(s *rdfSetup, t *testing.T) int {
	t.Helper()
	expApply(s, t)
	return expMark(s, t)
}

// expMark does what the apply does for the mark: the objects that a retirable reference of the run points at are marked for good.
func expMark(s *rdfSetup, t *testing.T) int {
	t.Helper()
	tx, err := s.e.admin.BeginTx(s.e.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	n, err := MarkRetirableObjects(s.e.ctx, tx, s.ref.KGNo)
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return n
}

// expServerRetiring is the mapping of the servers with the feed that retires what it makes.
func expServerRetiring() string {
	return strings.Replace(idnServerMapping, `"creates_objects":true,`, `"creates_objects":true,"retires_objects":true,`, 1)
}

// expStep is the texts of the datasets of one run of a series, by dataset.
type expStep map[string]string

// expParity gives a series of runs (13 hours apart) to the old pipeline and to the new flow (the identification, the expansion, and the
// apply that expApplyMarked stands for) and says in how many of them the objects that are kept, or the number that is retired, differ.
func expParity(t *testing.T, source, mapping string, steps []expStep) (diffs int) {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	src, err := ingest.ParseSource(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	mp, err := ingest.ParseMapping(strings.NewReader(mapping))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ingest.Compile(reg, src, mp)
	if err != nil {
		t.Fatal(err)
	}
	clock := kernel.NewManualClock(rdfT0)
	ids := kernel.NewSequentialIDs()
	store := graph.NewScratchStore()
	kgid, _ := kernel.NewID[graph.KGKind](ids, "kg")
	kg, err := store.CreateKG(context.Background(), graph.KG{ID: kgid, Name: "exp", Role: graph.RoleProduction, State: graph.StateBuilding, DefinitionSetHash: reg.Hash(), CreatedAt: clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	fetch := &idnDsFetcher{text: map[string]string{}}
	runner := ingest.Runner{Store: store, Reg: reg, Clock: clock, IDs: ids, Fetcher: fetch, States: ingest.NewMemStateStore()}
	s := rdfNew(t, pgiPlan(t, source, mapping))
	made, retiredOld, retiredNew := map[string]bool{}, 0, 0
	for n, st := range steps {
		at := rdfT0.Add(time.Duration(n) * 13 * time.Hour)
		texts := pgiTexts{}
		for ds, text := range st {
			fetch.text[ds], texts[ds] = text, text
		}
		clock.Set(at)
		rep, err := runner.Run(context.Background(), plan, kg.ID)
		if err != nil {
			t.Fatalf("step %d: the old run: %v", n, err)
		}
		l, _ := idnPipeline(s, t, texts, at, 0)
		counts, err := ingest.RunExpand(s.e.ctx, s.e.st, l, ingest.ExpandStart{Cards: plan.ExpandCards(), Feeds: expFeeds(s)})
		if err != nil {
			t.Fatal(err)
		}
		expApplyMarked(s, t)
		s.rdfSettle(t, l)
		oldLive := expLiveOld(t, runner, kg.ID, made)
		newLive := expList(s.e, t, fmt.Sprintf(`SELECT ref FROM "s_%d".object WHERE retired_seq IS NULL ORDER BY ref`, s.ref.KGNo))
		if len(oldLive) != len(newLive) || counts.Retiring != rep.RetiredObjects {
			diffs++
			t.Errorf("step %d: %d objects are kept and %d candidates for retirement; the old pipeline keeps %d and retires %d", n, len(newLive), counts.Retiring, len(oldLive), rep.RetiredObjects)
		}
		retiredOld, retiredNew = retiredOld+rep.RetiredObjects, retiredNew+counts.Retiring
	}
	if retiredOld == 0 || retiredNew != retiredOld {
		t.Errorf("the old pipeline retired %d objects in all (the case has to retire some), the expansion found %d", retiredOld, retiredNew)
	}
	return diffs
}

const expValueSource = `{"id":"src_expv","name":"expv","adapter":"file","datasets":[
 {"id":"ds_a","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"serial"},{"name":"manufacturer"}]},
 {"id":"ds_v","parser":"csv","natural_key":["vid"],"columns":[{"name":"vid"},{"name":"serial"},{"name":"manufacturer"},{"name":"label"}]}]}`

// ds_a makes (and retires) the servers; ds_v only adds values to the servers there are
const expValueMapping = `{"feeds":[
 {"id":"f_a","dataset":"ds_a","object_type":"PhysicalServer","creates_objects":true,"retires_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"asset_id"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]},
 {"id":"f_v","dataset":"ds_v","object_type":"PhysicalServer","creates_objects":false,"basis":"observed",
  "attributes":[{"attribute":"name","column":"label"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]}]}`

func expA(rows ...string) string { return "asset_id,serial,manufacturer\n" + idnJoin(rows...) }
func expV(rows ...string) string { return "vid,serial,manufacturer,label\n" + idnJoin(rows...) }

// [REQ-892] [REQ-897] [REQ-950]
func TestExpAnObjectThatOnlyARecordOfAFeedThatAddsValuesStandsUnderIsRetiredAsTheOldPipelineRetiresIt(t *testing.T) {
	a3 := []string{"A,sn1,Zorvane", "B,sn2,Zorvane", "C,sn3,Zorvane"}
	without := []string{"A,sn1,Zorvane", "C,sn3,Zorvane"}
	v3 := []string{"vA,sn1,Zorvane,va", "vB,sn2,Zorvane,vb", "vC,sn3,Zorvane,vc"}
	steps := []expStep{{"ds_a": expA(a3...), "ds_v": expV(v3...)}, {"ds_a": expA(a3...), "ds_v": expV(v3...)}}
	// the server B leaves the feed that makes it, and the feed that only adds values still has a row of it, to the end
	for i := 0; i < 5; i++ {
		steps = append(steps, expStep{"ds_a": expA(without...), "ds_v": expV(v3...)})
	}
	expParity(t, expValueSource, expValueMapping, steps)
}

const expTwoSource = `{"id":"src_expt","name":"expt","adapter":"file","datasets":[
 {"id":"ds_a","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"serial"},{"name":"manufacturer"}]},
 {"id":"ds_b","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"serial"},{"name":"manufacturer"}]}]}`

// two feeds make the same servers, and the first retires what it makes
const expTwoMapping = `{"feeds":[
 {"id":"f_a","dataset":"ds_a","object_type":"PhysicalServer","creates_objects":true,"retires_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"asset_id"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]},
 {"id":"f_b","dataset":"ds_b","object_type":"PhysicalServer","creates_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"asset_id"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]}]}`

// expTwoSteps is a series in which the server B leaves one of the two feeds for good, and later the other: the first of them is given
// by the number 0 (ds_a, the feed that retires) or 1 (ds_b).
func expTwoSteps(first int) []expStep {
	both := []string{"A,sn1,Zorvane", "B,sn2,Zorvane"}
	oneA := []string{"A,sn1,Zorvane"}
	pair := func(a, b []string) expStep { return expStep{"ds_a": expA(a...), "ds_b": expA(b...)} }
	steps := []expStep{pair(both, both), pair(both, both)}
	for i := 0; i < 5; i++ {
		if first == 0 {
			steps = append(steps, pair(oneA, both))
		} else {
			steps = append(steps, pair(both, oneA))
		}
	}
	for i := 0; i < 5; i++ {
		steps = append(steps, pair(oneA, oneA))
	}
	return steps
}

// [REQ-892] [REQ-897] [REQ-951]
func TestExpAnObjectTwoFeedsMakeOfWhichOneRetiresIsRetiredAsTheOldPipelineRetiresItWhicheverFeedLetsGoOfItFirst(t *testing.T) {
	t.Run("the feed that retires lets go first", func(t *testing.T) { expParity(t, expTwoSource, expTwoMapping, expTwoSteps(0)) })
	t.Run("the feed that retires lets go last", func(t *testing.T) { expParity(t, expTwoSource, expTwoMapping, expTwoSteps(1)) })
}

// [REQ-892] [REQ-951]
func TestExpTheMarkOfTheRegistryIsOnTheObjectsThatARetirableReferenceOfTheRunPointsAtAndStays(t *testing.T) {
	w := expMakeWorld(t, expRetiringMapping(t), 0, false)
	k := w.s.ref.KGNo
	marked := func() []string {
		return expList(w.s.e, t, fmt.Sprintf(`SELECT o.ref FROM "s_%[1]d".object o JOIN "s_%[1]d".object_retire r ON r.obj_no = o.obj_no ORDER BY o.ref`, k))
	}
	if got := marked(); len(got) != 0 {
		t.Fatalf("the registry has marks before any run was applied: %v", got)
	}
	// the references the first run wrote are in the references that are kept, the mark is written from the references of the run
	w.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".calc_ref (rid, role, def_no, sub, lookup_hash, target_no, state, retirable)
		SELECT 0, role, def_no, sub, lookup_hash, target_no, state, retirable FROM "s_%[1]d".record_ref WHERE role = %[2]d AND state = %[3]d`, k, pgschema.RefRoleIdentity, pgschema.RefResolved))
	n := expMark(w.s, t)
	want := expList(w.s.e, t, fmt.Sprintf(`SELECT o.ref FROM "s_%[1]d".object o WHERE o.obj_no IN (SELECT target_no FROM "s_%[1]d".record_ref WHERE role = %[2]d AND retirable) ORDER BY o.ref`, k, pgschema.RefRoleIdentity))
	if got := marked(); len(want) == 0 || !slices.Equal(got, want) || n != len(want) {
		t.Errorf("%d objects are marked: %v, wanted %v", n, got, want)
	}
	// the virtual machines are made by a feed that does not retire: none of them is marked
	for _, key := range []string{"u-1"} {
		if slices.Contains(marked(), w.old[key]) {
			t.Errorf("the object %s of a feed that does not retire is marked", key)
		}
	}
	// a mark stays, and a second time marks nothing
	w.s.e.exec(t, fmt.Sprintf(`UPDATE "s_%d".calc_ref SET retirable = false`, k))
	if again := expMark(w.s, t); again != 0 || len(marked()) != len(want) {
		t.Errorf("a second time marked %d, and %d objects are marked", again, len(marked()))
	}
}

// [REQ-891] [REQ-952]
func TestExpWhileTheIdentificationLeavesPairsNoObjectIsACandidateForRetirementWhoeverGoesOnWithTheExpansion(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnServerSource, expServerRetiring()))
	rows := idnChain("c", 60, 30)
	l1, _ := idnPipelineWith(s, t, idnServers(rows...), rdfT0, 2, 0, 0, 0, 0)
	expApplyMarked(s, t)
	s.rdfSettle(t, l1)
	k := s.ref.KGNo
	live := func() int {
		return s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".object WHERE retired_seq IS NULL`, k))
	}
	before := live()
	// the records are changed in the eyes of the comparison, and the identification of this run is held to two turns and one pair
	s.e.exec(t, fmt.Sprintf(`UPDATE %s SET raw_hash = '\x00'`, pgiTable(s.ref, pgschema.TableRawRecord)))
	l2, id := idnPipelineWith(s, t, idnServers(rows...), rdfT0.Add(time.Hour), 2, 0, 2, 0, 1)
	if id.Unsettled == 0 {
		t.Fatalf("the identification left nothing (%+v): the test does not look at what it is meant to", id)
	}
	start := ingest.ExpandStart{Rows: 7, Cards: expCards(), Feeds: expFeeds(s)}
	counts, err := ingest.RunExpand(s.e.ctx, s.e.st, l2, start)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Retiring != 0 || counts.Unsettled != id.Unsettled {
		t.Errorf("the identification left %d pairs: %+v", id.Unsettled, counts)
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_object WHERE why = %d`, k, pgschema.ObjectWhyRetire)); n != 0 {
		t.Errorf("%d objects are written as candidates for retirement", n)
	}
	// another server that takes the expansion over from the middle reads what the identification left as well, and gives the same
	expReset(s, t, l2)
	if err := s.e.st.BeginExpand(s.e.ctx, l2, start); err != nil {
		t.Fatal(err)
	}
	if err := s.e.st.ExpandStep(s.e.ctx, l2); err != nil {
		t.Fatal(err)
	}
	other, err := ingest.RunExpand(s.e.ctx, New(s.e.pool), l2, start)
	if err != nil || other != counts {
		t.Errorf("the other server counts %+v (%v), the first %+v", other, err, counts)
	}
	expApplyMarked(s, t)
	s.rdfSettle(t, l2)
	if after := live(); after != before {
		t.Errorf("%d objects are kept after the run, and %d before", after, before)
	}
}

// [REQ-891] [REQ-953]
func TestExpTheExpansionOfEverythingTakesTheRecordsTheIdentificationReadsAndNoOther(t *testing.T) {
	w := expMakeWorld(t, expRetiringMapping(t), 0, false)
	k := w.s.ref.KGNo
	// a record of the first run's state that the second run does not touch, and whose cells were kept under another declaration than
	// the dataset has now: the identification does not read it
	w.s.e.exec(t, fmt.Sprintf(`UPDATE %s SET decl_rev = 99 WHERE key = 'X5'`, pgiTable(w.s.ref, pgschema.TableRawRecord)))
	// a record of a source whose plan the run was not given: the identification counts it as left, and the step after has no mapping for it
	other := w.s.e.attach(t, w.s.kg, pgiPlan(t, expOtherSource, expOtherMapping))
	w.s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate) VALUES ($1, $2, 'B1', $3, $4, 0, 1, 0)`, pgiTable(other, pgschema.TableRawRecord)),
		other.SrcNo, other.Datasets["ds_b"], rdfHash("B1"), textArray([]string{"B1", "snB", "Zorvane"}))
	counts, _ := w.expRun(t, 3, true)
	in := map[string]bool{}
	for _, r := range w.s.rdfRecords(t) {
		in[r.Key] = true
	}
	for _, key := range []string{"X5", "B1"} {
		if in[key] {
			t.Errorf("the record %s is a record of the run when everything is expanded", key)
		}
	}
	// what is taken is what the identification reads: every raw record of a source with a plan, of the declaration the dataset has
	left := w.s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%[1]d".raw_record r JOIN source_dataset d ON d.dataset_no = r.dataset_no AND d.decl_rev = r.decl_rev
		WHERE r.src_no = $1 AND NOT EXISTS (SELECT 1 FROM "s_%[1]d".calc_record c WHERE (c.src_no, c.dataset_no, c.key) = (r.src_no, r.dataset_no, r.key))`, k), w.s.ref.SrcNo)
	if left != 0 || counts.Everything == 0 {
		t.Errorf("%d records the identification reads are not records of the run (%+v)", left, counts)
	}
}

const expOtherSource = `{"id":"src_expo","name":"expo","adapter":"file","datasets":[
 {"id":"ds_b","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"serial"},{"name":"manufacturer"}]}]}`

// the feed 0 of the other source makes objects and does not retire; the feed 0 of the source of the run makes objects and retires
const expOtherMapping = `{"feeds":[
 {"id":"f_b","dataset":"ds_b","object_type":"PhysicalServer","creates_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"asset_id"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]}]}`

// [REQ-892] [REQ-954]
func TestExpTheReferencesOfRecordsOfTwoSourcesWhoseFeedsHaveTheSameNumberAreRetirableByTheFeedOfTheirOwnSource(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnServerSource, expServerRetiring()))
	other := pgiPlan(t, expOtherSource, expOtherMapping)
	ref := s.e.attach(t, s.kg, other)
	s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate) VALUES ($1, $2, 'B1', $3, $4, 0, 1, 0)`, pgiTable(ref, pgschema.TableRawRecord)),
		ref.SrcNo, ref.Datasets["ds_b"], rdfHash("B1"), textArray([]string{"B1", "snB", "Zorvane"}))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_ref (src_no, dataset_no, key, role, def_no, sub, state) VALUES ($1, $2, 'B1', 1, 0, 0, %d)`, s.ref.KGNo, pgschema.RefUnresolved),
		ref.SrcNo, ref.Datasets["ds_b"])
	l := idnReceived(s, t, idnServers("A,sn1,Zorvane,u1"), 0)
	plans := map[int64]*ingest.Plan{s.ref.SrcNo: s.plan, ref.SrcNo: other}
	start := idnStart(s, 0, 0)
	start.Feeds = ingest.IdentifyFeedsOf(plans)
	if _, err := ingest.RunIdentify(s.e.ctx, s.e.st, l, start, ingest.NewPlanKeys(plans)); err != nil {
		t.Fatal(err)
	}
	got := expList(s.e, t, fmt.Sprintf(`SELECT c.src_no::text || ':' || c.key || ':' || r.retirable::text FROM "s_%[1]d".calc_ref r JOIN "s_%[1]d".calc_record c ON c.rid = r.rid
		WHERE r.role = 1 AND r.state = 0 ORDER BY 1`, s.ref.KGNo))
	want := []string{fmt.Sprintf("%d:A:true", s.ref.SrcNo), fmt.Sprintf("%d:B1:false", ref.SrcNo)}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("the references are %v, wanted %v", got, want)
	}
}

// [REQ-892] [REQ-954]
func TestExpAReferenceOfAFeedThatOnlyAddsValuesIsNoSupportAndOneOfAFeedThatIsNotKnownIs(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnServerSource, expServerRetiring()))
	l1, _ := idnPipeline(s, t, idnServers("A,sn1,Zorvane,u1"), rdfT0, 0)
	expApplyMarked(s, t)
	s.rdfSettle(t, l1)
	obj := expObjOf(s, t, "ds_s", "A")
	k := s.ref.KGNo
	// a record of another dataset that has a reference to the object, by the feed 5 of the source, and the record of the run leaves the object
	s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate) VALUES ($1, $2, 'V1', $3, $4, 0, 1, 0)`, pgiTable(s.ref, pgschema.TableRawRecord)),
		s.ref.SrcNo, s.rdfNo("ds_s"), rdfHash("V1"), textArray([]string{"V1", "sn1", "Zorvane", "u1"}))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_ref (src_no, dataset_no, key, role, def_no, sub, target_no, state, retirable) VALUES ($1, $2, 'V1', %d, 5, 0, $3, %d, false)`,
		k, pgschema.RefRoleIdentity, pgschema.RefResolved), s.ref.SrcNo, s.rdfNo("ds_s"), obj)
	l, _ := idnPipeline(s, t, idnServers("A,sn1x,Zorvane,u1x"), rdfT0.Add(time.Hour), 0)
	why := func(feeds []ingest.ExpandFeed) int16 {
		expReset(s, t, l)
		if _, err := ingest.RunExpand(s.e.ctx, s.e.st, l, ingest.ExpandStart{Feeds: feeds}); err != nil {
			t.Fatal(err)
		}
		return int16(s.e.count(t, fmt.Sprintf(`SELECT why FROM "s_%d".calc_object WHERE obj_no = $1`, k), obj))
	}
	feeds := expFeeds(s)
	// the feed 5 is not known: the reference of V1 is a support, and the object is not retired for want of knowing
	if got := why(feeds); got != pgschema.ObjectWhyOldRef {
		t.Errorf("a reference of a feed that is not known supports the object, which is %d", got)
	}
	// the feed 5 is known to add values only: the reference of V1 is no support, and the object is a candidate
	if got := why(append(slices.Clone(feeds), ingest.ExpandFeed{SrcNo: s.ref.SrcNo, Feed: 5})); got != pgschema.ObjectWhyRetire {
		t.Errorf("a reference of a feed that only adds values supports the object, which is %d", got)
	}
	// and a feed that makes objects is a support
	if got := why(append(slices.Clone(feeds), ingest.ExpandFeed{SrcNo: s.ref.SrcNo, Feed: 5, Creates: true})); got != pgschema.ObjectWhyOldRef {
		t.Errorf("a reference of a feed that makes objects does not support the object, which is %d", got)
	}
	// the number of a feed is a number of its source: the feed 5 of another source that only adds values does not make the feed 5 of this source
	// one that only adds values
	both := []ingest.ExpandFeed{{SrcNo: s.ref.SrcNo + 1, Feed: 5}, {SrcNo: s.ref.SrcNo, Feed: 5, Creates: true}}
	if got := why(both); got != pgschema.ObjectWhyOldRef {
		t.Errorf("the feed 5 of another source that only adds values makes the feed 5 of this source add values only: the object is %d", got)
	}
}

// the competitor for the end that a record of the run asks for now: a record F of the run looks for a key that a record B (not in the
// run) offers; a link of a type that holds its end to one already ends at the object of B, made by P. Nothing else reads P: B is not in the
// run, F has no link yet. The new link of F competes with the link of P (the smaller id stays).
// [REQ-894] [REQ-955]
func TestExpTheRecordOfALinkThatEndsAtTheObjectAnEndKeyOfARecordFindsIsReadAgain(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnServerSource, expServerRetiring()))
	l1, _ := idnPipeline(s, t, idnServers("A,sn1,Zorvane,u1", "B,sn2,Zorvane,u2"), rdfT0, 0)
	expApply(s, t)
	s.rdfSettle(t, l1)
	k := s.ref.KGNo
	objA, objB := expObjOf(s, t, "ds_s", "A"), expObjOf(s, t, "ds_s", "B")
	s.rdfPutRaw(t, rdfRaw{dataset: "ds_s", key: "P", content: "P"})
	expPutLink(s, t, 9100, expTypeToOne, objA, objB)
	expPutFrom(s, t, "ds_s", "P", 9100)
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_ref (src_no, dataset_no, key, role, def_no, sub, lookup_hash, state, target_no, retirable) VALUES ($1, $2, 'B', %d, 5, 0, '\x07', %d, $3, false)`,
		k, pgschema.RefRoleLinkTo, pgschema.RefResolved), s.ref.SrcNo, s.rdfNo("ds_s"), objB)
	l, _ := idnPipeline(s, t, idnServers("A,sn1,Zorvane,u1", "B,sn2,Zorvane,u2", "F,snF,Zorvane,uF"), rdfT0.Add(time.Hour), 0)
	rid := s.e.count(t, fmt.Sprintf(`SELECT rid FROM "s_%d".calc_record WHERE key = 'F'`, k))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_end (rid, def_no, side, lookup_hash, held) VALUES ($1, 5, %d, '\x07', false)`, k, pgschema.RefRoleLinkFrom), rid)
	if _, err := ingest.RunExpand(s.e.ctx, s.e.st, l, ingest.ExpandStart{Cards: expCards()}); err != nil {
		t.Fatal(err)
	}
	if got := expWhyOf(s, t, "P"); got != pgschema.CalcWhyExpanded {
		t.Errorf("the record P, whose link competes for the end that F asks for, is read again for the reason %d", got)
	}
}

// expWhyOf is the reason a record is in the run (-1 when it is not).
func expWhyOf(s *rdfSetup, t *testing.T, key string) int16 {
	t.Helper()
	for _, r := range s.rdfRecords(t) {
		if r.Key == key {
			return r.Why
		}
	}
	return -1
}

// the competitor for the end the record had: a second live link of the type ends at the end the record had
// [REQ-894] [REQ-955]
func TestExpTheRecordOfASecondLinkThatEndsWhereTheLinkOfAChangedRecordEndedIsReadAgain(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnServerSource, expServerRetiring()))
	l1, _ := idnPipeline(s, t, idnServers("A,sn1,Zorvane,u1", "B,sn2,Zorvane,u2", "F,snF,Zorvane,uF"), rdfT0, 0)
	expApply(s, t)
	s.rdfSettle(t, l1)
	objA, objB, objF := expObjOf(s, t, "ds_s", "A"), expObjOf(s, t, "ds_s", "B"), expObjOf(s, t, "ds_s", "F")
	// F made a link F -> B, and P a second one A -> B (a state the cardinality does not allow, as after a change of the model)
	s.rdfPutRaw(t, rdfRaw{dataset: "ds_s", key: "P", content: "P"})
	expPutLink(s, t, 9101, expTypeToOne, objF, objB)
	expPutFrom(s, t, "ds_s", "F", 9101)
	expPutLink(s, t, 9102, expTypeToOne, objA, objB)
	expPutFrom(s, t, "ds_s", "P", 9102)
	l, _ := idnPipeline(s, t, idnServers("A,sn1,Zorvane,u1", "B,sn2,Zorvane,u2", "F,snF2,Zorvane,uF"), rdfT0.Add(time.Hour), 0)
	if _, err := ingest.RunExpand(s.e.ctx, s.e.st, l, ingest.ExpandStart{Cards: expCards()}); err != nil {
		t.Fatal(err)
	}
	if got := expWhyOf(s, t, "P"); got != pgschema.CalcWhyExpanded {
		t.Errorf("the record P is read again for the reason %d", got)
	}
}

// the competitor at the object a record is bound to now (the binding of the identification): F changes its serial and is bound to the
// object of B, at which P has a link of a type that holds its start to one. The old binding of F has no links.
// [REQ-894] [REQ-955]
func TestExpTheRecordOfALinkThatStartsAtTheObjectAChangedRecordIsNowBoundToIsReadAgain(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnServerSource, expServerRetiring()))
	l1, _ := idnPipeline(s, t, idnServers("A,sn1,Zorvane,u1", "B,sn2,Zorvane,u2", "F,snF,Zorvane,uF"), rdfT0, 0)
	expApply(s, t)
	s.rdfSettle(t, l1)
	objA, objB := expObjOf(s, t, "ds_s", "A"), expObjOf(s, t, "ds_s", "B")
	s.rdfPutRaw(t, rdfRaw{dataset: "ds_s", key: "P", content: "P"})
	expPutLink(s, t, 9103, expTypeFromOne, objB, objA)
	expPutFrom(s, t, "ds_s", "P", 9103)
	l, _ := idnPipeline(s, t, idnServers("A,sn1,Zorvane,u1", "B,sn2,Zorvane,u2", "F,sn2,Zorvane,uF"), rdfT0.Add(time.Hour), 0)
	if bound := idnRefOf(idnBounds(s, t), "ds_s", "F"); bound == "" || bound != expRefOf(s, t, "ds_s", "B") {
		t.Fatal("F is not bound to the object of B: the test does not look at what it is meant to")
	}
	if _, err := ingest.RunExpand(s.e.ctx, s.e.st, l, ingest.ExpandStart{Cards: expCards()}); err != nil {
		t.Fatal(err)
	}
	if got := expWhyOf(s, t, "P"); got != pgschema.CalcWhyExpanded {
		t.Errorf("the record P, whose link competes at the new object of F, is read again for the reason %d", got)
	}
}

// [REQ-890] [REQ-956]
func TestExpTheUnitsDoneAreTheUnitsTheExpansionWentByAndTheRunBeginsAtTheFirstRule(t *testing.T) {
	w := expMakeWorld(t, expRetiringMapping(t), 0, false)
	expReset(w.s, t, w.l)
	st, ctx := w.s.e.st, w.s.e.ctx
	if err := st.BeginExpand(ctx, w.l, ingest.ExpandStart{Rows: 3, Cards: expCards()}); err != nil {
		t.Fatal(err)
	}
	steps := 0
	for {
		place, err := st.ExpandPlace(ctx, w.l)
		if err != nil {
			t.Fatal(err)
		}
		if place.Unit != steps {
			t.Fatalf("after %d units the place says %d", steps, place.Unit)
		}
		if place.Done() {
			break
		}
		if err := st.ExpandStep(ctx, w.l); err != nil {
			t.Fatal(err)
		}
		steps++
	}
	if steps < len(expRules())-1 {
		t.Errorf("%d units for %d rules", steps, len(expRules()))
	}
}
