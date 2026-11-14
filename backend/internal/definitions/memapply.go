package definitions

import (
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// apply makes the write on the state, which is a copy; an error leaves the copy to be thrown away.
func (s *state) apply(w Write) error {
	if w.Head != nil && w.Head.Expect != s.head {
		return kernel.Errorf(kernel.CodeConflict, "the head is D-%d, not the D-%d that was expected", s.head, w.Head.Expect)
	}
	if err := s.applyItems(w); err != nil {
		return err
	}
	if err := s.applySet(w); err != nil {
		return err
	}
	if err := s.applyCounters(w.Counters); err != nil {
		return err
	}
	if err := s.applyBranches(w.Branches); err != nil {
		return err
	}
	return s.applyProposals(w.Proposals)
}

func (s *state) applyItems(w Write) error {
	for _, it := range w.Items {
		if it.ID == "" || it.Kind == "" {
			return kernel.Errorf(kernel.CodeInvalidValue, "an item needs an ID and a kind")
		}
		if _, ok := s.items[it.ID]; ok {
			return kernel.Errorf(kernel.CodeConflict, "the item %q is there already", it.ID)
		}
		it.LastRev = 0
		s.items[it.ID] = it
	}
	for _, r := range w.Revisions {
		it, ok := s.items[r.ItemID]
		if !ok {
			return kernel.Errorf(kernel.CodeNotFound, "a revision belongs to the unknown item %q", r.ItemID)
		}
		if r.RevNo != it.LastRev+1 {
			return kernel.Errorf(kernel.CodeConflict, "the next revision of %q is %d, not %d", r.ItemID, it.LastRev+1, r.RevNo)
		}
		if err := checkRevisionHash(it.Kind, r); err != nil {
			return err
		}
		it.LastRev++
		s.items[r.ItemID] = it
		s.revs[r.ItemID] = append(s.revs[r.ItemID], cloneRevision(r))
	}
	return nil
}

func (s *state) applySet(w Write) error {
	if w.Set == nil {
		if w.Head != nil && w.Head.New != s.head {
			return kernel.Errorf(kernel.CodeInvalidValue, "the head moves only with the set that makes it")
		}
		return nil
	}
	if w.Head == nil || w.Head.New != w.Set.No {
		return kernel.Errorf(kernel.CodeInvalidValue, "a new set is the new head, and the write must say so")
	}
	if err := s.checkSet(*w.Set, int64(len(s.sets))+1); err != nil {
		return kernel.Errorf(kernel.CodeInvalidValue, "the new set is not sound: %v", err)
	}
	s.sets = append(s.sets, cloneSet(*w.Set, true))
	s.head = w.Set.No
	return nil
}

func (s *state) applyCounters(writes []CounterWrite) error {
	for _, c := range writes {
		have := s.counters[c.Name]
		switch {
		case c.Name == "" || c.Value < 1:
			return kernel.Errorf(kernel.CodeInvalidValue, "a counter needs a name and a value from 1")
		case have != c.Expect:
			return kernel.Errorf(kernel.CodeConflict, "the counter %q is %d, not the %d that was read", c.Name, have, c.Expect)
		case c.Value < have:
			return kernel.Errorf(kernel.CodeConflict, "the counter %q is %d and does not go down to %d", c.Name, have, c.Value)
		}
		s.counters[c.Name] = c.Value
	}
	return nil
}

func (s *state) applyBranches(writes []BranchWrite) error {
	for _, bw := range writes {
		have := s.branches[bw.Branch.ID].Version
		if bw.Branch.ID == "" || have != bw.Expect {
			return kernel.Errorf(kernel.CodeConflict, "the branch %q is at version %d, not the %d that was read", bw.Branch.ID, have, bw.Expect)
		}
		b := cloneBranch(bw.Branch)
		b.Version = bw.Expect + 1
		s.branches[b.ID] = b
	}
	return nil
}

func (s *state) applyProposals(writes []ProposalWrite) error {
	for _, pw := range writes {
		have := s.proposals[pw.Proposal.ID].Version
		if pw.Proposal.ID == "" || have != pw.Expect {
			return kernel.Errorf(kernel.CodeConflict, "the proposal %q is at version %d, not the %d that was read", pw.Proposal.ID, have, pw.Expect)
		}
		p := cloneProposal(pw.Proposal)
		p.Version = pw.Expect + 1
		s.proposals[p.ID] = p
	}
	return nil
}
