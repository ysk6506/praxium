package query

import (
	"context"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// Directions of neighbors.
const (
	DirOut  = "out"
	DirIn   = "in"
	DirBoth = "both"
)

// NeighborsInput is the input of neighbors. MaxSteps defaults to 1 and is at most MaxSteps.
type NeighborsInput struct {
	Common
	ObjectID  string
	LinkTypes []string
	Meanings  []string
	Direction string
	MaxSteps  int
	PeerTypes []string
}

// NeighborGroup counts the links of one kind among the answer, without those that reach hidden objects.
type NeighborGroup struct {
	LinkType  string `json:"link_type"`
	Meaning   string `json:"meaning"`
	Direction string `json:"direction"`
	PeerType  string `json:"peer_type"`
	Count     int    `json:"count"`
}

// NeighborItem is one link of the walk and the object at its far end. Step is
// how many links from the start it lies; ViaObjectID is the object it was
// followed from (absent when that object is hidden or is the start).
type NeighborItem struct {
	LinkID          string  `json:"link_id"`
	LinkType        string  `json:"link_type"`
	Meaning         string  `json:"meaning"`
	Direction       string  `json:"direction"`
	Step            int     `json:"step"`
	ViaObjectID     *string `json:"via_object_id"`
	ViaHidden       bool    `json:"via_hidden,omitempty"`
	PeerObjectID    string  `json:"peer_object_id"`
	PeerType        string  `json:"peer_type"`
	PeerDisplayName string  `json:"peer_display_name"`
	Grade           string  `json:"grade"`
	Basis           string  `json:"basis"`
}

// NeighborsData is the body of the answer of neighbors: the groups first, then the items.
type NeighborsData struct {
	Object ObjectRef       `json:"object"`
	Groups []NeighborGroup `json:"groups"`
	Items  []NeighborItem  `json:"items,omitempty"`
}

type walkPlan struct {
	linkTypes map[int]bool
	meanings  map[model.Meaning]bool
	peerTypes map[int]bool
	dir       graph.Direction
	steps     int
}

// Neighbors answers neighbors: the links around an object, up to MaxSteps links away.
func (e *Engine) Neighbors(ctx context.Context, in NeighborsInput, f ReadFilter) (Answer[NeighborsData], error) {
	var zero Answer[NeighborsData]
	plan, err := e.planWalk(in)
	if err != nil {
		return zero, err
	}
	fp := fingerprint("neighbors", in.ObjectID, strs(in.LinkTypes), strs(in.Meanings), in.Direction, plan.steps, strs(in.PeerTypes),
		in.View, in.KGVersionID, seqPart(in.ChangeSeq), in.Shape)
	r, err := e.open(ctx, in.Common, fp, f)
	if err != nil {
		return zero, err
	}
	start, err := kernel.ParseID[graph.ObjectKind](in.ObjectID)
	if err != nil {
		return zero, kernel.Errorf(CodeObjectNotFound, "no object %q", in.ObjectID)
	}
	sv, err := e.object(ctx, r, start)
	if err != nil {
		return zero, err
	}
	if !sv.ok || !sv.dec.Visible {
		return zero, kernel.Errorf(CodeObjectNotFound, "no object %s", start)
	}
	items, err := e.walk(ctx, r, plan, start)
	if err != nil {
		return zero, err
	}
	data := NeighborsData{
		Object: ObjectRef{ObjectID: start.String(), ObjectType: e.types.byNo[sv.row.TypeNo].typ.Name, DisplayName: sv.row.Name},
		Groups: groupItems(items),
	}
	page, rc := e.pageItems(r, items, fp)
	if r.c.Shape == ShapeItems {
		data.Items = page
	}
	env, err := e.envelope(ctx, r, rc)
	if err != nil {
		return zero, err
	}
	return Answer[NeighborsData]{Meta: env, Data: data}, nil
}

func strs(in []string) []any {
	out := []any{}
	for _, s := range in {
		out = append(out, s)
	}
	return out
}

func (e *Engine) planWalk(in NeighborsInput) (*walkPlan, error) {
	p := &walkPlan{steps: in.MaxSteps}
	if p.steps == 0 {
		p.steps = DefaultMaxSteps
	}
	if p.steps < 1 || p.steps > MaxSteps {
		return nil, kernel.Errorf(CodeInput, "max_steps %d is outside 1 to %d", p.steps, MaxSteps)
	}
	switch in.Direction {
	case "", DirBoth:
		p.dir = graph.Both
	case DirOut:
		p.dir = graph.Out
	case DirIn:
		p.dir = graph.In
	default:
		return nil, kernel.Errorf(CodeInput, "direction %q is not out, in or both", in.Direction)
	}
	var err error
	if p.linkTypes, err = e.linkTypeSet(in.LinkTypes); err != nil {
		return nil, err
	}
	if p.meanings, err = meaningSet(in.Meanings); err != nil {
		return nil, err
	}
	if p.peerTypes, err = e.peerTypeSet(in.PeerTypes); err != nil {
		return nil, err
	}
	return p, nil
}

func (e *Engine) linkTypeSet(names []string) (map[int]bool, error) {
	if len(names) == 0 {
		return nil, nil
	}
	set := map[int]bool{}
	for _, n := range names {
		lt, ok := e.reg.LinkTypeByName(n)
		if !ok {
			return nil, kernel.Errorf(CodeInput, "no link type %q", n)
		}
		set[lt.LinkTypeNo] = true
	}
	return set, nil
}

func meaningSet(names []string) (map[model.Meaning]bool, error) {
	if len(names) == 0 {
		return nil, nil
	}
	known := map[model.Meaning]bool{}
	for _, m := range model.Meanings() {
		known[m] = true
	}
	set := map[model.Meaning]bool{}
	for _, m := range names {
		if !known[model.Meaning(m)] {
			return nil, kernel.Errorf(CodeInput, "no link meaning %q", m)
		}
		set[model.Meaning(m)] = true
	}
	return set, nil
}

// peerTypeSet is the types the far end may be: the named types and their subtypes.
func (e *Engine) peerTypeSet(names []string) (map[int]bool, error) {
	if len(names) == 0 {
		return nil, nil
	}
	set := map[int]bool{}
	for _, n := range names {
		root, ok := e.types.byName[n]
		if !ok {
			return nil, kernel.Errorf(CodeInput, "no object type %q", n)
		}
		for _, t := range e.types.all {
			if t.ancestors[root.typ.ID] {
				set[t.typ.TypeNo] = true
			}
		}
	}
	return set, nil
}

// walker is the state of one breadth-first walk.
type walker struct {
	e        *Engine
	r        *read
	p        *walkPlan
	seenObj  map[graph.ObjectID]bool
	seenLink map[graph.LinkID]bool
}

// walk follows the links breadth first from the start, over the whole graph,
// hidden objects included: what the caller may see is decided when an item is
// made. A hidden object found before the last step was passed through, one
// found at the last step was reached. Items come in the order (step, link id).
func (e *Engine) walk(ctx context.Context, r *read, p *walkPlan, start graph.ObjectID) ([]NeighborItem, error) {
	w := &walker{e: e, r: r, p: p, seenObj: map[graph.ObjectID]bool{start: true}, seenLink: map[graph.LinkID]bool{}}
	var items []NeighborItem
	frontier := []graph.ObjectID{start}
	for step := 1; step <= p.steps && len(frontier) > 0; step++ {
		var next []graph.ObjectID
		var stepItems []NeighborItem
		for _, node := range frontier {
			found, peers, err := w.expand(ctx, node, step)
			if err != nil {
				return nil, err
			}
			stepItems = append(stepItems, found...)
			next = append(next, peers...)
		}
		sort.Slice(stepItems, func(i, j int) bool { return stepItems[i].LinkID < stepItems[j].LinkID })
		items = append(items, stepItems...)
		frontier = next
	}
	return items, nil
}

// admits says whether a link passes the link type, meaning and view conditions.
func (w *walker) admits(l graph.LinkRow, lt model.LinkType) bool {
	return !w.seenLink[l.ID] && w.r.designed(l.Basis) && (w.p.linkTypes == nil || w.p.linkTypes[l.TypeNo]) &&
		(w.p.meanings == nil || w.p.meanings[lt.Meaning])
}

// expand reads the links of one node and returns the items they make and the
// objects reached for the first time.
func (w *walker) expand(ctx context.Context, node graph.ObjectID, step int) ([]NeighborItem, []graph.ObjectID, error) {
	nv, err := w.e.object(ctx, w.r, node)
	if err != nil {
		return nil, nil, err
	}
	links, err := w.r.snap.LinksOf(ctx, node, w.p.dir)
	if err != nil {
		return nil, nil, err
	}
	var items []NeighborItem
	var next []graph.ObjectID
	for _, l := range links {
		lt := w.e.links[l.TypeNo]
		if !w.admits(l, lt) {
			continue
		}
		w.seenLink[l.ID] = true
		dir, peer := DirOut, l.Dst
		if l.Src != node {
			dir, peer = DirIn, l.Src
		}
		pv, err := w.e.object(ctx, w.r, peer)
		if err != nil {
			return nil, nil, err
		}
		if !pv.ok {
			continue
		}
		if it, shown := w.show(l, lt, dir, step, nv, pv); shown {
			items = append(items, it)
		}
		if !w.seenObj[peer] {
			w.seenObj[peer] = true
			next = append(next, peer)
		}
	}
	return items, next, nil
}

// show makes the item of a link if its far end is visible and of a wanted
// type; a hidden far end is counted as passed or reached instead.
func (w *walker) show(l graph.LinkRow, lt model.LinkType, dir string, step int, from, peer visible) (NeighborItem, bool) {
	if !peer.dec.Visible {
		if step < w.p.steps {
			w.r.hid.passed[peer.row.ID.String()] = true
		} else {
			w.r.hid.reached[peer.row.ID.String()] = true
		}
		return NeighborItem{}, false
	}
	if w.p.peerTypes != nil && !w.p.peerTypes[peer.row.TypeNo] {
		return NeighborItem{}, false
	}
	return w.e.item(l, lt, linkStep{dir: dir, step: step, from: from}, peer), true
}

// linkStep says how a link was reached: the direction, the step and the object it was followed from.
type linkStep struct {
	dir  string
	step int
	from visible
}

func (e *Engine) item(l graph.LinkRow, lt model.LinkType, how linkStep, peer visible) NeighborItem {
	it := NeighborItem{
		LinkID: l.ID.String(), LinkType: lt.Name, Meaning: string(lt.Meaning), Direction: how.dir, Step: how.step,
		PeerObjectID: peer.row.ID.String(), PeerType: e.types.byNo[peer.row.TypeNo].typ.Name, PeerDisplayName: peer.row.Name,
		Grade: string(l.Grade), Basis: string(l.Basis),
	}
	if how.step > 1 {
		if how.from.dec.Visible {
			id := how.from.row.ID.String()
			it.ViaObjectID = &id
		} else {
			it.ViaHidden = true
		}
	}
	return it
}

func groupItems(items []NeighborItem) []NeighborGroup {
	counts := map[NeighborGroup]int{}
	for _, it := range items {
		counts[NeighborGroup{LinkType: it.LinkType, Meaning: it.Meaning, Direction: it.Direction, PeerType: it.PeerType}]++
	}
	out := make([]NeighborGroup, 0, len(counts))
	for g, n := range counts {
		g.Count = n
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.LinkType != b.LinkType:
			return a.LinkType < b.LinkType
		case a.Direction != b.Direction:
			return a.Direction < b.Direction
		}
		return a.PeerType < b.PeerType
	})
	return out
}

