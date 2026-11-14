package query

import (
	"context"
	"sort"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The channels that a supplied_by link fills, in the order of the arrays.
var supplyChannels = [4]model.Channel{model.ChannelPower, model.ChannelCooling, model.ChannelNetwork, model.ChannelStorage}

func channelIndex(c model.Channel) int {
	for i, ch := range supplyChannels {
		if ch == c {
			return i
		}
	}
	return -1
}

// depKind says what a dependency edge of an object means for its state.
type depKind uint8

const (
	// kRunsOn: the service follows the target's service; a failover group may recover it.
	kRunsOn depKind = iota
	// kRequires: the service follows the target's service (hard) or is only degraded (soft).
	kRequires
	// kRealized: the service follows the target's service.
	kRealized
	// kPartOfWhole: a part fails when its whole fails.
	kPartOfWhole
	// kWholeNeeds: a whole fails (hard) or is degraded (soft) when its part fails.
	kWholeNeeds
	// kSupply: one channel follows the target's service.
	kSupply
	// kManage: the management channel follows the target's service.
	kManage
)

// edge is one dependency of an object: its state depends on the state of To.
type edge struct {
	kind     depKind
	ch       int8
	soft     bool
	to       int32
	link     int32
	failover []int32
}

// spreadsExposure says whether an exposed target exposes the object that depends on it.
func (d *edge) spreadsExposure() bool {
	switch d.kind {
	case kRunsOn, kRealized, kSupply:
		return true
	case kRequires:
		return !d.soft
	}
	return false
}

// Weakness of a link: how far its evidence carries a failure.
const (
	weakNone    uint8 = 0
	weakAssumed uint8 = 1
	weakUnknown uint8 = 2
	weakStale   uint8 = 3
)

// linkMeta is what the answer says about a link.
type linkMeta struct {
	id       graph.LinkID
	typeName string
	meaning  model.Meaning
	grade    model.Grade
	basis    model.Basis
	seen     string
	delay    string
	weak     uint8
	// cut is set on a link that the question names in dead_links; src and dst are the nodes it joins.
	cut      bool
	src, dst int32
}

// member is a member of a redundancy group and the link that makes it one.
type member struct {
	node int32
	link int32
}

// kInfo is the min_alive of a group and what it stands on.
type kInfo struct {
	present bool
	value   int32
	grade   model.Grade
}

// node is one object as the calculation sees it.
type node struct {
	id       graph.ObjectID
	idText   string
	typeNo   int
	typeName string
	name     string
	visible  bool
	need     [4]uint8
	supplied [4]bool
	manager  bool
	deps     []edge
	group    bool
	capacity bool
	k        kInfo
	members  []member
	children []member
	memberOf []int32
	parent   int32
	users    []int32
}

// Needs of a channel: no declaration, hard, soft.
const (
	needNone uint8 = 0
	needHard uint8 = 1
	needSoft uint8 = 2
)

// depGraph is the part of the KG the calculation reads: every object and the links that carry state.
type depGraph struct {
	nodes     []node
	links     []linkMeta
	byID      map[graph.ObjectID]int32
	weakLinks int
	stale     int
	capacity  bool
	cutFound  map[graph.LinkID]bool
}

// loadOptions are the conditions of one load.
type loadOptions struct {
	deadLinks map[graph.LinkID]bool
	maxStale  time.Duration
	now       time.Time
}

// pendingFailover is a runs_on edge that still needs its groups: they are found once every membership is read.
type pendingFailover struct {
	node  int32
	edge  int
	types []string
}

// linkParams are the parameters of a link's meaning, resolved from its link type and its binding.
type linkParams struct {
	soft     bool
	whole    string
	resource string
	failover []string
}

// loader holds the state of one load.
type loader struct {
	e       *Engine
	r       *read
	opt     loadOptions
	g       *depGraph
	pend    []pendingFailover
	params  map[[3]int]linkParams
	groupID model.ObjectTypeID
}

// loadGraph reads every object and link of the snapshot into the calculation's form.
func (e *Engine) loadGraph(ctx context.Context, r *read, opt loadOptions) (*depGraph, error) {
	root, _ := e.reg.ObjectTypeByName("RedundancyGroup")
	l := &loader{e: e, r: r, opt: opt, g: &depGraph{byID: map[graph.ObjectID]int32{}, cutFound: map[graph.LinkID]bool{}}, params: map[[3]int]linkParams{}, groupID: root.ID}
	if err := l.readObjects(ctx); err != nil {
		return nil, err
	}
	if err := l.readLinks(ctx); err != nil {
		return nil, err
	}
	l.resolveFailover()
	l.buildUsers()
	return l.g, nil
}

func (l *loader) readObjects(ctx context.Context) error {
	var after graph.ObjectID
	for {
		page, err := l.r.snap.ScanObjects(ctx, after, scanPage)
		if err != nil {
			return err
		}
		for _, row := range page {
			after = row.ID
			l.addNode(row)
		}
		if len(page) < scanPage {
			return nil
		}
	}
}

func (l *loader) addNode(row graph.ObjectRow) {
	info := l.e.types.byNo[row.TypeNo]
	if info == nil {
		return
	}
	n := node{id: row.ID, idText: row.ID.String(), typeNo: row.TypeNo, typeName: info.typ.Name, name: row.Name, parent: -1,
		visible: l.e.judge(l.r, row).Visible}
	for _, need := range l.e.reg.EffectiveNeeds(info.typ.ID) {
		if i := channelIndex(need.Channel); i >= 0 {
			n.need[i] = needHard
			if need.Strength == model.StrengthSoft {
				n.need[i] = needSoft
			}
		}
		n.manager = n.manager || need.Channel == model.ChannelManagement
	}
	if info.ancestors[l.groupID] {
		l.readGroup(&n, info, row)
	}
	l.g.byID[row.ID] = int32(len(l.g.nodes))
	l.g.nodes = append(l.g.nodes, n)
}

// readGroup reads min_alive and the kind of threshold of a redundancy group.
func (l *loader) readGroup(n *node, info *typeInfo, row graph.ObjectRow) {
	n.group = true
	if p, ok := info.byName["min_alive"]; ok {
		if a, has := row.Attrs[p.AttrNo]; has {
			if v, isNum := wholeNumber(a.Value); isNum {
				n.k = kInfo{present: true, value: v, grade: a.Grade}
			}
		}
	}
	if p, ok := info.byName["threshold_kind"]; ok {
		if a, has := row.Attrs[p.AttrNo]; has && valueText(a.Value) == "capacity_sum" {
			n.capacity = true
			l.g.capacity = true
		}
	}
}

func wholeNumber(v any) (int32, bool) {
	switch x := v.(type) {
	case int64:
		return int32(x), true
	case int:
		return int32(x), true
	case float64:
		return int32(x), x == float64(int32(x))
	}
	return 0, false
}

func (l *loader) readLinks(ctx context.Context) error {
	var after graph.LinkID
	for {
		page, err := l.r.snap.ScanLinks(ctx, after, scanPage)
		if err != nil {
			return err
		}
		for _, row := range page {
			after = row.ID
			l.addLink(row)
		}
		if len(page) < scanPage {
			return nil
		}
	}
}

// weakness grades how far the evidence of a link carries a failure at the time of the read.
func (l *loader) weakness(row graph.LinkRow, seen string) uint8 {
	w := weakNone
	switch row.Grade {
	case model.GradeAssumed:
		w = weakAssumed
	case model.GradeUnknown:
		w = weakUnknown
	}
	if row.Basis != model.BasisObserved || l.opt.maxStale <= 0 || seen == "" {
		return w
	}
	t, err := time.Parse(time.RFC3339, seen)
	if err == nil && l.opt.now.Sub(t) > l.opt.maxStale {
		l.g.stale++
		return weakStale
	}
	return w
}

// linkSeen is the newest time the source saw any attribute of a link; it is empty when none was recorded.
func linkSeen(row graph.LinkRow) string {
	var best string
	for _, a := range row.Attrs {
		if a.SourceTime > best {
			best = a.SourceTime
		}
	}
	return best
}

func (l *loader) addLink(row graph.LinkRow) {
	cut := l.opt.deadLinks[row.ID]
	if cut {
		l.g.cutFound[row.ID] = true
	}
	lt, ok := l.e.links[row.TypeNo]
	if !ok || !l.r.designed(row.Basis) {
		return
	}
	src, ok1 := l.g.byID[row.Src]
	dst, ok2 := l.g.byID[row.Dst]
	if !ok1 || !ok2 {
		return
	}
	switch lt.Meaning {
	case model.MeaningRunsOn, model.MeaningRequires, model.MeaningSuppliedBy, model.MeaningRealizedBy, model.MeaningPartOf,
		model.MeaningManagedBy, model.MeaningMemberOf, model.MeaningContainedIn:
	default:
		return
	}
	seen := linkSeen(row)
	meta := linkMeta{id: row.ID, typeName: lt.Name, meaning: lt.Meaning, grade: row.Grade, basis: row.Basis, seen: seen,
		delay: string(lt.DelayClass), weak: l.weakness(row, seen), cut: cut, src: src, dst: dst}
	if meta.weak != weakNone {
		l.g.weakLinks++
	}
	idx := int32(len(l.g.links))
	l.g.links = append(l.g.links, meta)
	l.wire(lt, src, dst, idx)
}

// params resolves the parameters of a link's meaning: the link type fixes some, the binding of the two types narrows the rest.
func (l *loader) paramsOf(lt model.LinkType, src, dst int32) linkParams {
	sn, dn := &l.g.nodes[src], &l.g.nodes[dst]
	key := [3]int{lt.LinkTypeNo, sn.typeNo, dn.typeNo}
	if p, ok := l.params[key]; ok {
		return p
	}
	p := linkParams{whole: "none", resource: lt.MeaningParams["resource"]}
	var constraints map[string][]string
	if b, ok := l.e.reg.BindingFor(l.e.types.byNo[sn.typeNo].typ.ID, l.e.types.byNo[dn.typeNo].typ.ID, lt.ID); ok {
		constraints = b.ParamConstraints
	}
	strength := lt.MeaningParams["strength"]
	if strength == "" && len(constraints["strength"]) == 1 {
		strength = constraints["strength"][0]
	}
	p.soft = strength == "soft"
	whole := lt.MeaningParams["whole_needs_part"]
	if whole == "" {
		whole = pickWhole(constraints["whole_needs_part"])
	}
	if whole != "" {
		p.whole = whole
	}
	p.failover = constraints["failover_to"]
	l.params[key] = p
	return p
}

// pickWhole reads the allowed values of whole_needs_part: one value is that value; several are read as the weaker, soft.
func pickWhole(values []string) string {
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	}
	return "soft"
}

