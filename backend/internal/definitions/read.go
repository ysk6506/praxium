package definitions

import "context"

// Head returns the set the main line is at; false when it has none.
func (s *Service) Head(ctx context.Context) (Set, bool, error) {
	set, _, err := s.loadHead(ctx)
	if err != nil || set == nil {
		return Set{}, false, err
	}
	return *set, true, nil
}

// Set returns the definition set D-n with its entries.
func (s *Service) Set(ctx context.Context, no int64) (Set, error) { return s.store.Set(ctx, no) }

// History returns sets without entries, newest first: those below the number before (0 for all), at most limit (0 for
// 100).
func (s *Service) History(ctx context.Context, before int64, limit int) ([]Set, error) {
	if limit <= 0 {
		limit = 100
	}
	return s.store.Sets(ctx, before, limit)
}

// SetItems returns the items of a set with their content, in the order of the item IDs.
func (s *Service) SetItems(ctx context.Context, no int64) ([]ResolvedItem, error) {
	set, err := s.store.Set(ctx, no)
	if err != nil {
		return nil, err
	}
	return s.resolve(ctx, set.Entries)
}

// ItemHistory returns every revision of an item, oldest first.
func (s *Service) ItemHistory(ctx context.Context, itemID string) ([]Revision, error) {
	return s.store.ItemRevisions(ctx, itemID)
}

// Item returns what an item is.
func (s *Service) Item(ctx context.Context, itemID string) (Item, error) {
	found, err := s.store.Items(ctx, []string{itemID})
	if err != nil {
		return Item{}, err
	}
	it, ok := found[itemID]
	if !ok {
		return Item{}, notFound("item", itemID)
	}
	return it, nil
}

// Branch returns a branch.
func (s *Service) Branch(ctx context.Context, id string) (Branch, error) {
	return s.store.Branch(ctx, id)
}

// Branches lists the branches in the states (all when none is given), ordered by ID.
func (s *Service) Branches(ctx context.Context, states ...BranchState) ([]Branch, error) {
	return s.store.Branches(ctx, states)
}

// Proposal returns a proposal.
func (s *Service) Proposal(ctx context.Context, id string) (Proposal, error) {
	return s.store.Proposal(ctx, id)
}

// Proposals lists the proposals in the states (all when none is given), ordered by ID.
func (s *Service) Proposals(ctx context.Context, states ...ProposalState) ([]Proposal, error) {
	return s.store.Proposals(ctx, states)
}

// Draft returns every item as the branch has it: the base set with the changes of the branch laid on it. A preview
// run and the checks of a draft read this.
func (s *Service) Draft(ctx context.Context, branchID string) ([]ResolvedItem, error) {
	b, err := s.store.Branch(ctx, branchID)
	if err != nil {
		return nil, err
	}
	base, err := s.loadSetOrEmpty(ctx, b.BaseSetNo)
	if err != nil {
		return nil, err
	}
	items, err := s.resolve(ctx, headEntries(base))
	if err != nil {
		return nil, err
	}
	return overlayItems(items, b.Entries), nil
}
