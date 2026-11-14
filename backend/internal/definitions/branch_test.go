package definitions

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// seeded makes a main line with two items and returns their IDs.
func seeded(t *testing.T, limits ...Limits) (*rig, string, string) {
	r := newRig(t, limits...)
	set := r.apply(0, put("source", "a", `{"v":1}`), put("feed", "b", `{"v":2}`)).Set
	return r, r.idOf(set, "a"), r.idOf(set, "b")
}

// [REQ-336]
func TestABranchKeepsOnlyWhatItChangesAndSavesAtEveryEdit(t *testing.T) {
	r, a, b := seeded(t)
	br, p, err := r.svc.CreateBranch(ctx, CreateBranchInput{Name: "work", Title: "Work", Summary: "s", Actor: alice})
	if err != nil || br.State != BranchOpen || br.BaseSetNo != 1 || br.Version != 1 || p.State != ProposalDraft || p.Version != 1 || p.BranchID != br.ID || br.ProposalID != p.ID {
		t.Fatalf("create: %+v %+v %v", br, p, err)
	}
	br, p, err = r.svc.EditBranch(ctx, EditBranchInput{BranchID: br.ID, ExpectVersion: 1, Actor: bob,
		Changes: []Change{putID(a, `{"v":10}`), {ItemID: b, Remove: true}, put("link", "c", `{"v":3}`)}})
	if err != nil || br.Version != 2 || p.Version != 2 || len(br.Entries) != 3 {
		t.Fatalf("edit: %+v %v", br, err)
	}
	ops := map[Op]int{}
	for _, e := range br.Entries {
		ops[e.Op]++
		if e.Op == OpModify && (e.BaseHash == "" || e.ContentHash == e.BaseHash) {
			t.Fatalf("a modification has the hash of the base and of the new content: %+v", e)
		}
	}
	if ops[OpAdd] != 1 || ops[OpModify] != 1 || ops[OpRemove] != 1 || len(br.Authors) != 2 {
		t.Fatalf("operations %v, authors %v", ops, br.Authors)
	}
	// The saved branch is what a reader gets, and the main line did not move.
	got, _ := r.svc.Branch(ctx, br.ID)
	if got.Version != 2 || len(got.Entries) != 3 {
		t.Fatalf("read back: %+v", got)
	}
	if head, _, _ := r.svc.Head(ctx); head.No != 1 {
		t.Fatalf("a branch moved the head to D-%d", head.No)
	}
	draft, err := r.svc.Draft(ctx, br.ID)
	if err != nil || len(draft) != 2 {
		t.Fatalf("draft: %+v %v", draft, err)
	}
	for _, it := range draft {
		if it.ItemID == a && string(it.Body) != `{"v":10}` {
			t.Fatalf("the draft of a: %s", it.Body)
		}
		if it.ItemID == b {
			t.Fatal("the removed item is in the draft")
		}
	}
	if _, err := r.svc.Draft(ctx, "nobody"); code(err) != kernel.CodeNotFound {
		t.Fatalf("a draft of nothing: %v", err)
	}
}

// [REQ-336]
func TestTwoEditorsOfOneBranchCannotOverwriteEachOther(t *testing.T) {
	r, a, _ := seeded(t)
	br, _, _ := r.svc.CreateBranch(ctx, CreateBranchInput{Name: "work", Actor: alice})
	if _, _, err := r.svc.EditBranch(ctx, EditBranchInput{BranchID: br.ID, ExpectVersion: 1, Actor: alice, Changes: []Change{putID(a, `{"v":5}`)}}); err != nil {
		t.Fatal(err)
	}
	_, _, err := r.svc.EditBranch(ctx, EditBranchInput{BranchID: br.ID, ExpectVersion: 1, Actor: bob, Changes: []Change{putID(a, `{"v":6}`)}})
	wantCode(t, err, kernel.CodeConflict)
	if got, _ := r.svc.Branch(ctx, br.ID); string(got.Entries[0].Body) != `{"v":5}` || got.Version != 2 {
		t.Fatalf("the late editor changed the branch: %+v", got)
	}
}

