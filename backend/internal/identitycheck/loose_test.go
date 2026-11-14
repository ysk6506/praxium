package identitycheck

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/identity"
)

// idcRecorder is a TB that notes a failure instead of ending the test, so a case can be run against a broken store.
type idcRecorder struct{ failed bool }

func (r *idcRecorder) Helper()               {}
func (r *idcRecorder) Fatalf(string, ...any) { r.failed = true; panic(idcStop{}) }
func (r *idcRecorder) Errorf(string, ...any) { r.failed = true }
func (r *idcRecorder) Logf(string, ...any)   {}
func (r *idcRecorder) Cleanup(func())        {}

type idcStop struct{}

// idcLooseStore is a store that writes whatever it is told: it never looks at the version.
type idcLooseStore struct{ *identity.MemoryStore }

func (s *idcLooseStore) Put(ctx context.Context, kind, id string, data []byte, _ int64) (int64, error) {
	_, version, err := s.MemoryStore.Get(ctx, kind, id)
	if err != nil {
		version = 0
	}
	return s.MemoryStore.Put(ctx, kind, id, data, version)
}

// idcFails runs a case on a store and says whether the case failed.
func idcFails(c Case, factory Factory) (failed bool) {
	rec := &idcRecorder{}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(idcStop); !ok {
				panic(r)
			}
		}
		failed = rec.failed
	}()
	c.Run(rec, factory)
	return rec.failed
}
