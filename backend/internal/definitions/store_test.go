package definitions

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func rev(id string, no int64, kind, body string) Revision {
	b, _ := NormalizeBody([]byte(body))
	h, _ := RevisionHash(kind, 1, b)
	return Revision{ItemID: id, RevNo: no, ContentHash: h, SchemaVersion: 1, Body: b}
}

func firstSet(revs ...Revision) *Set {
	var entries []SetEntry
	for _, r := range revs {
		entries = append(entries, SetEntry{ItemID: r.ItemID, RevNo: r.RevNo, ContentHash: r.ContentHash})
	}
	h, _ := SetHash("", entries)
	return &Set{No: 1, Hash: h, Entries: entries}
}

// [REQ-340]
func TestTheStoreRefusesAWriteThatBreaksItsRulesAndKeepsWhatItHad(t *testing.T) {
	s := NewMemoryStore()
	r1 := rev("a", 1, "k", `{"x":1}`)
	good := Write{Items: []Item{{ID: "a", Kind: "k", Name: "a"}}, Revisions: []Revision{r1}, Set: firstSet(r1), Head: &HeadMove{New: 1}}
	if err := s.Commit(ctx, good); err != nil {
		t.Fatal(err)
	}
	before := s.Export()
	r2 := rev("a", 2, "k", `{"x":2}`)
	cases := map[string]struct {
		w    Write
		want kernel.Code
	}{
		"stale head":         {Write{Head: &HeadMove{Expect: 0, New: 1}}, kernel.CodeConflict},
		"item twice":         {Write{Items: []Item{{ID: "a", Kind: "k"}}}, kernel.CodeConflict},
		"item without kind":  {Write{Items: []Item{{ID: "z"}}}, kernel.CodeInvalidValue},
		"unknown item":       {Write{Revisions: []Revision{rev("zz", 1, "k", `{}`)}}, kernel.CodeNotFound},
		"revision gap":       {Write{Revisions: []Revision{rev("a", 3, "k", `{}`)}}, kernel.CodeConflict},
		"revision twice":     {Write{Revisions: []Revision{rev("a", 1, "k", `{}`)}}, kernel.CodeConflict},
		"wrong hash":         {Write{Revisions: []Revision{{ItemID: "a", RevNo: 2, ContentHash: "x", SchemaVersion: 1, Body: []byte(`{}`)}}}, kernel.CodeInvalidValue},
		"head without a set": {Write{Head: &HeadMove{Expect: 1, New: 2}}, kernel.CodeInvalidValue},
		"set without a head": {Write{Revisions: []Revision{r2}, Set: &Set{No: 2}}, kernel.CodeInvalidValue},
		"set out of place":   {Write{Revisions: []Revision{r2}, Set: &Set{No: 3}, Head: &HeadMove{Expect: 1, New: 3}}, kernel.CodeInvalidValue},
		"set with a wrong hash": {Write{Revisions: []Revision{r2}, Head: &HeadMove{Expect: 1, New: 2},
			Set: &Set{No: 2, Hash: "x", Entries: []SetEntry{{ItemID: "a", RevNo: 2, ContentHash: r2.ContentHash}}}}, kernel.CodeInvalidValue},
		"set with a missing revision": {Write{Head: &HeadMove{Expect: 1, New: 2},
			Set: &Set{No: 2, Hash: "x", Entries: []SetEntry{{ItemID: "a", RevNo: 5, ContentHash: "h"}}}}, kernel.CodeInvalidValue},
		"unsorted entries": {Write{Head: &HeadMove{Expect: 1, New: 2},
			Set: &Set{No: 2, Hash: "x", Entries: []SetEntry{{ItemID: "b"}, {ItemID: "a"}}}}, kernel.CodeInvalidValue},
		"counter without a name": {Write{Counters: []CounterWrite{{Value: 1}}}, kernel.CodeInvalidValue},
		"counter from 0":         {Write{Counters: []CounterWrite{{Name: "c", Value: 0}}}, kernel.CodeInvalidValue},
		"counter read wrongly":   {Write{Counters: []CounterWrite{{Name: "c", Expect: 4, Value: 5}}}, kernel.CodeConflict},
		"branch exists":          {Write{Branches: []BranchWrite{{Branch: Branch{ID: "b1"}, Expect: 3}}}, kernel.CodeConflict},
		"branch without an ID":   {Write{Branches: []BranchWrite{{}}}, kernel.CodeConflict},
		"proposal exists":        {Write{Proposals: []ProposalWrite{{Proposal: Proposal{ID: "p1"}, Expect: 3}}}, kernel.CodeConflict},
		"proposal without an ID": {Write{Proposals: []ProposalWrite{{}}}, kernel.CodeConflict},
	}
	for name, c := range cases {
		if err := s.Commit(ctx, c.w); code(err) != c.want {
			t.Errorf("%s: %v", name, err)
		}
	}
	after, _ := json.Marshal(s.Export())
	was, _ := json.Marshal(before)
	if string(after) != string(was) {
		t.Fatalf("a refused write changed the store:\n%s\n%s", was, after)
	}
}