// [REQ-336]
func TestEditsThatUndoThemselvesLeaveNothing(t *testing.T) {
	r, a, b := seeded(t)
	br, _, _ := r.svc.CreateBranch(ctx, CreateBranchInput{Name: "work", Actor: alice})
	br, _, err := r.svc.EditBranch(ctx, EditBranchInput{BranchID: br.ID, ExpectVersion: 1, Actor: alice,
		Changes: []Change{putID(a, `{"v":9}`), put("k", "new", `{}`)}})
	if err != nil || len(br.Entries) != 2 {
		t.Fatal(err)
	}
	newID := ""
	for _, e := range br.Entries {
		if e.Op == OpAdd {
			newID = e.ItemID
		}
	}
	// Putting a back to the base content, and removing what the branch added, take both out.
	br, _, err = r.svc.EditBranch(ctx, EditBranchInput{BranchID: br.ID, ExpectVersion: 2, Actor: alice,
		Changes: []Change{putID(a, `{"v":1}`), {ItemID: newID, Remove: true}}})
	if err != nil || len(br.Entries) != 0 {
		t.Fatalf("entries left: %+v %v", br.Entries, err)
	}
	// An edit that changes nothing writes nothing: the version stays.
	same, _, err := r.svc.EditBranch(ctx, EditBranchInput{BranchID: br.ID, ExpectVersion: 3, Actor: alice, Changes: []Change{putID(b, `{"v":2}`)}})
	if err != nil || same.Version != 3 {
		t.Fatalf("a no-op edit: version %d, %v", same.Version, err)
	}
	// A removal of an item that is not there is an error, a second removal of one that is there is not.
	_, _, err = r.svc.EditBranch(ctx, EditBranchInput{BranchID: br.ID, ExpectVersion: 3, Actor: alice, Changes: []Change{{ItemID: "nobody", Remove: true}}})
	wantCode(t, err, kernel.CodeNotFound)
	if _, _, err = r.svc.EditBranch(ctx, EditBranchInput{BranchID: br.ID, ExpectVersion: 3, Actor: alice, Changes: []Change{{ItemID: b, Remove: true}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = r.svc.EditBranch(ctx, EditBranchInput{BranchID: br.ID, ExpectVersion: 4, Actor: alice, Changes: []Change{{ItemID: b, Remove: true}}}); err != nil {
		t.Fatalf("removing twice: %v", err)
	}
}

// [REQ-336]
func TestABranchOnAnEmptyMainLineAndItsLimits(t *testing.T) {
	r := newRig(t, Limits{MaxOpenBranches: 2, MaxBranchEntries: 1, MaxNameLen: 8, MaxMessageLen: 8})
	br, _, err := r.svc.CreateBranch(ctx, CreateBranchInput{Name: "first", Actor: alice})
	if err != nil || br.BaseSetNo != 0 {
		t.Fatalf("a branch on nothing: %+v %v", br, err)
	}
	if _, _, err := r.svc.EditBranch(ctx, EditBranchInput{BranchID: br.ID, ExpectVersion: 1, Actor: alice,
		Changes: []Change{put("k", "a", `{}`), put("k", "b", `{}`)}}); code(err) != kernel.CodeInvalidValue {
		t.Fatalf("too many entries: %v", err)
	}
	if _, _, err := r.svc.CreateBranch(ctx, CreateBranchInput{Name: "second", Actor: alice}); err != nil {
		t.Fatal(err)
	}
	_, _, err = r.svc.CreateBranch(ctx, CreateBranchInput{Name: "third", Actor: alice})
	if le, ok := isErr[*LimitError](err); !ok || le.Limit != "max_open_branches" {
		t.Fatalf("a third open branch: %v", err)
	}
	for name, in := range map[string]CreateBranchInput{
		"no name":   {Actor: alice},
		"long name": {Name: "123456789", Actor: alice},
		"summary":   {Name: "x", Summary: "123456789", Actor: alice},
		"no actor":  {Name: "x"},
		"origin":    {Name: "x", Actor: alice, Origin: OriginRestore},
	} {
		if _, _, err := r.svc.CreateBranch(ctx, in); code(err) != kernel.CodeInvalidValue {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, _, err := r.svc.EditBranch(ctx, EditBranchInput{BranchID: "nobody", Actor: alice}); code(err) != kernel.CodeNotFound {
		t.Fatalf("an edit of a missing branch: %v", err)
	}
	if _, _, err := r.svc.EditBranch(ctx, EditBranchInput{BranchID: br.ID, ExpectVersion: 1}); code(err) != kernel.CodeInvalidValue {
		t.Fatalf("an edit without an actor: %v", err)
	}
	if _, _, err := r.svc.EditBranch(ctx, EditBranchInput{BranchID: br.ID, ExpectVersion: 1, Actor: alice, Changes: []Change{{Remove: true}}}); code(err) != kernel.CodeInvalidValue {
		t.Fatalf("a bad change: %v", err)
	}
	branches, _ := r.svc.Branches(ctx, BranchOpen)
	closed, _ := r.svc.Branches(ctx, BranchClosed)
	all, _ := r.svc.Branches(ctx)
	if len(branches) != 2 || len(closed) != 0 || len(all) != 2 {
		t.Fatalf("lists: %d %d %d", len(branches), len(closed), len(all))
	}
}

// [REQ-337]
func TestAnEditTakesTheProposalBackToDraftAndVoidsTheApprovals(t *testing.T) {
	r, a, _ := seeded(t)
	br, p := r.propose("work", putID(a, `{"v":7}`))
	p = r.approve(p)
	if p.State != ProposalApproved || len(p.Approvals) != 1 || p.Approvals[0].ContentHash != p.ContentHash || p.Approvals[0].Solo {
		t.Fatalf("approved: %+v", p)
	}
	br, p, err := r.svc.EditBranch(ctx, EditBranchInput{BranchID: br.ID, ExpectVersion: br.Version, Actor: alice, Changes: []Change{putID(a, `{"v":8}`)}})
	if err != nil || p.State != ProposalDraft || len(p.Approvals) != 0 || p.ContentHash != br.ContentHash {
		t.Fatalf("after the edit: %+v %v", p, err)
	}
	// A merged or closed branch is not edited.
	if _, err := r.svc.Close(ctx, p.ID, alice, CloseAbandoned); err != nil {
		t.Fatal(err)
	}
	_, _, err = r.svc.EditBranch(ctx, EditBranchInput{BranchID: br.ID, ExpectVersion: br.Version + 1, Actor: alice, Changes: []Change{putID(a, `{"v":9}`)}})
	wantCode(t, err, kernel.CodeConflict)
}
