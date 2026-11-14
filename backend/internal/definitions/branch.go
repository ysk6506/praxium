package definitions

import (
	"context"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// CreateBranchInput opens a branch and, with it, its proposal in the draft state.
type CreateBranchInput struct {
	Name    string
	Title   string
	Summary string
	// Origin is change (the default) or import.
	Origin Origin
	Actor  Actor
}

// CreateBranch opens a branch on the set the main line is at now, and its proposal, in one write. The branch has no
// changes yet.
func (s *Service) CreateBranch(ctx context.Context, in CreateBranchInput) (Branch, Proposal, error) {
	switch in.Origin {
	case "":
		in.Origin = OriginChange
	case OriginChange, OriginImport:
	default:
		return Branch{}, Proposal{}, kernel.Errorf(kernel.CodeInvalidValue, "a branch is opened with the origin change or import, not %q", in.Origin)
	}
	return s.openBranch(ctx, openBranch{name: in.Name, title: in.Title, summary: in.Summary, origin: in.Origin, actor: in.Actor})
}

type openBranch struct {
	name, title, summary string
	origin               Origin
	actor                Actor
	entries              []BranchEntry
	restores             int64
}

func (s *Service) openBranch(ctx context.Context, in openBranch) (Branch, Proposal, error) {
	if err := s.checkActor(in.actor); err != nil {
		return Branch{}, Proposal{}, err
	}
	if in.title == "" {
		in.title = in.name
	}
	for _, t := range []struct {
		what, text string
		max        int
		limit      string
		required   bool
	}{
		{"a branch name", in.name, s.limits.MaxNameLen, "max_name_len", true},
		{"a title", in.title, s.limits.MaxNameLen, "max_name_len", true},
		{"a summary", in.summary, s.limits.MaxMessageLen, "max_message_len", false},
	} {
		if err := s.checkText(t.what, t.text, t.max, t.limit, t.required); err != nil {
			return Branch{}, Proposal{}, err
		}
	}
	if err := overLimit("max_branch_entries", s.limits.MaxBranchEntries, len(in.entries)); err != nil {
		return Branch{}, Proposal{}, err
	}
	open, err := s.store.Branches(ctx, []BranchState{BranchOpen})
	if err != nil {
		return Branch{}, Proposal{}, err
	}
	if err := overLimit("max_open_branches", s.limits.MaxOpenBranches, len(open)+1); err != nil {
		return Branch{}, Proposal{}, err
	}
	return s.writeNewBranch(ctx, in)
}

func (s *Service) writeNewBranch(ctx context.Context, in openBranch) (Branch, Proposal, error) {
	_, headNo, err := s.loadHead(ctx)
	if err != nil {
		return Branch{}, Proposal{}, err
	}
	branchID, err := s.freshID("B", func(id string) (bool, error) { return s.exists(s.store.Branch(ctx, id)) })
	if err != nil {
		return Branch{}, Proposal{}, err
	}
	proposalID, err := s.freshID("P", func(id string) (bool, error) { return s.exists(s.store.Proposal(ctx, id)) })
	if err != nil {
		return Branch{}, Proposal{}, err
	}
	hash, err := BranchChangesHash(in.entries)
	if err != nil {
		return Branch{}, Proposal{}, err
	}
	now := s.now()
	b := Branch{ID: branchID, Name: in.name, State: BranchOpen, Origin: in.origin, BaseSetNo: headNo, Entries: in.entries,
		ContentHash: hash, Authors: []Actor{in.actor}, ProposalID: proposalID, RestoresSetNo: in.restores,
		CreatedAt: now, UpdatedAt: now}
	p := Proposal{ID: proposalID, BranchID: branchID, Title: in.title, Summary: in.summary, State: ProposalDraft,
		Origin: in.origin, CreatedBy: in.actor, ContentHash: hash, CreatedAt: now, UpdatedAt: now}
	if err := s.store.Commit(ctx, Write{Branches: []BranchWrite{{Branch: b}}, Proposals: []ProposalWrite{{Proposal: p}}}); err != nil {
		return Branch{}, Proposal{}, err
	}
	b.Version, p.Version = 1, 1
	return b, p, nil
}

// exists turns the answer of a read by ID into "is it taken".
func (s *Service) exists(_ any, err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if kernel.CodeOf(err) == kernel.CodeNotFound {
		return false, nil
	}
	return false, err
}

// EditBranchInput changes the items of a branch.
type EditBranchInput struct {
	BranchID string
	// ExpectVersion is the version of the branch the caller read; the edit is refused if the branch is at another.
	ExpectVersion int64
	Changes       []Change
	Actor         Actor
}

// EditBranch puts changes into a branch and saves it at once. A change that brings an item back to what the base has
// takes it out of the branch, and removing an item the branch added takes that out too. An edit takes the proposal
// back to draft and voids its approvals. A request that changes nothing writes nothing.
func (s *Service) EditBranch(ctx context.Context, in EditBranchInput) (Branch, Proposal, error) {
	if err := s.checkActor(in.Actor); err != nil {
		return Branch{}, Proposal{}, err
	}
	b, p, err := s.loadPair(ctx, in.BranchID)
	if err != nil {
		return Branch{}, Proposal{}, err
	}
	if b.State != BranchOpen || p.State == ProposalMerged || p.State == ProposalClosed {
		return Branch{}, Proposal{}, kernel.Errorf(kernel.CodeConflict, "the branch %q is %s and can no longer be edited", b.ID, b.State)
	}
	if b.Version != in.ExpectVersion {
		return Branch{}, Proposal{}, kernel.Errorf(kernel.CodeConflict, "the branch %q is at version %d, not the %d that was read", b.ID, b.Version, in.ExpectVersion)
	}
	changes, err := s.prepare(ctx, in.Changes)
	if err != nil {
		return Branch{}, Proposal{}, err
	}
	entries, err := s.editOverlay(ctx, b, changes, in.Actor)
	if err != nil {
		return Branch{}, Proposal{}, err
	}
	hash, err := BranchChangesHash(entries)
	if err != nil {
		return Branch{}, Proposal{}, err
	}
	if hash == b.ContentHash {
		return b, p, nil
	}
	return s.saveEdit(ctx, b, p, entries, hash, in.Actor)
}

func (s *Service) saveEdit(ctx context.Context, b Branch, p Proposal, entries []BranchEntry, hash string, actor Actor) (Branch, Proposal, error) {
	if err := overLimit("max_branch_entries", s.limits.MaxBranchEntries, len(entries)); err != nil {
		return Branch{}, Proposal{}, err
	}
	bv, pv := b.Version, p.Version
	now := s.now()
	b.Entries, b.ContentHash, b.UpdatedAt = entries, hash, now
	b.Authors = withAuthor(b.Authors, actor)
	p.State, p.Approvals, p.ContentHash, p.UpdatedAt = ProposalDraft, nil, hash, now
	if err := s.store.Commit(ctx, Write{Branches: []BranchWrite{{Branch: b, Expect: bv}}, Proposals: []ProposalWrite{{Proposal: p, Expect: pv}}}); err != nil {
		return Branch{}, Proposal{}, err
	}
	b.Version, p.Version = bv+1, pv+1
	return b, p, nil
}

// loadPair reads a branch and its proposal.
func (s *Service) loadPair(ctx context.Context, branchID string) (Branch, Proposal, error) {
	b, err := s.store.Branch(ctx, branchID)
	if err != nil {
		return Branch{}, Proposal{}, err
	}
	p, err := s.store.Proposal(ctx, b.ProposalID)
	if err != nil {
		return Branch{}, Proposal{}, err
	}
	return b, p, nil
}

func withAuthor(authors []Actor, a Actor) []Actor {
	for _, have := range authors {
		if have == a {
			return authors
		}
	}
	return append(authors, a)
}

// editOverlay applies the changes to the entries of the branch, judging each against the base set.
func (s *Service) editOverlay(ctx context.Context, b Branch, changes []Change, actor Actor) ([]BranchEntry, error) {
	baseSet, err := s.loadSetOrEmpty(ctx, b.BaseSetNo)
	if err != nil {
		return nil, err
	}
	base := entryMap(baseSet)
	ids := make([]string, len(changes))
	for i, c := range changes {
		ids[i] = c.ItemID
	}
	known, err := s.store.Items(ctx, ids)
	if err != nil {
		return nil, err
	}
	overlay := map[string]BranchEntry{}
	for _, e := range b.Entries {
		overlay[e.ItemID] = e
	}
	for _, c := range changes {
		if c.Remove {
			err = overlayRemove(overlay, base, c.ItemID)
		} else {
			err = s.overlayPut(overlay, base, known, c, actor)
		}
		if err != nil {
			return nil, err
		}
	}
	out := make([]BranchEntry, 0, len(overlay))
	for _, e := range overlay {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ItemID < out[j].ItemID })
	return out, nil
}

