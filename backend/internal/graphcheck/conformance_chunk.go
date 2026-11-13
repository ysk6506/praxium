package graphcheck

import (
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
)

func chunkCases() []Case {
	return []Case{
		{"chunked_apply_is_invisible_until_the_last_chunk", caseChunkInvisible},
		{"chunked_apply_abandon_reverts_and_reuses_numbers", caseAbandon},
		{"chunked_apply_resumes_after_a_restart", caseResume},
		{"chunked_apply_refuses_other_work_while_open", caseInProgress},
		{"apply_retry_of_a_finished_apply_is_exactly_once", caseDuplicate},
		{"write_with_a_stale_expected_head_is_refused", caseStaleHead},
		{"first_load_serves_only_after_the_last_chunk", caseFirstLoad},
	}
}

func caseChunkInvisible(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{ChunkRows: 5})
	e.apply(e.fx.World(1, 1, 1))
	base, _ := e.count(e.snap(graph.ReadLatest, 0))
	before := e.head()
	run := e.begin(e.fx.World(2, 2, 2))
	e.step(run)
	e.step(run)
	h := e.head()
	check(tb, h.AppliedSeq == before.AppliedSeq+2 && h.ReadableThrough == before.ReadableThrough && h.Open(), "head %+v", h)
	latest := e.snap(graph.ReadLatest, 0)
	n, _ := e.count(latest)
	check(tb, n == base && latest.Info().Seq == before.ReadableThrough, "a reader saw %d objects of an open apply", n)
	_, err := e.store.Snapshot(e.ctx, graph.ReadPoint{KG: e.kg.ID, Kind: graph.ReadSeq, Seq: h.AppliedSeq})
	wantCode(tb, "read at an unreadable number", err, graph.CodeNotReadable)
	applied, _ := e.count(e.snap(graph.ReadApplied, 0))
	check(tb, applied > base, "the applier cannot see its own chunks")
	changes, err := e.store.AppliedChanges(e.ctx, e.kg.ID, 0, 100)
	fatalIf(tb, err, "feed")
	check(tb, len(changes) == int(before.ReadableThrough), "the applied-change feed went past readable_through: %d", len(changes))
	for !run.Done() {
		e.step(run)
	}
	h = e.head()
	check(tb, h.ReadableThrough == h.AppliedSeq && !h.Open(), "head after the last chunk %+v", h)
	total, _ := e.count(e.snap(graph.ReadLatest, 0))
	old, _ := e.count(e.snap(graph.ReadSeq, before.ReadableThrough))
	check(tb, total == 14 && old == base, "latest %d, old %d", total, old)
	e.verify()
}

func caseAbandon(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{ChunkRows: 3})
	e.apply(e.fx.World(1, 1, 3))
	site := e.fx.SiteID("S00")
	before := e.head()
	b := e.fx.World(1, 2, 3)
	b.Objects[0] = e.fx.Site("S00", "Moved Road")
	run := e.begin(b)
	for !run.Done() {
		e.step(run)
		row, _ := e.object(e.snap(graph.ReadApplied, 0), site)
		if row.Attrs[graph.AttrSiteAddress].Value == "Moved Road" {
			break
		}
	}
	if run.Done() {
		tb.Fatalf("the change of the site came in the last chunk: the case cannot abandon it")
		return
	}
	check(tb, e.head().AppliedSeq > before.AppliedSeq && e.head().Open(), "nothing was applied before the abandon")
	fatalIf(tb, e.a.Abandon(e.ctx, e.kg.ID, e.lease), "abandon")
	h := e.head()
	check(tb, h.AppliedSeq == before.AppliedSeq && !h.Open() && h.Hash.Equal(before.Hash), "head after abandon %+v", h)
	row, ok := e.object(e.snap(graph.ReadApplied, 0), site)
	check(tb, ok && row.Open() && row.Attrs[graph.AttrSiteAddress].Value == "Fixture Road S00", "the first row of the site did not come back: %+v", row)
	_, ok = e.object(e.snap(graph.ReadApplied, 0), e.fx.RackID(site, "R01"))
	check(tb, !ok, "a row of the abandoned chunks is still there")
	e.verify()
	res := e.apply(b)
	check(tb, res.FromSeq == before.AppliedSeq+1, "change numbers were not reused: from %d, head was %d", res.FromSeq, before.AppliedSeq)
	e.verify()
}

