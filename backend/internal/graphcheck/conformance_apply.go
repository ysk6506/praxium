package graphcheck

import (
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

func applyCases() []Case {
	return []Case{
		{"apply_read_round_trip", caseRoundTrip},
		{"apply_no_change_makes_no_change_number", caseNoChange},
		{"apply_rejects_invalid_batches_without_trace", caseRejects},
		{"apply_same_input_same_hashes_in_two_stores", caseTwoStores},
		{"apply_cardinality_and_dangling_links", caseLinkRules},
	}
}

func caseRoundTrip(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	res := e.apply(e.fx.World(2, 2, 2))
	check(tb, res.Changed() && res.FromSeq == 1 && res.ToSeq == 1 && res.IngestSeq == 1, "result %+v", res)
	s := e.snap(graph.ReadLatest, 0)
	objects, links := e.count(s)
	check(tb, objects == 14 && links == 12, "objects %d links %d, want 14 and 12", objects, links)
	server := e.fx.ServerID("SN-000000")
	row, ok := e.object(s, server)
	if !ok {
		tb.Fatalf("server %s not found", server)
		return
	}
	check(tb, row.Name == "Server SN-000000" && row.SeqFrom == 1 && row.Open(), "row %+v", row)
	checkServerParts(tb, e, s, row, server)
}

func checkServerParts(tb TB, e *env, s graph.Snapshot, row graph.ObjectRow, server graph.ObjectID) {
	model1 := row.Attrs[graph.AttrServerModel]
	check(tb, model1.Value == "M1" && model1.Grade == model.GradeFact && model1.Basis == model.BasisConfigured, "attribute %+v", model1)
	_, volatileInRow := row.Attrs[graph.AttrServerPower]
	check(tb, !volatileInRow, "a volatile attribute is in the versioned row")
	rows, err := s.LinksOf(e.ctx, server, graph.Out)
	fatalIf(tb, err, "links of server")
	check(tb, len(rows) == 1 && rows[0].Src == server && rows[0].Grade == model.GradeFact, "links of server %+v", rows)
	v, ok, err := s.Volatile(e.ctx, server, graph.AttrServerPower)
	fatalIf(tb, err, "volatile")
	check(tb, ok && v.Value == "on", "volatile %+v", v)
	kg, err := e.store.GetKG(e.ctx, "main")
	fatalIf(tb, err, "get main")
	check(tb, kg.ID == e.kg.ID && kg.State == graph.StateServing, "KG after the first load %+v", kg)
	e.verify()
}

func caseNoChange(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 2))
	before := e.head()
	res := e.apply(e.fx.World(1, 1, 2))
	after := e.head()
	check(tb, !res.Changed() && res.Chunks == 1, "result %+v", res)
	check(tb, after.AppliedSeq == before.AppliedSeq && after.Hash.Equal(before.Hash), "the head moved without a change")
	check(tb, after.IngestSeq == before.IngestSeq+1 && after.InputDigest != before.InputDigest, "the ordering event was not recorded")
	events, err := e.store.OrderEvents(e.ctx, e.kg.ID, 0, 100)
	fatalIf(tb, err, "order events")
	check(tb, len(events) == 2 && events[0].Kind == graph.OrderSourceChange && events[1].IngestSeq == 2, "order events %+v", events)
}

