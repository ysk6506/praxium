package identitycheck

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/identity"
	"github.com/ysk6506/praxium/backend/internal/identityfile"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

func idcRun(t *testing.T, factory Factory) {
	t.Helper()
	for _, c := range Cases() {
		t.Run(c.Name, func(t *testing.T) { c.Run(t, factory) })
	}
}

// [REQ-311]
func TestTheInMemoryStoreConformsToTheIdentityStorePort(t *testing.T) {
	idcRun(t, func(TB) Store { return identity.NewMemoryStore() })
}

// [REQ-311]
func TestTheFileStoreConformsToTheIdentityStorePort(t *testing.T) {
	idcRun(t, func(tb TB) Store {
		s, err := identityfile.Open(identityfile.NewMemDisk())
		if err != nil {
			tb.Fatalf("open: %v", err)
		}
		return s
	})
}

// [REQ-311]
func TestThePostgresStoreConformsToTheIdentityStorePort(t *testing.T) {
	idcRun(t, func(tb TB) Store {
		st := pgstore.NewTestStore(t)
		return st.Records()
	})
}

// [REQ-311]
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
