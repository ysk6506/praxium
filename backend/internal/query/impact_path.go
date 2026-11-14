package query

import (
	"fmt"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// reading is one run of the calculation under one policy, with its results.
type reading struct {
	g    *depGraph
	pol  policy
	s    *solver
	o    *outcome
	hid  *hiddenCounts
	used map[int32]bool
}

// pstep is one object of a path with the cause that led from the step before it.
type pstep struct {
	x int32
	c cause
}

// causeOf is the cause of an object's state as the path needs it: how the
// failure reached it. via is the role by which the path arrived at the object.
func (rd *reading) causeOf(x int32, via uint8, top uint8) cause {
	nd := &rd.g.nodes[x]
	switch {
	case via == roleExposure || via == roleSlack && !nd.group:
		return rd.o.ecause[x]
	case nd.group && rd.s.cur[x].svc == vUp:
		return rd.groupCause(x)
	case top == bitUnmanageable:
		return rd.s.mcauses[x]
	}
	return rd.s.causes[x]
}

// groupCause is the member that explains a group that lost members.
func (rd *reading) groupCause(x int32) cause {
	var best member
	best.node = -1
	worst := vUp
	for _, m := range rd.g.nodes[x].members {
		st := flow(rd.s.cur[m.node].svc, rd.pol.weak(rd.g.links[m.link].weak))
		if st > worst {
			worst, best = st, m
		}
	}
	return cause{from: best.node, link: best.link, group: -1, role: roleMember}
}

// chain follows the causes from an object back to a failed one. It returns the
// steps from the failed object to x, in that order.
func (rd *reading) chain(x int32, top uint8) []pstep {
	var rev []pstep
	seen := map[int32]bool{}
	var c cause
	switch top {
	case bitExposedDirect, bitExposedInherited, bitDegraded:
		c = rd.o.ecause[x]
	case bitUnmanageable:
		c = rd.s.mcauses[x]
	default:
		c = rd.s.causes[x]
	}
	cur := x
	for !seen[cur] {
		seen[cur] = true
		rev = append(rev, pstep{x: cur, c: c})
		if c.from < 0 {
			break
		}
		cur, c = c.from, rd.causeOf(c.from, c.role, 0)
	}
	out := make([]pstep, len(rev))
	for i, st := range rev {
		out[len(rev)-1-i] = st
	}
	return out
}

// pathOf makes the visible form of a chain: the steps, the weakest grade on it,
// whether it stands on an assumption, and whether a link on it delays the effect.
func (e *Engine) pathOf(rd *reading, steps []pstep) (path []PathStep, weakest model.Grade, assumed bool, delayed bool) {
	weakest = model.GradeFact
	for i, st := range steps {
		nd := &rd.g.nodes[st.x]
		ps := PathStep{}
		if nd.visible {
			id := nd.idText
			ps.ObjectID, ps.ObjectType, ps.DisplayName = &id, nd.typeName, nd.name
		} else {
			ps.Hidden = true
			rd.hid.passed[nd.idText] = true
		}
		if i > 0 && st.c.link >= 0 {
			pl, g, weak := e.pathLink(rd, st)
			ps.Link = &pl
			weakest = model.Lowest(weakest, g)
			assumed = assumed || weak
			delayed = delayed || pl.DelayClass == string(model.DelayDelayed)
			rd.used[st.c.link] = true
		}
		if st.c.group >= 0 {
			gn := &rd.g.nodes[st.c.group]
			ref := ObjectRef{ObjectID: gn.idText, ObjectType: gn.typeName, DisplayName: gn.name}
			ps.ViaGroup = &ref
		}
		path = append(path, ps)
	}
	return path, weakest, assumed, delayed
}

// pathLink describes the link of a step. The group steps also say what min_alive stands on.
func (e *Engine) pathLink(rd *reading, st pstep) (PathLink, model.Grade, bool) {
	lm := rd.g.links[st.c.link]
	pl := PathLink{LinkID: lm.id.String(), LinkType: lm.typeName, Meaning: string(lm.meaning), Role: roleNames[st.c.role],
		Grade: string(lm.grade), Basis: string(lm.basis)}
	if lm.delay == string(model.DelayDelayed) {
		pl.DelayClass = lm.delay
	}
	grade := lm.grade
	weak := rd.pol.weak(lm.weak) || lm.weak != weakNone && rd.pol == pReal
	if lm.weak == weakStale {
		pl.Note = "the observation is older than max_staleness"
	}
	if st.c.role == roleMember || st.c.role == roleSlack {
		g := &rd.g.nodes[st.x]
		if st.c.role == roleSlack {
			pl.Note = "failover group or group this object depends on lost members"
		} else if note, kg, kweak := e.minAliveNote(g); note != "" {
			pl.Note = note
			grade, weak = model.Lowest(grade, kg), weak || kweak
		}
	}
	return pl, grade, weak || lm.weak == weakAssumed
}

// minAliveNote says what min_alive of a group is and how far it can be trusted.
func (e *Engine) minAliveNote(g *node) (string, model.Grade, bool) {
	if !g.group {
		return "", model.GradeFact, false
	}
	if !g.k.present {
		return "min_alive is not known", model.GradeUnknown, true
	}
	return fmt.Sprintf("min_alive %d (%s)", g.k.value, g.k.grade), g.k.grade, g.k.grade != model.GradeFact
}
