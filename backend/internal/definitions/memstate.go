package definitions

import (
	"maps"
	"slices"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// state is the content of a MemoryStore. The sets and revisions in it are never changed once they are there, so a copy
// of the state shares them.
type state struct {
	head      int64
	items     map[string]Item
	revs      map[string][]Revision
	sets      []Set
	counters  map[string]int64
	branches  map[string]Branch
	proposals map[string]Proposal
}

func newState() *state {
	return &state{
		items:     map[string]Item{},
		revs:      map[string][]Revision{},
		counters:  map[string]int64{},
		branches:  map[string]Branch{},
		proposals: map[string]Proposal{},
	}
}

// clone is the copy a write is tried on. Revision lists are copied as slice headers: a write only appends to them.
func (s *state) clone() *state {
	return &state{
		head:      s.head,
		items:     maps.Clone(s.items),
		revs:      maps.Clone(s.revs),
		sets:      slices.Clone(s.sets),
		counters:  maps.Clone(s.counters),
		branches:  maps.Clone(s.branches),
		proposals: maps.Clone(s.proposals),
	}
}

// export is the content as a copy. view is the same without copies of what is never changed (bodies, entries), for
// a PersistFunc that only reads it.
func (s *state) export() Data { return s.data(true) }

func (s *state) data(deep bool) Data {
	d := Data{Head: s.head, Counters: maps.Clone(s.counters)}
	copyRev, copySet := func(r Revision) Revision { return r }, func(x Set, _ bool) Set { return x }
	if deep {
		copyRev, copySet = cloneRevision, cloneSet
	}
	for _, it := range s.items {
		d.Items = append(d.Items, it)
	}
	sort.Slice(d.Items, func(i, j int) bool { return d.Items[i].ID < d.Items[j].ID })
	for _, it := range d.Items {
		for _, r := range s.revs[it.ID] {
			d.Revisions = append(d.Revisions, copyRev(r))
		}
	}
	for _, set := range s.sets {
		d.Sets = append(d.Sets, copySet(set, true))
	}
	for _, b := range s.branches {
		d.Branches = append(d.Branches, cloneBranch(b))
	}
	sort.Slice(d.Branches, func(i, j int) bool { return d.Branches[i].ID < d.Branches[j].ID })
	for _, p := range s.proposals {
		d.Proposals = append(d.Proposals, cloneProposal(p))
	}
	sort.Slice(d.Proposals, func(i, j int) bool { return d.Proposals[i].ID < d.Proposals[j].ID })
	return d
}

func bad(format string, args ...any) error {
	return kernel.Errorf(kernel.CodeInvalidValue, "the definitions are damaged: "+format, args...)
}

// stateFromData checks data and makes a state of it. Every hash is computed again, so a body or an entry that was
// changed behind the store's back is found.
func stateFromData(d Data) (*state, error) {
	s := newState()
	for _, it := range d.Items {
		if it.ID == "" || it.Kind == "" {
			return nil, bad("an item has no ID or kind")
		}
		if _, dup := s.items[it.ID]; dup {
			return nil, bad("the item %q is there twice", it.ID)
		}
		s.items[it.ID] = it
	}
	if err := s.loadRevisions(d.Revisions); err != nil {
		return nil, err
	}
	for _, it := range s.items {
		if int64(len(s.revs[it.ID])) != it.LastRev {
			return nil, bad("the item %q says it has %d revisions and there are %d", it.ID, it.LastRev, len(s.revs[it.ID]))
		}
	}
	for i, set := range d.Sets {
		if err := s.checkSet(set, int64(i+1)); err != nil {
			return nil, err
		}
		s.sets = append(s.sets, cloneSet(set, true))
	}
	if d.Head != int64(len(s.sets)) {
		return nil, bad("the head is D-%d and the last set is D-%d", d.Head, len(s.sets))
	}
	s.head = d.Head
	return s, s.loadRest(d)
}

func (s *state) loadRevisions(revs []Revision) error {
	for _, r := range revs {
		it, ok := s.items[r.ItemID]
		if !ok {
			return bad("a revision belongs to the item %q, which is not there", r.ItemID)
		}
		if r.RevNo != int64(len(s.revs[r.ItemID]))+1 {
			return bad("the revisions of %q do not follow each other at %d", r.ItemID, r.RevNo)
		}
		if err := checkRevisionHash(it.Kind, r); err != nil {
			return err
		}
		s.revs[r.ItemID] = append(s.revs[r.ItemID], cloneRevision(r))
	}
	return nil
}

func checkRevisionHash(kind string, r Revision) error {
	want, err := RevisionHash(kind, r.SchemaVersion, r.Body)
	if err != nil {
		return err
	}
	if want != r.ContentHash {
		return bad("the revision %d of %q does not have the hash of its content", r.RevNo, r.ItemID)
	}
	return nil
}

// checkSet checks a set that is to be number no: its place, its entries and its hash.
func (s *state) checkSet(set Set, no int64) error {
	if set.No != no {
		return bad("the set at place %d is numbered D-%d", no, set.No)
	}
	for i, e := range set.Entries {
		if i > 0 && set.Entries[i-1].ItemID >= e.ItemID {
			return bad("the entries of D-%d are not in the order of the item IDs", no)
		}
		revs := s.revs[e.ItemID]
		if e.RevNo < 1 || e.RevNo > int64(len(revs)) || revs[e.RevNo-1].ContentHash != e.ContentHash {
			return bad("D-%d holds a revision of %q that is not there", no, e.ItemID)
		}
	}
	want, err := SetHash(set.Baseline, set.Entries)
	if err != nil {
		return err
	}
	if want != set.Hash {
		return bad("D-%d does not have the hash of its content", no)
	}
	return nil
}

func (s *state) loadRest(d Data) error {
	for name, v := range d.Counters {
		if v < 1 {
			return bad("the counter %q is %d", name, v)
		}
		s.counters[name] = v
	}
	for _, b := range d.Branches {
		if _, dup := s.branches[b.ID]; dup || b.ID == "" || b.Version < 1 {
			return bad("the branch %q is not sound", b.ID)
		}
		s.branches[b.ID] = cloneBranch(b)
	}
	for _, p := range d.Proposals {
		if _, dup := s.proposals[p.ID]; dup || p.ID == "" || p.Version < 1 {
			return bad("the proposal %q is not sound", p.ID)
		}
		if _, ok := s.branches[p.BranchID]; !ok {
			return bad("the proposal %q has no branch", p.ID)
		}
		s.proposals[p.ID] = cloneProposal(p)
	}
	return nil
}

func cloneRevision(r Revision) Revision {
	r.Body = slices.Clone(r.Body)
	return r
}

func cloneSet(s Set, withEntries bool) Set {
	if withEntries {
		s.Entries = slices.Clone(s.Entries)
	} else {
		s.Entries = nil
	}
	return s
}

func cloneBranch(b Branch) Branch {
	entries := make([]BranchEntry, len(b.Entries))
	for i, e := range b.Entries {
		e.Body = slices.Clone(e.Body)
		entries[i] = e
	}
	b.Entries = entries
	b.Authors = slices.Clone(b.Authors)
	return b
}

func cloneProposal(p Proposal) Proposal {
	p.Approvals = slices.Clone(p.Approvals)
	return p
}
