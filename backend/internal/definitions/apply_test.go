package definitions

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-332] [REQ-333] [REQ-334]
func TestApplyMakesTheFirstSetAndTheNextOneOnlyForNewContent(t *testing.T) {
	r := newRig(t)
	res := r.apply(0, put("source", "s1", `{"b":1,"a":2}`), put("feed", "f1", `{"x":true}`))
	if !res.Changed || res.Set.No != 1 || res.Set.ParentNo != 0 || len(res.Set.Entries) != 2 || res.Set.Origin != OriginChange {
		t.Fatalf("first set: %+v", res.Set)
	}
	id := r.idOf(res.Set, "s1")
	again := r.apply(1, putID(id, `{"a":2,"b":1}`))
	if again.Changed || again.Set.No != 1 {
		t.Fatalf("the same content in another order must change nothing: %+v", again)
	}
	if head, ok, _ := r.svc.Head(ctx); !ok || head.No != 1 {
		t.Fatalf("head: %+v %v", head, ok)
	}
	// An item that is named by ID gets a new revision when its content is new, and the set follows.
	next := r.apply(1, putID(id, `{"a":3}`))
	if !next.Changed || next.Set.No != 2 || next.Set.ParentNo != 1 || next.Set.Hash == res.Set.Hash {
		t.Fatalf("second set: %+v", next.Set)
	}
	revs, err := r.svc.ItemHistory(ctx, id)
	if err != nil || len(revs) != 2 || revs[1].RevNo != 2 || revs[0].Message != "m" {
		t.Fatalf("history: %+v %v", revs, err)
	}
	history, _ := r.svc.History(ctx, 0, 0)
	if len(history) != 2 || history[0].No != 2 || history[0].Entries != nil {
		t.Fatalf("history of sets: %+v", history)
	}
	older, _ := r.svc.History(ctx, 2, 5)
	if len(older) != 1 || older[0].No != 1 {
		t.Fatalf("history before D-2: %+v", older)
	}
}

// [REQ-333]
func TestAnEmptyStartKeepsTheBaselineAsTheHash(t *testing.T) {
	r := newRig(t)
	if res, err := r.svc.Apply(ctx, ApplyInput{Baseline: "std-1", Actor: alice}); err != nil || res.Changed {
		t.Fatalf("an apply of nothing on nothing: %+v %v", res, err)
	}
	r.apply(0, put("k", "n", `{}`))
	res, err := r.svc.Apply(ctx, ApplyInput{ExpectHead: 1, Baseline: "std-2", Actor: alice})
	if err != nil || !res.Changed || res.Set.Baseline != "std-2" || len(res.Set.Entries) != 1 {
		t.Fatalf("a new baseline makes a new set: %+v %v", res, err)
	}
	head, _, _ := r.svc.Head(ctx)
	kept, err := r.svc.Apply(ctx, ApplyInput{ExpectHead: 2, Actor: alice, Changes: []Change{{ItemID: head.Entries[0].ItemID, Remove: true}}})
	if err != nil || kept.Set.Baseline != "std-2" || kept.Set.Hash != "std-2" || len(kept.Set.Entries) != 0 {
		t.Fatalf("an empty set has the baseline as its hash and keeps it: %+v %v", kept.Set, err)
	}
}

// [REQ-334]
func TestApplyIsRefusedForAnotherHeadAndLeavesNothing(t *testing.T) {
	r := newRig(t)
	r.apply(0, put("k", "a", `{}`))
	_, err := r.svc.Apply(ctx, ApplyInput{ExpectHead: 0, Actor: alice, Changes: []Change{put("k", "b", `{}`)}})
	wantCode(t, err, kernel.CodeConflict)
	if head, _, _ := r.svc.Head(ctx); head.No != 1 {
		t.Fatalf("the head moved: %d", head.No)
	}
	if items, _ := r.store.Items(ctx, []string{"item-000002"}); len(items) != 0 {
		t.Fatalf("a refused apply left an item: %v", items)
	}
}

