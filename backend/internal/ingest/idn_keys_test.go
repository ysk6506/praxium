package ingest

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-881]
func TestIdnThePlanGivesTheFeedsAndMakesTheKeysAgainFromTheCellsOfARecord(t *testing.T) {
	r := loaded(t)
	feeds := r.plan.IdentifyFeeds()
	if len(feeds) == 0 {
		t.Fatal("no feed")
	}
	keys := NewPlanKeys(map[int64]*Plan{1: r.plan})
	rows := []RebuildRow{{Rid: 10, SrcNo: 1, Dataset: "ds_racks", Key: "RK1", Fields: []string{"RK1", "rack-a", "42"}},
		{Rid: 11, SrcNo: 1, Dataset: "ds_racks", Key: "RK2", Fields: []string{"RK2", "rack-b", "45"}}}
	out, err := keys.Rebuild(1, "ds_racks", rows)
	if err != nil || len(out) != 2 || out[0].Rid != 10 || len(out[0].Keys) == 0 || len(out[1].Keys) == 0 {
		t.Fatalf("%+v %v", out, err)
	}
	srv, err := keys.Rebuild(1, "ds_servers", []RebuildRow{{Rid: 12, SrcNo: 1, Dataset: "ds_servers", Key: "SV1", Fields: []string{"SV1", "sn100", "Zorvane", "RK1", "on"}}})
	if err != nil || len(srv[0].Keys) == 0 || len(srv[0].Ends) == 0 {
		t.Fatalf("%+v %v", srv, err)
	}
	if _, err := keys.Rebuild(2, "ds_racks", rows); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Errorf("a source with no plan: %v", err)
	}
	if _, err := keys.Rebuild(1, "ds_nothing", rows); err == nil {
		t.Error("a dataset the plan does not have")
	}
	rows[0].Fields = []string{"RK1"}
	if _, err := keys.Rebuild(1, "ds_racks", rows); err == nil {
		t.Error("cells that are not the columns")
	}
}

// [REQ-881] [REQ-884]
func TestIdnTheIdOfAnObjectIsTheOneTheKeyGivesAndARowWithNoKeyOrNoNameIsMissing(t *testing.T) {
	r := loaded(t)
	keys := NewPlanKeys(map[int64]*Plan{1: r.plan})
	var feed int
	for _, f := range r.plan.IdentifyFeeds() {
		if f.Creates {
			feed = f.Feed
			break
		}
	}
	// the feeds are numbered in the order of the mapping: the one of the racks is the first
	ask := AskRow{Round: 1, Label: 9, SrcNo: 1, Dataset: "ds_racks", Key: "RK1", Fields: []string{"RK1", "rack-a", "42"}, Feed: feed}
	ans, err := keys.Answer(ask)
	if err != nil || ans.Missing || ans.Ref == "" {
		t.Fatalf("%+v %v", ans, err)
	}
	if _, err := kernel.ParseID[graph.ObjectKind](ans.Ref); err != nil {
		t.Error(err)
	}
	bad := ask
	bad.Feed = 999
	if _, err := keys.Answer(bad); err == nil {
		t.Error("a feed the plan does not have")
	}
	bad = ask
	bad.Ord = 9
	if _, err := keys.Answer(bad); err == nil {
		t.Error("a key the feed does not have")
	}
	bad = ask
	bad.Fields = []string{"RK1"}
	if _, err := keys.Answer(bad); err == nil {
		t.Error("cells that are not the columns")
	}
	bad = ask
	bad.SrcNo = 5
	if _, err := keys.Answer(bad); err == nil {
		t.Error("a source with no plan")
	}
	empty := ask
	empty.Fields = []string{"", "rack-a", "42"}
	if got, err := keys.Answer(empty); err != nil || !got.Missing {
		t.Errorf("a row with no key: %+v %v", got, err)
	}
}
