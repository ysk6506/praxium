package graphcheck

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// PreviewCases is the suite of the change preview (REQ-410): what Applier.Preview says a
// batch changes is what applying it changes, whatever the store. It is a list of its own
// and not a part of Cases, whose names and order are fixed. An adapter's test runs it
// the same way as Cases.
func PreviewCases() []Case {
	return []Case{
		{"preview_lists_what_the_apply_changes", prevCaseMatchesApply},
		{"preview_writes_nothing", prevCaseWritesNothing},
		{"preview_refuses_what_the_apply_refuses", prevCaseRefuses},
		{"preview_waits_for_a_half_done_apply", prevCaseHalfDone},
		{"preview_fingerprint_follows_the_list_only", prevCaseFingerprint},
	}
}

// prevRow is what a store holds of one live object or link.
type prevRow struct {
	typeID string
	hash   kernel.Hash
}

// prevRows reads every live object and link, keyed by entity and id.
func prevRows(e *env) map[string]prevRow {
	e.tb.Helper()
	s := e.snap(graph.ReadLatest, 0)
	out := map[string]prevRow{}
	objs, err := s.ScanObjects(e.ctx, graph.ObjectID{}, 1<<20)
	fatalIf(e.tb, err, "scan objects")
	for _, r := range objs {
		t, _ := e.fx.Reg.ObjectTypeByNo(r.TypeNo)
		out["object|"+r.ID.String()] = prevRow{t.ID.String(), r.ContentHash}
	}
	links, err := s.ScanLinks(e.ctx, graph.LinkID{}, 1<<20)
	fatalIf(e.tb, err, "scan links")
	for _, r := range links {
		t, _ := e.fx.Reg.LinkTypeByNo(r.TypeNo)
		out["link|"+r.ID.String()] = prevRow{t.ID.String(), r.ContentHash}
	}
	return out
}

// prevDiff is what changed between two states, in the form of a preview's list.
func prevDiff(before, after map[string]prevRow) map[string]graph.ChangeItem {
	out := map[string]graph.ChangeItem{}
	for key, a := range after {
		b, was := before[key]
		if was && b.hash == a.hash {
			continue
		}
		item := graph.ChangeItem{TypeID: a.typeID, Change: graph.ChangeAdded, NewHash: a.hash}
		if was {
			item.Change, item.OldHash = graph.ChangeChanged, b.hash
		}
		item.Entity, item.ID = prevSplit(key)
		out[key] = item
	}
	for key, b := range before {
		if _, still := after[key]; !still {
			item := graph.ChangeItem{TypeID: b.typeID, Change: graph.ChangeRetired, OldHash: b.hash}
			item.Entity, item.ID = prevSplit(key)
			out[key] = item
		}
	}
	return out
}

