package definitions

import (
	"context"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// MergeInput merges an approved proposal into the main line.
type MergeInput struct {
	ProposalID string
	Actor      Actor
	// Baseline is the hash of what stands outside the items for the new set; empty keeps the one of the head.
	Baseline string
	// Counters are raised in the same write as the set.
	Counters map[string]int64
	// Check, when given, judges the whole merged set just before the write; a problem stops the merge.
	Check CheckFunc
}

// MergeResult is what a merge made. When Changed is false the branch changed nothing on the head: the proposal is
// merged and no set was made, and Set is the head.
type MergeResult struct {
	Set      Set
	Changed  bool
	Proposal Proposal
}

// Merge puts the branch of an approved proposal on the head. A branch whose base is not the head is merged if none of
// its items was changed on the main line since the base; otherwise it is refused as stale and nothing is written (no
// three-way merge and no resolving of conflicts). The check runs on the merged set last. The set, the revisions, the
// ledger, the head and the new state of the branch and the proposal are one atomic write, so a merge that fails leaves
// the branch and the proposal as they were.
func (s *Service) Merge(ctx context.Context, in MergeInput) (MergeResult, error) {
	if err := s.checkActor(in.Actor); err != nil {
		return MergeResult{}, err
	}
	b, p, err := s.loadPairOfProposal(ctx, in.ProposalID)
	if err != nil {
		return MergeResult{}, err
	}
	if p.State != ProposalApproved || b.State != BranchOpen || p.ContentHash != b.ContentHash {
		return MergeResult{}, kernel.Errorf(kernel.CodeConflict, "the proposal %q is %s; only an approved proposal with the content that was approved is merged", p.ID, p.State)
	}
	head, headNo, err := s.loadHead(ctx)
	if err != nil {
		return MergeResult{}, err
	}
	if err := s.checkStale(ctx, b, head, headNo); err != nil {
		return MergeResult{}, err
	}
	if err := s.runCheck(ctx, in.Check, head, b); err != nil {
		return MergeResult{}, err
	}
	return s.writeMerge(ctx, in, b, p, head)
}

// checkStale refuses a branch that touches an item the main line has changed since the base.
func (s *Service) checkStale(ctx context.Context, b Branch, head *Set, headNo int64) error {
	if b.BaseSetNo == headNo {
		return nil
	}
	baseSet, err := s.loadSetOrEmpty(ctx, b.BaseSetNo)
	if err != nil {
		return err
	}
	base, cur := entryMap(baseSet), entryMap(head)
	var items []string
	for _, e := range b.Entries {
		if base[e.ItemID].ContentHash != cur[e.ItemID].ContentHash {
			items = append(items, e.ItemID)
		}
	}
	if len(items) == 0 {
		return nil
	}
	sort.Strings(items)
	return staleError(b.BaseSetNo, headNo, items)
}

func (s *Service) runCheck(ctx context.Context, check CheckFunc, head *Set, b Branch) error {
	if check == nil {
		return nil
	}
	items, err := s.resolve(ctx, headEntries(head))
	if err != nil {
		return err
	}
	if problems := check(ctx, overlayItems(items, b.Entries)); len(problems) > 0 {
		return checkFailed(problems)
	}
	return nil
}

func headEntries(head *Set) []SetEntry {
	if head == nil {
		return nil
	}
	return head.Entries
}

func (s *Service) writeMerge(ctx context.Context, in MergeInput, b Branch, p Proposal, head *Set) (MergeResult, error) {
	changes := make([]Change, len(b.Entries))
	for i, e := range b.Entries {
		changes[i] = Change{ItemID: e.ItemID, Kind: e.Kind, Name: e.Name, SchemaVersion: e.SchemaVersion, Body: e.Body, Remove: e.Op == OpRemove}
	}
	now := s.now()
	merged := func(setNo int64) (BranchWrite, ProposalWrite) {
		mb, mp := b, p
		mb.State, mb.UpdatedAt = BranchMerged, now
		mp.State, mp.MergedSetNo, mp.UpdatedAt = ProposalMerged, setNo, now
		return BranchWrite{Branch: mb, Expect: b.Version}, ProposalWrite{Proposal: mp, Expect: p.Version}
	}
	res, err := s.applyOn(ctx, head, ApplyInput{Origin: p.Origin, Baseline: in.Baseline, Changes: changes,
		Counters: in.Counters, Actor: in.Actor, Message: p.Title}, p.ID,
		func(w *Write, pl planned) {
			bw, pw := merged(pl.set.No)
			w.Branches, w.Proposals = append(w.Branches, bw), append(w.Proposals, pw)
		})
	if err != nil {
		return MergeResult{}, err
	}
	if !res.Changed {
		bw, pw := merged(res.Set.No)
		if err := s.store.Commit(ctx, Write{Branches: []BranchWrite{bw}, Proposals: []ProposalWrite{pw}}); err != nil {
			return MergeResult{}, err
		}
	}
	return s.mergeResult(ctx, res, p.ID)
}

func (s *Service) mergeResult(ctx context.Context, res ApplyResult, proposalID string) (MergeResult, error) {
	merged, err := s.store.Proposal(ctx, proposalID)
	if err != nil {
		return MergeResult{}, err
	}
	return MergeResult{Set: res.Set, Changed: res.Changed, Proposal: merged}, nil
}

// overlayItems lays the entries of a branch on items: removals leave, adds and modifications put their content in.
func overlayItems(items []ResolvedItem, entries []BranchEntry) []ResolvedItem {
	byID := make(map[string]ResolvedItem, len(items)+len(entries))
	for _, it := range items {
		byID[it.ItemID] = it
	}
	for _, e := range entries {
		if e.Op == OpRemove {
			delete(byID, e.ItemID)
			continue
		}
		byID[e.ItemID] = ResolvedItem{ItemID: e.ItemID, Kind: e.Kind, Name: e.Name, ContentHash: e.ContentHash,
			SchemaVersion: e.SchemaVersion, Body: e.Body}
	}
	out := make([]ResolvedItem, 0, len(byID))
	for _, it := range byID {
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ItemID < out[j].ItemID })
	return out
}
