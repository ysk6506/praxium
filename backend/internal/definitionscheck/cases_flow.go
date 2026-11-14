package definitionscheck

import (
	"context"
	"errors"

	"github.com/ysk6506/praxium/backend/internal/definitions"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func seedTwo(tb TB, svc *definitions.Service) {
	tb.Helper()
	apply(tb, svc, 0, item("item-a", `{"v":1}`), item("item-b", `{"v":2}`))
}

func merge(tb TB, svc *definitions.Service, p definitions.Proposal) (definitions.MergeResult, error) {
	tb.Helper()
	return svc.Merge(ctx, definitions.MergeInput{ProposalID: p.ID, Actor: bob})
}

func staleAndDisjoint(tb TB, newSubject Factory) {
	tb.Helper()
	svc := newSvc(tb, newSubject(tb).Store)
	seedTwo(tb, svc)
	_, overlapping := propose(tb, svc, "overlapping", item("item-a", `{"v":10}`), item("item-b", `{"v":20}`))
	_, disjoint := propose(tb, svc, "disjoint", item("item-b", `{"v":21}`), item("item-c", `{"v":3}`))
	_, first := propose(tb, svc, "first", item("item-a", `{"v":11}`))
	overlapping, disjoint, first = approved(tb, svc, overlapping), approved(tb, svc, disjoint), approved(tb, svc, first)
	_, err := merge(tb, svc, first)
	need(tb, err == nil, "the first merge: %v", err)
	_, err = merge(tb, svc, overlapping)
	var stale *definitions.StaleError
	need(tb, errors.As(err, &stale) && codeIs(err, kernel.CodeConflict) && len(stale.Items) == 1 && stale.Items[0] == "item-a", "a branch that overlaps: %v", err)
	need(tb, head(tb, svc) == 2, "a stale merge moved the head to D-%d", head(tb, svc))
	res, err := merge(tb, svc, disjoint)
	need(tb, err == nil && res.Set.No == 3 && len(res.Set.Entries) == 3, "a branch that does not overlap: %+v %v", res.Set, err)
	items, _ := svc.SetItems(ctx, 3)
	bodies := map[string]string{}
	for _, it := range items {
		bodies[it.ItemID] = string(it.Body)
	}
	need(tb, bodies["item-a"] == `{"v":11}` && bodies["item-b"] == `{"v":21}` && bodies["item-c"] == `{"v":3}`, "both changes must be in D-3: %v", bodies)
}

func selfApproval(tb TB, newSubject Factory) {
	tb.Helper()
	svc := newSvc(tb, newSubject(tb).Store)
	seedTwo(tb, svc)
	_, p := propose(tb, svc, "work", item("item-a", `{"v":10}`))
	p, err := svc.RequestReview(ctx, p.ID, alice)
	need(tb, err == nil, "review: %v", err)
	own := definitions.ApproveInput{ProposalID: p.ID, ContentHash: p.ContentHash, Actor: alice}
	_, err = svc.Approve(ctx, own)
	need(tb, codeIs(err, kernel.CodeConflict), "an author approving: %v", err)
	own.Policy.SoloApproval = true
	solo, err := svc.Approve(ctx, own)
	need(tb, err == nil && solo.State == definitions.ProposalApproved && len(solo.Approvals) == 1 && solo.Approvals[0].Solo,
		"a solo approval under a policy that allows it: %+v %v", solo, err)
	_, other := propose(tb, svc, "other", item("item-b", `{"v":30}`))
	other = approved(tb, svc, other)
	need(tb, len(other.Approvals) == 1 && !other.Approvals[0].Solo, "the approval of someone else is marked solo")
}

func forbiddenTransitions(tb TB, newSubject Factory) {
	tb.Helper()
	svc := newSvc(tb, newSubject(tb).Store)
	seedTwo(tb, svc)
	_, p := propose(tb, svc, "work", item("item-a", `{"v":10}`))
	approve := func() error {
		_, err := svc.Approve(ctx, definitions.ApproveInput{ProposalID: p.ID, ContentHash: p.ContentHash, Actor: bob})
		return err
	}
	mergeIt := func() error { _, err := merge(tb, svc, p); return err }
	for name, err := range map[string]error{"approving a draft": approve(), "merging a draft": mergeIt()} {
		need(tb, codeIs(err, kernel.CodeConflict), "%s: %v", name, err)
	}
	p = approved(tb, svc, p)
	_, err := svc.RequestReview(ctx, p.ID, alice)
	need(tb, codeIs(err, kernel.CodeConflict), "reviewing an approved proposal: %v", err)
	err = approve()
	need(tb, codeIs(err, kernel.CodeConflict), "approving twice: %v", err)
	_, err = merge(tb, svc, p)
	need(tb, err == nil, "merge: %v", err)
	err = mergeIt()
	need(tb, codeIs(err, kernel.CodeConflict), "merging twice: %v", err)
	_, err = svc.Close(ctx, p.ID, alice, definitions.CloseAbandoned)
	need(tb, codeIs(err, kernel.CodeConflict), "closing a merged proposal: %v", err)
	_, q := propose(tb, svc, "other", item("item-b", `{"v":5}`))
	_, err = svc.Close(ctx, q.ID, alice, definitions.CloseRejected)
	need(tb, err == nil, "close: %v", err)
	_, err = svc.RequestReview(ctx, q.ID, alice)
	need(tb, codeIs(err, kernel.CodeConflict), "reviewing a closed proposal: %v", err)
}

func branchVersions(tb TB, newSubject Factory) {
	tb.Helper()
	s := newSubject(tb)
	svc := newSvc(tb, s.Store)
	seedTwo(tb, svc)
	b, _ := propose(tb, svc, "work", item("item-a", `{"v":10}`))
	edit := func(actor definitions.Actor, expect int64, body string) error {
		_, _, err := svc.EditBranch(ctx, definitions.EditBranchInput{BranchID: b.ID, ExpectVersion: expect, Actor: actor, Changes: []definitions.Change{item("item-a", body)}})
		return err
	}
	err := edit(bob, b.Version, `{"v":11}`)
	need(tb, err == nil, "the edit with the version read: %v", err)
	err = edit(alice, b.Version, `{"v":12}`)
	need(tb, codeIs(err, kernel.CodeConflict), "the edit with an old version: %v", err)
	got, _ := svc.Branch(ctx, b.ID)
	need(tb, got.Version == b.Version+1 && len(got.Entries) == 1 && string(got.Entries[0].Body) == `{"v":11}`, "the late editor changed the branch: %+v", got)
	// The same at the port, where two writers that read the same version meet.
	late := definitions.Write{Branches: []definitions.BranchWrite{{Branch: got, Expect: b.Version}}}
	err = s.Store.Commit(ctx, late)
	need(tb, codeIs(err, kernel.CodeConflict), "a write of a branch from an old version at the port: %v", err)
}

func checkStopsTheMerge(tb TB, newSubject Factory) {
	tb.Helper()
	svc := newSvc(tb, newSubject(tb).Store)
	seedTwo(tb, svc)
	_, p := propose(tb, svc, "work", item("item-a", `{"v":10}`))
	p = approved(tb, svc, p)
	var seen int
	check := func(_ context.Context, merged []definitions.ResolvedItem) []definitions.Problem {
		seen = len(merged)
		return []definitions.Problem{{ItemID: "item-a", Code: "bad", Message: "the check says no"}}
	}
	_, err := svc.Merge(ctx, definitions.MergeInput{ProposalID: p.ID, Actor: bob, Check: check, Counters: map[string]int64{"n": 9}})
	var failed *definitions.CheckFailedError
	need(tb, errors.As(err, &failed) && len(failed.Problems) == 1 && seen == 2, "a merge that the check stops: %v (the check saw %d items)", err, seen)
	got, _ := svc.Proposal(ctx, p.ID)
	need(tb, head(tb, svc) == 1 && number(tb, svc, "n") == 1 && got.State == definitions.ProposalApproved,
		"a stopped merge changed something: head D-%d, proposal %s", head(tb, svc), got.State)
}

func restoreMakesANewSet(tb TB, newSubject Factory) {
	tb.Helper()
	svc := newSvc(tb, newSubject(tb).Store)
	first := apply(tb, svc, 0, item("item-a", `{"v":1}`), item("item-b", `{"v":2}`))
	_, p := propose(tb, svc, "change", item("item-a", `{"v":10}`), definitions.Change{ItemID: "item-b", Remove: true}, item("item-c", `{}`))
	_, err := merge(tb, svc, approved(tb, svc, p))
	need(tb, err == nil, "merge: %v", err)
	b, rp, err := svc.Restore(ctx, definitions.RestoreInput{SetNo: 1, Actor: alice})
	need(tb, err == nil && b.Origin == definitions.OriginRestore && rp.Origin == definitions.OriginRestore, "restore: %+v %v", b, err)
	res, err := merge(tb, svc, approved(tb, svc, rp))
	need(tb, err == nil && res.Set.No == 3 && res.Set.Hash == first.Set.Hash && res.Set.Origin == definitions.OriginRestore, "the restored set: %+v %v", res.Set, err)
	old, _ := svc.Set(ctx, 1)
	need(tb, old.Hash == first.Set.Hash && len(old.Entries) == 2, "an old set changed: %+v", old)
	_, _, err = svc.Restore(ctx, definitions.RestoreInput{SetNo: 1, Actor: alice})
	need(tb, errors.Is(err, definitions.ErrNothingToRestore), "a restore of the content that is there: %v", err)
}

func sameAfterRestart(tb TB, newSubject Factory) {
	tb.Helper()
	s := newSubject(tb)
	svc := newSvc(tb, s.Store)
	_, err := svc.Apply(ctx, definitions.ApplyInput{Baseline: "std", Counters: map[string]int64{"n": 100003}, Actor: alice,
		Changes: []definitions.Change{item("item-a", `{"v":1}`), item("item-b", `{"v":2}`)}})
	need(tb, err == nil, "apply: %v", err)
	_, p := propose(tb, svc, "work", item("item-a", `{"v":10}`))
	p = approved(tb, svc, p)
	_, open := propose(tb, svc, "open", item("item-b", `{"v":20}`))
	reopened, err := s.Reopen()
	need(tb, err == nil, "reopen: %v", err)
	again := newSvc(tb, reopened)
	set, _, _ := again.Head(ctx)
	h, _ := again.History(ctx, 0, 0)
	need(tb, len(h) == 1 && set.No == 1 && set.Baseline == "std" && number(tb, again, "n") == 100003, "head, sets or ledger after the restart: %+v", set)
	got, err := again.Proposal(ctx, p.ID)
	need(tb, err == nil && got.State == definitions.ProposalApproved && len(got.Approvals) == 1, "the approved proposal after the restart: %+v %v", got, err)
	got, err = again.Proposal(ctx, open.ID)
	need(tb, err == nil && got.State == definitions.ProposalDraft, "the draft proposal after the restart: %+v %v", got, err)
	// The work goes on where it was: the merge of the approved proposal, and a new branch with an ID of its own.
	res, err := merge(tb, again, p)
	need(tb, err == nil && res.Set.No == 2, "the merge after the restart: %+v %v", res, err)
	b, _, err := again.CreateBranch(ctx, definitions.CreateBranchInput{Name: "new", Actor: alice})
	need(tb, err == nil && b.BaseSetNo == 2, "a new branch after the restart: %+v %v", b, err)
}

func limitsAreNamed(tb TB, newSubject Factory) {
	tb.Helper()
	svc := newSvc(tb, newSubject(tb).Store, definitions.Limits{MaxBodyBytes: 16, MaxItemsPerSet: 2, MaxOpenBranches: 1})
	cases := []struct {
		limit string
		err   func() error
	}{
		{"max_body_bytes", func() error {
			_, err := svc.Apply(ctx, definitions.ApplyInput{Actor: alice, Changes: []definitions.Change{item("item-a", `{"text":"this is longer than sixteen"}`)}})
			return err
		}},
		{"max_items_per_set", func() error {
			_, err := svc.Apply(ctx, definitions.ApplyInput{Actor: alice, Changes: []definitions.Change{item("item-a", `{}`), item("item-b", `{}`), item("item-c", `{}`)}})
			return err
		}},
		{"max_open_branches", func() error {
			if _, _, err := svc.CreateBranch(ctx, definitions.CreateBranchInput{Name: "one", Actor: alice}); err != nil {
				return err
			}
			_, _, err := svc.CreateBranch(ctx, definitions.CreateBranchInput{Name: "two", Actor: alice})
			return err
		}},
	}
	for _, c := range cases {
		err := c.err()
		var limit *definitions.LimitError
		need(tb, errors.As(err, &limit) && limit.Limit == c.limit && codeIs(err, kernel.CodeInvalidValue), "the limit %s: %v", c.limit, err)
	}
	need(tb, head(tb, svc) == 0, "a request over a limit made a set")
}
