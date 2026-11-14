package definitions

import (
	"context"
	"slices"
	"sort"
	"sync"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Data is the whole content of a store, in a form that is easy to write down in a file and read back: lists in a fixed
// order. Open checks it, so a damaged one is refused.
type Data struct {
	Head      int64            `json:"head"`
	Items     []Item           `json:"items"`
	Revisions []Revision       `json:"revisions"`
	Sets      []Set            `json:"sets"`
	Counters  map[string]int64 `json:"counters"`
	Branches  []Branch         `json:"branches"`
	Proposals []Proposal       `json:"proposals"`
}

// PersistFunc keeps the new content of a store somewhere durable. It must only read the data and not keep it: the lists
// share their bodies and entries with the store, to spare a copy of everything at every write. It says whether that content did replace the old
// one, which tells a failure before that (the store stays as it was) from one after it (the content is the new one, but
// it is in doubt whether it survives a crash).
type PersistFunc func(Data) (replaced bool, err error)

// MemoryStore is the Store that lives in memory. It is the reference of the port, and with a PersistFunc it is the base
// of the file store: the rules of a write are in one place.
type MemoryStore struct {
	mu      sync.Mutex
	st      *state
	persist PersistFunc
}

var _ Store = (*MemoryStore)(nil)

// NewMemoryStore makes an empty store that keeps nothing outside memory.
func NewMemoryStore() *MemoryStore { return &MemoryStore{st: newState()} }

// OpenMemoryStore makes a store from data that was written before, after checking it. persist may be nil.
func OpenMemoryStore(d Data, persist PersistFunc) (*MemoryStore, error) {
	st, err := stateFromData(d)
	if err != nil {
		return nil, err
	}
	return &MemoryStore{st: st, persist: persist}, nil
}

// Export returns the whole content, as a copy.
func (m *MemoryStore) Export() Data {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st.export()
}

// Head returns the number of the set the main line is at.
func (m *MemoryStore) Head(context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st.head, nil
}

// Set returns a set with its entries.
func (m *MemoryStore) Set(_ context.Context, no int64) (Set, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if no < 1 || no > int64(len(m.st.sets)) {
		return Set{}, kernel.Errorf(kernel.CodeNotFound, "there is no definition set D-%d", no)
	}
	return cloneSet(m.st.sets[no-1], true), nil
}

// Sets returns sets without entries, newest first.
func (m *MemoryStore) Sets(_ context.Context, before int64, limit int) ([]Set, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	top := int64(len(m.st.sets))
	if before > 0 && before-1 < top {
		top = before - 1
	}
	var out []Set
	for no := top; no >= 1 && (limit <= 0 || len(out) < limit); no-- {
		out = append(out, cloneSet(m.st.sets[no-1], false))
	}
	return out, nil
}

// Items returns the known items among the IDs.
func (m *MemoryStore) Items(_ context.Context, ids []string) (map[string]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]Item, len(ids))
	for _, id := range ids {
		if it, ok := m.st.items[id]; ok {
			out[id] = it
		}
	}
	return out, nil
}

// ItemRevisions returns every revision of an item.
func (m *MemoryStore) ItemRevisions(_ context.Context, itemID string) ([]Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.st.items[itemID]; !ok {
		return nil, kernel.Errorf(kernel.CodeNotFound, "there is no item %q", itemID)
	}
	revs := m.st.revs[itemID]
	out := make([]Revision, len(revs))
	for i, r := range revs {
		out[i] = cloneRevision(r)
	}
	return out, nil
}

// Revisions returns the revisions asked for.
func (m *MemoryStore) Revisions(_ context.Context, refs []RevRef) ([]Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Revision, len(refs))
	for i, ref := range refs {
		revs := m.st.revs[ref.ItemID]
		if ref.RevNo < 1 || ref.RevNo > int64(len(revs)) {
			return nil, kernel.Errorf(kernel.CodeNotFound, "there is no revision %d of item %q", ref.RevNo, ref.ItemID)
		}
		out[i] = cloneRevision(revs[ref.RevNo-1])
	}
	return out, nil
}

// Counter returns the value of a counter, 0 when it is not there.
func (m *MemoryStore) Counter(_ context.Context, name string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st.counters[name], nil
}

// Branch returns a branch.
func (m *MemoryStore) Branch(_ context.Context, id string) (Branch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.st.branches[id]
	if !ok {
		return Branch{}, kernel.Errorf(kernel.CodeNotFound, "there is no branch %q", id)
	}
	return cloneBranch(b), nil
}

// Branches returns the branches in the states, ordered by ID.
func (m *MemoryStore) Branches(_ context.Context, states []BranchState) ([]Branch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Branch
	for _, b := range m.st.branches {
		if len(states) == 0 || slices.Contains(states, b.State) {
			out = append(out, cloneBranch(b))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Proposal returns a proposal.
func (m *MemoryStore) Proposal(_ context.Context, id string) (Proposal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.st.proposals[id]
	if !ok {
		return Proposal{}, kernel.Errorf(kernel.CodeNotFound, "there is no proposal %q", id)
	}
	return cloneProposal(p), nil
}

// Proposals returns the proposals in the states, ordered by ID.
func (m *MemoryStore) Proposals(_ context.Context, states []ProposalState) ([]Proposal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Proposal
	for _, p := range m.st.proposals {
		if len(states) == 0 || slices.Contains(states, p.State) {
			out = append(out, cloneProposal(p))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Commit makes one atomic write: the rules are checked on a copy, the copy is kept through the PersistFunc, and only
// then does it become the content of the store. A write that fails leaves the store as it was.
func (m *MemoryStore) Commit(_ context.Context, w Write) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.st.clone()
	if err := next.apply(w); err != nil {
		return err
	}
	if m.persist != nil {
		replaced, err := m.persist(next.data(false))
		if err != nil {
			if replaced {
				m.st = next
			}
			return err
		}
	}
	m.st = next
	return nil
}