// wire turns a link into the edges it means.
func (l *loader) wire(lt model.LinkType, src, dst, idx int32) {
	p := l.paramsOf(lt, src, dst)
	s, d := &l.g.nodes[src], &l.g.nodes[dst]
	switch lt.Meaning {
	case model.MeaningRunsOn:
		s.deps = append(s.deps, edge{kind: kRunsOn, to: dst, link: idx})
		if len(p.failover) > 0 {
			l.pend = append(l.pend, pendingFailover{node: src, edge: len(s.deps) - 1, types: p.failover})
		}
	case model.MeaningRequires:
		s.deps = append(s.deps, edge{kind: kRequires, soft: p.soft, to: dst, link: idx})
	case model.MeaningRealizedBy:
		s.deps = append(s.deps, edge{kind: kRealized, to: dst, link: idx})
	case model.MeaningSuppliedBy:
		if ch := channelIndex(model.Channel(p.resource)); ch >= 0 {
			s.deps = append(s.deps, edge{kind: kSupply, ch: int8(ch), to: dst, link: idx})
			s.supplied[ch] = true
		}
	case model.MeaningManagedBy:
		s.deps = append(s.deps, edge{kind: kManage, to: dst, link: idx})
	case model.MeaningPartOf:
		s.deps = append(s.deps, edge{kind: kPartOfWhole, to: dst, link: idx})
		if p.whole != "none" {
			d.deps = append(d.deps, edge{kind: kWholeNeeds, soft: p.whole == "soft", to: src, link: idx})
		}
	default:
		l.wireStructure(lt.Meaning, src, dst, idx)
	}
}

