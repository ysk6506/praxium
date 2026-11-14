package ingest

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// RawRecord is a row as it was received, kept as it came and never changed (ingestion.md 7.1). Its interval is
// numbered by the runs of the source (FromRun, ToRun); ToRun is 0 while the record is open. The design numbers
// intervals with ingest_seq, which the ordering step gives; the slice has no such step (ADR-0033), so a run is the
// unit of order, and the run keeps the ingest_seq of the batch it applied.
type RawRecord struct {
	Key     string            `json:"key"`
	Hash    string            `json:"hash"`
	Line    int               `json:"line"`
	Where   string            `json:"where,omitempty"`
	Fields  map[string]string `json:"fields"`
	FromRun int               `json:"from_run"`
	ToRun   int               `json:"to_run,omitempty"`
}

// Unseen counts how many complete captures in a row did not show a record, and when that began (pipeline.md,
// kg-storage.md 3.3: unseen_object). It is the only place the count is kept.
type Unseen struct {
	Runs  int    `json:"runs"`
	First string `json:"first"`
}

// DatasetState is what the pipeline keeps of one dataset between runs.
type DatasetState struct {
	Open        map[string]*RawRecord `json:"open"`
	Closed      []RawRecord           `json:"closed,omitempty"`
	Unseen      map[string]Unseen     `json:"unseen,omitempty"`
	LastCapture string                `json:"last_capture,omitempty"`
}

// ObjectInfo is what the pipeline knows of an object it made. Retirable is set once a feed that may retire
// objects has seen it (pipeline.md 3.3).
type ObjectInfo struct {
	Type      string   `json:"type"`
	Retirable bool     `json:"retirable,omitempty"`
	Idents    []string `json:"idents,omitempty"`
}

// State is everything a source keeps between runs: its raw records, the identity index (which key stands for
// which object: the identity history is part of the truth, kg-model.md 10.5) and the objects and links it made.
type State struct {
	Run      int                      `json:"run"`
	Datasets map[string]*DatasetState `json:"datasets"`
	Identity map[string]string        `json:"identity"`
	Objects  map[string]ObjectInfo    `json:"objects"`
	Links    map[string]bool          `json:"links"`
	// Keys is the key every object of the identity index was first seen with: the id of an object comes from it,
	// so it is kept for good, also for an object that was retired (kg-model.md 10.5).
	Keys map[string][]graph.KeyPart `json:"keys"`
	// IngestSeq is the ordering event of the last applied batch.
	IngestSeq int64 `json:"ingest_seq,omitempty"`
}

func newState() *State {
	return &State{Datasets: map[string]*DatasetState{}, Identity: map[string]string{}, Objects: map[string]ObjectInfo{}, Links: map[string]bool{}, Keys: map[string][]graph.KeyPart{}}
}

func (s *State) dataset(id string) *DatasetState {
	d := s.Datasets[id]
	if d == nil {
		d = &DatasetState{Open: map[string]*RawRecord{}, Unseen: map[string]Unseen{}}
		s.Datasets[id] = d
	}
	return d
}

// StateStore keeps the state of a source between runs. A run loads it, works on its own copy and saves it only
// when the batch was applied, so a failed run leaves the state as it was.
type StateStore interface {
	Load(ctx context.Context, source string) (*State, error)
	Save(ctx context.Context, source string, s *State) error
}

// MemStateStore keeps states in memory as JSON, so what it returns shares nothing with what it was given and
// whatever a durable store would keep is already known to be serialisable.
type MemStateStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

// NewMemStateStore returns an empty store.
func NewMemStateStore() *MemStateStore { return &MemStateStore{data: map[string][]byte{}} }

// Load returns the saved state of a source, or an empty one.
func (m *MemStateStore) Load(_ context.Context, source string) (*State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, ok := m.data[source]
	if !ok {
		return newState(), nil
	}
	s := newState()
	if err := json.Unmarshal(raw, s); err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "the saved state of source "+source+" cannot be read")
	}
	return s, nil
}

// Save keeps the state of a source.
func (m *MemStateStore) Save(_ context.Context, source string, s *State) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "the state cannot be saved")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[source] = raw
	return nil
}

func parseObject(text string) (graph.ObjectID, error) { return kernel.ParseID[graph.ObjectKind](text) }
