package graphcheck

import (
	"fmt"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"sync"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func readCases() []Case {
	return []Case{
		{"time_travel_reads_any_past_change_number", caseTimeTravel},
		{"volatile_values_are_in_latest_reads_only", caseVolatilePast},
		{"scans_page_in_id_order_and_links_are_found_from_both_ends", caseScan},
		{"kgs_are_found_by_id_name_and_main_and_do_not_mix", caseCatalog},
		{"restore_raises_the_generation_and_old_readers_are_told", caseGeneration},
		{"one_applier_and_many_readers_see_whole_states", caseConcurrency},
	}
}

func caseTimeTravel(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 2))
	server, other := e.fx.ServerID("SN-000000"), e.fx.ServerID("SN-000001")
	rows, err := e.snap(graph.ReadLatest, 0).LinksOf(e.ctx, other, graph.Out)
	fatalIf(tb, err, "links")
	changed := e.fx.Server("SN-000000", "M2")
	e.apply(graph.Batch{Objects: []graph.ObjectUpsert{changed}, RetireObjects: []graph.ObjectID{other}, RetireLinks: []graph.LinkID{rows[0].ID}})
	e.apply(graph.Batch{Objects: []graph.ObjectUpsert{e.fx.Server("SN-000000", "M3")}})
	for seq, want := range map[int64]string{1: "M1", 2: "M2", 3: "M3"} {
		s := e.snap(graph.ReadSeq, seq)
		row, ok := e.object(s, server)
		check(tb, ok && row.Attrs[graph.AttrServerModel].Value == want && row.ValidAt(seq), "at %d: %+v", seq, row)
		_, there := e.object(s, other)
		check(tb, there == (seq == 1), "at %d the retired server is there: %v", seq, there)
		_, linked, err := s.Link(e.ctx, rows[0].ID)
		fatalIf(tb, err, "link")
		check(tb, linked == (seq == 1), "at %d the retired link is there: %v", seq, linked)
	}
	latest, _ := e.object(e.snap(graph.ReadLatest, 0), server)
	check(tb, latest.SeqFrom == 3 && latest.Open(), "latest row %+v", latest)
	e.verify()
}

func caseVolatilePast(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 1))
	server := e.fx.ServerID("SN-000000")
	e.apply(graph.Batch{Volatile: []graph.VolatileWrite{e.fx.Power(server, "off", "2030-01-01T00:05:00Z")}})
	now, ok, err := e.snap(graph.ReadLatest, 0).Volatile(e.ctx, server, graph.AttrServerPower)
	fatalIf(tb, err, "latest volatile")
	check(tb, ok && now.Value == "off" && now.SeenAt == "2030-01-01T00:05:00Z", "latest %+v", now)
	past := e.snap(graph.ReadSeq, 1)
	check(tb, past.Info().Volatile == graph.VolatileOmitted && e.snap(graph.ReadLatest, 0).Info().Volatile == graph.VolatileLatest, "volatile_as_of is wrong")
	_, _, err = past.Volatile(e.ctx, server, graph.AttrServerPower)
	wantCode(tb, "volatile in a past read", err, graph.CodeVolatileOmitted)
}

func caseScan(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(2, 2, 2))
	s := e.snap(graph.ReadLatest, 0)
	var seen []graph.ObjectID
	var after graph.ObjectID
	for {
		page, err := s.ScanObjects(e.ctx, after, 3)
		fatalIf(tb, err, "scan")
		for _, r := range page {
			seen = append(seen, r.ID)
			after = r.ID
		}
		if len(page) < 3 {
			break
		}
	}
	check(tb, len(seen) == 14, "scanned %d objects", len(seen))
	for i := 1; i < len(seen); i++ {
		check(tb, seen[i-1].String() < seen[i].String(), "ids out of order at %d", i)
	}
	rack := e.fx.RackID(e.fx.SiteID("S00"), "R00")
	for dir, want := range map[graph.Direction]int{graph.Out: 1, graph.In: 2, graph.Both: 3} {
		rows, err := s.LinksOf(e.ctx, rack, dir)
		fatalIf(tb, err, "links of rack")
		check(tb, len(rows) == want, "direction %d: %d links, want %d", dir, len(rows), want)
	}
}

func caseCatalog(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	for _, ref := range []string{e.kg.ID.String(), "prod", graph.MainAlias} {
		kg, err := e.store.GetKG(e.ctx, ref)
		fatalIf(tb, err, "get "+ref)
		check(tb, kg.ID == e.kg.ID && kg.Role == graph.RoleProduction && kg.Generation == 1, "KG by %s: %+v", ref, kg)
	}
	_, err := e.store.GetKG(e.ctx, "nothing")
	wantCode(tb, "unknown KG", err, graph.CodeKGNotFound)
	_, err = graph.NewTestKG(e.ctx, e.store, e.fx.Reg, e.ids, e.clock, graph.KGSpec{Name: "second", Role: graph.RoleProduction})
	wantCode(tb, "a second production KG", err, kernel.CodeConflict)
	cand, err := graph.NewTestKG(e.ctx, e.store, e.fx.Reg, e.ids, e.clock, graph.KGSpec{Name: "cand", Role: graph.RoleCandidate})
	fatalIf(tb, err, "candidate")
	lease, err := e.a.Acquire(e.ctx, cand.ID)
	fatalIf(tb, err, "lease of the candidate")
	_, err = e.a.Apply(e.ctx, cand.ID, lease, e.fx.World(1, 1, 1))
	fatalIf(tb, err, "apply to the candidate")
	n, _ := e.count(e.snap(graph.ReadLatest, 0))
	check(tb, n == 0, "an apply to one KG showed in another: %d objects", n)
	all, err := e.store.ListKGs(e.ctx)
	fatalIf(tb, err, "list")
	check(tb, len(all) == 2 && all[0].ID == e.kg.ID, "KGs %+v", all)
}

