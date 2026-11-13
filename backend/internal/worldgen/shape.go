package worldgen

import (
	"math"
	"sort"
)

// ShapeFormat names the shape card (docs/design-docs/worldgen.md).
const (
	ShapeFormat = "praxium-worldgen-shape"
	ShapeFile   = "shape-card.json"
)

// Bucket is a range of values (Lo..Hi) and how many values fell in it.
type Bucket struct {
	Lo int   `json:"lo"`
	Hi int   `json:"hi"`
	N  int64 `json:"n"`
}

// Dist is the distribution of a count: summary numbers and power-of-two buckets.
type Dist struct {
	Count   int64    `json:"count"`
	Mean    float64  `json:"mean"`
	Max     int      `json:"max"`
	P50     int      `json:"p50"`
	P90     int      `json:"p90"`
	P99     int      `json:"p99"`
	Buckets []Bucket `json:"buckets"`
}

// distCounts accumulates a distribution value by value.
type distCounts map[int]int64

func (d distCounts) add(v int) { d[v]++ }

func (d distCounts) dist() Dist {
	keys := make([]int, 0, len(d))
	var n int64
	var sum float64
	for k, c := range d {
		keys = append(keys, k)
		n += c
		sum += float64(k) * float64(c)
	}
	sort.Ints(keys)
	out := Dist{Count: n, Buckets: []Bucket{}}
	if n == 0 {
		return out
	}
	out.Mean = math.Round(sum/float64(n)*1000) / 1000
	out.Max = keys[len(keys)-1]
	var run int64
	for _, k := range keys {
		run += d[k]
		for _, q := range []struct {
			at  float64
			dst *int
		}{{0.5, &out.P50}, {0.9, &out.P90}, {0.99, &out.P99}} {
			if *q.dst == 0 && float64(run) >= q.at*float64(n) {
				*q.dst = k
			}
		}
		lo, hi := bucketOf(k)
		if m := len(out.Buckets); m > 0 && out.Buckets[m-1].Lo == lo {
			out.Buckets[m-1].N += d[k]
		} else {
			out.Buckets = append(out.Buckets, Bucket{lo, hi, d[k]})
		}
	}
	return out
}

// bucketOf gives the bucket of v: 0, 1, 2-3, 4-7, 8-15, and so on.
func bucketOf(v int) (lo, hi int) {
	if v <= 0 {
		return 0, 0
	}
	lo = 1
	for lo*2 <= v {
		lo *= 2
	}
	return lo, lo*2 - 1
}

// GroupShape describes the groups of one type.
type GroupShape struct {
	Groups  int64            `json:"groups"`
	Members Dist             `json:"members"`
	Slack   Dist             `json:"slack"`
	Modes   map[string]int64 `json:"modes"`
}

// CrossSite counts the links whose ends are in different sites.
type CrossSite struct {
	Links     int64   `json:"links"`
	Cross     int64   `json:"cross"`
	Ratio     float64 `json:"ratio"`
	CallEdges int64   `json:"call_edges"`
	CallCross int64   `json:"call_cross"`
	CallRatio float64 `json:"call_ratio"`
}

// ShapeCard is the shape of a world (or of a knowledge graph): what
// query-engine-shape.md asks for to set the generator again.
type ShapeCard struct {
	Format             string                `json:"format"`
	Version            int                   `json:"version"`
	Objects            int64                 `json:"objects"`
	Links              int64                 `json:"links"`
	OutDegree          Dist                  `json:"out_degree"`
	InDegree           Dist                  `json:"in_degree"`
	ContainmentDepth   Dist                  `json:"containment_depth"`
	ServiceDepth       Dist                  `json:"service_depth"`
	ServiceCallsPerSvc float64               `json:"service_calls_per_service"`
	ServicesInCycles   int64                 `json:"services_unreached_from_roots"`
	CyclePairs         int64                 `json:"service_cycle_pairs"`
	Groups             map[string]GroupShape `json:"groups"`
	CrossSite          CrossSite             `json:"cross_site"`
}

type node struct {
	out, in int32
	depth   int8
	typ     int8
	members int32
	minA    int32
	mode    string
}

// ShapeCollector is an Observer that folds the statistics site by site, so
// its memory is that of the largest site and not of the world.
type ShapeCollector struct {
	nodes map[string]*node
	calls [][2]string
	card  ShapeCard

	out, in, depth, svc distCounts
	groups              map[string]*groupAcc
	callTotal           int64
	services            int64
	unreached           int64
	cycles              int64
	names               [nTypes]string
}

type groupAcc struct {
	n       int64
	members distCounts
	slack   distCounts
	modes   map[string]int64
}

// NewShapeCollector makes an empty collector.
func NewShapeCollector() *ShapeCollector {
	return &ShapeCollector{nodes: map[string]*node{}, out: distCounts{}, in: distCounts{}, depth: distCounts{}, svc: distCounts{},
		groups: map[string]*groupAcc{}, card: ShapeCard{Format: ShapeFormat, Version: FormatVersion}, names: typeNames}
}

// Object starts a node.
func (c *ShapeCollector) Object(o *Object) error {
	n := &node{depth: 1, typ: int8(o.T.idx)}
	if s, ok := o.get("min_alive"); ok {
		n.minA = int32(atoi(s))
		n.mode = o.str("mode")
		n.members = -1
	}
	c.nodes[o.ID] = n
	c.card.Objects++
	if o.T.idx == tAppService {
		c.services++
	}
	return nil
}

func atoi(s string) int {
	n := 0
	for i := 0; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		n = n*10 + int(s[i]-'0')
	}
	return n
}