func prevSplit(key string) (entity, id string) {
	for i := range key {
		if key[i] == '|' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

// prevCheckList compares a preview with the change an apply made. The order of the
// list is the applier's: objects before links, by id.
func prevCheckList(tb TB, what string, p graph.ChangePreview, want map[string]graph.ChangeItem, candidates int) {
	tb.Helper()
	check(tb, len(p.Changes) == len(want), "%s: the preview lists %d changes, the apply made %d", what, len(p.Changes), len(want))
	for i, c := range p.Changes {
		w, ok := want[c.Entity+"|"+c.ID]
		check(tb, ok && w == c, "%s: preview item %+v, the apply made %+v (found %v)", what, c, w, ok)
		if i > 0 {
			a, b := p.Changes[i-1], c
			inOrder := a.Entity == b.Entity && a.ID < b.ID || a.Entity == "object" && b.Entity == "link"
			check(tb, inOrder, "%s: the list is out of order at %d", what, i)
		}
	}
	s := p.Summary
	byChange := 0
	for _, n := range s.ByChange {
		byChange += n
	}
	check(tb, s.Candidates == candidates && s.Changed == len(want) && s.Unchanged == candidates-len(want),
		"%s: summary %+v for %d candidates and %d changes", what, s, candidates, len(want))
	check(tb, byChange == len(want) && s.ByEntity["object"]+s.ByEntity["link"] == len(want), "%s: summary counts %+v", what, s)
	typed := 0
	for _, n := range s.ByType {
		typed += n
	}
	check(tb, typed == len(want), "%s: the types count %d of %d", what, typed, len(want))
}

func prevCandidates(b graph.Batch) int {
	return len(b.Objects) + len(b.Links) + len(b.RetireObjects) + len(b.RetireLinks)
}

// prevServer is a server of the random world and where it is mounted.
type prevServer struct {
	serial string
	rack   graph.ObjectID
	model  string
	pos    int64
}

// prevWorld is the random world: the servers that are live and the racks they may be in.
type prevWorld struct {
	e       *env
	rng     *kernel.SeededRng
	racks   []graph.ObjectID
	sites   []string
	servers []prevServer
	fresh   int
}

func (w *prevWorld) prevPick(n int) int { return int(w.rng.Uint64() % uint64(n)) }

// prevStart applies the first world and returns it.
func prevStart(e *env, seed uint64) *prevWorld {
	w := &prevWorld{e: e, rng: kernel.NewSeededRng(seed)}
	e.apply(e.fx.World(2, 2, 2))
	for s := 0; s < 2; s++ {
		code := fmt.Sprintf("S%02d", s)
		w.sites = append(w.sites, code)
		for r := 0; r < 2; r++ {
			rack := e.fx.RackID(e.fx.SiteID(code), fmt.Sprintf("R%02d", r))
			w.racks = append(w.racks, rack)
			for k := 0; k < 2; k++ {
				w.servers = append(w.servers, prevServer{fmt.Sprintf("SN-%02d%02d%02d", s, r, k), rack, "M1", int64(k + 1)})
			}
		}
	}
	return w
}

// prevBatch makes a batch of a random mix against the world: servers left alone, written
// again as they are, with another model, mounted at another position, or retired with their
// link, new servers, a site that may change its address, a retirement of what was never
// there, and power states.
func (w *prevWorld) prevBatch() (graph.Batch, []prevServer) {
	fx := w.e.fx
	b := graph.Batch{Kind: graph.BatchSync, SourceID: "preview"}
	var next []prevServer
	for _, s := range w.servers {
		id := fx.ServerID(s.serial)
		switch w.prevPick(6) {
		case 0:
		case 1:
			b.Objects = append(b.Objects, fx.Server(s.serial, s.model))
			b.Links = append(b.Links, fx.MountedIn(id, s.rack, s.pos))
		case 2:
			s.model = fmt.Sprintf("M%d", 2+w.prevPick(3))
			b.Objects = append(b.Objects, fx.Server(s.serial, s.model))
		case 3:
			s.pos = int64(1 + w.prevPick(40))
			b.Links = append(b.Links, fx.MountedIn(id, s.rack, s.pos))
		case 4:
			b.RetireObjects = append(b.RetireObjects, id)
			b.RetireLinks = append(b.RetireLinks, fx.MountedInID(id, s.rack))
			continue
		default:
			b.Volatile = append(b.Volatile, fx.Power(id, "off", fmt.Sprintf("2030-01-01T00:%02d:00Z", w.prevPick(60))))
		}
		next = append(next, s)
	}
	for i := w.prevPick(3); i > 0; i-- {
		s := prevServer{fmt.Sprintf("SN-N%03d", w.fresh), w.racks[w.prevPick(len(w.racks))], "M1", int64(1 + w.prevPick(40))}
		w.fresh++
		b.Objects = append(b.Objects, fx.Server(s.serial, s.model))
		b.Links = append(b.Links, fx.MountedIn(fx.ServerID(s.serial), s.rack, s.pos))
		next = append(next, s)
	}
	if w.prevPick(2) == 0 {
		b.Objects = append(b.Objects, fx.Site(w.sites[w.prevPick(2)], fmt.Sprintf("Fixture Road %d", w.prevPick(2))))
	}
	if w.prevPick(2) == 0 {
		ghost := fx.ServerID("SN-GHOST")
		b.RetireObjects = append(b.RetireObjects, ghost)
		b.RetireLinks = append(b.RetireLinks, fx.MountedInID(ghost, w.racks[0]))
	}
	return b, next
}

// prevCaseMatchesApply is the property of REQ-410: the preview of a batch is what the apply of the batch changes,
// for a random state and a random batch, cut into several chunks.
func prevCaseMatchesApply(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{ChunkRows: 3})
	w := prevStart(e, 410)
	for round := 0; round < 12; round++ {
		b, next := w.prevBatch()
		what := fmt.Sprintf("round %d", round)
		before := prevRows(e)
		p, err := e.a.Preview(e.ctx, e.kg.ID, b)
		fatalIf(tb, err, what+": preview")
		e.apply(b)
		w.servers = next
		want := prevDiff(before, prevRows(e))
		prevCheckList(tb, what, p, want, prevCandidates(b))
		check(tb, p.Fingerprint != kernel.Hash{}, "%s: no fingerprint", what)
		again, err := e.a.Preview(e.ctx, e.kg.ID, b)
		fatalIf(tb, err, what+": preview after the apply")
		check(tb, len(again.Changes) == 0 && again.Summary.Unchanged == prevCandidates(b), "%s: the applied batch still changes %d things", what, len(again.Changes))
	}
	e.verify()
}

// prevCaseWritesNothing: a preview leaves the head, the lease, the version list and the feeds as they were, and
// it needs no lease.
func prevCaseWritesNothing(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{ChunkRows: 4})
	w := prevStart(e, 411)
	b, _ := w.prevBatch()
	b.Objects = append(b.Objects, e.fx.Server("SN-NEW", "M1"))
	before := prevMarkOf(e)
	p, err := e.a.Preview(e.ctx, e.kg.ID, b)
	fatalIf(tb, err, "preview")
	check(tb, len(p.Changes) > 0, "the batch changes nothing")
	prevSameMark(tb, "after a preview", before, prevMarkOf(e))
	other := e.applier(graph.Options{})
	q, err := other.Preview(e.ctx, e.kg.ID, b)
	fatalIf(tb, err, "preview by an applier without a lease")
	check(tb, q.Fingerprint == p.Fingerprint, "two appliers see two fingerprints")
	prevSameMark(tb, "after a preview without a lease", before, prevMarkOf(e))
	e.apply(b)
	e.verify()
}

