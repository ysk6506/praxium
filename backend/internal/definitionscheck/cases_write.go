package definitionscheck

import (
	"github.com/ysk6506/praxium/backend/internal/definitions"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func headMoved(tb TB, newSubject Factory) {
	tb.Helper()
	s := newSubject(tb)
	first, second := newSvc(tb, s.Store), newSvc(tb, s.Store)
	_, err := first.Apply(ctx, definitions.ApplyInput{Actor: alice, Changes: []definitions.Change{item("item-a", `{"v":1}`)}})
	need(tb, err == nil, "the first writer: %v", err)
	// The second writer read the head before the first wrote: it expects no head.
	_, err = second.Apply(ctx, definitions.ApplyInput{Actor: bob, Changes: []definitions.Change{item("item-b", `{"v":2}`)}})
	need(tb, codeIs(err, kernel.CodeConflict), "the second writer: %v", err)
	found, _ := s.Store.Items(ctx, []string{"item-b"})
	need(tb, head(tb, first) == 1 && len(found) == 0, "the head is D-%d and the loser left %d item(s)", head(tb, first), len(found))
	// The same at the port: a write that expects the head it did not find writes nothing.
	err = s.Store.Commit(ctx, definitions.Write{Head: &definitions.HeadMove{Expect: 0, New: 1}})
	need(tb, codeIs(err, kernel.CodeConflict), "a stale head at the port: %v", err)
}

func sameContentTwice(tb TB, newSubject Factory) {
	tb.Helper()
	svc := newSvc(tb, newSubject(tb).Store)
	first := apply(tb, svc, 0, item("item-a", `{"b":1,"a":[1,2]}`))
	again := apply(tb, svc, 1, item("item-a", "{ \"a\": [1, 2],\n \"b\": 1.0 }"))
	need(tb, first.Changed && !again.Changed && again.Set.No == 1, "the same content in another form: %+v", again)
	revs, err := svc.ItemHistory(ctx, "item-a")
	sets, _ := svc.History(ctx, 0, 0)
	need(tb, err == nil && len(revs) == 1 && len(sets) == 1, "%d revision(s) and %d set(s), %v", len(revs), len(sets), err)
}

func nothingWritten(tb TB, newSubject Factory) {
	tb.Helper()
	s := newSubject(tb)
	svc := newSvc(tb, s.Store)
	apply(tb, svc, 0, item("item-a", `{"v":1}`))
	// With the disk failing, a request that changes nothing still succeeds, because it does not write.
	s.FailWrites(true)
	defer s.FailWrites(false)
	res, err := svc.Apply(ctx, definitions.ApplyInput{ExpectHead: 1, Actor: alice, Counters: map[string]int64{"n": 50}, Changes: []definitions.Change{item("item-a", `{"v":1}`)}})
	need(tb, err == nil && !res.Changed, "a request that changes nothing: %+v %v", res, err)
	need(tb, number(tb, svc, "n") == 1, "the ledger rose for a request that changed nothing")
}

func ledgerKeepsItsHeight(tb TB, newSubject Factory) {
	tb.Helper()
	s := newSubject(tb)
	svc := newSvc(tb, s.Store)
	withCounter := func(expect int64, n int64, id string) error {
		_, err := svc.Apply(ctx, definitions.ApplyInput{ExpectHead: expect, Actor: alice, Counters: map[string]int64{"attr": n}, Changes: []definitions.Change{item(id, `{}`)}})
		return err
	}
	err := withCounter(0, 100002, "item-a")
	need(tb, err == nil, "the first write: %v", err)
	err = withCounter(0, 100009, "item-b")
	need(tb, codeIs(err, kernel.CodeConflict), "a discarded write: %v", err)
	err = withCounter(1, 100001, "item-b")
	need(tb, codeIs(err, kernel.CodeInvalidValue), "a write that lowers the ledger: %v", err)
	s.FailWrites(true)
	err = withCounter(1, 100020, "item-b")
	s.FailWrites(false)
	need(tb, err != nil, "a write on a failing disk succeeded")
	need(tb, number(tb, svc, "attr") == 100002, "the ledger after the discarded writes is %d, not 100002", number(tb, svc, "attr"))
	// The port refuses a lower value too, whoever asks.
	err = s.Store.Commit(ctx, definitions.Write{Counters: []definitions.CounterWrite{{Name: "attr", Expect: 100002, Value: 5}}})
	need(tb, codeIs(err, kernel.CodeConflict), "a lower value at the port: %v", err)
	reopened, err := s.Reopen()
	need(tb, err == nil, "reopen: %v", err)
	need(tb, number(tb, newSvc(tb, reopened), "attr") == 100002, "the ledger after a restart is %d", number(tb, newSvc(tb, reopened), "attr"))
}

func hashIsDeterministic(tb TB, newSubject Factory) {
	tb.Helper()
	one := newSvc(tb, newSubject(tb).Store)
	two := newSvc(tb, newSubject(tb).Store)
	a := apply(tb, one, 0, item("item-x", `{"v":1}`), item("item-y", `{"v":2}`))
	apply(tb, two, 0, item("item-y", `{"v":2}`))
	b := apply(tb, two, 1, item("item-x", `{"v":1}`))
	need(tb, a.Set.Hash == b.Set.Hash && a.Set.ParentNo != b.Set.ParentNo && a.Set.No != b.Set.No,
		"the same items must give the same hash whatever the order and the parent: %s (D-%d, parent D-%d) and %s (D-%d, parent D-%d)",
		a.Set.Hash, a.Set.No, a.Set.ParentNo, b.Set.Hash, b.Set.No, b.Set.ParentNo)
	empty := newSvc(tb, newSubject(tb).Store)
	_, err := empty.Apply(ctx, definitions.ApplyInput{Baseline: "std-hash", Actor: alice, Changes: []definitions.Change{item("item-z", `{}`)}})
	need(tb, err == nil, "apply: %v", err)
	gone, err := empty.Apply(ctx, definitions.ApplyInput{ExpectHead: 1, Actor: alice, Changes: []definitions.Change{{ItemID: "item-z", Remove: true}}})
	need(tb, err == nil && gone.Set.Hash == "std-hash", "a set without items has the baseline as its hash: %+v %v", gone.Set, err)
}

func noPartialWrite(tb TB, newSubject Factory) {
	tb.Helper()
	s := newSubject(tb)
	svc := newSvc(tb, s.Store)
	s.FailWrites(true)
	_, err := svc.Apply(ctx, definitions.ApplyInput{Actor: alice, Counters: map[string]int64{"n": 7}, Changes: []definitions.Change{item("item-a", `{"v":1}`), item("item-b", `{"v":2}`)}})
	s.FailWrites(false)
	need(tb, err != nil, "the write must fail when it cannot be kept")
	found, _ := s.Store.Items(ctx, []string{"item-a", "item-b"})
	_, revErr := s.Store.ItemRevisions(ctx, "item-a")
	need(tb, len(found) == 0 && codeIs(revErr, kernel.CodeNotFound), "items %v or revisions (%v) of a write that failed", found, revErr)
	need(tb, head(tb, svc) == 0 && number(tb, svc, "n") == 1, "a write that failed left a head or a ledger")
	reopened, err := s.Reopen()
	need(tb, err == nil, "reopen: %v", err)
	need(tb, head(tb, newSvc(tb, reopened)) == 0, "what was kept is not the old state")
	// The same write goes through when the disk does.
	need(tb, apply(tb, svc, 0, item("item-a", `{"v":1}`), item("item-b", `{"v":2}`)).Changed, "the retry changed nothing")
}
