package definitions

import "context"

// HeadMove moves the head of the main line from the set it is at to a new one. It happens only when the head is at
// Expect (0 means that there is no head yet).
type HeadMove struct {
	Expect int64
	New    int64
}

// CounterWrite raises a counter of the ledger from the value the caller read (0 for a counter that is not there) to a
// new value. A counter never goes down, and its values start at 1.
type CounterWrite struct {
	Name   string
	Expect int64
	Value  int64
}

// BranchWrite puts a branch. Expect is the version the caller read, or 0 for a branch that must not exist yet; the
// store stores the branch with the version after it.
type BranchWrite struct {
	Branch Branch
	Expect int64
}

// ProposalWrite puts a proposal, with the same rule of versions as BranchWrite.
type ProposalWrite struct {
	Proposal Proposal
	Expect   int64
}

// Write is one atomic step of the store: everything in it happens or nothing does, and no reader sees half of it. A
// database adapter makes it one transaction.
//
// The store keeps the rules that make the data sound, whoever asks: new items and revisions must not exist yet and
// revisions must follow each other without a gap; a set must be the next number, must hold revisions that exist and
// must have the hash of its baseline and entries; the head moves only from the value expected, and only with the set
// that makes the new head; a counter never goes down; a branch or proposal changes only from the version read.
type Write struct {
	Head      *HeadMove
	Items     []Item
	Revisions []Revision
	Set       *Set
	Counters  []CounterWrite
	Branches  []BranchWrite
	Proposals []ProposalWrite
}

// RevRef names one revision.
type RevRef struct {
	ItemID string
	RevNo  int64
}

// Store is the port of the definitions. Reads give copies. An error carries kernel.CodeNotFound (something that is not
// there) or kernel.CodeConflict (a write whose expectation is not met; nothing was written); anything else is a
// failure of the store itself.
type Store interface {
	// Head returns the number of the set the main line is at, or 0 when it has none.
	Head(ctx context.Context) (int64, error)
	// Set returns a definition set with its entries, or a not_found error.
	Set(ctx context.Context, no int64) (Set, error)
	// Sets returns sets without entries, newest first: those below the number before (0 for all), at most limit.
	Sets(ctx context.Context, before int64, limit int) ([]Set, error)
	// Items returns the known items among the IDs; an ID that is not known is not in the map.
	Items(ctx context.Context, ids []string) (map[string]Item, error)
	// ItemRevisions returns every revision of an item, oldest first, or a not_found error.
	ItemRevisions(ctx context.Context, itemID string) ([]Revision, error)
	// Revisions returns the revisions asked for, in that order, or a not_found error.
	Revisions(ctx context.Context, refs []RevRef) ([]Revision, error)
	// Counter returns the value of a counter of the ledger, 0 when it is not there.
	Counter(ctx context.Context, name string) (int64, error)
	// Branch returns a branch, or a not_found error.
	Branch(ctx context.Context, id string) (Branch, error)
	// Branches returns the branches in the states, ordered by ID; no state means every one.
	Branches(ctx context.Context, states []BranchState) ([]Branch, error)
	// Proposal returns a proposal, or a not_found error.
	Proposal(ctx context.Context, id string) (Proposal, error)
	// Proposals returns the proposals in the states, ordered by ID; no state means every one.
	Proposals(ctx context.Context, states []ProposalState) ([]Proposal, error)
	// Commit makes one atomic write.
	Commit(ctx context.Context, w Write) error
}
