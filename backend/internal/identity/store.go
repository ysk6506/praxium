package identity

import (
	"context"
	"slices"
	"sort"
	"sync"
	"unicode/utf8"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The kinds of record the service keeps.
const (
	kindSubject = "subject"
	kindToken   = "token"
	kindMeta    = "meta"
)

// Store is the port of the identity data: records of bytes, named by a kind and an ID, each with a version that
// goes up by one at every write. It uses only built-in types, so an adapter that cannot import this unit still
// implements it by its method signatures. An error carries kernel.CodeNotFound (a record that is not there) or
// kernel.CodeConflict (a write whose expected version is not the stored one); anything else is a failure of the
// store itself.
type Store interface {
	// Get returns a copy of the record and its version.
	Get(ctx context.Context, kind, id string) (data []byte, version int64, err error)
	// Put stores a copy of data, which is UTF-8 text without NUL (a record of another kind of bytes is refused), and returns the new version. expect is the version the caller read, or 0 for a
	// record that must not exist yet; when the stored version is another, nothing is written and the error is a
	// conflict. This makes the first writer win when two try to make the same record.
	Put(ctx context.Context, kind, id string, data []byte, expect int64) (version int64, err error)
	// Scan calls fn for every record of the kind in the order of the IDs, with copies of the data. An error of fn
	// stops the scan and is returned.
	Scan(ctx context.Context, kind string, fn func(id string, data []byte, version int64) error) error
}

// MemoryStore is the Store that lives in memory. It is the store of the tests, and the model of what the other
// stores must do: the conformance suite runs on all of them.
type MemoryStore struct {
	mu      sync.Mutex
	records map[string]map[string]memRecord
}

type memRecord struct {
	data    []byte
	version int64
}

var _ Store = (*MemoryStore)(nil)

// NewMemoryStore makes an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: map[string]map[string]memRecord{}}
}

// Get returns the record, or a not_found error.
func (m *MemoryStore) Get(_ context.Context, kind, id string) ([]byte, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[kind][id]
	if !ok {
		return nil, 0, kernel.Errorf(kernel.CodeNotFound, "no %s record %q", kind, id)
	}
	return slices.Clone(rec.data), rec.version, nil
}

// Put writes the record when the stored version is expect.
func (m *MemoryStore) Put(_ context.Context, kind, id string, data []byte, expect int64) (int64, error) {
	if !utf8.Valid(data) || slices.Contains(data, 0) {
		return 0, kernel.Errorf(kernel.CodeInvalidValue, "a record is UTF-8 text without NUL")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	have := m.records[kind][id]
	if have.version != expect {
		return 0, kernel.Errorf(kernel.CodeConflict, "the %s record %q is at version %d, not %d", kind, id, have.version, expect)
	}
	if m.records[kind] == nil {
		m.records[kind] = map[string]memRecord{}
	}
	m.records[kind][id] = memRecord{data: slices.Clone(data), version: have.version + 1}
	return have.version + 1, nil
}

// Scan visits the records of the kind in the order of the IDs. The records are copied first, so fn may use the store.
func (m *MemoryStore) Scan(_ context.Context, kind string, fn func(id string, data []byte, version int64) error) error {
	m.mu.Lock()
	ids := make([]string, 0, len(m.records[kind]))
	copies := map[string]memRecord{}
	for id, rec := range m.records[kind] {
		ids = append(ids, id)
		copies[id] = memRecord{data: slices.Clone(rec.data), version: rec.version}
	}
	m.mu.Unlock()
	sort.Strings(ids)
	for _, id := range ids {
		if err := fn(id, copies[id].data, copies[id].version); err != nil {
			return err
		}
	}
	return nil
}