// pageItems cuts the items to the page after the cursor and fills the
// completeness; the evidence counts the links that are returned.
func (e *Engine) pageItems(r *read, items []NeighborItem, fp string) ([]NeighborItem, ResultComplete) {
	rc := ResultComplete{Total: len(items)}
	if r.c.Shape != ShapeItems {
		return nil, rc
	}
	start := 0
	if r.cur != nil {
		start = sort.Search(len(items), func(i int) bool {
			if items[i].Step != r.cur.Step {
				return items[i].Step > r.cur.Step
			}
			return items[i].LinkID > r.cur.After
		})
	}
	end := start + r.c.Limit
	if end < len(items) {
		rc.Truncated = true
		reason := "limit"
		rc.Reason = &reason
	} else {
		end = len(items)
	}
	page := append([]NeighborItem{}, items[start:end]...)
	rc.Returned = len(page)
	if rc.Truncated {
		last := page[len(page)-1]
		next := cursor{Gen: r.info.Generation, Seq: r.info.Seq, Version: versionOf(r.info), Query: fp, Step: last.Step, After: last.LinkID}.encode()
		rc.NextCursor = &next
	}
	for _, it := range page {
		r.use.add(model.Grade(it.Grade), model.Basis(it.Basis), "", "")
	}
	return page, rc
}