// [REQ-335] [REQ-340]
func TestTheStoreKeepsTheLedgerAndVersionsOfBranchesAndProposals(t *testing.T) {
	s := NewMemoryStore()
	w := Write{Counters: []CounterWrite{{Name: "c", Value: 5}},
		Branches:  []BranchWrite{{Branch: Branch{ID: "b1", Version: 99}}},
		Proposals: []ProposalWrite{{Proposal: Proposal{ID: "p1", BranchID: "b1", Version: 99}}}}
	if err := s.Commit(ctx, w); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Counter(ctx, "c"); v != 5 {
		t.Fatalf("counter %d", v)
	}
	if b, _ := s.Branch(ctx, "b1"); b.Version != 1 {
		t.Fatalf("a branch's version is the store's to set: %d", b.Version)
	}
	// A counter is raised from the value read, stays when it is written as it is, and does not go down.
	for _, c := range []struct {
		expect, value int64
		ok            bool
	}{{5, 5, true}, {5, 7, true}, {7, 6, false}, {5, 8, false}} {
		err := s.Commit(ctx, Write{Counters: []CounterWrite{{Name: "c", Expect: c.expect, Value: c.value}}})
		if (err == nil) != c.ok {
			t.Errorf("counter %d to %d: %v", c.expect, c.value, err)
		}
	}
	if v, _ := s.Counter(ctx, "c"); v != 7 {
		t.Fatalf("counter %d", v)
	}
	// All or nothing: the counter and the stale proposal are in one write.
	err := s.Commit(ctx, Write{Counters: []CounterWrite{{Name: "c", Expect: 7, Value: 9}}, Proposals: []ProposalWrite{{Proposal: Proposal{ID: "p1"}, Expect: 0}}})
	wantCode(t, err, kernel.CodeConflict)
	if v, _ := s.Counter(ctx, "c"); v != 7 {
		t.Fatalf("a write that failed raised the counter to %d", v)
	}
}

