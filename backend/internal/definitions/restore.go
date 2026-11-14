package definitions

import (
	"context"
	"fmt"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// ErrNothingToRestore is the answer to a restore of a set that has the content of the head already.
var ErrNothingToRestore = kernel.Errorf(kernel.CodeConflict, "the set already has the content of the head: nothing to restore")

// RestoreInput asks for the content of an earlier set to come back.
type RestoreInput struct {
	SetNo   int64
	Title   string
	Summary string
	Actor   Actor
}

// Restore opens a branch (origin restore) on the head whose changes make the head hold what the set D-n held: items
// that differ are put back as they were, items the set did not have are removed, and items it had and the head lost
// are added again. Nothing is rewound: sets are never changed, and merging the proposal makes a new set after the
// head, with new revisions, after the checks and the approvals of any other proposal. When the head already has the
// content, ErrNothingToRestore is the answer.
func (s *Service) Restore(ctx context.Context, in RestoreInput) (Branch, Proposal, error) {
	target, err := s.store.Set(ctx, in.SetNo)
	if err != nil {
		return Branch{}, Proposal{}, err
	}
	head, _, err := s.loadHead(ctx)
	if err != nil {
		return Branch{}, Proposal{}, err
	}
	entries, err := s.restoreEntries(ctx, target, head)
	if err != nil {
		return Branch{}, Proposal{}, err
	}
	if len(entries) == 0 {
		return Branch{}, Proposal{}, ErrNothingToRestore
	}
	title := in.Title
	if title == "" {
		title = fmt.Sprintf("Restore D-%d", in.SetNo)
	}
	return s.openBranch(ctx, openBranch{name: fmt.Sprintf("restore-D-%d", in.SetNo), title: title, summary: in.Summary,
		origin: OriginRestore, actor: in.Actor, entries: entries, restores: in.SetNo})
}

// restoreEntries lists what the head must change to hold what the target holds.
func (s *Service) restoreEntries(ctx context.Context, target Set, head *Set) ([]BranchEntry, error) {
	cur := entryMap(head)
	want := entryMap(&target)
	var put []SetEntry
	var out []BranchEntry
	for id, w := range want {
		if c, ok := cur[id]; !ok || c.ContentHash != w.ContentHash {
			put = append(put, w)
		}
	}
	sort.Slice(put, func(i, j int) bool { return put[i].ItemID < put[j].ItemID })
	items, err := s.resolve(ctx, put)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		op, base := OpAdd, ""
		if c, ok := cur[it.ItemID]; ok {
			op, base = OpModify, c.ContentHash
		}
		out = append(out, BranchEntry{ItemID: it.ItemID, Op: op, Kind: it.Kind, Name: it.Name, SchemaVersion: it.SchemaVersion,
			Body: it.Body, ContentHash: it.ContentHash, BaseHash: base})
	}
	for id, c := range cur {
		if _, ok := want[id]; !ok {
			out = append(out, BranchEntry{ItemID: id, Op: OpRemove, BaseHash: c.ContentHash})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ItemID < out[j].ItemID })
	return out, nil
}
