package identitycheck

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/identity"
	"github.com/ysk6506/praxium/backend/internal/identityfile"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// caseFloor is how many cases the suite had when it was made; a suite that shrinks proves less.
const caseFloor = 12

func idcRun(t *testing.T, factory Factory) {
	t.Helper()
	if len(Cases()) < caseFloor {
		t.Fatalf("the suite has %d cases, and it must keep at least %d", len(Cases()), caseFloor)
	}
	for _, c := range Cases() {
		t.Run(c.Name, func(t *testing.T) { c.Run(t, factory) })
	}
}

// [REQ-310]
func TestTheInMemoryStoreConformsToTheIdentityStorePort(t *testing.T) {
	idcRun(t, func(TB) Store { return identity.NewMemoryStore() })
}

// [REQ-310]
func TestTheFileStoreConformsToTheIdentityStorePort(t *testing.T) {
	idcRun(t, func(tb TB) Store {
		s, err := identityfile.Open(identityfile.NewMemDisk())
		if err != nil {
			tb.Fatalf("open: %v", err)
		}
		return s
	})
}

// [REQ-310]
func TestThePostgresStoreConformsToTheIdentityStorePort(t *testing.T) {
	idcRun(t, func(tb TB) Store {
		st := pgstore.NewTestStore(t)
		return st.Records()
	})
}

// [REQ-310]
func TestTheSuiteCatchesAStoreThatIgnoresTheVersion(t *testing.T) {
	caught := 0
	for _, c := range Cases() {
		if idcFails(c, func(TB) Store { return &idcLooseStore{identity.NewMemoryStore()} }) {
			caught++
		}
	}
	if caught < 4 {
		t.Fatalf("the suite caught a store that ignores versions in only %d cases", caught)
	}
}
