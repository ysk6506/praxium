package definitions

import (
	"context"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-338]
func TestMergingAnApprovedProposalMakesOneSetAndMarksBothMerged(t *testing.T) {
	r, a, b := seeded(t)
	br, p := r.propose("work", putID(a, `{"v":10}`), Change{ItemID: b, Remove: true}, put("link", "c", `{"v":3}`))
	p = r.approve(p)
	res, err := r.svc.Merge(ctx, MergeInput{ProposalID: p.ID, Actor: bob, Counters: map[string]int64{"n": 5}})
	if err != nil || !res.Changed || res.Set.No != 2 || res.Set.ProposalID != p.ID || res.Set.CreatedBy != "bob" ||
		res.Set.Message != p.Title || len(res.Set.Entries) != 2 || res.Set.ParentNo != 1 {
		t.Fatalf("merge: %+v %v", res, err)
	}
	if res.Proposal.State != ProposalMerged || res.Proposal.MergedSetNo != 2 {
		t.Fatalf("proposal: %+v", res.Proposal)
	}
	if got, _ := r.svc.Branch(ctx, br.ID); got.State != BranchMerged {
		t.Fatalf("branch: %s", got.State)
	}
	if n, _ := r.svc.NextNumber(ctx, "n", 1); n != 5 {
		t.Fatalf("the ledger rose with the set: %d", n)
	}
	items, _ := r.svc.SetItems(ctx, 2)
	if len(items) != 2 || string(items[0].Body) != `{"v":10}` && string(items[1].Body) != `{"v":10}` {
		t.Fatalf("items of D-2: %+v", items)
	}
}

// [REQ-338]
func TestABranchThatTouchesAnItemTheMainLineChangedIsStale(t *testing.T) {
	r, a, b := seeded(t)
	_, mine := r.propose("mine", putID(a, `{"v":10}`), putID(b, `{"v":20}`))
	_, theirs := r.propose("theirs", putID(a, `{"v":99}`))
	mine, theirs = r.approve(mine), r.approve(theirs)
	if _, err := r.svc.Merge(ctx, MergeInput{ProposalID: theirs.ID, Actor: bob}); err != nil {
		t.Fatal(err)
	}
	_, err := r.svc.Merge(ctx, MergeInput{ProposalID: mine.ID, Actor: bob})
	se, ok := isErr[*StaleError](err)
	if !ok || code(err) != kernel.CodeConflict || len(se.Items) != 1 || se.Items[0] != a || se.BaseSetNo != 1 || se.HeadSetNo != 2 {
		t.Fatalf("stale: %v", err)
	}
	if head, _, _ := r.svc.Head(ctx); head.No != 2 {
		t.Fatalf("a refused merge moved the head to D-%d", head.No)
	}
	if got, _ := r.svc.Proposal(ctx, mine.ID); got.State != ProposalApproved {
		t.Fatalf("a refused merge changed the proposal: %s", got.State)
	}
}

// [REQ-338]
func TestBranchesThatDoNotOverlapBothMergeAndTheSameChangeTwiceIsStale(t *testing.T) {
	r, a, b := seeded(t)
	_, p1 := r.propose("one", putID(a, `{"v":10}`))
	_, p2 := r.propose("two", putID(b, `{"v":20}`), put("link", "c", `{}`))
	_, p3 := r.propose("three", putID(a, `{"v":10}`))
	p1, p2, p3 = r.approve(p1), r.approve(p2), r.approve(p3)
	if _, err := r.svc.Merge(ctx, MergeInput{ProposalID: p1.ID, Actor: bob}); err != nil {
		t.Fatal(err)
	}
	res, err := r.svc.Merge(ctx, MergeInput{ProposalID: p2.ID, Actor: bob})
	if err != nil || res.Set.No != 3 || len(res.Set.Entries) != 3 {
		t.Fatalf("a branch of an older base that overlaps nothing: %+v %v", res.Set, err)
	}
	items, _ := r.svc.SetItems(ctx, 3)
	bodies := map[string]string{}
	for _, it := range items {
		bodies[it.ItemID] = string(it.Body)
	}
	if bodies[a] != `{"v":10}` || bodies[b] != `{"v":20}` {
		t.Fatalf("both changes must be in the merged set: %v", bodies)
	}
	// Even an identical change is refused: there is no three-way merge.
	_, err = r.svc.Merge(ctx, MergeInput{ProposalID: p3.ID, Actor: bob})
	wantCode(t, err, kernel.CodeConflict)
}

