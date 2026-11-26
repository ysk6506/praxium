package ingest

import "sort"

// Which record's link is used when several records make the same link (ADR-0122). The old flow made the links of a source in the order of
// the link definitions and, in a definition, of the rows (the rows that a rule filtered out after the others), and the later one was used:
// that order is LinkOrder, with the source in front of it, and the record that comes last is used. The choice is made of the records of a
// page and the one that was used before, so it is the same whatever the pages are.

// LinkHolder is the record that is used for a link the run made so far, with the number the link has.
type LinkHolder struct {
	No  int64
	Who LinkWho
}

// LinkWin is a link to write: the row of stage_link, the record that is used for it, and whether it replaces a record that was used before
// (the link is written already then).
type LinkWin struct {
	Row      LinkRow
	Who      LinkWho
	Replaces bool
}

// LinkLost is a record whose end made a link that is not used because the end of another record made it: the record, and the link.
type LinkLost struct {
	Ref string
	Who LinkWho
}

// LinkElection is what the choice gave for a page: the links to write (by the id of the link), the records that lost (by the id of the
// link and the order), and the records that were used and are not any more.
type LinkElection struct {
	Wins      []LinkWin
	Lost      []LinkLost
	Displaced []LinkLost
	// Used is the ends (the record and the definition) whose record is used for the link the end made.
	Used map[LinkUse]bool
}

// LinkUse names an end: a record has one end of a definition.
type LinkUse struct {
	Rid int64
	Def int
}

// ElectLinks chooses, for every link the candidates make, the record that is used: the one that comes last in the order among the
// candidates and the holder the link has already. A candidate that is not used is lost, and so is a holder that a candidate replaces.
func ElectLinks(holders map[string]LinkHolder, candidates []LinkCandidate) LinkElection {
	byRef := map[string][]LinkCandidate{}
	for _, c := range candidates {
		byRef[c.Row.Ref] = append(byRef[c.Row.Ref], c)
	}
	refs := make([]string, 0, len(byRef))
	for ref := range byRef {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	out := LinkElection{Used: map[LinkUse]bool{}}
	for _, ref := range refs {
		list := byRef[ref]
		sort.SliceStable(list, func(i, j int) bool { return list[i].Who.Order.Before(list[j].Who.Order) })
		best := list[len(list)-1]
		holder, held := holders[ref]
		if held && best.Who.Order.Before(holder.Who.Order) {
			for _, c := range list {
				out.Lost = append(out.Lost, LinkLost{Ref: ref, Who: c.Who})
			}
			continue
		}
		for _, c := range list[:len(list)-1] {
			out.Lost = append(out.Lost, LinkLost{Ref: ref, Who: c.Who})
		}
		if held {
			out.Lost = append(out.Lost, LinkLost{Ref: ref, Who: holder.Who})
			out.Displaced = append(out.Displaced, LinkLost{Ref: ref, Who: holder.Who})
		}
		out.Wins = append(out.Wins, LinkWin{Row: best.Row, Who: best.Who, Replaces: held})
		out.Used[LinkUse{Rid: best.Who.Rid, Def: best.Who.Order.Def}] = true
	}
	return out
}