// Link counts a link in the degrees, the depth and the cross-site ratio.
func (c *ShapeCollector) Link(l *Link) error {
	c.card.Links++
	c.card.CrossSite.Links++
	cross := siteOf(l.From.id) != siteOf(l.To.id) && l.T.idx != lLocatedIn
	if cross {
		c.card.CrossSite.Cross++
	}
	from, to := c.nodes[l.From.id], c.nodes[l.To.id]
	if from != nil {
		from.out++
	}
	if to != nil {
		to.in++
	}
	switch l.T.idx {
	case lLocatedIn, lPartOf, lMountedIn:
		deepen(from, to)
	case lMemberOf:
		member(to)
	case lCalls:
		c.call(l, cross)
	}
	return nil
}

// deepen puts a part one level below its whole.
func deepen(part, whole *node) {
	if part == nil {
		return
	}
	part.depth = 2
	if whole != nil && whole.depth > 0 {
		part.depth = whole.depth + 1
	}
}

func member(group *node) {
	if group == nil {
		return
	}
	if group.members < 0 {
		group.members = 0
	}
	group.members++
}

func (c *ShapeCollector) call(l *Link, cross bool) {
	c.card.CrossSite.CallEdges++
	if cross {
		c.card.CrossSite.CallCross++
	} else {
		c.calls = append(c.calls, [2]string{l.From.id, l.To.id})
	}
	c.callTotal++
}

// Scope folds a site when it ends.
func (c *ShapeCollector) Scope(kind Scope, site, index int, begin bool) error {
	if kind == ScopeSite && !begin {
		c.fold()
	}
	return nil
}

func (c *ShapeCollector) fold() {
	for _, n := range c.nodes {
		c.out.add(int(n.out))
		c.in.add(int(n.in))
		c.depth.add(int(n.depth))
		if n.minA > 0 {
			c.group(n)
		}
	}
	c.serviceDepth()
	c.nodes = map[string]*node{}
	c.calls = c.calls[:0]
}

func (c *ShapeCollector) group(n *node) {
	name := c.names[n.typ]
	g := c.groups[name]
	if g == nil {
		g = &groupAcc{members: distCounts{}, slack: distCounts{}, modes: map[string]int64{}}
		c.groups[name] = g
	}
	m := int(max(n.members, 0))
	g.n++
	g.members.add(m)
	g.slack.add(m - int(n.minA))
	g.modes[n.mode]++
}

// serviceDepth measures the call graph of the site: the distance of every
// service from the nearest service that nobody in the site calls.
func (c *ShapeCollector) serviceDepth() {
	adj := map[string][]string{}
	indeg := map[string]int{}
	edges := map[[2]string]bool{}
	for _, e := range c.calls {
		edges[e] = true
		if edges[[2]string{e[1], e[0]}] {
			c.cycles++
		}
		adj[e[0]] = append(adj[e[0]], e[1])
		indeg[e[1]]++
		if _, ok := indeg[e[0]]; !ok {
			indeg[e[0]] = 0
		}
	}
	dist := map[string]int{}
	var queue []string
	for id, d := range indeg {
		if d == 0 {
			dist[id] = 0
			queue = append(queue, id)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, to := range adj[id] {
			if _, ok := dist[to]; !ok {
				dist[to] = dist[id] + 1
				queue = append(queue, to)
			}
		}
	}
	for id := range indeg {
		if d, ok := dist[id]; ok {
			c.svc.add(d)
		} else {
			c.unreached++
		}
	}
}

// Card finishes the collection and returns the card.
func (c *ShapeCollector) Card() *ShapeCard {
	if len(c.nodes) > 0 {
		c.fold()
	}
	card := c.card
	card.OutDegree, card.InDegree = c.out.dist(), c.in.dist()
	card.ContainmentDepth, card.ServiceDepth = c.depth.dist(), c.svc.dist()
	card.ServicesInCycles, card.CyclePairs = c.unreached, c.cycles
	if c.services > 0 {
		card.ServiceCallsPerSvc = math.Round(float64(c.callTotal)/float64(c.services)*1000) / 1000
	}
	card.Groups = map[string]GroupShape{}
	for name, g := range c.groups {
		card.Groups[name] = GroupShape{Groups: g.n, Members: g.members.dist(), Slack: g.slack.dist(), Modes: g.modes}
	}
	cs := &card.CrossSite
	if cs.Links > 0 {
		cs.Ratio = math.Round(float64(cs.Cross)/float64(cs.Links)*1e6) / 1e6
	}
	if cs.CallEdges > 0 {
		cs.CallRatio = math.Round(float64(cs.CallCross)/float64(cs.CallEdges)*1e6) / 1e6
	}
	return &card
}

// ApplyShape sets the parameters that a shape card says directly and lists
// what it cannot set. A degree distribution, for one, has no parameter: it
// follows from the fan-outs.
func ApplyShape(card *ShapeCard, p *Params) (applied, unsupported []string) {
	if card.CrossSite.CallEdges > 0 {
		p.CrossSiteRatio = card.CrossSite.CallRatio
		applied = append(applied, "cross_site_ratio from cross_site.call_ratio")
	}
	if card.ServiceDepth.Count > 0 {
		p.ServiceDepth = min(max(card.ServiceDepth.Max+1, 1), 12)
		applied = append(applied, "service_depth from service_depth.max + 1")
	}
	if card.ServiceCallsPerSvc > 0 {
		p.DependencyDegree = card.ServiceCallsPerSvc
		applied = append(applied, "dependency_degree from service_calls_per_service")
	}
	unsupported = []string{
		"out_degree and in_degree: no parameter; they follow from the fan-outs",
		"containment_depth: fixed by the hierarchy of the standard model",
		"groups: the slack of each group type is a design choice of the recipes (see cooling_n2_share, cluster_spare2_share, strict_service_share)",
		"hubs: the generator makes no object that many services share",
	}
	return applied, unsupported
}
