package definitions

import (
	"context"
	"encoding/json"
	"time"
)

// Origin tells how a definition set or a proposal came to be.
type Origin string

// The origins. A set made by merging carries the origin of its proposal and the ID of that proposal.
const (
	// OriginChange is an ordinary change: a direct apply or a merged proposal.
	OriginChange Origin = "change"
	// OriginImport is a change made from files that were imported.
	OriginImport Origin = "import"
	// OriginBootstrap is the first set of a deployment.
	OriginBootstrap Origin = "bootstrap"
	// OriginRestore is a change that brings back the content of an earlier set (a new set, never a rewind).
	OriginRestore Origin = "restore"
)

// Actor is who does a thing. The unit does not know roles: the caller says who acts and under what policy.
type Actor struct {
	ID         string `json:"id"`
	OnBehalfOf string `json:"on_behalf_of,omitempty"`
}

// Item is a definition that keeps its ID, kind and name for good. A used item ID is never used for another item.
type Item struct {
	ID        string    `json:"item_id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	// LastRev is the number of the newest revision; the store keeps it.
	LastRev int64 `json:"last_rev"`
}

// Revision is one immutable content of an item. RevNo goes up at every new content, also when the content equals an
// older revision (a restore).
type Revision struct {
	ItemID        string          `json:"item_id"`
	RevNo         int64           `json:"rev_no"`
	ContentHash   string          `json:"content_hash"`
	SchemaVersion int             `json:"schema_version"`
	Body          json.RawMessage `json:"body"`
	CreatedBy     string          `json:"created_by"`
	Message       string          `json:"message,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

// SetEntry says which revision of an item a set holds.
type SetEntry struct {
	ItemID      string `json:"item_id"`
	RevNo       int64  `json:"rev_no"`
	ContentHash string `json:"content_hash"`
}

// Set is a definition set (D-n): every item of the main line at one moment. It is never changed.
type Set struct {
	No int64 `json:"set_no"`
	// Hash is SetHash of the baseline and the entries. The parent is not part of it.
	Hash     string `json:"content_hash"`
	ParentNo int64  `json:"parent_set_no"`
	Origin   Origin `json:"origin"`
	// Baseline is the hash the caller gave for what stands outside the items (the standard model, for one).
	Baseline   string     `json:"baseline"`
	Entries    []SetEntry `json:"entries"`
	ProposalID string     `json:"proposal_id,omitempty"`
	CreatedBy  string     `json:"created_by"`
	Message    string     `json:"message,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Change is one wish for an item, as a caller states it. A put without an ItemID makes a new item (Kind and Name are
// needed); a put with the ID of a known item gives it a new revision (a Kind or Name must be the item's own); a put
// with an unknown ID makes the item with that ID. Remove takes the item out of the set; its history stays.
type Change struct {
	ItemID        string
	Kind          string
	Name          string
	SchemaVersion int
	Body          json.RawMessage
	Remove        bool
}

// ResolvedItem is an item as one set or one draft holds it, with its content.
type ResolvedItem struct {
	ItemID        string          `json:"item_id"`
	Kind          string          `json:"kind"`
	Name          string          `json:"name"`
	RevNo         int64           `json:"rev_no"`
	ContentHash   string          `json:"content_hash"`
	SchemaVersion int             `json:"schema_version"`
	Body          json.RawMessage `json:"body"`
}

// Op is what a branch does to an item.
type Op string

// The operations of a branch entry.
const (
	OpAdd    Op = "add"
	OpModify Op = "modify"
	OpRemove Op = "remove"
)

// BranchEntry is one changed item of a branch. Only changed items are kept.
type BranchEntry struct {
	ItemID        string          `json:"item_id"`
	Op            Op              `json:"op"`
	Kind          string          `json:"kind,omitempty"`
	Name          string          `json:"name,omitempty"`
	SchemaVersion int             `json:"schema_version,omitempty"`
	Body          json.RawMessage `json:"body,omitempty"`
	ContentHash   string          `json:"content_hash,omitempty"`
	// BaseHash is the content hash of the item in the base set; empty for an item the base does not have.
	BaseHash string `json:"base_content_hash,omitempty"`
}

// BranchState is the state of a branch.
type BranchState string

// The states of a branch.
const (
	BranchOpen   BranchState = "open"
	BranchMerged BranchState = "merged"
	BranchClosed BranchState = "closed"
)

// Branch is a draft: the changes to the items of a base set. Every edit is saved at once and raises Version, so two
// editors cannot overwrite each other.
type Branch struct {
	ID            string        `json:"branch_id"`
	Name          string        `json:"name"`
	State         BranchState   `json:"state"`
	Origin        Origin        `json:"origin"`
	BaseSetNo     int64         `json:"base_set_no"`
	Entries       []BranchEntry `json:"entries"`
	ContentHash   string        `json:"content_hash"`
	Authors       []Actor       `json:"authors"`
	ProposalID    string        `json:"proposal_id"`
	RestoresSetNo int64         `json:"restores_set_no,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
	Version       int64         `json:"version"`
}

// ProposalState is the state of a proposal.
type ProposalState string

// The states of a proposal: draft, in_review, approved, then merged or closed.
const (
	ProposalDraft    ProposalState = "draft"
	ProposalInReview ProposalState = "in_review"
	ProposalApproved ProposalState = "approved"
	ProposalMerged   ProposalState = "merged"
	ProposalClosed   ProposalState = "closed"
)

// CloseReason tells why a proposal was closed.
type CloseReason string

// The reasons to close a proposal.
const (
	CloseAbandoned  CloseReason = "abandoned"
	CloseRejected   CloseReason = "rejected"
	CloseSuperseded CloseReason = "superseded"
)

// Approval is an approval of one content of a branch. Solo is true when an author approved their own proposal under a
// policy that allows it, so the audit can show it.
type Approval struct {
	By          Actor     `json:"by"`
	ContentHash string    `json:"content_hash"`
	At          time.Time `json:"at"`
	Solo        bool      `json:"solo,omitempty"`
}

// Proposal is the request to merge a branch; the two are one to one. An edit of the branch takes the proposal back to
// draft and voids its approvals.
type Proposal struct {
	ID          string        `json:"proposal_id"`
	BranchID    string        `json:"branch_id"`
	Title       string        `json:"title"`
	Summary     string        `json:"summary,omitempty"`
	State       ProposalState `json:"state"`
	Origin      Origin        `json:"origin"`
	CreatedBy   Actor         `json:"created_by"`
	ContentHash string        `json:"content_hash"`
	Approvals   []Approval    `json:"approvals"`
	CloseReason CloseReason   `json:"close_reason,omitempty"`
	ClosedBy    string        `json:"closed_by,omitempty"`
	MergedSetNo int64         `json:"merged_set_no,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	Version     int64         `json:"version"`
}

// Problem is one thing a check found in a merged set.
type Problem struct {
	ItemID  string `json:"item_id,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CheckFunc judges the whole set a merge would make. It gets every item with its content; a problem stops the merge.
type CheckFunc func(ctx context.Context, merged []ResolvedItem) []Problem