func overlayRemove(overlay map[string]BranchEntry, base map[string]SetEntry, id string) error {
	if e, ok := base[id]; ok {
		overlay[id] = BranchEntry{ItemID: id, Op: OpRemove, BaseHash: e.ContentHash}
		return nil
	}
	if e, ok := overlay[id]; ok && e.Op == OpAdd {
		delete(overlay, id)
		return nil
	}
	return kernel.Errorf(kernel.CodeNotFound, "the item %q is not in the branch's set, so it cannot be removed", id)
}

func (s *Service) overlayPut(overlay map[string]BranchEntry, base map[string]SetEntry, known map[string]Item, c Change, actor Actor) error {
	it, _, err := itemFor(c, known, actor.ID, s.now())
	if err != nil {
		return err
	}
	hash, err := RevisionHash(it.Kind, c.SchemaVersion, c.Body)
	if err != nil {
		return err
	}
	baseEntry, inBase := base[c.ItemID]
	if inBase && baseEntry.ContentHash == hash {
		delete(overlay, c.ItemID)
		return nil
	}
	op := OpAdd
	if inBase {
		op = OpModify
	}
	overlay[c.ItemID] = BranchEntry{ItemID: c.ItemID, Op: op, Kind: it.Kind, Name: it.Name, SchemaVersion: c.SchemaVersion,
		Body: c.Body, ContentHash: hash, BaseHash: baseEntry.ContentHash}
	return nil
}
