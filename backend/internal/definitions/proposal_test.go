package definitions

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-337]
func TestAProposalGoesDraftReviewApprovedAndNoOtherWay(t *testing.T) {
	r, a, _ := seeded(t)
	_, p := r.propose("work", putID(a, `{"v":7}`))
	// draft: not approvable, not mergeable.
	_, err := r.svc.Approve(ctx, ApproveInput{ProposalID: p.ID, ContentHash: p.ContentHash, Actor: bob})
	wantCode(t, err, kernel.CodeConflict)
	_, err = r.svc.Merge(ctx, MergeInput{ProposalID: p.ID, Actor: bob})
	wantCode(t, err, kernel.CodeConflict)
	p, err = r.svc.RequestReview(ctx, p.ID, alice)
	if err != nil || p.State != ProposalInReview {
		t.Fatalf("review: %+v %v", p, err)
	}
	// in_review: not requested again, not mergeable, not approved for other content.
	_, err = r.svc.RequestReview(ctx, p.ID, alice)
	wantCode(t, err, kernel.CodeConflict)
	_, err = r.svc.Merge(ctx, MergeInput{ProposalID: p.ID, Actor: bob})
	wantCode(t, err, kernel.CodeConflict)
	_, err = r.svc.Approve(ctx, ApproveInput{ProposalID: p.ID, ContentHash: "something else", Actor: bob})
	wantCode(t, err, kernel.CodeConflict)
	p, err = r.svc.Approve(ctx, ApproveInput{ProposalID: p.ID, ContentHash: p.ContentHash, Actor: bob})
	if err != nil || p.State != ProposalApproved {
		t.Fatalf("approve: %+v %v", p, err)
	}
	// approved: not approved again, not sent back to review.
	_, err = r.svc.Approve(ctx, ApproveInput{ProposalID: p.ID, ContentHash: p.ContentHash, Actor: bob})
	wantCode(t, err, kernel.CodeConflict)
	_, err = r.svc.RequestReview(ctx, p.ID, alice)
	wantCode(t, err, kernel.CodeConflict)
	// merged: final.
	if _, err = r.svc.Merge(ctx, MergeInput{ProposalID: p.ID, Actor: bob}); err != nil {
		t.Fatal(err)
	}
	_, err = r.svc.Close(ctx, p.ID, alice, CloseAbandoned)
	wantCode(t, err, kernel.CodeConflict)
	_, err = r.svc.Merge(ctx, MergeInput{ProposalID: p.ID, Actor: bob})
	wantCode(t, err, kernel.CodeConflict)
	for name, f := range map[string]func() error{
		"review of nothing":  func() error { _, err := r.svc.RequestReview(ctx, "nobody", alice); return err },
		"approve of nothing": func() error { _, err := r.svc.Approve(ctx, ApproveInput{ProposalID: "nobody", Actor: bob}); return err },
		"close of nothing":   func() error { _, err := r.svc.Close(ctx, "nobody", alice, CloseAbandoned); return err },
		"merge of nothing":   func() error { _, err := r.svc.Merge(ctx, MergeInput{ProposalID: "nobody", Actor: bob}); return err },
	} {
		if code(f()) != kernel.CodeNotFound {
			t.Errorf("%s: %v", name, f())
		}
	}
	if _, err := r.svc.RequestReview(ctx, p.ID, Actor{}); code(err) != kernel.CodeInvalidValue {
		t.Fatalf("no actor: %v", err)
	}
	if _, err := r.svc.Approve(ctx, ApproveInput{ProposalID: p.ID}); code(err) != kernel.CodeInvalidValue {
		t.Fatalf("no actor: %v", err)
	}
	if _, err := r.svc.Close(ctx, p.ID, Actor{}, CloseAbandoned); code(err) != kernel.CodeInvalidValue {
		t.Fatalf("no actor: %v", err)
	}
	if _, err := r.svc.Merge(ctx, MergeInput{ProposalID: p.ID}); code(err) != kernel.CodeInvalidValue {
		t.Fatalf("no actor: %v", err)
	}
}