type prevMark struct {
	head                      graph.Head
	lease                     graph.Lease
	versions, orders, changes int
	objects, links            int
}

func prevMarkOf(e *env) prevMark {
	e.tb.Helper()
	m := prevMark{head: e.head()}
	var err error
	m.lease, _, err = e.store.Lease(e.ctx, e.kg.ID)
	fatalIf(e.tb, err, "lease")
	versions, err := e.store.Versions(e.ctx, e.kg.ID)
	fatalIf(e.tb, err, "versions")
	orders, err := e.store.OrderEvents(e.ctx, e.kg.ID, 0, 1<<20)
	fatalIf(e.tb, err, "order events")
	changes, err := e.store.AppliedChanges(e.ctx, e.kg.ID, 0, 1<<20)
	fatalIf(e.tb, err, "applied changes")
	m.versions, m.orders, m.changes = len(versions), len(orders), len(changes)
	m.objects, m.links = e.count(e.snap(graph.ReadLatest, 0))
	return m
}

func prevSameMark(tb TB, what string, a, b prevMark) {
	tb.Helper()
	check(tb, prevSameHead(a.head, b.head), "%s: the head moved: %+v to %+v", what, a.head, b.head)
	check(tb, a.lease.Owner == b.lease.Owner && a.lease.Attempt == b.lease.Attempt && a.lease.Until.Equal(b.lease.Until),
		"%s: the lease moved: %+v to %+v", what, a.lease, b.lease)
	check(tb, a.versions == b.versions && a.orders == b.orders && a.changes == b.changes && a.objects == b.objects && a.links == b.links,
		"%s: rows were written: %+v to %+v", what, a, b)
}

func prevSameHead(a, b graph.Head) bool {
	return a.AppliedSeq == b.AppliedSeq && a.ReadableThrough == b.ReadableThrough && a.IngestSeq == b.IngestSeq &&
		a.InputDigest == b.InputDigest && a.Hash.Equal(b.Hash) && a.ApplyID == b.ApplyID && a.LastApplyID == b.LastApplyID
}

