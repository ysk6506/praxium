package graphcheck

import (
	"github.com/ysk6506/praxium/backend/internal/graph"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func feedCases() []Case {
	return []Case{
		{"applied_change_feed_follows_from_a_position", caseAppliedFeed},
		{"light_change_feed_carries_volatile_changes_only", caseLightFeed},
		{"wait_readable_waits_for_the_change_number", caseWaitReadable},
	}
}

func caseAppliedFeed(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 2))
	server, other := e.fx.ServerID("SN-000000"), e.fx.ServerID("SN-000001")
	links, err := e.snap(graph.ReadLatest, 0).LinksOf(e.ctx, other, graph.Out)
	fatalIf(tb, err, "links")
	e.apply(graph.Batch{Objects: []graph.ObjectUpsert{e.fx.Server("SN-000000", "M2"), e.fx.Server("SN-000009", "M1")},
		RetireObjects: []graph.ObjectID{other}, RetireLinks: []graph.LinkID{links[0].ID}})
	e.apply(graph.Batch{Objects: []graph.ObjectUpsert{e.fx.Site("S00", "Third Road")}})
	all, err := e.store.AppliedChanges(e.ctx, e.kg.ID, 0, 10)
	fatalIf(tb, err, "feed")
	check(tb, len(all) == 3 && all[0].Batch.Seq == 1 && all[2].Batch.Seq == 3, "feed from the start: %d chunks", len(all))
	later, err := e.store.AppliedChanges(e.ctx, e.kg.ID, 1, 10)
	fatalIf(tb, err, "feed")
	check(tb, len(later) == 2 && later[0].Batch.Seq == 2, "feed after 1: %d chunks", len(later))
	kinds := map[string]graph.ChangeKind{}
	for _, en := range later[0].Entries {
		kinds[en.ID] = en.Change
		if en.Change == graph.ChangeChanged {
			check(tb, en.OldHash != en.NewHash && en.Seq == 2, "changed entry %+v", en)
		}
	}
	check(tb, kinds[server.String()] == graph.ChangeChanged && kinds[other.String()] == graph.ChangeRetired && kinds[links[0].ID.String()] == graph.ChangeRetired, "entries %+v", kinds)
	check(tb, kinds[e.fx.ServerID("SN-000009").String()] == graph.ChangeAdded, "the added server is not an added entry: %+v", kinds)
	one, err := e.store.AppliedChanges(e.ctx, e.kg.ID, 0, 1)
	fatalIf(tb, err, "feed")
	check(tb, len(one) == 1, "limit 1 gave %d chunks", len(one))
}

func caseLightFeed(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 2))
	one, two := e.fx.ServerID("SN-000000"), e.fx.ServerID("SN-000001")
	first, err := e.store.LightChanges(e.ctx, e.kg.ID, 0, 100)
	fatalIf(tb, err, "light changes")
	check(tb, len(first) == 2 && first[0].Seq == 1, "the first load gave %d lines", len(first))
	res := e.apply(graph.Batch{Volatile: []graph.VolatileWrite{e.fx.Power(one, "off", "2030-01-01T00:03:00Z"), e.fx.Power(two, "on", "2030-01-01T00:03:00Z")}})
	check(tb, res.Changed() && res.FromSeq == 2, "a volatile change needs a change number: %+v", res)
	lines, err := e.store.LightChanges(e.ctx, e.kg.ID, 1, 100)
	fatalIf(tb, err, "light changes")
	check(tb, len(lines) == 1 && lines[0].Object == one && lines[0].AttrNo == graph.AttrServerPower && lines[0].Seq == 2, "lines after 1: %+v", lines)
	wantMark, err := graph.ValueMark("off")
	fatalIf(tb, err, "mark")
	check(tb, lines[0].ValueMark == wantMark, "value mark %q, wanted %q", lines[0].ValueMark, wantMark)
	res = e.apply(graph.Batch{Volatile: []graph.VolatileWrite{e.fx.Power(one, "off", "2030-01-01T00:04:00Z")}})
	check(tb, !res.Changed(), "the same value again made a change")
	hist, err := e.store.AppliedChanges(e.ctx, e.kg.ID, 1, 10)
	fatalIf(tb, err, "feed")
	check(tb, len(hist) == 1 && len(hist[0].Entries) == 0, "a volatile change is no entry of the applied change: %+v", hist)
	at1 := e.snap(graph.ReadSeq, 1)
	_, _, err = at1.Volatile(e.ctx, one, graph.AttrServerPower)
	wantCode(tb, "history of a volatile value", err, graph.CodeVolatileOmitted)
}

func caseWaitReadable(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{ChunkRows: 3})
	e.apply(e.fx.World(1, 1, 1))
	ready := e.head().ReadableThrough
	got, err := e.store.WaitReadable(e.ctx, e.kg.ID, ready, 0)
	fatalIf(tb, err, "wait for what is there")
	check(tb, got == ready, "readable_through %d, wanted %d", got, ready)
	_, err = e.store.WaitReadable(e.ctx, e.kg.ID, ready+100, 0)
	wantCode(tb, "zero timeout", err, graph.CodeWaitTimeout)
	_, err = e.store.WaitReadable(e.ctx, e.kg.ID, ready+100, 20*time.Millisecond)
	wantCode(tb, "timeout", err, graph.CodeWaitTimeout)
	_, err = e.store.WaitReadable(e.ctx, kernelID(tb), 1, 0)
	wantCode(tb, "unknown KG", err, graph.CodeKGNotFound)
	run := e.begin(e.fx.World(1, 2, 2))
	e.step(run)
	e.step(run)
	_, err = e.store.WaitReadable(e.ctx, e.kg.ID, e.head().AppliedSeq, 20*time.Millisecond)
	wantCode(tb, "an open apply is not readable", err, graph.CodeWaitTimeout)
	type outcome struct {
		seq int64
		err error
	}
	done := make(chan outcome, 1)
	want := int64(0)
	go func() {
		seq, err := e.store.WaitReadable(e.ctx, e.kg.ID, ready+1, 10*time.Second)
		done <- outcome{seq, err}
	}()
	for !run.Done() {
		e.step(run)
	}
	want = e.head().ReadableThrough
	out := <-done
	fatalIf(tb, out.err, "the waiter")
	check(tb, out.seq > ready && out.seq <= want, "the waiter got %d", out.seq)
}

func kernelID(tb TB) graph.KGID {
	tb.Helper()
	id, err := kernel.ParseID[graph.KGKind]("kg-not-there")
	fatalIf(tb, err, "id")
	return id
}
