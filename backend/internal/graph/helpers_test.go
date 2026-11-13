package graph

import (
	"context"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// unit is an applier on the live store of these tests.
type unit struct {
	t     *testing.T
	ctx   context.Context
	fx    *Fixture
	store *liveStore
	a     *Applier
	kg    KG
	lease Lease
}

func newUnit(t *testing.T, reg *model.Registry, opts Options) *unit {
	t.Helper()
	fx, err := NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	if reg != nil {
		fx.Reg = reg
	}
	u := &unit{t: t, ctx: context.Background(), fx: fx, store: newLiveStore()}
	clock := kernel.NewManualClock(epoch)
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(3))
	if u.kg, err = NewTestKG(u.ctx, u.store, fx.Reg, ids, clock, KGSpec{Name: "main-kg", Role: RoleProduction}); err != nil {
		t.Fatal(err)
	}
	if opts.Semantic == 0 {
		opts.Semantic = 1
	}
	if u.a, err = NewApplier(u.store, fx.Reg, NewTestKeyProvider("unit"), clock, ids, opts); err != nil {
		t.Fatal(err)
	}
	if u.lease, err = u.a.Acquire(u.ctx, u.kg.ID); err != nil {
		t.Fatal(err)
	}
	return u
}

func (u *unit) apply(b Batch) Result {
	u.t.Helper()
	res, err := u.a.Apply(u.ctx, u.kg.ID, u.lease, b)
	if err != nil {
		u.t.Fatalf("apply: %v", err)
	}
	return res
}

func (u *unit) seal() SealResult {
	u.t.Helper()
	r, err := u.a.Seal(u.ctx, u.kg.ID, u.lease, SealSync)
	if err != nil {
		u.t.Fatalf("seal: %v", err)
	}
	return r
}

func mustFixture(t *testing.T) *Fixture {
	t.Helper()
	fx, err := NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	return fx
}

func wantCode(t *testing.T, what string, err error, code kernel.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: no error, wanted %s", what, code)
	}
	if got := kernel.CodeOf(err); got != code {
		t.Fatalf("%s: code %s (%v), wanted %s", what, got, err, code)
	}
}

var epoch = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