// [REQ-332] [REQ-334]
func TestApplyRefusesWhatIsNotSound(t *testing.T) {
	r := newRig(t, Limits{MaxBodyBytes: 20, MaxItemsPerSet: 2, MaxNameLen: 10, MaxMessageLen: 5})
	first := r.apply(0, put("k", "a", `{}`))
	id := first.Set.Entries[0].ItemID
	cases := map[string]struct {
		in   ApplyInput
		want kernel.Code
	}{
		"no actor":         {ApplyInput{ExpectHead: 1, Changes: []Change{put("k", "b", `{}`)}}, kernel.CodeInvalidValue},
		"bad origin":       {ApplyInput{ExpectHead: 1, Actor: alice, Origin: OriginRestore}, kernel.CodeInvalidValue},
		"long message":     {ApplyInput{ExpectHead: 1, Actor: alice, Message: "123456"}, kernel.CodeInvalidValue},
		"body too big":     {ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{put("k", "b", `{"a":"aaaaaaaaaaaaaaaaaaaaaaaa"}`)}}, kernel.CodeInvalidValue},
		"too many items":   {ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{put("k", "b", `{}`), put("k", "c", `{}`)}}, kernel.CodeInvalidValue},
		"name too long":    {ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{put("k", "01234567890", `{}`)}}, kernel.CodeInvalidValue},
		"kind too long":    {ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{put("01234567890", "b", `{}`)}}, kernel.CodeInvalidValue},
		"no schema":        {ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{{Kind: "k", Name: "b", Body: []byte(`{}`)}}}, kernel.CodeInvalidValue},
		"bad body":         {ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{put("k", "b", `[]`)}}, kernel.CodeInvalidValue},
		"new without kind": {ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{putID("brand-new", `{}`)}}, kernel.CodeInvalidValue},
		"another kind":     {ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{{ItemID: id, Kind: "other", SchemaVersion: 1, Body: []byte(`{}`)}}}, kernel.CodeInvalidValue},
		"a rename":         {ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{{ItemID: id, Name: "z", SchemaVersion: 1, Body: []byte(`{}`)}}}, kernel.CodeInvalidValue},
		"remove unknown":   {ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{{ItemID: "nobody", Remove: true}}}, kernel.CodeNotFound},
		"remove no ID":     {ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{{Remove: true}}}, kernel.CodeInvalidValue},
		"bad ID":           {ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{putID("no good", `{}`)}}, kernel.CodeInvalidValue},
		"item twice":       {ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{putID(id, `{"a":1}`), putID(id, `{"a":2}`)}}, kernel.CodeInvalidValue},
		"counter down":     {ApplyInput{ExpectHead: 1, Actor: alice, Counters: map[string]int64{"c": 0}, Changes: []Change{put("k", "b", `{}`)}}, kernel.CodeInvalidValue},
	}
	for name, c := range cases {
		if _, err := r.svc.Apply(ctx, c.in); code(err) != c.want {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := r.svc.Apply(ctx, ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{put("k", "b", `{"a":"aaaaaaaaaaaaaaaaaaaaaaaa"}`)}}); err != nil {
		if le, ok := isErr[*LimitError](err); !ok || le.Limit != "max_body_bytes" || le.Max != 20 || !strings.Contains(err.Error(), "max_body_bytes") {
			t.Fatalf("the error must name the limit: %v", err)
		}
	}
	if head, _, _ := r.svc.Head(ctx); head.No != 1 {
		t.Fatalf("a refused apply moved the head to D-%d", head.No)
	}
}

// [REQ-332]
func TestARemovedItemComesBackWithANewRevisionNumber(t *testing.T) {
	r := newRig(t)
	set := r.apply(0, put("k", "a", `{"v":1}`), put("k", "b", `{"v":2}`)).Set
	id := r.idOf(set, "a")
	gone, err := r.svc.Apply(ctx, ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{{ItemID: id, Remove: true}}})
	if err != nil || len(gone.Set.Entries) != 1 {
		t.Fatalf("remove: %+v %v", gone, err)
	}
	back := r.apply(2, putID(id, `{"v":1}`))
	revs, _ := r.svc.ItemHistory(ctx, id)
	if len(back.Set.Entries) != 2 || len(revs) != 2 || revs[1].RevNo != 2 || revs[0].ContentHash != revs[1].ContentHash {
		t.Fatalf("the item came back as %+v with revisions %+v", back.Set.Entries, revs)
	}
	if it, err := r.svc.Item(ctx, id); err != nil || it.LastRev != 2 || it.Kind != "k" {
		t.Fatalf("item: %+v %v", it, err)
	}
	if _, err := r.svc.Item(ctx, "nobody"); code(err) != kernel.CodeNotFound {
		t.Fatalf("a missing item: %v", err)
	}
	if _, err := r.svc.ItemHistory(ctx, "nobody"); code(err) != kernel.CodeNotFound {
		t.Fatalf("a missing history: %v", err)
	}
}

