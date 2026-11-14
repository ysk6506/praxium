package query

import "sort"

// The results as bits, so that one object can hold several (kg-queries.md 5.4).
const (
	bitDown uint8 = 1 << iota
	bitInterrupted
	bitUnknown
	bitExposedDirect
	bitExposedInherited
	bitDegraded
	bitUnmanageable
)

// resultBits lists the results from the most severe to the least.
var resultBits = []struct {
	bit  uint8
	name string
}{
	{bitDown, ResultDown}, {bitInterrupted, ResultInterrupted}, {bitUnknown, ResultUnknown}, {bitExposedDirect, ResultExposedDirect},
	{bitExposedInherited, ResultExposedInherited}, {bitDegraded, ResultDegraded}, {bitUnmanageable, ResultUnmanageable},
}

// outcome is what a run came to: a result mask per changed object, the cause of
// its exposure, and the groups whose members were hit.
type outcome struct {
	mask   map[int32]uint8
	ecause map[int32]cause
	eprio  map[int32]uint8
	groups []int32
}

// Priorities of the exposure results: a higher one replaces a lower one.
const (
	prioDegraded  uint8 = 1
	prioInherited uint8 = 2
	prioDirect    uint8 = 3
)

func (o *outcome) expose(x int32, bit uint8, prio uint8, c cause) {
	o.mask[x] |= bit
	if prio > o.eprio[x] {
		o.eprio[x], o.ecause[x] = prio, c
	}
}

// bitsOf is the result of the state of one object.
func bitsOf(st nstate) uint8 {
	switch st.svc {
	case vDown:
		return bitDown
	case vInterrupted:
		return bitInterrupted
	case vUnknown:
		return bitUnknown
	}
	if st.mgmt >= vInterrupted {
		return bitUnmanageable
	}
	return 0
}

// classify turns the states of a run into results.
func (s *solver) classify() *outcome {
	o := &outcome{mask: map[int32]uint8{}, ecause: map[int32]cause{}, eprio: map[int32]uint8{}}
	touched := append([]int32(nil), s.touched...)
	sort.Slice(touched, func(i, j int) bool { return touched[i] < touched[j] })
	var failed []int32
	for _, x := range touched {
		if s.g.nodes[x].group {
			o.groups = append(o.groups, x)
		}
		if s.seed[x] {
			continue
		}
		if b := bitsOf(s.cur[x]); b != 0 {
			o.mask[x] |= b
		}
		if s.cur[x].svc >= vInterrupted {
			failed = append(failed, x)
		}
	}
	s.exposeThroughGroups(o)
	s.degradeThroughSoft(o, failed)
	s.inheritExposure(o)
	return o
}

// groupClass says how a group that lost members stands: it is still up, and
// either has no slack left (exposed) or still some (degraded).
func (s *solver) groupClass(x int32) uint8 {
	nd := &s.g.nodes[x]
	st := s.cur[x]
	if st.svc != vUp || st.a >= s.base[x].a {
		return 0
	}
	k, ok := s.kKnown(nd)
	if !ok {
		return 0
	}
	if st.a-k == 0 {
		return bitExposedDirect
	}
	return bitDegraded
}

func (s *solver) alive(x int32) bool { return s.cur[x].svc == vUp && !s.seed[x] }

// exposeThroughGroups marks the objects that depend on a group that lost members.
func (s *solver) exposeThroughGroups(o *outcome) {
	for _, g := range o.groups {
		class := s.groupClass(g)
		if class == 0 {
			continue
		}
		bit, prio := class, prioDirect
		if class == bitDegraded {
			prio = prioDegraded
		}
		for _, u := range s.g.nodes[g].users {
			if !s.alive(u) {
				continue
			}
			if d, via := s.dependsOnGroup(u, g); d != nil {
				c := cause{from: g, link: d.link, group: -1, role: roleSlack}
				if via {
					c.role = roleFailover
				}
				o.expose(u, bit, prio, c)
			}
		}
	}
}

// dependsOnGroup finds the edge by which an object depends on a group: directly,
// or as the failover group of a runs_on edge (via is then true).
func (s *solver) dependsOnGroup(u, g int32) (*edge, bool) {
	nd := &s.g.nodes[u]
	for i := range nd.deps {
		d := &nd.deps[i]
		if d.to == g && d.spreadsExposure() && !s.pol.weak(s.g.links[d.link].weak) {
			return d, false
		}
		for _, f := range d.failover {
			if f == g && !s.pol.weak(s.g.links[d.link].weak) {
				return d, true
			}
		}
	}
	return nil, false
}

// degradeThroughSoft marks the objects that depend softly on something that failed.
func (s *solver) degradeThroughSoft(o *outcome, failed []int32) {
	seen := map[int32]bool{}
	for _, t := range failed {
		for _, u := range s.g.nodes[t].users {
			if seen[u] || !s.alive(u) {
				continue
			}
			seen[u] = true
			if d := s.softEdge(u); d != nil {
				o.expose(u, bitDegraded, prioDegraded, cause{from: d.to, link: d.link, group: -1, role: roleDependency})
			}
		}
	}
}

// softEdge is the first soft dependency of an object whose target has failed and whose link is believed.
func (s *solver) softEdge(u int32) *edge {
	nd := &s.g.nodes[u]
	for i := range nd.deps {
		d := &nd.deps[i]
		soft := d.soft && (d.kind == kRequires || d.kind == kWholeNeeds) || d.kind == kSupply && nd.need[d.ch] == needSoft
		if soft && s.cur[d.to].svc >= vInterrupted && !s.pol.weak(s.g.links[d.link].weak) {
			return d
		}
	}
	return nil
}

// inheritExposure hands exposure on, without a limit of depth: whatever runs on
// or is built on an exposed object is exposed too (kg-queries.md 5.4).
func (s *solver) inheritExposure(o *outcome) {
	var queue []int32
	for x, m := range o.mask {
		if m&bitExposedDirect != 0 {
			queue = append(queue, x)
		}
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i] < queue[j] })
	for head := 0; head < len(queue); head++ {
		y := queue[head]
		for _, u := range s.g.nodes[y].users {
			if !s.alive(u) || o.mask[u]&bitExposedInherited != 0 {
				continue
			}
			if d := s.exposingEdge(u, y); d != nil {
				o.expose(u, bitExposedInherited, prioInherited, cause{from: y, link: d.link, group: -1, role: roleExposure})
				queue = append(queue, u)
			}
		}
	}
}

func (s *solver) exposingEdge(u, y int32) *edge {
	nd := &s.g.nodes[u]
	for i := range nd.deps {
		d := &nd.deps[i]
		if d.to == y && d.spreadsExposure() && !s.pol.weak(s.g.links[d.link].weak) {
			return d
		}
	}
	return nil
}