func caseRejects(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 1))
	site, rack, server := e.fx.SiteID("S00"), e.fx.RackID(e.fx.SiteID("S00"), "R00"), e.fx.ServerID("SN-000000")
	volatileInRow := e.fx.Server("SN-9", "")
	volatileInRow.Attrs = map[int]graph.Attr{graph.AttrServerPower: {Value: "on", Grade: model.GradeFact, Basis: model.BasisObserved}}
	unknownAttr := e.fx.Server("SN-9", "")
	unknownAttr.Attrs = map[int]graph.Attr{99999: {Value: "x", Grade: model.GradeFact, Basis: model.BasisObserved}}
	wrongKey := e.fx.Server("SN-9", "")
	wrongKey.Key = wrongKey.Key[1:]
	noValue := e.fx.Server("SN-9", "")
	noValue.Attrs = map[int]graph.Attr{graph.AttrServerModel: {Grade: model.GradeFact, Basis: model.BasisConfigured}}
	wrongKind := e.fx.Server("SN-9", "")
	wrongKind.Attrs = map[int]graph.Attr{graph.AttrServerModel: fact(int64(4))}
	cases := []struct {
		name  string
		batch graph.Batch
		code  kernel.Code
	}{
		{"volatile attribute in a row", graph.Batch{Objects: []graph.ObjectUpsert{volatileInRow}}, graph.CodeVolatileInRow},
		{"unknown attribute number", graph.Batch{Objects: []graph.ObjectUpsert{unknownAttr}}, graph.CodeBatchInvalid},
		{"key of the wrong shape", graph.Batch{Objects: []graph.ObjectUpsert{wrongKey}}, graph.CodeBatchInvalid},
		{"no value but a grade", graph.Batch{Objects: []graph.ObjectUpsert{noValue}}, graph.CodeBatchInvalid},
		{"value of the wrong kind", graph.Batch{Objects: []graph.ObjectUpsert{wrongKind}}, graph.CodeBatchInvalid},
		{"versioned attribute as volatile", graph.Batch{Volatile: []graph.VolatileWrite{{Object: server, AttrNo: graph.AttrServerModel, Value: "x", Grade: model.GradeFact, Basis: model.BasisObserved}}}, graph.CodeBatchInvalid},
		{"link without a binding", graph.Batch{Links: []graph.LinkUpsert{e.fx.LocatedIn(server, rack)}}, graph.CodeBindingMissing},
		{"seal is not a batch", graph.Batch{Event: graph.OrderSeal}, graph.CodeBatchInvalid},
		{"unknown ordering event", graph.Batch{Event: "other"}, graph.CodeBatchInvalid},
		{"same object twice", graph.Batch{Objects: []graph.ObjectUpsert{e.fx.Site("S00", "a"), e.fx.Site("S00", "b")}}, graph.CodeBatchInvalid},
		{"link end that is not live", graph.Batch{Links: []graph.LinkUpsert{e.fx.LocatedIn(rack, e.fx.SiteID("S77"))}}, graph.CodeDanglingLink},
		{"retired object with a link", graph.Batch{RetireObjects: []graph.ObjectID{site}}, graph.CodeDanglingLink},
	}
	for _, c := range cases {
		before := e.head()
		_, err := e.a.Apply(e.ctx, e.kg.ID, e.lease, c.batch)
		wantCode(tb, c.name, err, c.code)
		after := e.head()
		check(tb, after.AppliedSeq == before.AppliedSeq && after.IngestSeq == before.IngestSeq && !after.Open(), "%s: left a trace in the head", c.name)
	}
}

func caseLinkRules(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 1))
	site, other := e.fx.SiteID("S00"), e.fx.SiteID("S01")
	rack := e.fx.RackID(site, "R00")
	_, err := e.a.Apply(e.ctx, e.kg.ID, e.lease, graph.Batch{Objects: []graph.ObjectUpsert{e.fx.Site("S01", "x")}, Links: []graph.LinkUpsert{e.fx.LocatedIn(rack, other)}})
	wantCode(tb, "a rack in two sites", err, graph.CodeCardinality)
	moved := e.fx.LocatedIn(rack, other)
	oldLink, err := graph.NewLinkID(moved.Type, rack, site, "")
	fatalIf(tb, err, "link id")
	e.apply(graph.Batch{Objects: []graph.ObjectUpsert{e.fx.Site("S01", "x")}, Links: []graph.LinkUpsert{moved}, RetireLinks: []graph.LinkID{oldLink}})
	rows, err := e.snap(graph.ReadLatest, 0).LinksOf(e.ctx, rack, graph.Out)
	fatalIf(tb, err, "links of rack")
	check(tb, len(rows) == 1 && rows[0].Dst == other, "the rack should be in the other site now: %+v", rows)
	e.verify()
}

func caseTwoStores(tb TB, f Factory) {
	one, two := newEnv(tb, f, graph.Options{ChunkRows: 4}), newEnv(tb, f, graph.Options{ChunkRows: 1000})
	for _, e := range []*env{one, two} {
		e.apply(e.fx.World(2, 2, 2))
		e.apply(graph.Batch{Volatile: []graph.VolatileWrite{e.fx.Power(e.fx.ServerID("SN-000000"), "off", "2030-01-01T00:01:00Z")}})
		_, err := e.a.Seal(e.ctx, e.kg.ID, e.lease, graph.SealSync)
		fatalIf(tb, err, "seal")
	}
	h1, h2 := one.head(), two.head()
	check(tb, h1.Hash.Equal(h2.Hash), "graph hash differs with the chunk limit")
	check(tb, h1.InputDigest == h2.InputDigest, "input digest differs with the chunk limit")
	v1, err := one.store.Versions(one.ctx, one.kg.ID)
	fatalIf(tb, err, "versions")
	v2, err := two.store.Versions(two.ctx, two.kg.ID)
	fatalIf(tb, err, "versions")
	check(tb, len(v1) == 1 && len(v2) == 1 && v1[0].ID == v2[0].ID && v1[0].GraphHash == v2[0].GraphHash, "version ids differ: %v %v", v1, v2)
}
