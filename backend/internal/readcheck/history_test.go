package readcheck

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/query"
)

// [REQ-250]
func TestFindPagesRoundTripAndStayWhereTheyWereWhileTheKGMoves(t *testing.T) {
	r := newRig(t, false)
	r.world()
	all := r.mustFind(query.FindInput{Common: query.Common{KG: "main", Shape: query.ShapeItems, Limit: 1000}})
	if all.Meta.ResultComplete.Truncated || all.Meta.ResultComplete.Total != 18 {
		t.Fatalf("whole answer: %+v", all.Meta.ResultComplete)
	}
	ids := func(items []query.FindItem) []string {
		out := []string{}
		for _, it := range items {
			out = append(out, it.ObjectID)
		}
		return out
	}
	if !sort.StringsAreSorted(ids(all.Data.Items)) {
		t.Fatal("items are not in the order of the object id")
	}
	var got []string
	in := query.FindInput{Common: query.Common{KG: "main", Shape: query.ShapeItems, Limit: 5}}
	moved := false
	for pages := 0; ; pages++ {
		a, err := r.find(in, query.AllowAll())
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ids(a.Data.Items)...)
		if !moved { // the KG changes between the first and the second page
			moved = true
			r.apply(graph.Batch{Kind: graph.BatchSync, SourceID: "t", Objects: []graph.ObjectUpsert{r.fx.Site("S99", "later")}})
		}
		if a.Meta.ResultComplete.NextCursor == nil {
			if a.Meta.ResultComplete.Truncated {
				t.Fatal("truncated without a cursor")
			}
			break
		}
		if pages > 10 {
			t.Fatal("too many pages")
		}
		in.Cursor = *a.Meta.ResultComplete.NextCursor
	}
	if !reflect.DeepEqual(got, ids(all.Data.Items)) {
		t.Fatalf("pages do not add up to the whole answer:\n%v\n%v", got, ids(all.Data.Items))
	}
	// A cursor of another query is refused.
	in.Cursor = *func() *string {
		a, _ := r.find(query.FindInput{Common: query.Common{KG: "main", Shape: query.ShapeItems, Limit: 5}}, query.AllowAll())
		return a.Meta.ResultComplete.NextCursor
	}()
	in.ObjectType = "Site"
	if _, err := r.find(in, query.AllowAll()); codeOf(err) != query.CodeCursorInvalid {
		t.Fatalf("a cursor of another query: %v", err)
	}
}

// [REQ-250] [REQ-252]
func TestFindPagesAtASealedVersionKeepTheVersion(t *testing.T) {
	r := newRig(t, false)
	r.world()
	sealed, err := r.a.Seal(r.ctx, r.kg.ID, r.lease, graph.SealSync)
	if err != nil || !sealed.Created {
		t.Fatalf("seal: %v", err)
	}
	r.apply(graph.Batch{Kind: graph.BatchSync, SourceID: "t", Objects: []graph.ObjectUpsert{r.fx.Site("S77", "after the seal")}})
	in := query.FindInput{Common: query.Common{KG: "main", Shape: query.ShapeItems, Limit: 10, KGVersionID: sealed.Version.ID.String()}}
	first, err := r.find(in, query.AllowAll())
	if err != nil || first.Meta.ResultComplete.NextCursor == nil || first.Meta.VolatileAsOf != "omitted" {
		t.Fatalf("first page: %v %+v", err, first.Meta)
	}
	next, err := r.find(query.FindInput{Common: query.Common{KG: "main", Shape: query.ShapeItems, Limit: 10, Cursor: *first.Meta.ResultComplete.NextCursor, KGVersionID: in.KGVersionID}}, query.AllowAll())
	if err != nil || next.Meta.ResultComplete.Total != 18 || next.Meta.AsOf.SealedVersion == nil || len(next.Data.Items) != 8 {
		t.Fatalf("second page: %v total %d items %d", err, next.Meta.ResultComplete.Total, len(next.Data.Items))
	}
}