// wireStructure wires the links that group and contain objects.
func (l *loader) wireStructure(meaning model.Meaning, src, dst, idx int32) {
	s, d := &l.g.nodes[src], &l.g.nodes[dst]
	switch meaning {
	case model.MeaningMemberOf:
		if d.group {
			d.members = append(d.members, member{node: src, link: idx})
			s.memberOf = append(s.memberOf, dst)
		}
	case model.MeaningContainedIn:
		if s.parent < 0 {
			s.parent = dst
		}
		d.children = append(d.children, member{node: src, link: idx})
	}
}

// resolveFailover finds the groups a runs_on edge may fail over to: groups of the declared type that the target is a member of.
func (l *loader) resolveFailover() {
	for _, p := range l.pend {
		target := l.g.nodes[l.g.nodes[p.node].deps[p.edge].to]
		var groups []int32
		for _, g := range target.memberOf {
			if l.groupHasType(g, p.types) {
				groups = append(groups, g)
			}
		}
		l.g.nodes[p.node].deps[p.edge].failover = groups
	}
}

func (l *loader) groupHasType(g int32, types []string) bool {
	info := l.e.types.byNo[l.g.nodes[g].typeNo]
	for _, t := range types {
		if id, err := kernel.ParseID[model.ObjectTypeKind](t); err == nil && info.ancestors[id] {
			return true
		}
	}
	return false
}

// buildUsers lists, for every object, the objects whose state must be worked out again when its state changes.
func (l *loader) buildUsers() {
	nodes := l.g.nodes
	for x := range nodes {
		for _, d := range nodes[x].deps {
			nodes[d.to].users = append(nodes[d.to].users, int32(x))
			for _, g := range d.failover {
				nodes[g].users = append(nodes[g].users, int32(x))
			}
		}
		for _, m := range nodes[x].members {
			nodes[m.node].users = append(nodes[m.node].users, int32(x))
		}
	}
	for x := range nodes {
		nodes[x].users = uniqueSorted(nodes[x].users)
	}
}

func uniqueSorted(in []int32) []int32 {
	if len(in) < 2 {
		return in
	}
	sort.Slice(in, func(i, j int) bool { return in[i] < in[j] })
	out := in[:1]
	for _, v := range in[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}
