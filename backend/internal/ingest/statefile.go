package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"sync"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// StateFileName is the file of the data folder that keeps the ingestion state of every source that was run into
// the KG of the folder (ADR-0041).
const StateFileName = "ingest-state.json"

const (
	stateFormat  = "praxium-ingest-state"
	stateVersion = 1
	stateTmpName = StateFileName + ".tmp"
)

// StateDisk is the one folder a state file lives in, and nothing more: plain file names, never paths. The
// composition root implements it over the operating system (the core knows no operating system); the tests
// implement it over memory and make it fail.
type StateDisk interface {
	// ReadWhole returns the whole file. A file that is not there is an error that is fs.ErrNotExist.
	ReadWhole(name string) ([]byte, error)
	// Create makes a new file for writing and fails if the name exists.
	Create(name string) (StateFile, error)
	// Rename renames a file atomically, replacing the target if there is one.
	Rename(from, to string) error
	// Remove deletes a file.
	Remove(name string) error
	// SyncDir makes the creations and renames made so far durable.
	SyncDir() error
}

// StateFile is a file being written.
type StateFile interface {
	io.Writer
	// Sync makes everything written so far durable.
	Sync() error
	Close() error
}

// stateDoc is the content of the state file: a versioned JSON document. It names the KG the state belongs to, so
// a state is never used against another KG, and holds the state of each source as it is (State).
type stateDoc struct {
	Format  string                     `json:"format"`
	Version int                        `json:"version"`
	KG      string                     `json:"kg_id"`
	Sources map[string]json.RawMessage `json:"sources"`
}

// FileStateStore keeps the state of the sources in one file, so the next run, in another process, goes on from it.
// Whatever a run saved is on disk when Save returns: the file is written to a temporary name, synced, and only
// then renamed over the old one, so a crash leaves the old file or the new one, never half of one.
type FileStateStore struct {
	disk StateDisk
	mu   sync.Mutex
	doc  stateDoc
}

var _ StateStore = (*FileStateStore)(nil)

// OpenFileStateStore reads the state file of the folder. No file is an empty store that belongs to no KG yet. A file
// that is not a state file of this format, or is of a newer version, is an error and nothing is overwritten.
func OpenFileStateStore(disk StateDisk) (*FileStateStore, error) {
	f := &FileStateStore{disk: disk, doc: stateDoc{Format: stateFormat, Version: stateVersion, Sources: map[string]json.RawMessage{}}}
	raw, err := disk.ReadWhole(StateFileName)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot read "+StateFileName)
	}
	var doc stateDoc
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Format != stateFormat {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s is not an ingestion state file of praxium; move it away to start over", StateFileName)
	}
	if doc.Version != stateVersion {
		return nil, kernel.Errorf(kernel.CodeUnsupported, "%s has version %d; this praxium reads version %d (a newer praxium wrote it?)", StateFileName, doc.Version, stateVersion)
	}
	if doc.Sources == nil {
		doc.Sources = map[string]json.RawMessage{}
	}
	f.doc = doc
	return f, nil
}

// KG is the id of the KG the state belongs to, or "" while it belongs to none.
func (f *FileStateStore) KG() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.doc.KG
}

// Sources is how many sources the file holds a state of.
func (f *FileStateStore) Sources() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.doc.Sources)
}

// Bind makes the file belong to a KG and writes it, so a state that exists always names its KG.
func (f *FileStateStore) Bind(kg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.doc.KG = kg
	return f.write()
}

// Load returns the saved state of a source, or an empty one.
func (f *FileStateStore) Load(_ context.Context, source string) (*State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, ok := f.doc.Sources[source]
	if !ok {
		return newState(), nil
	}
	s := newState()
	if err := json.Unmarshal(raw, s); err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "the saved state of source "+source+" cannot be read")
	}
	return s, nil
}

// Save keeps the state of a source and writes the file. When the write fails the state in memory is as it was.
func (f *FileStateStore) Save(_ context.Context, source string, s *State) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "the state cannot be saved")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	old, had := f.doc.Sources[source]
	f.doc.Sources[source] = raw
	if err := f.write(); err != nil {
		if had {
			f.doc.Sources[source] = old
		} else {
			delete(f.doc.Sources, source)
		}
		return err
	}
	return nil
}

// write is the atomic replacement of the file: temporary name, sync, rename, sync of the folder.
func (f *FileStateStore) write() error {
	data, err := json.Marshal(f.doc)
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "the state cannot be written")
	}
	data = append(data, '\n')
	if err := f.disk.Remove(stateTmpName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return kernel.Wrap(kernel.CodeInternal, err, "cannot remove the left-over "+stateTmpName)
	}
	file, err := f.disk.Create(stateTmpName)
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "cannot create "+stateTmpName)
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = f.disk.Remove(stateTmpName)
		return kernel.Wrap(kernel.CodeInternal, err, "cannot write "+stateTmpName)
	}
	if err := f.disk.Rename(stateTmpName, StateFileName); err != nil {
		_ = f.disk.Remove(stateTmpName)
		return kernel.Wrap(kernel.CodeInternal, err, "cannot replace "+StateFileName)
	}
	if err := f.disk.SyncDir(); err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "cannot sync the data folder")
	}
	return nil
}