// prevCaseRefuses: a batch that the apply refuses is refused by the preview with the same error.
func prevCaseRefuses(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 1))
	site, rack, server := e.fx.SiteID("S00"), e.fx.RackID(e.fx.SiteID("S00"), "R00"), e.fx.ServerID("SN-000000")
	cases := []struct {
		name  string
		batch graph.Batch
	}{
		{"link without a binding", graph.Batch{Links: []graph.LinkUpsert{e.fx.LocatedIn(server, rack)}}},
		{"same object twice", graph.Batch{Objects: []graph.ObjectUpsert{e.fx.Site("S00", "a"), e.fx.Site("S00", "b")}}},
		{"unknown ordering event", graph.Batch{Event: "other"}},
		{"link end that is not live", graph.Batch{Links: []graph.LinkUpsert{e.fx.LocatedIn(rack, e.fx.SiteID("S77"))}}},
		{"retired object with a link", graph.Batch{RetireObjects: []graph.ObjectID{site}}},
		{"a rack in two sites", graph.Batch{Objects: []graph.ObjectUpsert{e.fx.Site("S01", "x")}, Links: []graph.LinkUpsert{e.fx.LocatedIn(rack, e.fx.SiteID("S01"))}}},
		{"volatile value of an object that is not live", graph.Batch{Volatile: []graph.VolatileWrite{e.fx.Power(e.fx.ServerID("SN-GHOST"), "on", "2030-01-01T00:00:00Z")}}},
	}
	for _, c := range cases {
		_, perr := e.a.Preview(e.ctx, e.kg.ID, c.batch)
		_, aerr := e.a.Apply(e.ctx, e.kg.ID, e.lease, c.batch)
		check(tb, perr != nil && aerr != nil, "%s: preview error %v, apply error %v", c.name, perr, aerr)
		if perr != nil && aerr != nil {
			check(tb, kernel.CodeOf(perr) == kernel.CodeOf(aerr) && perr.Error() == aerr.Error(), "%s: preview says %q, apply says %q", c.name, perr, aerr)
		}
	}
	ok := graph.Batch{Objects: []graph.ObjectUpsert{e.fx.Site("S00", "Another Road")}}
	_, err := e.a.Preview(e.ctx, e.kg.ID, ok)
	fatalIf(tb, err, "a good batch")
}

// prevCaseHalfDone: while an apply is half done the preview says so instead of computing.
func prevCaseHalfDone(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{ChunkRows: 3})
	e.apply(e.fx.World(1, 1, 1))
	b := e.fx.World(2, 2, 2)
	b.ApplyID = "apply-half"
	run := e.begin(b)
	e.step(run)
	before := prevMarkOf(e)
	for _, other := range []graph.Batch{b, e.fx.World(1, 1, 1)} {
		_, err := e.a.Preview(e.ctx, e.kg.ID, other)
		wantCode(tb, "preview of a half done apply", err, graph.CodeApplyInProgress)
	}
	prevSameMark(tb, "after the refused previews", before, prevMarkOf(e))
	fatalIf(tb, e.a.Abandon(e.ctx, e.kg.ID, e.lease), "abandon")
	p, err := e.a.Preview(e.ctx, e.kg.ID, b)
	fatalIf(tb, err, "preview after the abandon")
	check(tb, len(p.Changes) > 0, "nothing to change after the abandon")
}

// prevStamped is the batch again with every value the source saw at another time, from another record, under another apply id.
func prevStamped(b graph.Batch, stamp string) graph.Batch {
	out := b
	out.ApplyID, out.EventRef = "apply-"+stamp, "ref-"+stamp
	out.Objects = append([]graph.ObjectUpsert(nil), b.Objects...)
	for i := range out.Objects {
		out.Objects[i].Attrs = prevAttrsAt(out.Objects[i].Attrs, stamp)
		out.Objects[i].SourceRefs = []string{stamp}
	}
	out.Links = append([]graph.LinkUpsert(nil), b.Links...)
	for i := range out.Links {
		out.Links[i].Attrs = prevAttrsAt(out.Links[i].Attrs, stamp)
		out.Links[i].SourceRefs = []string{stamp}
	}
	out.Volatile = append([]graph.VolatileWrite(nil), b.Volatile...)
	for i := range out.Volatile {
		out.Volatile[i].SeenAt = stamp
	}
	return out
}