// [REQ-338]
func TestTheCheckSeesTheWholeMergedSetAndAProblemStopsTheMerge(t *testing.T) {
	r, a, _ := seeded(t)
	_, p := r.propose("work", putID(a, `{"v":10}`), put("link", "c", `{"v":3}`))
	p = r.approve(p)
	var seen []ResolvedItem
	bad := func(_ context.Context, merged []ResolvedItem) []Problem {
		seen = merged
		return []Problem{{ItemID: a, Code: "no_good", Message: "the check says no"}, {Code: "two", Message: "again"}}
	}
	_, err := r.svc.Merge(ctx, MergeInput{ProposalID: p.ID, Actor: bob, Check: bad, Counters: map[string]int64{"n": 9}})
	cf, ok := isErr[*CheckFailedError](err)
	if !ok || code(err) != kernel.CodeInvalidValue || len(cf.Problems) != 2 || cf.Problems[0].Code != "no_good" {
		t.Fatalf("check failure: %v", err)
	}
	if len(seen) != 3 || seen[0].Body == nil || seen[0].Kind == "" {
		t.Fatalf("the check must get all three items with their content: %+v", seen)
	}
	if head, _, _ := r.svc.Head(ctx); head.No != 1 {
		t.Fatal("a merge that the check stopped made a set")
	}
	if n, _ := r.svc.NextNumber(ctx, "n", 1); n != 1 {
		t.Fatalf("a merge that the check stopped raised the ledger to %d", n)
	}
	if got, _ := r.svc.Proposal(ctx, p.ID); got.State != ProposalApproved {
		t.Fatalf("proposal after the stopped merge: %s", got.State)
	}
	fine := func(context.Context, []ResolvedItem) []Problem { return nil }
	if res, err := r.svc.Merge(ctx, MergeInput{ProposalID: p.ID, Actor: bob, Check: fine}); err != nil || !res.Changed {
		t.Fatalf("the same proposal merges once the check is happy: %v", err)
	}
}

// [REQ-338]
func TestAMergeThatChangesNothingOnTheHeadMakesNoSet(t *testing.T) {
	r, a, _ := seeded(t)
	// An edit never leaves an entry that equals the base, and a branch with no entries is not reviewed, so a branch
	// like this one cannot come from the service; it is written by hand to show that the merge copes with it.
	br, p := r.propose("same")
	body := []byte(`{"v":1}`)
	hash, _ := RevisionHash("source", 1, body)
	br.Entries = []BranchEntry{{ItemID: a, Op: OpModify, Kind: "source", Name: "a", SchemaVersion: 1, Body: body, ContentHash: hash}}
	br.ContentHash, _ = OverlayHash(br.Entries)
	p.ContentHash, p.State = br.ContentHash, ProposalApproved
	write := Write{Branches: []BranchWrite{{Branch: br, Expect: br.Version}}, Proposals: []ProposalWrite{{Proposal: p, Expect: p.Version}}}
	if err := r.store.Commit(ctx, write); err != nil {
		t.Fatal(err)
	}
	res, err := r.svc.Merge(ctx, MergeInput{ProposalID: p.ID, Actor: bob})
	if err != nil || res.Changed || res.Set.No != 1 || res.Proposal.State != ProposalMerged || res.Proposal.MergedSetNo != 1 {
		t.Fatalf("an empty merge: %+v %v", res, err)
	}
	if head, _, _ := r.svc.Head(ctx); head.No != 1 {
		t.Fatalf("head D-%d", head.No)
	}
	if got, _ := r.svc.Branch(ctx, br.ID); got.State != BranchMerged {
		t.Fatalf("branch %s", got.State)
	}
}

// [REQ-338]
func TestAFirstProposalMergesIntoAMainLineWithoutASet(t *testing.T) {
	r := newRig(t)
	_, p := r.propose("first", put("k", "a", `{"v":1}`))
	p = r.approve(p)
	res, err := r.svc.Merge(ctx, MergeInput{ProposalID: p.ID, Actor: bob, Baseline: "std-1", Check: func(_ context.Context, m []ResolvedItem) []Problem {
		if len(m) != 1 {
			return []Problem{{Code: "count", Message: "one item expected"}}
		}
		return nil
	}})
	if err != nil || res.Set.No != 1 || res.Set.ParentNo != 0 || res.Set.Baseline != "std-1" || res.Set.Origin != OriginChange {
		t.Fatalf("first merge: %+v %v", res, err)
	}
}

