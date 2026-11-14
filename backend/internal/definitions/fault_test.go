package definitions

import (
	"context"
	"errors"
	"testing"
)

var errFault = errors.New("the store failed")

// faulty is a store that fails the calls of one method once it is armed.
type faulty struct {
	Store
	method string
	armed  bool
}

func (f *faulty) hit(m string) error {
	if f.armed && f.method == m {
		return errFault
	}
	return nil
}

func (f *faulty) Head(c context.Context) (int64, error) {
	if err := f.hit("Head"); err != nil {
		return 0, err
	}
	return f.Store.Head(c)
}

func (f *faulty) Set(c context.Context, no int64) (Set, error) {
	if err := f.hit("Set"); err != nil {
		return Set{}, err
	}
	return f.Store.Set(c, no)
}

func (f *faulty) Sets(c context.Context, before int64, limit int) ([]Set, error) {
	if err := f.hit("Sets"); err != nil {
		return nil, err
	}
	return f.Store.Sets(c, before, limit)
}

func (f *faulty) Items(c context.Context, ids []string) (map[string]Item, error) {
	if err := f.hit("Items"); err != nil {
		return nil, err
	}
	return f.Store.Items(c, ids)
}

func (f *faulty) ItemRevisions(c context.Context, id string) ([]Revision, error) {
	if err := f.hit("ItemRevisions"); err != nil {
		return nil, err
	}
	return f.Store.ItemRevisions(c, id)
}

func (f *faulty) Revisions(c context.Context, refs []RevRef) ([]Revision, error) {
	if err := f.hit("Revisions"); err != nil {
		return nil, err
	}
	return f.Store.Revisions(c, refs)
}

func (f *faulty) Counter(c context.Context, name string) (int64, error) {
	if err := f.hit("Counter"); err != nil {
		return 0, err
	}
	return f.Store.Counter(c, name)
}

func (f *faulty) Branch(c context.Context, id string) (Branch, error) {
	if err := f.hit("Branch"); err != nil {
		return Branch{}, err
	}
	return f.Store.Branch(c, id)
}

func (f *faulty) Branches(c context.Context, s []BranchState) ([]Branch, error) {
	if err := f.hit("Branches"); err != nil {
		return nil, err
	}
	return f.Store.Branches(c, s)
}

func (f *faulty) Proposal(c context.Context, id string) (Proposal, error) {
	if err := f.hit("Proposal"); err != nil {
		return Proposal{}, err
	}
	return f.Store.Proposal(c, id)
}

func (f *faulty) Commit(c context.Context, w Write) error {
	if err := f.hit("Commit"); err != nil {
		return err
	}
	return f.Store.Commit(c, w)
}

type faultRig struct {
	*rig
	f          *faulty
	a          string
	approved   Proposal
	inReview   Proposal
	branchOpen string
}

func newFaultRig(t *testing.T, method string) *faultRig {
	f := &faulty{Store: NewMemoryStore(), method: method}
	r := newRigOn(t, f)
	set := r.apply(0, put("k", "a", `{"v":1}`), put("k", "b", `{"v":2}`)).Set
	a := set.Entries[0].ItemID
	_, p := r.propose("work", putID(a, `{"v":5}`))
	br, q := r.propose("review", putID(set.Entries[1].ItemID, `{"v":6}`))
	q, err := r.svc.RequestReview(ctx, q.ID, alice)
	if err != nil {
		t.Fatal(err)
	}
	return &faultRig{rig: r, f: f, a: a, approved: r.approve(p), inReview: q, branchOpen: br.ID}
}

// [REQ-340]
func TestAFailingStoreIsReportedByEveryOperationAndNothingIsHalfDone(t *testing.T) {
	methods := []string{"Head", "Set", "Sets", "Items", "ItemRevisions", "Revisions", "Counter", "Branch", "Branches", "Proposal", "Commit"}
	ops := map[string]func(x *faultRig) error{
		"apply": func(x *faultRig) error {
			_, err := x.svc.Apply(ctx, ApplyInput{ExpectHead: 1, Actor: alice, Counters: map[string]int64{"n": 3}, Changes: []Change{put("k", "z", `{}`)}})
			return err
		},
		"create": func(x *faultRig) error {
			_, _, err := x.svc.CreateBranch(ctx, CreateBranchInput{Name: "x", Actor: alice})
			return err
		},
		"edit": func(x *faultRig) error {
			_, _, err := x.svc.EditBranch(ctx, EditBranchInput{BranchID: x.branchOpen, ExpectVersion: 2, Actor: alice, Changes: []Change{putID(x.a, `{"v":7}`), {ItemID: "item-000002", Remove: true}}})
			return err
		},
		"approve": func(x *faultRig) error {
			_, err := x.svc.Approve(ctx, ApproveInput{ProposalID: x.inReview.ID, ContentHash: x.inReview.ContentHash, Actor: bob})
			return err
		},
		"close": func(x *faultRig) error { _, err := x.svc.Close(ctx, x.inReview.ID, alice, CloseAbandoned); return err },
		"merge": func(x *faultRig) error {
			_, err := x.svc.Merge(ctx, MergeInput{ProposalID: x.approved.ID, Actor: bob, Check: func(context.Context, []ResolvedItem) []Problem { return nil }})
			return err
		},
		"restore": func(x *faultRig) error {
			if _, err := x.svc.Apply(ctx, ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{putID(x.a, `{"v":9}`)}}); err != nil {
				return err
			}
			_, _, err := x.svc.Restore(ctx, RestoreInput{SetNo: 1, Actor: alice})
			return err
		},
		"draft":     func(x *faultRig) error { _, err := x.svc.Draft(ctx, x.branchOpen); return err },
		"items":     func(x *faultRig) error { _, err := x.svc.SetItems(ctx, 1); return err },
		"head":      func(x *faultRig) error { _, _, err := x.svc.Head(ctx); return err },
		"history":   func(x *faultRig) error { _, err := x.svc.History(ctx, 0, 0); return err },
		"item":      func(x *faultRig) error { _, err := x.svc.Item(ctx, x.a); return err },
		"itemhis":   func(x *faultRig) error { _, err := x.svc.ItemHistory(ctx, x.a); return err },
		"number":    func(x *faultRig) error { _, err := x.svc.NextNumber(ctx, "n", 1); return err },
		"branches":  func(x *faultRig) error { _, err := x.svc.Branches(ctx); return err },
		"proposals": func(x *faultRig) error { _, err := x.svc.Proposals(ctx); return err },
	}
	failed := 0
	for _, method := range methods {
		for name, op := range ops {
			x := newFaultRig(t, method)
			headBefore, _, _ := x.svc.Head(ctx)
			x.f.armed = true
			err := op(x)
			x.f.armed = false
			if err != nil {
				if !errors.Is(err, errFault) {
					t.Errorf("%s with %s failing: %v", name, method, err)
				}
				failed++
			}
			if head, _, _ := x.svc.Head(ctx); name != "merge" && name != "apply" && name != "restore" && head.No != headBefore.No {
				t.Errorf("%s with %s failing moved the head to D-%d", name, method, head.No)
			}
		}
	}
	if failed < 40 {
		t.Fatalf("only %d operations met a failing store", failed)
	}
}
