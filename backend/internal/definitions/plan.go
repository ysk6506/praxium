package definitions

import (
	"context"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// prepare checks the changes of a caller and puts them in normal form: bodies normalized, a new item given an ID.
func (s *Service) prepare(ctx context.Context, changes []Change) ([]Change, error) {
	out := make([]Change, 0, len(changes))
	seen := map[string]bool{}
	for _, c := range changes {
		c, err := s.prepareOne(ctx, c)
		if err != nil {
			return nil, err
		}
		if seen[c.ItemID] {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "the item %q is named twice in one change", c.ItemID)
		}
		seen[c.ItemID] = true
		out = append(out, c)
	}
	return out, nil
}

func (s *Service) prepareOne(ctx context.Context, c Change) (Change, error) {
	if c.ItemID != "" {
		if _, err := kernel.ParseID[struct{}](c.ItemID); err != nil {
			return c, err
		}
	}
	if c.Remove {
		if c.ItemID == "" {
			return c, kernel.Errorf(kernel.CodeInvalidValue, "a removal names the item")
		}
		return Change{ItemID: c.ItemID, Remove: true}, nil
	}
	if err := s.checkText("a name", c.Name, s.limits.MaxNameLen, "max_name_len", false); err != nil {
		return c, err
	}
	if err := s.checkText("a kind", c.Kind, s.limits.MaxNameLen, "max_name_len", false); err != nil {
		return c, err
	}
	if c.SchemaVersion < 1 {
		return c, kernel.Errorf(kernel.CodeInvalidValue, "a schema version starts at 1")
	}
	body, err := NormalizeBody(c.Body)
	if err != nil {
		return c, err
	}
	if err := overLimit("max_body_bytes", s.limits.MaxBodyBytes, len(body)); err != nil {
		return c, err
	}
	c.Body = body
	if c.ItemID == "" {
		if c.ItemID, err = s.freshItemID(ctx); err != nil {
			return c, err
		}
	}
	return c, nil
}

// itemFor finds the item a put is for: a known one (whose kind and name a put may repeat but not change) or a new one,
// which needs a kind and a name.
func itemFor(c Change, known map[string]Item, actor string, at time.Time) (Item, bool, error) {
	if it, ok := known[c.ItemID]; ok {
		if c.Kind != "" && c.Kind != it.Kind {
			return it, false, kernel.Errorf(kernel.CodeInvalidValue, "the item %q is of kind %q and cannot be put as %q", it.ID, it.Kind, c.Kind)
		}
		if c.Name != "" && c.Name != it.Name {
			return it, false, kernel.Errorf(kernel.CodeInvalidValue, "the item %q is named %q; a name does not change", it.ID, it.Name)
		}
		return it, false, nil
	}
	if c.Kind == "" || c.Name == "" {
		return Item{}, false, kernel.Errorf(kernel.CodeInvalidValue, "a new item %q needs a kind and a name", c.ItemID)
	}
	return Item{ID: c.ItemID, Kind: c.Kind, Name: c.Name, CreatedBy: actor, CreatedAt: at}, true, nil
}

type planInput struct {
	head       *Set
	changes    []Change
	baseline   string
	origin     Origin
	proposalID string
	actor      Actor
	message    string
}

type planned struct {
	write     Write
	set       Set
	unchanged bool
}

// plan works out what a list of prepared changes does to the set the main line is at: the new items, the revisions of
// the content that is really new, and the set. It writes nothing. When the result has the content of the head, it says
// so and makes no set.
func (s *Service) plan(ctx context.Context, in planInput) (planned, error) {
	cur := entryMap(in.head)
	ids := make([]string, 0, len(in.changes))
	for _, c := range in.changes {
		ids = append(ids, c.ItemID)
	}
	known, err := s.store.Items(ctx, ids)
	if err != nil {
		return planned{}, err
	}
	var w Write
	for _, c := range in.changes {
		if err := s.planOne(c, known, cur, in.actor, in.message, &w); err != nil {
			return planned{}, err
		}
	}
	entries := sortedEntries(cur)
	if err := overLimit("max_items_per_set", s.limits.MaxItemsPerSet, len(entries)); err != nil {
		return planned{}, err
	}
	hash, err := SetHash(in.baseline, entries)
	if err != nil {
		return planned{}, err
	}
	if (in.head == nil && len(entries) == 0) || (in.head != nil && in.head.Hash == hash) {
		return planned{unchanged: true}, nil
	}
	set := Set{No: 1, Hash: hash, Origin: in.origin, Baseline: in.baseline, Entries: entries,
		ProposalID: in.proposalID, CreatedBy: in.actor.ID, Message: in.message, CreatedAt: s.now()}
	move := HeadMove{New: 1}
	if in.head != nil {
		set.No, set.ParentNo = in.head.No+1, in.head.No
		move = HeadMove{Expect: in.head.No, New: set.No}
	}
	w.Set, w.Head = &set, &move
	return planned{write: w, set: set}, nil
}

// planOne puts one change into the running entries and the write.
func (s *Service) planOne(c Change, known map[string]Item, cur map[string]SetEntry, actor Actor, message string, w *Write) error {
	if c.Remove {
		if _, ok := cur[c.ItemID]; !ok {
			return kernel.Errorf(kernel.CodeNotFound, "the item %q is not in the set, so it cannot be removed", c.ItemID)
		}
		delete(cur, c.ItemID)
		return nil
	}
	it, isNew, err := itemFor(c, known, actor.ID, s.now())
	if err != nil {
		return err
	}
	hash, err := RevisionHash(it.Kind, c.SchemaVersion, c.Body)
	if err != nil {
		return err
	}
	if e, ok := cur[c.ItemID]; ok && e.ContentHash == hash {
		return nil
	}
	if isNew {
		w.Items = append(w.Items, it)
	}
	rev := Revision{ItemID: it.ID, RevNo: it.LastRev + 1, ContentHash: hash, SchemaVersion: c.SchemaVersion, Body: c.Body,
		CreatedBy: actor.ID, Message: message, CreatedAt: s.now()}
	w.Revisions = append(w.Revisions, rev)
	cur[it.ID] = SetEntry{ItemID: it.ID, RevNo: rev.RevNo, ContentHash: hash}
	return nil
}
