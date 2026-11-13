package graphcheck

import "github.com/ysk6506/praxium/backend/internal/graph"

func sealCases() []Case {
	return []Case{
		{"seal_makes_a_version_only_when_the_hash_changed", caseSealRules},
		{"a_sealed_version_reads_its_own_state", caseSealRead},
		{"a_version_carries_the_closed_input_list", caseSealInputs},
	}
}

func caseSealRules(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	empty, err := e.a.Seal(e.ctx, e.kg.ID, e.lease, graph.SealSync)
	fatalIf(tb, err, "seal of an empty KG")
	check(tb, !empty.Created, "an empty KG sealed a version")
	_, err = e.a.Seal(e.ctx, e.kg.ID, e.lease, "other")
	wantCode(tb, "unknown seal kind", err, graph.CodeBatchInvalid)
	e.apply(e.fx.World(1, 1, 1))
	one, err := e.a.Seal(e.ctx, e.kg.ID, e.lease, graph.SealSync)
	fatalIf(tb, err, "seal 1")
	check(tb, one.Created && one.Version.No == 1 && one.Version.SealedSeq == 1 && one.Version.Prev.IsZero(), "version 1: %+v", one.Version)
	again, err := e.a.Seal(e.ctx, e.kg.ID, e.lease, graph.SealSync)
	fatalIf(tb, err, "seal again")
	check(tb, !again.Created && again.Version.ID == one.Version.ID, "a seal without a change made a version")
	e.apply(e.fx.World(1, 1, 1))
	same, err := e.a.Seal(e.ctx, e.kg.ID, e.lease, graph.SealPeriodic)
	fatalIf(tb, err, "seal after an apply without a change")
	check(tb, !same.Created, "an apply without a change made a version")
	e.apply(graph.Batch{Objects: []graph.ObjectUpsert{e.fx.Site("S00", "Other Road")}})
	two, err := e.a.Seal(e.ctx, e.kg.ID, e.lease, graph.SealMerge)
	fatalIf(tb, err, "seal 2")
	check(tb, two.Created && two.Version.No == 2 && two.Version.Prev == one.Version.ID && two.Version.ID != one.Version.ID && two.Version.Kind == graph.SealMerge, "version 2: %+v", two.Version)
	versions, err := e.store.Versions(e.ctx, e.kg.ID)
	fatalIf(tb, err, "versions")
	check(tb, len(versions) == 2, "%d versions", len(versions))
	events, err := e.store.OrderEvents(e.ctx, e.kg.ID, 0, 100)
	fatalIf(tb, err, "order events")
	seals := 0
	for _, ev := range events {
		if ev.Kind == graph.OrderSeal {
			seals++
		}
	}
	check(tb, seals == 2, "%d seal events in the order, wanted 2", seals)
}

func caseSealRead(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 1))
	sealed, err := e.a.Seal(e.ctx, e.kg.ID, e.lease, graph.SealSync)
	fatalIf(tb, err, "seal")
	e.apply(graph.Batch{Objects: []graph.ObjectUpsert{e.fx.Site("S00", "Other Road")}})
	s, err := e.store.Snapshot(e.ctx, graph.ReadPoint{KG: e.kg.ID, Kind: graph.ReadVersion, Version: sealed.Version.ID})
	fatalIf(tb, err, "version read")
	row, _ := e.object(s, e.fx.SiteID("S00"))
	check(tb, s.Info().Version == sealed.Version.ID && s.Info().Seq == 1 && row.Attrs[graph.AttrSiteAddress].Value == "Fixture Road S00", "version read: %+v %+v", s.Info(), row)
	check(tb, s.Info().Volatile == graph.VolatileOmitted, "a version read carries volatile values")
	_, err = e.store.Snapshot(e.ctx, graph.ReadPoint{KG: e.kg.ID, Kind: graph.ReadVersion, Version: kvID(tb)})
	wantCode(tb, "unknown version", err, graph.CodeKGNotFound)
}

func caseSealInputs(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{Semantic: 4})
	e.apply(e.fx.World(1, 2, 2))
	head := e.head()
	sealed, err := e.a.Seal(e.ctx, e.kg.ID, e.lease, graph.SealSync)
	fatalIf(tb, err, "seal")
	v := sealed.Version
	want, err := graph.NewVersionID(graph.VersionID{}, e.fx.Reg.Hash(), head.InputDigest, 4)
	fatalIf(tb, err, "version id")
	check(tb, v.ID == want, "the version id is not the hash of the closed input list: %s vs %s", v.ID, want)
	check(tb, v.EngineVersion == 4 && v.HashRuleVersion == graph.HashRuleVersion && v.DefinitionSetHash == e.fx.Reg.Hash(), "version header %+v", v)
	check(tb, v.GraphHash == head.Hash.Sum && v.Rows == head.Hash.Rows && len(v.Parts) == len(head.Hash.Parts) && len(v.Parts) > 0, "hash of the version %+v", v)
	check(tb, v.Generation == 1 && !v.SealedAt.IsZero(), "generation and time %+v", v)
	after := e.head()
	check(tb, after.InputDigest.String() != head.InputDigest.String() && after.IngestSeq == head.IngestSeq+1, "the seal did not restart the input chain")
}

func kvID(tb TB) graph.VersionID {
	tb.Helper()
	id, err := graph.NewVersionID(graph.VersionID{}, [32]byte{9}, [32]byte{9}, 99)
	fatalIf(tb, err, "id")
	return id
}
