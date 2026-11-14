package query

import (
	"fmt"
	"sort"
	"time"
)

// upNode is an object a target depends on: how many dependencies away it is and
// the dependency that leads from it towards the target.
type upNode struct {
	x     int32
	depth int
	c     cause
}

// closure lists everything the target depends on, nearest first: the objects
// its edges point to, the members of the groups among them and the failover
// groups of its runs_on edges, and so on. The cause of each is the step towards the target.
func (g *depGraph) closure(t int32) []upNode {
	seen := map[int32]bool{t: true}
	var out []upNode
	queue := []int32{t}
	depth := map[int32]int{t: 0}
	visit := func(x, y int32, c cause) {
		if !seen[y] {
			seen[y] = true
			depth[y] = depth[x] + 1
			out = append(out, upNode{x: y, depth: depth[y], c: c})
			queue = append(queue, y)
		}
	}
	for head := 0; head < len(queue); head++ {
		x := queue[head]
		nd := &g.nodes[x]
		for i := range nd.deps {
			d := &nd.deps[i]
			visit(x, d.to, cause{from: x, link: d.link, group: -1, role: roleOfEdge(d)})
			for _, f := range d.failover {
				visit(x, f, cause{from: x, link: d.link, group: -1, role: roleFailover})
			}
		}
		for _, m := range nd.members {
			visit(x, m.node, cause{from: x, link: m.link, group: -1, role: roleMember})
		}
	}
	return out
}

func roleOfEdge(d *edge) uint8 {
	switch d.kind {
	case kSupply:
		return roleSupply
	case kManage:
		return roleManagement
	case kPartOfWhole:
		return rolePartFollowsWhole
	case kWholeNeeds:
		return roleWholeNeedsPart
	}
	return roleDependency
}

// stepsTo is the path from a dependency to the target, as steps.
func stepsTo(up map[int32]upNode, u, t int32) []pstep {
	steps := []pstep{{x: u, c: noCause}}
	cur := u
	for guard := 0; cur != t && guard < len(up)+1; guard++ {
		pr := up[cur].c
		steps = append(steps, pstep{x: pr.from, c: cause{from: cur, link: pr.link, group: -1, role: pr.role}})
		cur = pr.from
	}
	return steps
}

// upResult is one dependency with the effect of its failure on the target.
type upResult struct {
	n       upNode
	effects map[string]string
	spof    bool
	key     string
	path    []PathStep
	assumed bool
	first   string
}

func effectName(mask uint8) string {
	if mask == 0 {
		return "none"
	}
	top, _ := topBit(mask)
	return resultName(top)
}

func effectRank(name string) int {
	for i, rb := range resultBits {
		if rb.name == name {
			return i
		}
	}
	return len(resultBits)
}

// upRun is one upstream question: the target, the readings and what has been tried.
type upRun struct {
	e        *Engine
	r        *read
	g        *depGraph
	plan     *impactPlan
	t        int32
	pols     []policy
	solvers  map[policy]*solver
	deadline time.Time
	rd       *reading
	up       map[int32]upNode
	stopped  string
	rounds   int
}

// upstreamAnswer answers impact for what the target depends on: each dependency
// is made to fail alone and the effect on the target is read.
func (e *Engine) upstreamAnswer(r *read, g *depGraph, plan *impactPlan, fp string) (ImpactData, ResultComplete, []string, error) {
	t, err := g.objectIndex("target_object_id", plan.in.TargetObjectID)
	if err != nil {
		return ImpactData{}, ResultComplete{}, nil, err
	}
	ref := g.ref(t)
	run := &upRun{e: e, r: r, g: g, plan: plan, t: t, pols: plan.policies(g), solvers: map[policy]*solver{}, up: map[int32]upNode{},
		deadline: e.clock.Now().Add(plan.budget)}
	for _, pol := range run.pols {
		run.solvers[pol] = newSolver(g, pol, solveBaseline(g, pol))
	}
	run.rd = &reading{g: g, pol: pReal, hid: r.hid, used: map[int32]bool{}, s: run.solvers[run.pols[0]]}
	nodes := g.closure(t)
	over := len(nodes) > MaxCandidates
	if over {
		nodes = nodes[:MaxCandidates]
	}
	for _, n := range nodes {
		run.up[n.x] = n
	}
	results := run.tryAll(nodes)
	sec, rc := run.section(results, fp, over)
	data := ImpactData{Direction: DirectionUpstream, Target: &ref, Upstream: sec,
		Computation: computationOf(plan, g, run.stopped, run.rounds, []pageState{{overCap: over}})}
	r.addUsage(&readings{by: map[policy]*reading{pReal: run.rd}}, g)
	return data, rc, []string{}, nil
}

