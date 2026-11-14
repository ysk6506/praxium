package query

import (
	"time"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// val is the value of a channel: up, unknown, interrupted (down, but a declared
// failover will recover it) or down, in the order of the join. A failure only
// ever raises a value, which is why the calculation ends and does not depend on
// the order of the seeds (kg-queries.md 5.3).
type val uint8

const (
	vUp val = iota
	vUnknown
	vInterrupted
	vDown
)

func join(a, b val) val {
	if b > a {
		return b
	}
	return a
}

// policy says which links count as weak, so that a failure is not carried by them
// but only possibly carried (kg-queries.md 5.6).
type policy uint8

const (
	// pReal reads every link as true; it finds what could be affected.
	pReal policy = iota
	// pAssumed reads assumed links as true: the answer with assumptions.
	pAssumed
	// pFacts reads assumed links as unknown: the answer with facts only.
	pFacts
)

func (p policy) weak(w uint8) bool {
	switch p {
	case pAssumed:
		return w >= weakUnknown
	case pFacts:
		return w >= weakAssumed
	}
	return false
}

// nstate is the state of one object: its service, what the service depends on
// (dep), its management, its four supplied channels, and for a group how many
// members are alive and unknown.
type nstate struct {
	svc, dep, mgmt val
	ch             [4]val
	a, u           int32
}

// Roles of the link that carried a failure on.
const (
	roleSeed uint8 = iota
	roleDependency
	roleSupply
	roleManagement
	roleMember
	roleFailover
	rolePartFollowsWhole
	roleWholeNeedsPart
	roleContainment
	roleExposure
	roleSlack
)

var roleNames = map[uint8]string{
	roleSeed: "seed", roleDependency: "dependency", roleSupply: "supply", roleManagement: "management", roleMember: "group_member",
	roleFailover: "failover", rolePartFollowsWhole: "part_follows_whole", roleWholeNeedsPart: "whole_needs_part",
	roleContainment: "containment", roleExposure: "exposure", roleSlack: "group_slack",
}

// cause is why an object has its state: the object the failure came from, the
// link that carried it, the failover group that decided it, and the role.
type cause struct {
	from  int32
	link  int32
	group int32
	role  uint8
}

var noCause = cause{from: -1, link: -1, group: -1, role: roleSeed}

// accum collects what one object is made of while its edges are read.
type accum struct {
	n    nstate
	v    val
	c    cause
	chV  [4]val
	chC  [4]cause
	mV   val
	mC   cause
	best member
}

func (a *accum) raise(v val, c cause) {
	if v > a.v {
		a.v, a.c = v, c
	}
}

func (a *accum) raiseDep(v val, c cause) {
	a.n.dep = join(a.n.dep, v)
	a.raise(v, c)
}

// solver finds the least fixed point of the state of every object, round by
// round: in a round only the objects that depend on something that changed in
// the round before are worked out again, from the states of the round before.
type solver struct {
	g         *depGraph
	pol       policy
	base      []nstate
	cur       []nstate
	causes    []cause
	mcauses   []cause
	seed      []bool
	seedCause map[int32]cause
	seedList  []int32
	touched   []int32
	marked    []bool
	stamp     []uint32
	epoch     uint32
	rounds    int
	// cutOn makes the links that the question names as cut carry nothing: their target counts as down.
	cutOn bool
	// cutFrontier are the objects at the two ends of a cut link, which are worked out in the first round.
	cutFrontier []int32
}

// isCut says whether the link is cut in this run.
func (s *solver) isCut(link int32) bool { return s.cutOn && link >= 0 && s.g.links[link].cut }

// stateVia is the service of the object at the far end of a link, as the link hands it on: a cut link hands on down.
func (s *solver) stateVia(to, link int32) val {
	if s.isCut(link) {
		return vDown
	}
	return s.cur[to].svc
}

func newSolver(g *depGraph, pol policy, base []nstate) *solver {
	n := len(g.nodes)
	s := &solver{g: g, pol: pol, base: base, cur: make([]nstate, n), causes: make([]cause, n), mcauses: make([]cause, n),
		seed: make([]bool, n), seedCause: map[int32]cause{}, marked: make([]bool, n), stamp: make([]uint32, n)}
	if base != nil {
		copy(s.cur, base)
	}
	return s
}

// reset takes the solver back to the baseline so that it can run another question.
func (s *solver) reset() {
	for _, x := range s.touched {
		s.cur[x] = s.base[x]
		s.marked[x] = false
	}
	for x := range s.seedCause {
		s.seed[x] = false
	}
	s.seedCause = map[int32]cause{}
	s.cutFrontier = s.cutFrontier[:0]
	s.seedList = s.seedList[:0]
	s.touched = s.touched[:0]
	s.rounds = 0
}

// failSeed marks an object as failed, with the reason.
func (s *solver) failSeed(x int32, c cause) {
	if s.seed[x] {
		return
	}
	s.seed[x] = true
	s.seedCause[x] = c
	s.seedList = append(s.seedList, x)
}

// flow is what a dependency hands on: its state, or only "unknown" when the
// link is weak and the target is not up. A weak link says nothing about a
// target that is up: with nothing failed, nothing is unknown (kg-queries.md 5.6).
func flow(tv val, weak bool) val {
	if weak && tv != vUp {
		return vUnknown
	}
	return tv
}

func (s *solver) eval(x int32) (nstate, cause, cause) {
	nd := &s.g.nodes[x]
	a := accum{c: noCause, mC: noCause, best: member{node: -1}}
	for i := range a.chC {
		a.chC[i] = noCause
	}
	if s.seed[x] {
		a.v, a.c = vDown, s.seedCause[x]
	}
	for i := range nd.deps {
		s.applyEdge(&a, &nd.deps[i])
	}
	if nd.group {
		s.applyGroup(&a, nd)
	}
	for c := range a.chV {
		a.n.ch[c] = a.chV[c]
		if nd.need[c] == needHard || nd.need[c] == needNone && nd.supplied[c] {
			a.raise(a.chV[c], a.chC[c])
		}
	}
	a.n.svc, a.n.mgmt = a.v, a.mV
	return a.n, a.c, a.mC
}

func (s *solver) applyEdge(a *accum, d *edge) {
	tv := s.stateVia(d.to, d.link)
	weak := s.pol.weak(s.g.links[d.link].weak)
	c := cause{from: d.to, link: d.link, group: -1, role: roleDependency}
	switch d.kind {
	case kSupply:
		c.role = roleSupply
		if v := flow(tv, weak); v > a.chV[d.ch] {
			a.chV[d.ch], a.chC[d.ch] = v, c
		}
	case kManage:
		c.role = roleManagement
		if v := flow(tv, weak); v > a.mV {
			a.mV, a.mC = v, c
		}
	case kRunsOn:
		v, c := s.runsOn(d, tv, weak, c)
		a.raiseDep(v, c)
	case kPartOfWhole:
		c.role = rolePartFollowsWhole
		a.raiseDep(flow(tv, weak), c)
	case kWholeNeeds:
		c.role = roleWholeNeedsPart
		if !d.soft {
			a.raiseDep(flow(tv, weak), c)
		}
	default:
		if !d.soft {
			a.raiseDep(flow(tv, weak), c)
		}
	}
}

// runsOn is what a runs_on edge hands on. When the target has failed and a
// failover group is declared, the group decides: an object it can take over is
// only interrupted, one it cannot is down, one it may not be able to is unknown.
func (s *solver) runsOn(d *edge, tv val, weak bool, c cause) (val, cause) {
	if weak && tv != vUp {
		return vUnknown, c
	}
	if tv < vInterrupted || len(d.failover) == 0 {
		return tv, c
	}
	best, bestGroup := vDown, d.failover[0]
	for _, g := range d.failover {
		if gv := s.cur[g].svc; gv < best {
			best, bestGroup = gv, g
		}
	}
	c.role, c.group = roleFailover, bestGroup
	switch best {
	case vUp:
		return vInterrupted, c
	case vUnknown:
		return vUnknown, c
	}
	return vDown, c
}

// applyGroup counts the members of a redundancy group and gives the group its value.
func (s *solver) applyGroup(a *accum, nd *node) {
	var alive, unknown int32
	worst := vUp
	for _, m := range nd.members {
		state := flow(s.stateVia(m.node, m.link), s.pol.weak(s.g.links[m.link].weak))
		switch state {
		case vUp:
			alive++
		case vUnknown:
			unknown++
		}
		if state > worst {
			worst, a.best = state, m
		}
	}
	a.n.a, a.n.u = alive, unknown
	v, _, _ := s.groupValue(nd, alive, unknown)
	if v != vUp {
		a.raiseDep(v, cause{from: a.best.node, link: a.best.link, group: -1, role: roleMember})
	}
}

// kKnown reads min_alive under the policy: a fact is known, an assumed value is
// known only when assumptions are believed, anything else is not known.
func (s *solver) kKnown(nd *node) (int32, bool) {
	if !nd.k.present {
		return 0, false
	}
	switch nd.k.grade {
	case model.GradeFact:
		return nd.k.value, true
	case model.GradeAssumed:
		return nd.k.value, s.pol != pFacts
	}
	return 0, false
}

// groupValue is the value of a group (kg-model.md 7.2), and its slack (members
// alive above min_alive) when min_alive is known and the group is up. An unknown
// min_alive gives a value only if every possible min_alive from 1 to n gives
// the same one.
func (s *solver) groupValue(nd *node, alive, unknown int32) (val, int32, bool) {
	n := int32(len(nd.members))
	if n == 0 {
		return vUnknown, 0, false
	}
	if nd.capacity {
		if alive == n {
			return vUp, 0, false
		}
		return vUnknown, 0, false
	}
	if k, ok := s.kKnown(nd); ok {
		switch {
		case alive >= k:
			return vUp, alive - k, true
		case alive+unknown < k:
			return vDown, 0, false
		}
		return vUnknown, 0, false
	}
	canUp, canDown, canUnknown := alive >= 1, alive+unknown < n, unknown > 0 && alive < n
	switch {
	case canUp && !canDown && !canUnknown:
		return vUp, 0, false
	case canDown && !canUp && !canUnknown:
		return vDown, 0, false
	}
	return vUnknown, 0, false
}

// evalRound works out the objects of the frontier from the states of the round
// before, then applies the changes together, and returns the objects to work out next.
func (s *solver) evalRound(frontier []int32) []int32 {
	type change struct {
		x    int32
		st   nstate
		c, m cause
	}
	var changes []change
	for _, x := range frontier {
		st, c, m := s.eval(x)
		st.svc, st.dep = join(st.svc, s.cur[x].svc), join(st.dep, s.cur[x].dep)
		if st != s.cur[x] {
			changes = append(changes, change{x: x, st: st, c: c, m: m})
		}
	}
	s.epoch++
	var next []int32
	for _, ch := range changes {
		// A cause is kept from the round in which the value first reached its height: a later tie must not replace it, or causes would go in circles.
		prev := s.cur[ch.x]
		if ch.st.svc > prev.svc {
			s.causes[ch.x] = ch.c
		}
		if ch.st.mgmt > prev.mgmt {
			s.mcauses[ch.x] = ch.m
		}
		s.cur[ch.x] = ch.st
		if !s.marked[ch.x] {
			s.marked[ch.x] = true
			s.touched = append(s.touched, ch.x)
		}
		for _, u := range s.g.nodes[ch.x].users {
			if s.stamp[u] != s.epoch {
				s.stamp[u] = s.epoch
				next = append(next, u)
			}
		}
	}
	return next
}

// run spreads the failure until nothing changes, the round limit is reached or
// the time is used up; it names what stopped it, or is empty when it ended.
func (s *solver) run(frontier []int32, maxHops int, now func() time.Time, deadline time.Time) string {
	for len(frontier) > 0 {
		if maxHops > 0 && s.rounds >= maxHops {
			return StopMaxHops
		}
		if now().After(deadline) {
			return StopTimeBudget
		}
		s.rounds++
		frontier = s.evalRound(frontier)
	}
	return ""
}

// solveBaseline works out the state of every object when nothing has failed.
func solveBaseline(g *depGraph, pol policy) []nstate {
	s := newSolver(g, pol, nil)
	all := make([]int32, len(g.nodes))
	for i := range all {
		all[i] = int32(i)
	}
	far := time.Unix(1<<40, 0)
	s.run(all, 0, func() time.Time { return time.Unix(0, 0) }, far)
	return s.cur
}

// fail runs a question: the failed objects (failSeed) and their failure spreads.
func (s *solver) fail(maxHops int, now func() time.Time, deadline time.Time) string {
	frontier := append(append([]int32(nil), s.seedList...), s.cutFrontier...)
	return s.run(frontier, maxHops, now, deadline)
}
