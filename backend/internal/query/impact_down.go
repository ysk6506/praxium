package query

import (
	"sort"
	"strings"
)

// seedSet is the objects that fail in a question: the named ones and, for a
// site failure, everything contained in them.
type seedSet struct {
	dead []int32
	site []int32
}

// resolveSeeds finds the objects of the input in the graph.
func resolveSeeds(g *depGraph, in ImpactInput) (seedSet, error) {
	var ss seedSet
	var err error
	if ss.dead, err = g.objectIndexes("dead", in.Dead); err != nil {
		return ss, err
	}
	ss.site, err = g.objectIndexes("site_failure", in.SiteFailure)
	return ss, err
}

// applyTo makes the objects fail in a solver. A contained object fails with its
// container when the link that says so is believed under the policy of the solver.
func (ss seedSet) applyTo(s *solver) {
	for _, x := range ss.dead {
		s.failSeed(x, noCause)
	}
	queue := make([]int32, 0, len(ss.site))
	for _, x := range ss.site {
		s.failSeed(x, noCause)
		queue = append(queue, x)
	}
	for head := 0; head < len(queue); head++ {
		for _, ch := range s.g.nodes[queue[head]].children {
			if s.seed[ch.node] || s.pol.weak(s.g.links[ch.link].weak) {
				continue
			}
			s.failSeed(ch.node, cause{from: queue[head], link: ch.link, group: -1, role: roleContainment})
			queue = append(queue, ch.node)
		}
	}
}

// readings runs the question under every policy the plan needs.
type readings struct {
	by      map[policy]*reading
	stopped string
	rounds  int
}

func (e *Engine) runReadings(r *read, g *depGraph, plan *impactPlan, ss seedSet) *readings {
	out := &readings{by: map[policy]*reading{}}
	deadline := e.clock.Now().Add(plan.budget)
	for _, pol := range plan.policies(g) {
		s := newSolver(g, pol, solveBaseline(g, pol))
		ss.applyTo(s)
		if stop := s.fail(plan.in.MaxHops, e.now(), deadline); stop != "" && out.stopped == "" {
			out.stopped = stop
		}
		out.rounds = max(out.rounds, s.rounds)
		out.by[pol] = &reading{g: g, pol: pol, s: s, o: s.classify(), hid: r.hid, used: map[int32]bool{}}
	}
	return out
}

// pageState is where a reading's page of items starts and ends.
type pageState struct {
	total    int
	returned int
	more     bool
	last     string
	overCap  bool
}

// downstreamAnswer answers impact for the objects that failed.
func (e *Engine) downstreamAnswer(r *read, g *depGraph, plan *impactPlan, fp string) (ImpactData, ResultComplete, []string, error) {
	ss, err := resolveSeeds(g, plan.in)
	if err != nil {
		return ImpactData{}, ResultComplete{}, nil, err
	}
	runs := e.runReadings(r, g, plan, ss)
	data := ImpactData{Direction: DirectionDownstream}
	for _, x := range append(append([]int32{}, ss.dead...), ss.site...) {
		data.Seeds = append(data.Seeds, g.ref(x))
	}
	after := afterKeys(r)
	possible := runs.by[pReal]
	var states []pageState
	if rd := runs.by[pFacts]; rd != nil {
		sec, ps := e.section(r, rd, possible, plan, after[0])
		data.FactsOnly = sec
		states = append(states, ps)
		after[0] = ps.last
	}
	if rd := runs.by[pAssumed]; rd != nil {
		sec, ps := e.section(r, rd, possible, plan, after[1])
		data.WithAssumptions = sec
		states = append(states, ps)
		after[1] = ps.last
	}
	data.Computation = computationOf(plan, g, runs.stopped, runs.rounds, states)
	rc := e.completeness(r, fp, states, after, runs.stopped)
	r.addUsage(runs, g)
	return data, rc, e.warnings(g, runs), nil
}