// tryAll makes each dependency the caller may read fail alone, until the time is used up.
func (u *upRun) tryAll(nodes []upNode) []upResult {
	var results []upResult
	for _, n := range nodes {
		if !u.g.nodes[n.x].visible {
			u.r.hid.reached[u.g.nodes[n.x].idText] = true
			continue
		}
		if u.e.clock.Now().After(u.deadline) {
			u.stopped = StopTimeBudget
			break
		}
		res := u.tryFailure(n)
		path, _, assumed, _ := u.e.pathOf(u.rd, stepsTo(u.up, n.x, u.t))
		res.path, res.assumed = path, res.assumed || assumed
		results = append(results, res)
	}
	return results
}

// tryFailure makes one dependency fail alone, under every reading, and reads what happens to the target.
func (u *upRun) tryFailure(n upNode) upResult {
	outs := map[policy]*outcome{}
	for _, pol := range u.pols {
		s := u.solvers[pol]
		s.reset()
		s.failSeed(n.x, noCause)
		s.fail(u.plan.in.MaxHops, u.e.now(), u.deadline)
		u.rounds = max(u.rounds, s.rounds)
		outs[pol] = s.classify()
	}
	possible := outs[pReal]
	res := upResult{n: n, effects: map[string]string{}}
	for _, pol := range u.pols {
		if pol == pReal {
			continue
		}
		mask := outs[pol].mask[u.t]
		inP := possible == nil && mask&^bitUnknown != 0 || possible != nil && possible.mask[u.t]&^bitUnknown != 0
		res.effects[readingName(pol)] = effectName(reportable(mask, u.solvers[pol].base[u.t], inP))
	}
	first := res.effects[ModeFactsOnly]
	if first == "" {
		first = res.effects[ModeWithAssumptions]
	}
	res.spof = first == ResultDown
	f := res.effects[ModeFactsOnly]
	res.assumed = res.spof && f != "" && f != ResultDown || !res.spof && res.effects[ModeWithAssumptions] == ResultDown
	res.key = fmt.Sprintf("%d:%d:%06d:%s", boolRank(res.spof), effectRank(first), n.depth, u.g.nodes[n.x].idText)
	res.first = first
	return res
}

func boolRank(b bool) int {
	if b {
		return 0
	}
	return 1
}

func readingName(p policy) string {
	if p == pFacts {
		return ModeFactsOnly
	}
	return ModeWithAssumptions
}

// section builds the answer for the target: the counts, then the page of items.
func (u *upRun) section(results []upResult, fp string, over bool) (*UpstreamSection, ResultComplete) {
	sort.Slice(results, func(i, j int) bool { return results[i].key < results[j].key })
	sec := &UpstreamSection{Total: len(results), CountsByEffect: map[string]int{}}
	byType := map[string]int{}
	for _, res := range results {
		nd := &u.g.nodes[res.n.x]
		byType[nd.typeName]++
		sec.CountsByEffect[res.first]++
		if res.spof {
			sec.SinglePoints++
		}
	}
	sec.CountsByType = sortedCounts(byType)
	rc := ResultComplete{Total: len(results)}
	if u.r.c.Shape == ShapeItems {
		start := 0
		if u.r.cur != nil {
			after := u.r.cur.After
			start = sort.Search(len(results), func(i int) bool { return results[i].key > after })
		}
		end := min(start+u.r.c.Limit, len(results))
		sec.Items = []UpstreamItem{}
		for _, res := range results[start:end] {
			sec.Items = append(sec.Items, u.item(res))
		}
		rc.Returned = len(sec.Items)
		if end < len(results) {
			next := cursor{Gen: u.r.info.Generation, Seq: u.r.info.Seq, Version: versionOf(u.r.info), Query: fp, After: results[end-1].key}.encode()
			rc.NextCursor = &next
		}
	}
	rc.Truncated, rc.Reason = u.truncation(rc.NextCursor != nil, over)
	return sec, rc
}

func (u *upRun) truncation(more, over bool) (bool, *string) {
	reason := ""
	switch {
	case u.stopped != "":
		reason = u.stopped
	case more:
		reason = "limit"
	case over:
		reason = "size_budget"
	}
	if reason == "" {
		return false, nil
	}
	return true, &reason
}

func (u *upRun) item(res upResult) UpstreamItem {
	nd := &u.g.nodes[res.n.x]
	return UpstreamItem{ObjectID: nd.idText, ObjectType: nd.typeName, DisplayName: nd.name, Hops: res.n.depth, Effects: res.effects,
		SinglePointOfFailure: res.spof, AssumptionBased: res.assumed, Path: res.path}
}
