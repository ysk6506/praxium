package secrets

import (
	"context"
	"sort"
	"sync"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Store is the port of the stored secrets: whole records, each named by its id. A record holds only ciphertext, so
// a store sees no value. Get and List return copies; Put and Delete take effect completely or not at all.
type Store interface {
	// Get returns the record, and whether it is there.
	Get(ctx context.Context, id string) (Record, bool, error)
	// List returns every record in the order of the ids.
	List(ctx context.Context) ([]Record, error)
	// Put stores the record, replacing the one with the same id.
	Put(ctx context.Context, rec Record) error
	// Delete removes a record and says whether it was there.
	Delete(ctx context.Context, id string) (bool, error)
}

// MemoryStore is the Store that lives in memory: the store of the tests and the base of the file store, which gives
// it a persist function that keeps each new state before it is taken.
type MemoryStore struct {
	mu      sync.Mutex
	records map[string]Record
	persist func([]Record) (bool, error)
	webhook bool // the store of the receiving webhooks: only their kinds are kept, and no kind of a source
}

var _ Store = (*MemoryStore)(nil)

// NewMemoryStore makes an empty store of the secrets of the sources.
func NewMemoryStore() *MemoryStore { return &MemoryStore{records: map[string]Record{}} }

// NewWebhookMemoryStore makes an empty store of the secrets of the receiving webhooks (ADR-0107): the kinds of a source
// are not kept in it, and the kinds of a webhook are kept in no other store.
func NewWebhookMemoryStore() *MemoryStore {
	return &MemoryStore{records: map[string]Record{}, webhook: true}
}

// OpenMemoryStore makes a store that starts with records and calls persist with the whole new state (in the order
// of the ids) before each change takes effect. persist says whether the new state was kept even though it failed (a
// file that is in place but whose folder could not be synced): then the change takes effect and the error is still
// returned. When persist fails otherwise the change does not happen. A record whose shape is wrong, or a repeated id,
// is an error.
func OpenMemoryStore(records []Record, persist func([]Record) (bool, error)) (*MemoryStore, error) {
	return openMemoryStore(records, persist, false)
}

// OpenWebhookMemoryStore is OpenMemoryStore for the secrets of the receiving webhooks.
func OpenWebhookMemoryStore(records []Record, persist func([]Record) (bool, error)) (*MemoryStore, error) {
	return openMemoryStore(records, persist, true)
}

func (m *MemoryStore) validate(r Record) error {
	if m.webhook {
		return r.ValidateWebhook()
	}
	return r.Validate()
}

func openMemoryStore(records []Record, persist func([]Record) (bool, error), webhook bool) (*MemoryStore, error) {
	m := &MemoryStore{records: make(map[string]Record, len(records)), persist: persist, webhook: webhook}
	for _, r := range records {
		if err := m.validate(r); err != nil {
			return nil, err
		}
		if _, dup := m.records[r.ID]; dup {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "the secret %q is stored twice", r.ID)
		}
		m.records[r.ID] = r.Clone()
	}
	return m, nil
}

func sortedRecords(m map[string]Record) []Record {
	out := make([]Record, 0, len(m))
	for _, r := range m {
		out = append(out, r.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns a copy of the record.
func (m *MemoryStore) Get(_ context.Context, id string) (Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	return r.Clone(), ok, nil
}

// List returns copies of the records in the order of the ids.
func (m *MemoryStore) List(context.Context) ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return sortedRecords(m.records), nil
}

// Put stores a copy of the record.
func (m *MemoryStore) Put(_ context.Context, rec Record) error {
	if err := m.validate(rec); err != nil {
		return err
	}
	return m.change(func(next map[string]Record) bool {
		next[rec.ID] = rec.Clone()
		return true
	})
}

// Delete removes the record.
func (m *MemoryStore) Delete(_ context.Context, id string) (bool, error) {
	found := false
	err := m.change(func(next map[string]Record) bool {
		_, found = next[id]
		delete(next, id)
		return found
	})
	return found, err
}

// change applies fn to a copy of the state, keeps the copy through persist, then takes it. fn says whether it changed
// anything.
func (m *MemoryStore) change(fn func(next map[string]Record) bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := make(map[string]Record, len(m.records)+1)
	for id, r := range m.records {
		next[id] = r
	}
	if !fn(next) {
		return nil
	}
	if m.persist == nil {
		m.records = next
		return nil
	}
	kept, err := m.persist(sortedRecords(next))
	if kept {
		m.records = next
	}
	return err
}
