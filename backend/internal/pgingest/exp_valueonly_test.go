package pgingest

import (
	"fmt"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the second follow-up of the expansion (ADR-0121): the record of a feed that only adds values does not keep an object even
// when it changes and the identification binds it again (the identification puts the object in calc_object for it). Every helper starts
// with expVo, so that none meets a name of another test file of this folder.

// [REQ-892] [REQ-897] [REQ-980]
func TestExpAnObjectIsRetiredAsTheOldPipelineRetiresItWhenTheRecordOfAFeedThatAddsValuesChangesInEveryRun(t *testing.T) {
	a3 := []string{"A,sn1,Zorvane", "B,sn2,Zorvane", "C,sn3,Zorvane"}
	without := []string{"A,sn1,Zorvane", "C,sn3,Zorvane"}
	// the column the feed that only adds values reads changes in every run, as the status of a source that adds values does
	v := func(n int) string {
		return expV(fmt.Sprintf("vA,sn1,Zorvane,va%d", n), fmt.Sprintf("vB,sn2,Zorvane,vb%d", n), fmt.Sprintf("vC,sn3,Zorvane,vc%d", n))
	}
	steps := []expStep{{"ds_a": expA(a3...), "ds_v": v(0)}, {"ds_a": expA(a3...), "ds_v": v(1)}}
	// the server B leaves the feed that makes it for good, and the feed that only adds values has a row of it that changes, to the end
	for i := 0; i < 6; i++ {
		steps = append(steps, expStep{"ds_a": expA(without...), "ds_v": v(i + 2)})
	}
	expParity(t, expValueSource, expValueMapping, steps)
}

// expVoSetup is a KG whose first run made the servers A and B (the feed f_a, which makes and retires them) and gave B a value (the feed
// f_v, which only adds values), and a second run whose identification is done.
type expVoSetup struct {
	s *rdfSetup
	l ingest.RunLease
	// obj is the object of B after the first run
	obj int64
}

func expVoRuns(t *testing.T, second expStep) *expVoSetup {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, expValueSource, expValueMapping))
	l1, _ := idnPipeline(s, t, pgiTexts{"ds_a": expA("A,sn1,Zorvane", "B,sn2,Zorvane"), "ds_v": expV("vB,sn2,Zorvane,b0")}, rdfT0, 0)
	expApplyMarked(s, t)
	s.rdfSettle(t, l1)
	v := &expVoSetup{s: s, obj: expObjOf(s, t, "ds_a", "B")}
	if v.obj == 0 || expObjOf(s, t, "ds_v", "vB") != v.obj {
		t.Fatalf("the record of the feed that adds values is not bound to the object of B (%d): the test does not look at what it is meant to", v.obj)
	}
	texts := pgiTexts{}
	for ds, text := range second {
		texts[ds] = text
	}
	v.l, _ = idnPipeline(s, t, texts, rdfT0.Add(13*time.Hour), 0)
	return v
}

// expVoWhy is the reason the object of B is in the set of the run (0 when it is not in it).
func (v *expVoSetup) expVoWhy(t *testing.T) int16 {
	t.Helper()
	return int16(v.s.e.count(t, fmt.Sprintf(`SELECT COALESCE((SELECT why FROM "s_%d".calc_object WHERE obj_no = $1), 0)`, v.s.ref.KGNo), v.obj))
}

// expVoBack puts the run back to the end of its identification, the reasons the identification gave included (expReset leaves out the
// objects whose reason the expansion changed).
func (v *expVoSetup) expVoBack(t *testing.T) {
	t.Helper()
	v.s.e.exec(t, fmt.Sprintf(`UPDATE "s_%[1]d".calc_object SET why = %[2]d WHERE obj_no IN (SELECT obj_no FROM "s_%[1]d".calc_bind)`, v.s.ref.KGNo, pgschema.ObjectWhyIdentified))
	expReset(v.s, t, v.l)
}

// expVoMakers is the feeds of the plan without the one that only adds values (f_v): a table in which that feed is not known.
func (v *expVoSetup) expVoMakers(t *testing.T) []ingest.ExpandFeed {
	t.Helper()
	var out []ingest.ExpandFeed
	for _, f := range expFeeds(v.s) {
		if f.Creates {
			out = append(out, f)
		}
	}
	if len(out) != 1 || len(expFeeds(v.s)) != 2 {
		t.Fatalf("the feeds of the plan are %+v", expFeeds(v.s))
	}
	return out
}

