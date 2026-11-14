package definitionscheck

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ysk6506/praxium/backend/internal/definitions"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// TB is the small part of *testing.T the suite uses.
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	Errorf(format string, args ...any)
	Logf(format string, args ...any)
	Cleanup(func())
}

// Subject is a store under test with the means to break it and to start it again.
type Subject struct {
	Store definitions.Store
	// FailWrites makes the writes that would keep the content fail just before the new content replaces the old one
	// (the rename of the file, for the file store), until it is called again with false.
	FailWrites func(on bool)
	// Reopen makes a store from what was kept, as a restart of the program does. A write that was not kept is not in it.
	Reopen func() (definitions.Store, error)
}

// Factory returns a new, empty subject. The suite calls it once per case, and twice in a case that needs two stores.
type Factory func(tb TB) Subject

// Case is one case of the suite. A store's test runs each as a subtest:
//
//	for _, c := range definitionscheck.Cases() {
//		t.Run(c.Name, func(t *testing.T) { c.Run(t, factory) })
//	}
type Case struct {
	Name string
	Run  func(tb TB, newSubject Factory)
}

// Cases returns the whole suite in a fixed order.
func Cases() []Case {
	return []Case{
		{"a head that moved refuses the write and the second writer loses", headMoved},
		{"the same content written twice is one change", sameContentTwice},
		{"nothing is written when nothing changes", nothingWritten},
		{"the ledger stays raised after a discarded write and never goes down", ledgerKeepsItsHeight},
		{"the hash of a set does not depend on the order of the items or on the parent", hashIsDeterministic},
		{"a write that fails halfway leaves nothing behind", noPartialWrite},
		{"a branch that overlaps the main line is stale and one that does not is merged", staleAndDisjoint},
		{"an author cannot approve their own proposal unless the policy allows it", selfApproval},
		{"a proposal moves only on the allowed paths", forbiddenTransitions},
		{"two editors of one branch cannot overwrite each other", branchVersions},
		{"a check that finds a problem stops the merge and changes nothing", checkStopsTheMerge},
		{"a restore makes a new set with the old content and never changes an old set", restoreMakesANewSet},
		{"the state is the same after the store is opened again", sameAfterRestart},
		{"the limits are kept and the error names the limit", limitsAreNamed},
	}
}

var (
	ctx   = context.Background()
	alice = definitions.Actor{ID: "alice"}
	bob   = definitions.Actor{ID: "bob"}
)

func codeIs(err error, want kernel.Code) bool { return err != nil && kernel.CodeOf(err) == want }

func newSvc(tb TB, st definitions.Store, limits ...definitions.Limits) *definitions.Service {
	tb.Helper()
	var l definitions.Limits
	if len(limits) > 0 {
		l = limits[0]
	}
	svc, err := definitions.New(definitions.Config{Store: st, Clock: kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)),
		IDs: kernel.NewSequentialIDs(), Limits: l})
	need(tb, err == nil, "service: %v", err)
	return svc
}

// need fails the case unless ok. The checks of the cases go through it, so a check is one call and the line that fails
// is one place.
func need(tb TB, ok bool, format string, args ...any) {
	tb.Helper()
	if !ok {
		tb.Fatalf(format, args...)
	}
}

// item is a change that makes (or, by its ID, changes) an item of kind "k" named after its ID.
func item(id, body string) definitions.Change {
	return definitions.Change{ItemID: id, Kind: "k", Name: id, SchemaVersion: 1, Body: json.RawMessage(body)}
}

func apply(tb TB, svc *definitions.Service, expect int64, changes ...definitions.Change) definitions.ApplyResult {
	tb.Helper()
	res, err := svc.Apply(ctx, definitions.ApplyInput{ExpectHead: expect, Changes: changes, Actor: alice, Message: "m"})
	need(tb, err == nil, "apply: %v", err)
	return res
}

func propose(tb TB, svc *definitions.Service, name string, changes ...definitions.Change) (definitions.Branch, definitions.Proposal) {
	tb.Helper()
	b, p, err := svc.CreateBranch(ctx, definitions.CreateBranchInput{Name: name, Actor: alice})
	need(tb, err == nil, "create branch: %v", err)
	b, p, err = svc.EditBranch(ctx, definitions.EditBranchInput{BranchID: b.ID, ExpectVersion: b.Version, Changes: changes, Actor: alice})
	need(tb, err == nil, "edit branch: %v", err)
	return b, p
}

func approved(tb TB, svc *definitions.Service, p definitions.Proposal) definitions.Proposal {
	tb.Helper()
	p, err := svc.RequestReview(ctx, p.ID, alice)
	need(tb, err == nil, "review: %v", err)
	p, err = svc.Approve(ctx, definitions.ApproveInput{ProposalID: p.ID, ContentHash: p.ContentHash, Actor: bob})
	need(tb, err == nil, "approve: %v", err)
	return p
}

func head(tb TB, svc *definitions.Service) int64 {
	tb.Helper()
	set, _, err := svc.Head(ctx)
	need(tb, err == nil, "head: %v", err)
	return set.No
}

func number(tb TB, svc *definitions.Service, name string) int64 {
	tb.Helper()
	n, err := svc.NextNumber(ctx, name, 1)
	need(tb, err == nil, "next number: %v", err)
	return n
}
