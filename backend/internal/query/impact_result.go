package query

import (
	"fmt"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// candidate is an object that the failure changed: its results as bits and
// the most severe of them.
type candidate struct {
	x    int32
	mask uint8
	top  uint8
	rank int
	key  string
}

// reportable drops the results that do not come from the failure. An unknown
// object is reported only when it was not unknown before, or when the failure
// would reach it if every link were believed (inP).
func reportable(mask uint8, base nstate, inP bool) uint8 {
	if mask&bitUnknown != 0 && base.svc == vUnknown && !inP {
		mask &^= bitUnknown
	}
	return mask
}

func topBit(mask uint8) (uint8, int) {
	for i, rb := range resultBits {
		if mask&rb.bit != 0 {
			return rb.bit, i
		}
	}
	return 0, len(resultBits)
}

// candidates lists the changed objects of a reading in the order of the answer:
// the most severe result first, then by object id.
func (rd *reading) candidates(possible *outcome) []candidate {
	var out []candidate
	for x, m := range rd.o.mask {
		inP := m&^bitUnknown != 0
		if possible != nil && possible != rd.o {
			inP = possible.mask[x]&^bitUnknown != 0
		}
		m = reportable(m, rd.s.base[x], inP)
		if m == 0 {
			continue
		}
		top, rank := topBit(m)
		out = append(out, candidate{x: x, mask: m, top: top, rank: rank, key: fmt.Sprintf("%d:%s", rank, rd.g.nodes[x].idText)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

func resultName(bit uint8) string {
	for _, rb := range resultBits {
		if rb.bit == bit {
			return rb.name
		}
	}
	return ""
}

// lostChannels names what the object lost: the supplied channels and the
// service it depends on, which are down, and its management.
func (rd *reading) lostChannels(x int32, mask uint8) []string {
	st := rd.s.cur[x]
	out := []string{}
	if mask&(bitDown|bitInterrupted) != 0 {
		for c, v := range st.ch {
			if v >= vInterrupted {
				out = append(out, string(supplyChannels[c]))
			}
		}
		if st.dep >= vInterrupted || len(out) == 0 {
			out = append(out, string(model.ChannelService))
		}
	}
	if mask&bitUnmanageable != 0 {
		out = append(out, string(model.ChannelManagement))
	}
	sort.Strings(out)
	return out
}

// unfilledChannels names the channels the type of an object needs but no link supplies.
func (rd *reading) unfilledChannels(x int32) []string {
	nd := &rd.g.nodes[x]
	var out []string
	for c, need := range nd.need {
		if need == needHard && !nd.supplied[c] {
			out = append(out, string(supplyChannels[c]))
		}
	}
	return out
}

// item builds the answer for one changed object, with its path.
func (e *Engine) impactItem(rd *reading, c candidate) ImpactItem {
	nd := &rd.g.nodes[c.x]
	it := ImpactItem{ObjectID: nd.idText, ObjectType: nd.typeName, DisplayName: nd.name, Result: resultName(c.top), Also: []string{},
		LostChannels: rd.lostChannels(c.x, c.mask), UnfilledChannels: rd.unfilledChannels(c.x)}
	for _, rb := range resultBits {
		if c.mask&rb.bit != 0 && rb.bit != c.top {
			it.Also = append(it.Also, rb.name)
		}
	}
	steps := rd.chain(c.x, c.top)
	path, weakest, assumed, delayed := e.pathOf(rd, steps)
	it.Path, it.WeakestGrade, it.AssumptionBased = path, string(weakest), assumed
	it.Hops = len(path) - 1
	if delayed {
		it.DelayClass = string(model.DelayDelayed)
	}
	if len(path) >= 2 {
		before := path[len(path)-2]
		it.Via, it.ViaHidden = before.ObjectID, before.Hidden
	}
	return it
}

// visibleCandidates keeps the objects the caller may read and counts the others as reached.
func (rd *reading) visibleCandidates(cands []candidate) []candidate {
	out := make([]candidate, 0, len(cands))
	for _, c := range cands {
		if rd.g.nodes[c.x].visible {
			out = append(out, c)
		} else {
			rd.hid.reached[rd.g.nodes[c.x].idText] = true
		}
	}
	return out
}

// counts summarizes the visible candidates: by result, by type and by the place they are contained in.
func (rd *reading) counts(cands []candidate) (map[string]int, []TypeCount, []ParentCount) {
	byResult := map[string]int{}
	byType := map[string]int{}
	byParent := map[int32]int{}
	for _, c := range cands {
		nd := &rd.g.nodes[c.x]
		byResult[resultName(c.top)]++
		byType[nd.typeName]++
		if nd.parent >= 0 && rd.g.nodes[nd.parent].visible {
			byParent[nd.parent]++
		}
	}
	parents := make([]ParentCount, 0, len(byParent))
	for p, n := range byParent {
		pn := &rd.g.nodes[p]
		parents = append(parents, ParentCount{ObjectID: pn.idText, DisplayName: pn.name, Count: n})
	}
	sort.Slice(parents, func(i, j int) bool {
		if parents[i].Count != parents[j].Count {
			return parents[i].Count > parents[j].Count
		}
		return parents[i].ObjectID < parents[j].ObjectID
	})
	if len(parents) > topParents {
		parents = parents[:topParents]
	}
	return byResult, sortedCounts(byType), parents
}

// topParents is how many places the counts by parent name.
const topParents = 20

// touchedGroups describes the groups whose members were hit.
func (rd *reading) touchedGroups() []GroupTouched {
	out := []GroupTouched{}
	for _, x := range rd.o.groups {
		nd := &rd.g.nodes[x]
		if !nd.visible {
			continue
		}
		st := rd.s.cur[x]
		gt := GroupTouched{GroupID: nd.idText, GroupType: nd.typeName, DisplayName: nd.name, Mode: "count", Alive: int(st.a),
			Unknown: int(st.u), N: len(nd.members), Value: groupValueName(st.svc)}
		if nd.capacity {
			gt.Mode = "capacity"
		}
		if k, ok := rd.s.kKnown(nd); ok {
			kk := int(k)
			gt.MinAlive = &kk
			if st.svc == vUp && !nd.capacity {
				slack := int(st.a) - kk
				gt.Slack = &slack
			}
		}
		out = append(out, gt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GroupID < out[j].GroupID })
	return out
}

func groupValueName(v val) string {
	switch v {
	case vUp:
		return "up"
	case vUnknown:
		return ResultUnknown
	}
	return "down"
}