// the record of B leaves the object (it says another serial, so it makes another server), and the record of the feed that adds values
// changes and is bound to the object again: no record of a feed that makes objects stands under it, and it is a candidate
// [REQ-892] [REQ-952] [REQ-980]
func TestExpAnObjectThatOnlyAChangedRecordOfAFeedThatAddsValuesIsBoundToIsACandidateWhoeverGoesOnWithTheExpansion(t *testing.T) {
	v := expVoRuns(t, expStep{"ds_a": expA("A,sn1,Zorvane", "B,sn9,Zorvane"), "ds_v": expV("vB,sn2,Zorvane,b1")})
	if bound := idnRefOf(idnBounds(v.s, t), "ds_v", "vB"); bound == "" || bound != expRefOf(v.s, t, "ds_v", "vB") {
		t.Fatal("the record of the feed that adds values is not bound to the object in the second run: the test does not look at what it is meant to")
	}
	if why := v.expVoWhy(t); why != pgschema.ObjectWhyIdentified {
		t.Fatalf("before the expansion the object is %d, not one the identification put in the run", why)
	}
	start := ingest.ExpandStart{Rows: 1, Cards: expCards(), Feeds: expFeeds(v.s)}
	counts, err := ingest.RunExpand(v.s.e.ctx, v.s.e.st, v.l, start)
	if err != nil {
		t.Fatal(err)
	}
	if why := v.expVoWhy(t); why != pgschema.ObjectWhyRetire || counts.Retiring != 1 {
		t.Errorf("the object only a record of a feed that adds values is bound to is %d (%+v)", why, counts)
	}
	// what the counts say is what the table holds, and the object was judged once
	k := v.s.ref.KGNo
	if n := v.s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_object WHERE why = %d`, k, pgschema.ObjectWhyRetire)); n != counts.Retiring {
		t.Errorf("the counts say %+v and the table holds %d candidates", counts, n)
	}
	if n := v.s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".%s WHERE %s = %d AND %s = $1`, k, pgschema.TableCalcXKey, pgschema.ColKind, xkIdent, pgschema.ColKeyNo), v.obj); n != 1 {
		t.Errorf("the object was judged %d times", n)
	}
	// another server that takes the expansion over in the middle of the rule old (a record a unit) judges the same, from the database
	for cut := 1; cut <= 4; cut++ {
		v.expVoBack(t)
		if err := v.s.e.st.BeginExpand(v.s.e.ctx, v.l, start); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < cut; i++ {
			if err := v.s.e.st.ExpandStep(v.s.e.ctx, v.l); err != nil {
				t.Fatal(err)
			}
		}
		other, err := ingest.RunExpand(v.s.e.ctx, New(v.s.e.pool), v.l, start)
		if err != nil || other != counts || v.expVoWhy(t) != pgschema.ObjectWhyRetire {
			t.Errorf("cut after %d units: the other server counts %+v (%v) and the object is %d; straight through %+v", cut, other, err, v.expVoWhy(t), counts)
		}
	}
	// a binding of a feed that is not known stands under the object: what it stands for is not known
	v.expVoBack(t)
	if _, err := ingest.RunExpand(v.s.e.ctx, v.s.e.st, v.l, ingest.ExpandStart{Cards: expCards(), Feeds: v.expVoMakers(t)}); err != nil {
		t.Fatal(err)
	}
	if why := v.expVoWhy(t); why != pgschema.ObjectWhyIdentified {
		t.Errorf("an object a record of a feed that is not known is bound to is %d", why)
	}
	// while the identification leaves pairs no object is a candidate, the ones it put in the run neither
	v.expVoBack(t)
	v.s.e.exec(t, `UPDATE sync_run SET counts = jsonb_set(counts, '{identify,counts,unsettled}', '3') WHERE run_no = $1`, v.l.No)
	left, err := ingest.RunExpand(v.s.e.ctx, v.s.e.st, v.l, start)
	if err != nil {
		t.Fatal(err)
	}
	if why := v.expVoWhy(t); why != pgschema.ObjectWhyIdentified || left.Retiring != 0 || left.Unsettled != 3 {
		t.Errorf("the identification left pairs: the object is %d (%+v)", why, left)
	}
}

// the object stays out of the candidates when a record of a feed that makes objects stands under it: one that is bound to it in the same
// run as the changed record of the feed that adds values, or one that is not in the run
// [REQ-892] [REQ-980]
func TestExpAnObjectThatARecordOfAFeedThatMakesObjectsStandsUnderBesideAChangedRecordOfAFeedThatAddsValuesIsNoCandidate(t *testing.T) {
	cases := []struct {
		name   string
		second expStep
	}{
		// B goes, and the new record B2 of the feed that makes the servers says the same server: both feeds bind a record to it in the run
		{"bound in the same run", expStep{"ds_a": expA("A,sn1,Zorvane", "B2,sn2,Zorvane"), "ds_v": expV("vB,sn2,Zorvane,b1")}},
		// B does not change and is not in the run: its reference stands
		{"not in the run", expStep{"ds_a": expA("A,sn1,Zorvane", "B,sn2,Zorvane"), "ds_v": expV("vB,sn2,Zorvane,b1")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := expVoRuns(t, c.second)
			counts, err := ingest.RunExpand(v.s.e.ctx, v.s.e.st, v.l, ingest.ExpandStart{Cards: expCards(), Feeds: expFeeds(v.s)})
			if err != nil {
				t.Fatal(err)
			}
			if why := v.expVoWhy(t); why != pgschema.ObjectWhyIdentified || counts.Retiring != 0 {
				t.Errorf("the object is %d (%+v)", why, counts)
			}
		})
	}
}