// [REQ-251] [REQ-252]
func TestGetReadsThePastWithoutVolatileValues(t *testing.T) {
	r := newRig(t, false)
	r.world()
	id := r.fx.ServerID("SN-000102")
	r.powered("SN-000102")
	before := r.headSeq()
	sealed, err := r.a.Seal(r.ctx, r.kg.ID, r.lease, graph.SealSync)
	if err != nil || !sealed.Created {
		t.Fatalf("seal: %v %+v", err, sealed)
	}
	up := r.fx.Server("SN-000102", "M2")
	r.apply(graph.Batch{Objects: []graph.ObjectUpsert{up}})
	now := r.headSeq()
	read := func(c query.Common) query.Answer[query.GetData] {
		c.KG = "main"
		a, err := r.get(query.GetInput{Common: c, ObjectID: id.String()}, query.AllowAll())
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	latest := read(query.Common{})
	if m, _ := attrByName(latest.Data.Attributes, "model"); m.Value != "M2" || latest.Meta.AsOf.ChangeSeq != now || latest.Meta.AsOf.SealedVersion != nil {
		t.Fatalf("latest: %+v %+v", latest.Data.Attributes, latest.Meta.AsOf)
	}
	for name, c := range map[string]query.Common{"by change number": {ChangeSeq: &before}, "by version": {KGVersionID: sealed.Version.ID.String()}} {
		a := read(c)
		m, _ := attrByName(a.Data.Attributes, "model")
		if m.Value != "M1" || a.Meta.VolatileAsOf != "omitted" || a.Meta.AsOf.ChangeSeq != before {
			t.Fatalf("%s: %+v %+v", name, m, a.Meta)
		}
		if _, has := attrByName(a.Data.Attributes, "power_state"); has {
			t.Fatalf("%s: a past read gave a volatile value", name)
		}
		if a.Meta.AsOf.SealedVersion == nil || *a.Meta.AsOf.SealedVersion != sealed.Version.ID.String() {
			t.Fatalf("%s: as_of %+v does not name the sealed version", name, a.Meta.AsOf)
		}
	}
	future := now + 5
	if _, err := r.get(query.GetInput{Common: query.Common{KG: "main", ChangeSeq: &future}, ObjectID: id.String()}, query.AllowAll()); codeOf(err) != graph.CodeNotReadable {
		t.Fatalf("a change number ahead of readable_through: %v", err)
	}
	gone := r.fx.ServerID("SN-000102")
	r.apply(graph.Batch{RetireLinks: []graph.LinkID{r.fx.MountedInID(gone, r.fx.RackID(r.fx.SiteID("S00"), "R01"))}, RetireObjects: []graph.ObjectID{gone}})
	if _, err := r.get(query.GetInput{ObjectID: gone.String()}, query.AllowAll()); codeOf(err) != query.CodeObjectNotFound {
		t.Fatalf("a retired object: %v", err)
	}
	if a := read(query.Common{ChangeSeq: &before}); a.Data.Object.ObjectID != gone.String() {
		t.Fatal("the retired object is gone from the past too")
	}
}

// [REQ-252] [REQ-254]
func TestNeighborsReadThePastAndTheView(t *testing.T) {
	r := newRig(t, false)
	r.world()
	rack := r.fx.RackID(r.fx.SiteID("S00"), "R00")
	before := r.headSeq()
	extra := r.fx.Server("SN-000099", "M1")
	extraID := r.fx.ServerID("SN-000099")
	observed := r.fx.MountedIn(extraID, rack, 9)
	observed.Basis = model.BasisObserved
	r.apply(graph.Batch{Objects: []graph.ObjectUpsert{extra}, Links: []graph.LinkUpsert{observed}})
	now := r.must(query.NeighborsInput{ObjectID: rack.String()}, query.AllowAll())
	if len(now.Data.Items) != 5 {
		t.Fatalf("latest: %d", len(now.Data.Items))
	}
	past := r.must(query.NeighborsInput{Common: query.Common{KG: "main", Shape: query.ShapeItems, ChangeSeq: &before}, ObjectID: rack.String()}, query.AllowAll())
	if len(past.Data.Items) != 4 || past.Meta.AsOf.ChangeSeq != before {
		t.Fatalf("past: %d %+v", len(past.Data.Items), past.Meta.AsOf)
	}
	designed := r.must(query.NeighborsInput{Common: query.Common{KG: "main", Shape: query.ShapeItems, View: query.ViewAsDesigned}, ObjectID: rack.String()}, query.AllowAll())
	if len(designed.Data.Items) != 4 {
		t.Fatalf("as_designed gave an observed link: %d", len(designed.Data.Items))
	}
	if now.Meta.Evidence.Counts.Fact != 5 {
		t.Fatalf("evidence %+v", now.Meta.Evidence)
	}
}

// [REQ-252] [REQ-254]
func TestReadsWaitForMinSeqAndNoticeANewGeneration(t *testing.T) {
	r := newRig(t, false)
	r.world()
	head := r.headSeq()
	rack := r.fx.RackID(r.fx.SiteID("S00"), "R00").String()
	ok := query.NeighborsInput{Common: query.Common{KG: "main", MinSeq: head, WaitTimeout: time.Millisecond}, ObjectID: rack}
	if _, err := r.neighbors(ok, query.AllowAll()); err != nil {
		t.Fatalf("min_seq already reached: %v", err)
	}
	ahead := ok
	ahead.MinSeq = head + 10
	if _, err := r.neighbors(ahead, query.AllowAll()); codeOf(err) != graph.CodeWaitTimeout {
		t.Fatalf("min_seq ahead: %v", err)
	}
	first := r.mustFind(query.FindInput{Common: query.Common{KG: "main", Shape: query.ShapeItems, Limit: 2}})
	cur := *first.Meta.ResultComplete.NextCursor
	if _, err := r.a.Restore(r.ctx, r.kg.ID, head); err != nil {
		t.Fatalf("restore: %v", err)
	}
	_, err := r.find(query.FindInput{Common: query.Common{KG: "main", Shape: query.ShapeItems, Limit: 2, Cursor: cur}}, query.AllowAll())
	if codeOf(err) != graph.CodeGenerationChanged {
		t.Fatalf("a cursor of an older generation: %v", err)
	}
}