// [REQ-339]
func TestRestoreOpensANewBranchThatBringsBackAnEarlierSetAndNeverRewinds(t *testing.T) {
	r, a, b := seeded(t)
	d1, _ := r.svc.Set(ctx, 1)
	_, p := r.propose("change", putID(a, `{"v":10}`), Change{ItemID: b, Remove: true}, put("link", "c", `{}`))
	p = r.approve(p)
	if _, err := r.svc.Merge(ctx, MergeInput{ProposalID: p.ID, Actor: bob}); err != nil {
		t.Fatal(err)
	}
	br, rp, err := r.svc.Restore(ctx, RestoreInput{SetNo: 1, Actor: alice})
	if err != nil || br.Origin != OriginRestore || rp.Origin != OriginRestore || br.RestoresSetNo != 1 || br.BaseSetNo != 2 || len(br.Entries) != 3 || rp.Title != "Restore D-1" {
		t.Fatalf("restore: %+v %+v %v", br, rp, err)
	}
	ops := map[Op]int{}
	for _, e := range br.Entries {
		ops[e.Op]++
	}
	if ops[OpModify] != 1 || ops[OpAdd] != 1 || ops[OpRemove] != 1 {
		t.Fatalf("operations: %v", ops)
	}
	rp = r.approve(rp)
	res, err := r.svc.Merge(ctx, MergeInput{ProposalID: rp.ID, Actor: bob})
	if err != nil || res.Set.No != 3 || res.Set.Origin != OriginRestore || res.Set.Hash != d1.Hash {
		t.Fatalf("the merged restore must make D-3 with the content of D-1: %+v %v", res.Set, err)
	}
	if back, _ := r.svc.Set(ctx, 1); back.Hash != d1.Hash || len(back.Entries) != 2 {
		t.Fatal("an earlier set changed")
	}
	revs, _ := r.svc.ItemHistory(ctx, a)
	if len(revs) != 3 || revs[2].ContentHash != revs[0].ContentHash || revs[2].RevNo != 3 {
		t.Fatalf("the restored item must have a new revision number with the old content: %+v", revs)
	}
	// D-3 has the content of D-1 already.
	_, _, err = r.svc.Restore(ctx, RestoreInput{SetNo: 1, Actor: alice})
	if err != ErrNothingToRestore || code(err) != kernel.CodeConflict {
		t.Fatalf("a second restore: %v", err)
	}
	if _, _, err = r.svc.Restore(ctx, RestoreInput{SetNo: 9, Actor: alice}); code(err) != kernel.CodeNotFound {
		t.Fatalf("a set that is not there: %v", err)
	}
	if _, _, err = r.svc.Restore(ctx, RestoreInput{SetNo: 2, Actor: Actor{}}); code(err) != kernel.CodeInvalidValue {
		t.Fatalf("no actor: %v", err)
	}
}

// [REQ-339]
func TestRestoreToAnEmptyMainLineRemovesEverything(t *testing.T) {
	r := newRig(t)
	if _, err := r.svc.Apply(ctx, ApplyInput{Baseline: "std", Actor: alice}); err != nil {
		t.Fatal(err)
	}
	r.apply(0, put("k", "a", `{}`))
	br, rp, err := r.svc.Restore(ctx, RestoreInput{SetNo: 1, Title: "back", Actor: alice})
	_ = br
	_ = rp
	// D-1 is the set with the item (the first apply made no set because it had no items): there is nothing earlier.
	if err != ErrNothingToRestore {
		t.Fatalf("restore: %v", err)
	}
	second := r.apply(1, put("k", "b", `{}`))
	br, rp, err = r.svc.Restore(ctx, RestoreInput{SetNo: 1, Title: "back", Actor: alice})
	if err != nil || rp.Title != "back" || len(br.Entries) != 1 || br.Entries[0].Op != OpRemove {
		t.Fatalf("restore of D-1 on D-%d: %+v %v", second.Set.No, br, err)
	}
}