func caseGeneration(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 1))
	_, err := e.a.Seal(e.ctx, e.kg.ID, e.lease, graph.SealSync)
	fatalIf(tb, err, "seal 1")
	e.apply(graph.Batch{Objects: []graph.ObjectUpsert{e.fx.Site("S00", "Other Road")}})
	_, err = e.a.Seal(e.ctx, e.kg.ID, e.lease, graph.SealSync)
	fatalIf(tb, err, "seal 2")
	old := e.snap(graph.ReadLatest, 0)
	kg, err := e.a.Restore(e.ctx, e.kg.ID, 1)
	fatalIf(tb, err, "restore")
	check(tb, kg.Generation == 2, "generation %d after the restore", kg.Generation)
	_, _, err = old.Object(e.ctx, e.fx.SiteID("S00"))
	wantCode(tb, "a read by a snapshot of the old generation", err, graph.CodeGenerationChanged)
	_, err = e.store.Snapshot(e.ctx, graph.ReadPoint{KG: e.kg.ID, Generation: 1, Kind: graph.ReadLatest})
	wantCode(tb, "a read point of the old generation", err, graph.CodeGenerationChanged)
	s, err := e.store.Snapshot(e.ctx, graph.ReadPoint{KG: e.kg.ID, Generation: 2, Kind: graph.ReadLatest})
	fatalIf(tb, err, "read point of the new generation")
	row, _ := e.object(s, e.fx.SiteID("S00"))
	check(tb, s.Info().Generation == 2 && s.Info().Seq == 1 && row.Attrs[graph.AttrSiteAddress].Value == "Fixture Road S00", "state after the restore: %+v %+v", s.Info(), row)
	gens, err := e.store.Generations(e.ctx, e.kg.ID)
	fatalIf(tb, err, "generations")
	check(tb, len(gens) == 2 && gens[1].Generation == 2 && gens[1].RestoredTo == 1, "generation events %+v", gens)
	versions, err := e.store.Versions(e.ctx, e.kg.ID)
	fatalIf(tb, err, "versions")
	check(tb, len(versions) == 1 && versions[0].Generation == 1, "versions after the restore: %d", len(versions))
	e.verify()
	res := e.apply(graph.Batch{Objects: []graph.ObjectUpsert{e.fx.Site("S00", "Third Road")}})
	check(tb, res.FromSeq == 2, "the number after the restore is %d, wanted 2 again", res.FromSeq)
	sealed, err := e.a.Seal(e.ctx, e.kg.ID, e.lease, graph.SealSync)
	fatalIf(tb, err, "seal after the restore")
	check(tb, sealed.Created && sealed.Version.Generation == 2 && sealed.Version.No == 2, "version after the restore %+v", sealed.Version)
	_, err = e.a.Restore(e.ctx, e.kg.ID, 99)
	wantCode(tb, "restore past readable_through", err, graph.CodeNotReadable)
}

// caseConcurrency applies batches (two chunks each) that add a site, a rack and
// a server with one link while readers take snapshots: a snapshot always holds
// whole batches, never the half of one.
func caseConcurrency(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{ChunkRows: 2})
	const batches, readers = 12, 4
	var wg sync.WaitGroup
	problems := make(chan string, 64)
	stop := make(chan struct{})
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Every reader takes at least one snapshot, however early the writer finishes.
			for stopped := false; !stopped; {
				s, err := e.store.Snapshot(e.ctx, graph.ReadPoint{KG: e.kg.ID, Kind: graph.ReadLatest})
				if err != nil {
					problems <- "snapshot: " + err.Error()
					return
				}
				rows, err := s.ScanObjects(e.ctx, graph.ObjectID{}, 1<<20)
				if err != nil {
					problems <- "scan: " + err.Error()
					return
				}
				links, err := s.ScanLinks(e.ctx, graph.LinkID{}, 1<<20)
				if err != nil {
					problems <- "scan links: " + err.Error()
					return
				}
				if len(rows)%3 != 0 || len(links)*3 != len(rows) {
					problems <- fmt.Sprintf("at %d: %d objects and %d links are not whole batches", s.Info().Seq, len(rows), len(links))
					return
				}
				select {
				case <-stop:
					stopped = true
				default:
				}
			}
		}()
	}
	for i := 0; i < batches; i++ {
		site := fmt.Sprintf("S%02d", i)
		sid := e.fx.SiteID(site)
		rack := e.fx.Rack(sid, "R00")
		_, err := e.a.Apply(e.ctx, e.kg.ID, e.lease, graph.Batch{
			Objects: []graph.ObjectUpsert{e.fx.Site(site, "Road"), rack, e.fx.Server("SN-"+site, "M1")},
			Links:   []graph.LinkUpsert{e.fx.LocatedIn(e.fx.RackID(sid, "R00"), sid)}})
		if err != nil {
			problems <- "apply: " + err.Error()
			break
		}
	}
	close(stop)
	wg.Wait()
	close(problems)
	for p := range problems {
		tb.Errorf("%s", p)
	}
	check(tb, e.head().ReadableThrough >= batches, "readable_through %d", e.head().ReadableThrough)
	e.verify()
}