// [REQ-337]
func TestAProposalThatChangesNothingIsNotSentToReview(t *testing.T) {
	r, _, _ := seeded(t)
	_, p := r.propose("empty")
	_, err := r.svc.RequestReview(ctx, p.ID, alice)
	wantCode(t, err, kernel.CodeInvalidValue)
}

// [REQ-337]
func TestAnAuthorCannotApproveUnlessThePolicyAllowsItAndThenItIsRecorded(t *testing.T) {
	r, a, _ := seeded(t)
	br, p := r.propose("work", putID(a, `{"v":7}`))
	// bob joined the authors by editing; a service account acting for alice is alice.
	if _, _, err := r.svc.EditBranch(ctx, EditBranchInput{BranchID: br.ID, ExpectVersion: br.Version, Actor: bob, Changes: []Change{putID(a, `{"v":8}`)}}); err != nil {
		t.Fatal(err)
	}
	p, _ = r.svc.RequestReview(ctx, p.ID, alice)
	for name, who := range map[string]Actor{
		"the creator":                  alice,
		"another author":               bob,
		"acting for an author":         {ID: "bot", OnBehalfOf: "alice"},
		"an author acting for the one": {ID: "bot2", OnBehalfOf: "bob"},
		"on behalf of the same":        {ID: "carol", OnBehalfOf: "bob"},
	} {
		_, err := r.svc.Approve(ctx, ApproveInput{ProposalID: p.ID, ContentHash: p.ContentHash, Actor: who})
		if code(err) != kernel.CodeConflict {
			t.Errorf("%s was allowed to approve: %v", name, err)
		}
	}
	// A principal acting for the creator is the creator, too, even when the creator is named as the ID.
	_, err := r.svc.Approve(ctx, ApproveInput{ProposalID: p.ID, ContentHash: p.ContentHash, Actor: Actor{ID: "alice", OnBehalfOf: "x"}})
	wantCode(t, err, kernel.CodeConflict)
	solo, err := r.svc.Approve(ctx, ApproveInput{ProposalID: p.ID, ContentHash: p.ContentHash, Actor: alice, Policy: Policy{SoloApproval: true}})
	if err != nil || solo.State != ProposalApproved || !solo.Approvals[0].Solo || solo.Approvals[0].By != alice {
		t.Fatalf("a solo approval must be allowed and recorded: %+v %v", solo, err)
	}
	// Someone else's approval is not solo.
	br2, p2 := r.propose("other", putID(a, `{"v":11}`))
	_ = br2
	p2 = r.approve(p2)
	if p2.Approvals[0].Solo {
		t.Fatal("an approval by someone else is not solo")
	}
}

// [REQ-337]
func TestClosingIsFinalAndClosesTheBranch(t *testing.T) {
	r, a, _ := seeded(t)
	br, p := r.propose("work", putID(a, `{"v":7}`))
	_, err := r.svc.Close(ctx, p.ID, alice, "because")
	wantCode(t, err, kernel.CodeInvalidValue)
	closed, err := r.svc.Close(ctx, p.ID, bob, CloseRejected)
	if err != nil || closed.State != ProposalClosed || closed.CloseReason != CloseRejected || closed.ClosedBy != "bob" || closed.Version != p.Version+1 {
		t.Fatalf("close: %+v %v", closed, err)
	}
	if got, _ := r.svc.Branch(ctx, br.ID); got.State != BranchClosed {
		t.Fatalf("the branch is %s", got.State)
	}
	_, err = r.svc.Close(ctx, p.ID, bob, CloseRejected)
	wantCode(t, err, kernel.CodeConflict)
	_, err = r.svc.RequestReview(ctx, p.ID, alice)
	wantCode(t, err, kernel.CodeConflict)
	list, _ := r.svc.Proposals(ctx, ProposalClosed)
	none, _ := r.svc.Proposals(ctx, ProposalMerged)
	got, err := r.svc.Proposal(ctx, p.ID)
	if len(list) != 1 || len(none) != 0 || err != nil || got.State != ProposalClosed {
		t.Fatalf("lists: %d %d %v", len(list), len(none), err)
	}
	// A closed branch no longer counts against the limit of open ones.
	if open, _ := r.svc.Branches(ctx, BranchOpen); len(open) != 0 {
		t.Fatalf("open branches: %d", len(open))
	}
}
