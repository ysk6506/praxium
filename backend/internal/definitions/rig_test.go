package definitions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

var ctx = context.Background()

var alice = Actor{ID: "alice"}
var bob = Actor{ID: "bob"}

// rig is a service on a memory store with a clock and IDs that only move when told to.
type rig struct {
	t     *testing.T
	svc   *Service
	store *MemoryStore
	clock *kernel.ManualClock
}

func newRig(t *testing.T, limits ...Limits) *rig {
	t.Helper()
	store := NewMemoryStore()
	return newRigOn(t, store, limits...)
}

func newRigOn(t *testing.T, store Store, limits ...Limits) *rig {
	t.Helper()
	var l Limits
	if len(limits) > 0 {
		l = limits[0]
	}
	clock := kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	svc, err := New(Config{Store: store, Clock: clock, IDs: kernel.NewSequentialIDs(), Limits: l})
	if err != nil {
		t.Fatal(err)
	}
	ms, _ := store.(*MemoryStore)
	return &rig{t: t, svc: svc, store: ms, clock: clock}
}

func put(kind, name, body string) Change {
	return Change{Kind: kind, Name: name, SchemaVersion: 1, Body: json.RawMessage(body)}
}

func putID(id, body string) Change {
	return Change{ItemID: id, SchemaVersion: 1, Body: json.RawMessage(body)}
}

func code(err error) kernel.Code { return kernel.CodeOf(err) }

func wantCode(t *testing.T, err error, want kernel.Code) {
	t.Helper()
	if err == nil || code(err) != want {
		t.Fatalf("want an error with code %s, got %v", want, err)
	}
}

func (r *rig) apply(expect int64, changes ...Change) ApplyResult {
	r.t.Helper()
	res, err := r.svc.Apply(ctx, ApplyInput{ExpectHead: expect, Changes: changes, Actor: alice, Message: "m"})
	if err != nil {
		r.t.Fatalf("apply: %v", err)
	}
	return res
}

// items returns the item IDs of a set made by an apply of new items, in the order of the changes' kinds.
func (r *rig) idOf(set Set, name string) string {
	r.t.Helper()
	items, err := r.svc.SetItems(ctx, set.No)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, it := range items {
		if it.Name == name {
			return it.ItemID
		}
	}
	r.t.Fatalf("no item named %q in D-%d", name, set.No)
	return ""
}

func (r *rig) propose(base string, changes ...Change) (Branch, Proposal) {
	r.t.Helper()
	b, p, err := r.svc.CreateBranch(ctx, CreateBranchInput{Name: base, Actor: alice})
	if err != nil {
		r.t.Fatalf("create branch: %v", err)
	}
	if len(changes) > 0 {
		if b, p, err = r.svc.EditBranch(ctx, EditBranchInput{BranchID: b.ID, ExpectVersion: b.Version, Changes: changes, Actor: alice}); err != nil {
			r.t.Fatalf("edit branch: %v", err)
		}
	}
	return b, p
}

func (r *rig) approve(p Proposal) Proposal {
	r.t.Helper()
	p, err := r.svc.RequestReview(ctx, p.ID, alice)
	if err != nil {
		r.t.Fatalf("review: %v", err)
	}
	if p, err = r.svc.Approve(ctx, ApproveInput{ProposalID: p.ID, ContentHash: p.ContentHash, Actor: bob}); err != nil {
		r.t.Fatalf("approve: %v", err)
	}
	return p
}

func isErr[T error](err error) (T, bool) {
	var target T
	ok := errors.As(err, &target)
	return target, ok
}
