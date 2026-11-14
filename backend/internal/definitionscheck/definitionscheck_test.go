package definitionscheck

import (
	"errors"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/definitions"
	"github.com/ysk6506/praxium/backend/internal/definitionsfile"
)

// caseFloor is how many cases the suite had when it was made; a suite that shrinks proves less.
const caseFloor = 14

var errDisk = errors.New("the disk failed")

func runSuite(t *testing.T, factory Factory) {
	t.Helper()
	if len(Cases()) < caseFloor {
		t.Fatalf("the suite has %d cases, and it must keep at least %d", len(Cases()), caseFloor)
	}
	for _, c := range Cases() {
		t.Run(c.Name, func(t *testing.T) { c.Run(t, factory) })
	}
}

// memorySubject is a memory store that keeps each write in a second copy, which is what a restart opens.
func memorySubject(TB) Subject {
	var kept definitions.Data
	var failing bool
	persist := func(d definitions.Data) (bool, error) {
		if failing {
			return false, errDisk
		}
		kept = d
		return true, nil
	}
	st, _ := definitions.OpenMemoryStore(definitions.Data{}, persist)
	return Subject{
		Store:      st,
		FailWrites: func(on bool) { failing = on },
		Reopen:     func() (definitions.Store, error) { return definitions.OpenMemoryStore(kept, persist) },
	}
}

func fileSubject(tb TB) Subject {
	disk := definitionsfile.NewMemDisk()
	st, err := definitionsfile.Open(disk)
	if err != nil {
		tb.Fatalf("open: %v", err)
	}
	return Subject{
		Store: st,
		FailWrites: func(on bool) {
			if on {
				disk.Fail["rename"] = errDisk
			} else {
				delete(disk.Fail, "rename")
			}
		},
		Reopen: func() (definitions.Store, error) { return definitionsfile.Open(disk) },
	}
}

// [REQ-341]
func TestTheMemoryStoreConformsToTheDefinitionsStorePort(t *testing.T) {
	runSuite(t, memorySubject)
}

// [REQ-341]
func TestTheFileStoreConformsToTheDefinitionsStorePort(t *testing.T) {
	runSuite(t, fileSubject)
}
