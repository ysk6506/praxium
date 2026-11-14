package definitionscheck

import (
	"context"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/definitions"
)

// recorder is a TB that notes a failure instead of ending the test, so a case can be run against a broken store.
type recorder struct{ failed bool }

type stop struct{}

func (r *recorder) Helper()               {}
func (r *recorder) Fatalf(string, ...any) { r.failed = true; panic(stop{}) }
func (r *recorder) Errorf(string, ...any) { r.failed = true }
func (r *recorder) Logf(string, ...any)   {}
func (r *recorder) Cleanup(func())        {}

// fails runs a case on a store and says whether the case failed.
func fails(c Case, factory Factory) (failed bool) {
	rec := &recorder{}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(stop); !ok {
				panic(r)
			}
		}
		failed = rec.failed
	}()
	c.Run(rec, factory)
	return rec.failed
}

// flawed is a store with one flaw at a time. Without a flaw it keeps the rules of the port, so each flaw is shown to be
// caught by the suite and the store without it is shown to pass.
type flawed struct {
	definitions.Store
	flaw    string
	failing bool
	kept    definitions.Data
}

func newFlawed(flaw string) Subject {
	f := &flawed{flaw: flaw}
	persist := func(d definitions.Data) (bool, error) { f.kept = d; return true, nil }
	f.Store, _ = definitions.OpenMemoryStore(definitions.Data{}, persist)
	return Subject{
		Store:      f,
		FailWrites: func(on bool) { f.failing = on },
		Reopen: func() (definitions.Store, error) {
			if flaw == "forgets at a restart" {
				return definitions.OpenMemoryStore(definitions.Data{}, nil)
			}
			return definitions.OpenMemoryStore(f.kept, persist)
		},
	}
}

func (f *flawed) Commit(ctx context.Context, w definitions.Write) error {
	if f.failing {
		if f.flaw == "keeps what a failed write began" {
			_ = f.Store.Commit(ctx, definitions.Write{Items: w.Items, Revisions: w.Revisions})
		}
		return errDisk
	}
	switch f.flaw {
	case "ignores the head":
		if w.Head != nil {
			w.Head.Expect, _ = f.Store.Head(ctx)
		}
	case "drops the ledger":
		w.Counters = nil
	case "ignores the version of a branch":
		for i, bw := range w.Branches {
			if b, err := f.Store.Branch(ctx, bw.Branch.ID); err == nil {
				w.Branches[i].Expect = b.Version
			}
		}
	}
	return f.Store.Commit(ctx, w)
}

// [REQ-341]
func TestTheSuiteCatchesEachFlawOfAStore(t *testing.T) {
	for flaw, want := range map[string]string{
		"ignores the head":                "a head that moved refuses the write and the second writer loses",
		"drops the ledger":                "the ledger stays raised after a discarded write and never goes down",
		"ignores the version of a branch": "two editors of one branch cannot overwrite each other",
		"keeps what a failed write began": "a write that fails halfway leaves nothing behind",
		"forgets at a restart":            "the state is the same after the store is opened again",
	} {
		caught := false
		for _, c := range Cases() {
			if fails(c, func(TB) Subject { return newFlawed(flaw) }) && c.Name == want {
				caught = true
			}
		}
		if !caught {
			t.Errorf("the case %q did not catch a store that %s", want, flaw)
		}
	}
	for _, c := range Cases() {
		if fails(c, func(TB) Subject { return newFlawed("") }) {
			t.Errorf("the case %q fails on a store without a flaw", c.Name)
		}
	}
}
