package definitions

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Policy is what the caller decides about approvals; the unit does not know roles.
type Policy struct {
	// SoloApproval lets an author approve their own proposal. The approval says that it was solo, so the audit shows it.
	SoloApproval bool
}

// ApproveInput approves one content of a proposal.
type ApproveInput struct {
	ProposalID string
	// ContentHash is the content the approver looked at (the ContentHash of the proposal); an approval of another
	// content is refused, so an edit between looking and approving is not approved by mistake.
	ContentHash string
	Actor       Actor
	Policy      Policy
}

// sameParty tells whether two actors are one party for approvals: the same principal, or one acting on behalf of
// the other, or both on behalf of the same one.
func sameParty(a, b Actor) bool {
	return a.ID == b.ID ||
		(a.OnBehalfOf != "" && (a.OnBehalfOf == b.OnBehalfOf || a.OnBehalfOf == b.ID)) ||
		(b.OnBehalfOf != "" && b.OnBehalfOf == a.ID)
}

// RequestReview takes a draft to in_review. A proposal that changes nothing cannot be reviewed.
func (s *Service) RequestReview(ctx context.Context, proposalID string, actor Actor) (Proposal, error) {
	if err := s.checkActor(actor); err != nil {
		return Proposal{}, err
	}
	p, err := s.store.Proposal(ctx, proposalID)
	if err != nil {
		return Proposal{}, err
	}
	if p.State != ProposalDraft {
		return Proposal{}, kernel.Errorf(kernel.CodeConflict, "the proposal %q is %s; only a draft goes to review", p.ID, p.State)
	}
	b, err := s.store.Branch(ctx, p.BranchID)
	if err != nil {
		return Proposal{}, err
	}
	if len(b.Entries) == 0 {
		return Proposal{}, kernel.Errorf(kernel.CodeInvalidValue, "the proposal %q changes nothing, so there is nothing to review", p.ID)
	}
	return s.saveProposal(ctx, p, func(p *Proposal) { p.State = ProposalInReview })
}

// Approve records an approval of the content the approver saw and takes the proposal from in_review to approved. An
// author of the branch, or whoever acts for one, cannot approve unless the policy allows a solo approval.
func (s *Service) Approve(ctx context.Context, in ApproveInput) (Proposal, error) {
	if err := s.checkActor(in.Actor); err != nil {
		return Proposal{}, err
	}
	p, err := s.store.Proposal(ctx, in.ProposalID)
	if err != nil {
		return Proposal{}, err
	}
	if p.State != ProposalInReview {
		return Proposal{}, kernel.Errorf(kernel.CodeConflict, "the proposal %q is %s; only one in review is approved", p.ID, p.State)
	}
	if in.ContentHash != p.ContentHash {
		return Proposal{}, kernel.Errorf(kernel.CodeConflict, "the proposal %q has other content than the one that was looked at", p.ID)
	}
	b, err := s.store.Branch(ctx, p.BranchID)
	if err != nil {
		return Proposal{}, err
	}
	solo := sameParty(in.Actor, p.CreatedBy)
	for _, author := range b.Authors {
		solo = solo || sameParty(in.Actor, author)
	}
	if solo && !in.Policy.SoloApproval {
		return Proposal{}, kernel.Errorf(kernel.CodeConflict, "an author of the proposal %q cannot approve it", p.ID)
	}
	now := s.now()
	return s.saveProposal(ctx, p, func(p *Proposal) {
		p.State = ProposalApproved
		p.Approvals = append(p.Approvals, Approval{By: in.Actor, ContentHash: p.ContentHash, At: now, Solo: solo})
	})
}

// Close closes a proposal that is not merged or closed, and its branch with it. It is final: a closed proposal does
// not open again, and a new one is made from a new branch.
func (s *Service) Close(ctx context.Context, proposalID string, actor Actor, reason CloseReason) (Proposal, error) {
	if err := s.checkActor(actor); err != nil {
		return Proposal{}, err
	}
	switch reason {
	case CloseAbandoned, CloseRejected, CloseSuperseded:
	default:
		return Proposal{}, kernel.Errorf(kernel.CodeInvalidValue, "a proposal is closed as abandoned, rejected or superseded, not %q", reason)
	}
	b, p, err := s.loadPairOfProposal(ctx, proposalID)
	if err != nil {
		return Proposal{}, err
	}
	if p.State == ProposalMerged || p.State == ProposalClosed {
		return Proposal{}, kernel.Errorf(kernel.CodeConflict, "the proposal %q is %s and cannot be closed", p.ID, p.State)
	}
	bv, pv := b.Version, p.Version
	b.State, b.UpdatedAt = BranchClosed, s.now()
	p.State, p.CloseReason, p.ClosedBy, p.UpdatedAt = ProposalClosed, reason, actor.ID, b.UpdatedAt
	if err := s.store.Commit(ctx, Write{Branches: []BranchWrite{{Branch: b, Expect: bv}}, Proposals: []ProposalWrite{{Proposal: p, Expect: pv}}}); err != nil {
		return Proposal{}, err
	}
	p.Version = pv + 1
	return p, nil
}

func (s *Service) loadPairOfProposal(ctx context.Context, proposalID string) (Branch, Proposal, error) {
	p, err := s.store.Proposal(ctx, proposalID)
	if err != nil {
		return Branch{}, Proposal{}, err
	}
	b, err := s.store.Branch(ctx, p.BranchID)
	return b, p, err
}

// saveProposal changes a proposal alone, from the version that was read.
func (s *Service) saveProposal(ctx context.Context, p Proposal, change func(*Proposal)) (Proposal, error) {
	expect := p.Version
	change(&p)
	p.UpdatedAt = s.now()
	if err := s.store.Commit(ctx, Write{Proposals: []ProposalWrite{{Proposal: p, Expect: expect}}}); err != nil {
		return Proposal{}, err
	}
	p.Version = expect + 1
	return p, nil
}