// section builds one reading's answer: the counts, and the page of items when the shape is items.
func (e *Engine) section(r *read, rd *reading, possible *reading, plan *impactPlan, after string) (*ImpactSection, pageState) {
	var po *outcome
	if possible != nil {
		po = possible.o
	}
	cands := rd.visibleCandidates(rd.candidates(po))
	byResult, byType, byParent := rd.counts(cands)
	sec := &ImpactSection{CountsByResult: byResult, CountsByType: byType, CountsByParent: byParent, GroupsTouched: rd.touchedGroups()}
	ps := pageState{total: len(cands), last: after}
	if len(cands) > plan.maxItems {
		ps.overCap = true
		cands = cands[:plan.maxItems]
	}
	if r.c.Shape != ShapeItems {
		return sec, ps
	}
	start := 0
	if after != "" {
		start = sort.Search(len(cands), func(i int) bool { return cands[i].key > after })
	}
	end := min(start+r.c.Limit, len(cands))
	ps.more = end < len(cands)
	sec.Items = []ImpactItem{}
	for _, c := range cands[start:end] {
		sec.Items = append(sec.Items, e.impactItem(rd, c))
	}
	ps.returned = len(sec.Items)
	if ps.more {
		ps.last = cands[end-1].key
	} else {
		ps.last = keyDone
	}
	return sec, ps
}

// keyDone is a position after every item.
const keyDone = "~"

// afterKeys reads the positions of the pages from the cursor: one for each reading.
func afterKeys(r *read) [2]string {
	var keys [2]string
	if r.cur != nil {
		parts := strings.SplitN(r.cur.After, "\x1f", 2)
		keys[0] = parts[0]
		if len(parts) > 1 {
			keys[1] = parts[1]
		}
	}
	return keys
}

func joinKeys(keys [2]string) string { return keys[0] + "\x1f" + keys[1] }

func computationOf(plan *impactPlan, g *depGraph, stopped string, rounds int, states []pageState) Computation {
	c := Computation{Complete: stopped == "", Rounds: rounds, MaxHops: plan.in.MaxHops, MaxItems: plan.maxItems,
		TimeBudgetSeconds: plan.budget.Seconds(), WeakLinks: g.weakLinks, StaleInputs: g.stale}
	if stopped != "" {
		c.StoppedBy = &stopped
	}
	for _, ps := range states {
		c.ItemsOverCap = c.ItemsOverCap || ps.overCap
	}
	return c
}

// completeness fills result_complete: everything the readings matched, what the page holds, and why it is cut.
func (e *Engine) completeness(r *read, fp string, states []pageState, after [2]string, stopped string) ResultComplete {
	rc := ResultComplete{}
	more, over := false, false
	for _, ps := range states {
		rc.Total += ps.total
		rc.Returned += ps.returned
		more = more || ps.more
		over = over || ps.overCap
	}
	reason := ""
	switch {
	case stopped != "":
		reason = stopped
	case more:
		reason = "limit"
	case over:
		reason = "size_budget"
	}
	if reason != "" {
		rc.Truncated, rc.Reason = true, &reason
	}
	if more {
		next := cursor{Gen: r.info.Generation, Seq: r.info.Seq, Version: versionOf(r.info), Query: fp, After: joinKeys(after)}.encode()
		rc.NextCursor = &next
	}
	return rc
}

// addUsage records the links of the paths that were returned as the evidence of the answer.
func (r *read) addUsage(runs *readings, g *depGraph) {
	used := map[int32]bool{}
	for _, rd := range runs.by {
		for idx := range rd.used {
			used[idx] = true
		}
	}
	idxs := make([]int32, 0, len(used))
	for idx := range used {
		idxs = append(idxs, idx)
	}
	sort.Slice(idxs, func(i, j int) bool { return idxs[i] < idxs[j] })
	for _, idx := range idxs {
		lm := g.links[idx]
		r.use.add(lm.grade, lm.basis, lm.seen, "")
	}
}

// warnings are the notes of the envelope: what was not used and what could not be calculated.
func (e *Engine) warnings(g *depGraph, runs *readings) []string {
	out := []string{}
	if g.stale > 0 {
		out = append(out, "stale_observation_not_used")
	}
	for _, rd := range runs.by {
		for _, x := range rd.o.groups {
			if g.nodes[x].capacity {
				out = append(out, "capacity_group_unknown")
				return out
			}
		}
	}
	return out
}