// [REQ-335]
func TestTheLedgerRisesWithTheSetAndNeverFalls(t *testing.T) {
	r := newRig(t)
	if n, err := r.svc.NextNumber(ctx, "attr", 100000); err != nil || n != 100000 {
		t.Fatalf("a counter that is not there starts at the start: %d %v", n, err)
	}
	if _, err := r.svc.NextNumber(ctx, "", 1); code(err) != kernel.CodeInvalidValue {
		t.Fatalf("a nameless counter: %v", err)
	}
	if _, err := r.svc.NextNumber(ctx, "attr", 0); code(err) != kernel.CodeInvalidValue {
		t.Fatalf("a start of 0: %v", err)
	}
	if _, err := r.svc.Apply(ctx, ApplyInput{Actor: alice, Counters: map[string]int64{"attr": 100002}, Changes: []Change{put("k", "a", `{"n":100000}`)}}); err != nil {
		t.Fatal(err)
	}
	if n, _ := r.svc.NextNumber(ctx, "attr", 100000); n != 100002 {
		t.Fatalf("the ledger after the write: %d", n)
	}
	// A write that is discarded does not move it, and no write lowers it.
	_, err := r.svc.Apply(ctx, ApplyInput{ExpectHead: 0, Actor: alice, Counters: map[string]int64{"attr": 100009}, Changes: []Change{put("k", "b", `{}`)}})
	wantCode(t, err, kernel.CodeConflict)
	_, err = r.svc.Apply(ctx, ApplyInput{ExpectHead: 1, Actor: alice, Counters: map[string]int64{"attr": 100001}, Changes: []Change{put("k", "b", `{}`)}})
	wantCode(t, err, kernel.CodeInvalidValue)
	if n, _ := r.svc.NextNumber(ctx, "attr", 100000); n != 100002 {
		t.Fatalf("the ledger after discarded writes: %d", n)
	}
	// An apply that changes nothing writes nothing, not even the ledger.
	if res, _ := r.svc.Apply(ctx, ApplyInput{ExpectHead: 1, Actor: alice, Counters: map[string]int64{"attr": 100050}}); res.Changed {
		t.Fatal("nothing to change")
	}
	if n, _ := r.svc.NextNumber(ctx, "attr", 1); n != 100002 {
		t.Fatalf("the ledger after an apply of nothing: %d", n)
	}
}

// [REQ-340]
func TestNewServiceNeedsItsParts(t *testing.T) {
	if _, err := New(Config{}); code(err) != kernel.CodeInvalidValue {
		t.Fatalf("an empty config: %v", err)
	}
	l := Limits{}.withDefaults()
	if l.MaxBodyBytes != DefaultMaxBodyBytes || l.MaxItemsPerSet != DefaultMaxItemsPerSet || l.MaxBranchEntries != DefaultMaxBranchEntries ||
		l.MaxOpenBranches != DefaultMaxOpenBranches || l.MaxNameLen != DefaultMaxNameLen || l.MaxMessageLen != DefaultMaxMessageLen {
		t.Fatalf("defaults: %+v", l)
	}
}

// [REQ-332]
func TestIDsFromAGeneratorThatStartedAgainSkipTheOnesInUse(t *testing.T) {
	store := NewMemoryStore()
	first := newRigOn(t, store)
	set := first.apply(0, put("k", "a", `{}`)).Set
	second := newRigOn(t, store) // a restart: its generator starts at 1 again
	res, err := second.svc.Apply(ctx, ApplyInput{ExpectHead: 1, Actor: alice, Changes: []Change{put("k", "b", `{}`)}})
	if err != nil || res.Set.Entries[1].ItemID == set.Entries[0].ItemID {
		t.Fatalf("an ID in use was issued again: %+v %v", res.Set.Entries, err)
	}
}