func prevAttrsAt(attrs map[int]graph.Attr, stamp string) map[int]graph.Attr {
	out := map[int]graph.Attr{}
	for no, a := range attrs {
		a.SourceTime, a.SourceRef = stamp, stamp
		out[no] = a
	}
	return out
}

func prevFingerprint(e *env, b graph.Batch) graph.ChangePreview {
	e.tb.Helper()
	p, err := e.a.Preview(e.ctx, e.kg.ID, b)
	fatalIf(e.tb, err, "preview")
	return p
}

// prevCaseFingerprint: the fingerprint is made of the list and of nothing that changes from one run to the next.
func prevCaseFingerprint(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 2))
	s0, s1 := e.fx.ServerID("SN-000000"), e.fx.ServerID("SN-000001")
	rack := e.fx.RackID(e.fx.SiteID("S00"), "R00")
	base := graph.Batch{Kind: graph.BatchSync, SourceID: "preview",
		Objects: []graph.ObjectUpsert{e.fx.Server("SN-000000", "M9"), e.fx.Server("SN-NEW", "M1")},
		Links:   []graph.LinkUpsert{e.fx.MountedIn(s1, rack, 7), e.fx.MountedIn(e.fx.ServerID("SN-NEW"), rack, 9)}}
	p := prevFingerprint(e, base)
	check(tb, len(p.Changes) == 4, "the base batch changes %d things, wanted 4", len(p.Changes))
	check(tb, prevFingerprint(e, base).Fingerprint == p.Fingerprint, "the same batch gave two fingerprints")

	stamped := prevFingerprint(e, prevStamped(base, "2031-05-05T05:05:05Z"))
	check(tb, stamped.Fingerprint == p.Fingerprint && reflect.DeepEqual(stamped.Changes, p.Changes), "times and references changed the fingerprint")

	flipped := base
	flipped.Objects = []graph.ObjectUpsert{base.Objects[1], base.Objects[0]}
	flipped.Links = []graph.LinkUpsert{base.Links[1], base.Links[0]}
	check(tb, prevFingerprint(e, flipped).Fingerprint == p.Fingerprint, "the order of the batch changed the fingerprint")

	noop := base
	noop.RetireObjects = []graph.ObjectID{e.fx.ServerID("SN-GHOST")}
	noop.Objects = append(append([]graph.ObjectUpsert(nil), base.Objects...), e.fx.Site("S00", "Fixture Road S00"))
	check(tb, prevFingerprint(e, noop).Fingerprint == p.Fingerprint, "things that stay as they are changed the fingerprint")

	value := base
	value.Objects = []graph.ObjectUpsert{e.fx.Server("SN-000000", "M8"), base.Objects[1]}
	check(tb, prevFingerprint(e, value).Fingerprint != p.Fingerprint, "another value kept the fingerprint")

	more := base
	more.Objects = append(append([]graph.ObjectUpsert(nil), base.Objects...), e.fx.Site("S00", "Moved Road"))
	check(tb, prevFingerprint(e, more).Fingerprint != p.Fingerprint, "one more change kept the fingerprint")

	fewer := base
	fewer.Links = base.Links[:1]
	fewer.Objects = base.Objects[:1]
	check(tb, prevFingerprint(e, fewer).Fingerprint != p.Fingerprint, "one change less kept the fingerprint")

	retire := graph.Batch{RetireObjects: []graph.ObjectID{s0}, RetireLinks: []graph.LinkID{e.fx.MountedInID(s0, rack)}}
	r := prevFingerprint(e, retire)
	check(tb, len(r.Changes) == 2 && r.Summary.ByChange[graph.ChangeRetired] == 2, "retirement summary %+v", r.Summary)
	sorted := sort.SliceIsSorted(r.Changes, func(i, j int) bool { return r.Changes[i].Entity > r.Changes[j].Entity })
	check(tb, sorted, "objects come before links")
}