// [REQ-340]
func TestTheReadsOfTheStoreAreCopies(t *testing.T) {
	s := NewMemoryStore()
	r1 := rev("a", 1, "k", `{"x":1}`)
	if err := s.Commit(ctx, Write{Items: []Item{{ID: "a", Kind: "k"}}, Revisions: []Revision{r1}, Set: firstSet(r1), Head: &HeadMove{New: 1}}); err != nil {
		t.Fatal(err)
	}
	set, _ := s.Set(ctx, 1)
	set.Entries[0].ItemID = "zzz"
	revs, _ := s.Revisions(ctx, []RevRef{{ItemID: "a", RevNo: 1}})
	revs[0].Body[0] = 'X'
	all, _ := s.ItemRevisions(ctx, "a")
	all[0].Body[0] = 'Y'
	again, _ := s.Set(ctx, 1)
	body, _ := s.Revisions(ctx, []RevRef{{ItemID: "a", RevNo: 1}})
	if again.Entries[0].ItemID != "a" || string(body[0].Body) != `{"x":1}` {
		t.Fatalf("a caller changed the store: %+v %s", again, body[0].Body)
	}
	for name, err := range map[string]error{
		"set":      func() error { _, err := s.Set(ctx, 2); return err }(),
		"set zero": func() error { _, err := s.Set(ctx, 0); return err }(),
		"revision": func() error { _, err := s.Revisions(ctx, []RevRef{{ItemID: "a", RevNo: 2}}); return err }(),
		"branch":   func() error { _, err := s.Branch(ctx, "x"); return err }(),
		"proposal": func() error { _, err := s.Proposal(ctx, "x"); return err }(),
	} {
		if code(err) != kernel.CodeNotFound {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// [REQ-340]
func TestAStoreOpenedFromItsExportIsTheSameStore(t *testing.T) {
	r, a, _ := seeded(t)
	_, p := r.propose("work", putID(a, `{"v":10}`))
	p = r.approve(p)
	if _, err := r.svc.Merge(ctx, MergeInput{ProposalID: p.ID, Actor: bob, Counters: map[string]int64{"n": 3}}); err != nil {
		t.Fatal(err)
	}
	r.propose("open", putID(a, `{"v":11}`))
	data := r.store.Export()
	again, err := OpenMemoryStore(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	x, _ := json.Marshal(data)
	y, _ := json.Marshal(again.Export())
	if string(x) != string(y) {
		t.Fatalf("export of the reopened store differs:\n%s\n%s", x, y)
	}
}

// [REQ-340]
func TestDamagedDataIsRefusedWithAReason(t *testing.T) {
	r, a, _ := seeded(t)
	_, p := r.propose("work", putID(a, `{"v":10}`))
	_ = p
	damage := map[string]func(d *Data){
		"item without a kind":         func(d *Data) { d.Items[0].Kind = "" },
		"item twice":                  func(d *Data) { d.Items = append(d.Items, d.Items[0]) },
		"revision of nothing":         func(d *Data) { d.Revisions[0].ItemID = "ghost" },
		"revision out of order":       func(d *Data) { d.Revisions[0].RevNo = 4 },
		"body changed":                func(d *Data) { d.Revisions[0].Body = []byte(`{"v":99}`) },
		"revision count wrong":        func(d *Data) { d.Items[0].LastRev = 3 },
		"set out of place":            func(d *Data) { d.Sets[0].No = 2 },
		"entries out of order":        func(d *Data) { d.Sets[0].Entries[0], d.Sets[0].Entries[1] = d.Sets[0].Entries[1], d.Sets[0].Entries[0] },
		"entry of a missing revision": func(d *Data) { d.Sets[0].Entries[0].RevNo = 9 },
		"set hash changed":            func(d *Data) { d.Sets[0].Hash = "other" },
		"head wrong":                  func(d *Data) { d.Head = 0 },
		"counter below one":           func(d *Data) { d.Counters = map[string]int64{"c": 0} },
		"branch twice":                func(d *Data) { d.Branches = append(d.Branches, d.Branches[0]) },
		"branch without a version":    func(d *Data) { d.Branches[0].Version = 0 },
		"proposal twice":              func(d *Data) { d.Proposals = append(d.Proposals, d.Proposals[0]) },
		"proposal without a branch":   func(d *Data) { d.Proposals[0].BranchID = "ghost" },
		"proposal without a version":  func(d *Data) { d.Proposals[0].Version = 0 },
	}
	for name, f := range damage {
		d := r.store.Export()
		f(&d)
		if _, err := OpenMemoryStore(d, nil); code(err) != kernel.CodeInvalidValue {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// [REQ-340]
func TestAWriteThatCannotBeKeptLeavesTheStoreAsItWasUnlessTheFileWasReplaced(t *testing.T) {
	fail := errors.New("disk full")
	var replaced bool
	var failure error
	s, err := OpenMemoryStore(Data{}, func(Data) (bool, error) { return replaced, failure })
	if err != nil {
		t.Fatal(err)
	}
	r1 := rev("a", 1, "k", `{}`)
	w := Write{Items: []Item{{ID: "a", Kind: "k"}}, Revisions: []Revision{r1}, Set: firstSet(r1), Head: &HeadMove{New: 1}}
	failure = fail
	if err := s.Commit(ctx, w); !errors.Is(err, fail) {
		t.Fatalf("commit: %v", err)
	}
	if head, _ := s.Head(ctx); head != 0 {
		t.Fatal("a write that could not be kept is in the store")
	}
	replaced = true
	if err := s.Commit(ctx, w); !errors.Is(err, fail) {
		t.Fatalf("commit: %v", err)
	}
	if head, _ := s.Head(ctx); head != 1 {
		t.Fatal("a write whose file was replaced must be in the store, so that it agrees with the file")
	}
}