func caseResume(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{ChunkRows: 4})
	b := e.fx.World(2, 2, 2)
	b.ApplyID = "apply-fixed"
	run := e.begin(b)
	e.step(run)
	e.step(run)
	// A new worker takes over: a new applier, a new lease after the old one expired.
	if _, err := e.clock.Advance(time.Minute); err != nil {
		tb.Fatalf("advance: %v", err)
	}
	e.a = e.applier(graph.Options{ChunkRows: 4, Semantic: 1})
	var err error
	e.lease, err = e.a.Acquire(e.ctx, e.kg.ID)
	fatalIf(tb, err, "takeover")
	res := e.apply(b)
	h := e.head()
	check(tb, !h.Open() && h.LastApplyID == "apply-fixed" && res.ToSeq == h.AppliedSeq, "head %+v result %+v", h, res)
	other := newEnv(tb, f, graph.Options{ChunkRows: 1000})
	other.apply(other.fx.World(2, 2, 2))
	check(tb, h.Hash.Equal(other.head().Hash), "a resumed apply gives another graph hash than one that ran through")
	e.verify()
}

func caseInProgress(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{ChunkRows: 3})
	run := e.begin(e.fx.World(1, 2, 2))
	e.step(run)
	_, err := e.a.Apply(e.ctx, e.kg.ID, e.lease, e.fx.World(1, 1, 1))
	wantCode(tb, "a second apply", err, graph.CodeApplyInProgress)
	_, err = e.a.Seal(e.ctx, e.kg.ID, e.lease, graph.SealSync)
	wantCode(tb, "a seal during an apply", err, graph.CodeSealNotReady)
}

func caseDuplicate(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	b := e.fx.World(1, 1, 1)
	b.ApplyID = "apply-once"
	e.apply(b)
	before := e.head()
	res := e.apply(b)
	after := e.head()
	check(tb, res.Duplicate && !res.Changed(), "result %+v", res)
	check(tb, after.IngestSeq == before.IngestSeq && after.AppliedSeq == before.AppliedSeq, "the retry was applied again")
}

func caseStaleHead(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 1))
	h := e.head()
	w := graph.ChunkWrite{KG: e.kg.ID, Generation: e.kg.Generation, ExpectedSeq: h.AppliedSeq - 1, Lease: e.lease, Now: e.clock.Now(), Head: h}
	wantCode(tb, "stale expected head", e.store.ApplyChunk(e.ctx, w), graph.CodeHeadMoved)
	w.ExpectedSeq = h.AppliedSeq
	w.Generation = e.kg.Generation + 1
	wantCode(tb, "wrong generation", e.store.ApplyChunk(e.ctx, w), graph.CodeGenerationChanged)
	after := e.head()
	check(tb, after.AppliedSeq == h.AppliedSeq && after.Hash.Equal(h.Hash), "a refused write moved the head")
}

func caseFirstLoad(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{ChunkRows: 4})
	run := e.begin(e.fx.World(1, 2, 2))
	e.step(run)
	kg, err := e.store.GetKG(e.ctx, e.kg.ID.String())
	fatalIf(tb, err, "get KG")
	check(tb, kg.State == graph.StateBuilding, "KG state %s in the middle of the first load", kg.State)
	for !run.Done() {
		e.step(run)
	}
	kg, err = e.store.GetKG(e.ctx, e.kg.ID.String())
	fatalIf(tb, err, "get KG")
	check(tb, kg.State == graph.StateServing, "KG state %s after the first load", kg.State)
}
