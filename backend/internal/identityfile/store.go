package identityfile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"sort"
	"sync"
	"unicode/utf8"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// FileName is the file of the data folder that holds the identity data.
const FileName = "identity.json"

const (
	format     = "praxium-identity"
	version    = 1
	tmpName    = FileName + ".tmp"
	maxFileLen = 64 << 20
)

// Disk is the one folder the file lives in: plain file names, never paths. The composition root implements it over
// the operating system; Create must make a file only its owner can read.
type Disk interface {
	// ReadWhole returns the whole file. A file that is not there is an error that is fs.ErrNotExist.
	ReadWhole(name string) ([]byte, error)
	// Create makes a new file for writing and fails if the name exists.
	Create(name string) (File, error)
	// Rename renames a file atomically, replacing the target if there is one.
	Rename(from, to string) error
	// Remove deletes a file.
	Remove(name string) error
	// SyncDir makes the creations and renames made so far durable.
	SyncDir() error
}

// File is a file being written.
type File interface {
	io.Writer
	Sync() error
	Close() error
}

type fileRec struct {
	Version int64  `json:"version"`
	Data    string `json:"data"`
}

type fileDoc struct {
	Format  string                        `json:"format"`
	Version int                           `json:"version"`
	Records map[string]map[string]fileRec `json:"records"`
}

// Store keeps the records in memory and in the file. The process that has it open is the only writer: the caller
// holds the lock of the data folder.
type Store struct {
	mu   sync.Mutex
	disk Disk
	doc  fileDoc
}

// Open reads the file of the folder; a folder without one is an empty store, and nothing is written until the first
// Put. A file that is not an identity file of this format, or is of a newer version, is an error and nothing is
// overwritten.
func Open(disk Disk) (*Store, error) {
	s := &Store{disk: disk, doc: fileDoc{Format: format, Version: version, Records: map[string]map[string]fileRec{}}}
	// A leftover of a write that never became the file.
	if err := disk.Remove(tmpName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot remove the leftover of an interrupted write of "+FileName)
	}
	raw, err := disk.ReadWhole(FileName)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot read "+FileName)
	}
	if len(raw) > maxFileLen {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s is larger than %d bytes", FileName, maxFileLen)
	}
	var doc fileDoc
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Format != format {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s is not an identity file of praxium; move it away to start over", FileName)
	}
	if doc.Version != version {
		return nil, kernel.Errorf(kernel.CodeUnsupported, "%s has version %d; this praxium reads version %d (a newer praxium wrote it?)", FileName, doc.Version, version)
	}
	if doc.Records == nil {
		doc.Records = map[string]map[string]fileRec{}
	}
	s.doc = doc
	return s, nil
}

// Get returns the record and its version, or a not_found error.
func (s *Store) Get(_ context.Context, kind, id string) ([]byte, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.doc.Records[kind][id]
	if !ok {
		return nil, 0, kernel.Errorf(kernel.CodeNotFound, "no %s record %q", kind, id)
	}
	return []byte(rec.Data), rec.Version, nil
}

// Put writes the record when the stored version is expect, and the file with it. When the file cannot be written the
// record is as it was. When the new file is in place but the folder could not be synced, the record is the new one
// and the error says that it may not survive a crash.
func (s *Store) Put(_ context.Context, kind, id string, data []byte, expect int64) (int64, error) {
	if !utf8.Valid(data) || containsNUL(data) {
		return 0, kernel.Errorf(kernel.CodeInvalidValue, "a record is UTF-8 text without NUL")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, had := s.doc.Records[kind][id]
	if old.Version != expect {
		return 0, kernel.Errorf(kernel.CodeConflict, "the %s record %q is at version %d, not %d", kind, id, old.Version, expect)
	}
	if s.doc.Records[kind] == nil {
		s.doc.Records[kind] = map[string]fileRec{}
	}
	s.doc.Records[kind][id] = fileRec{Version: old.Version + 1, Data: string(data)}
	if replaced, err := s.write(); err != nil {
		if replaced {
			// The new file is in place; only its durability is in doubt. Memory keeps agreeing with the file.
			return old.Version + 1, err
		}
		if had {
			s.doc.Records[kind][id] = old
		} else {
			delete(s.doc.Records[kind], id)
		}
		return 0, err
	}
	return old.Version + 1, nil
}

// Scan visits the records of the kind in the order of the IDs. The records are copied first, so fn may use the store.
func (s *Store) Scan(_ context.Context, kind string, fn func(id string, data []byte, version int64) error) error {
	s.mu.Lock()
	ids := make([]string, 0, len(s.doc.Records[kind]))
	copies := make(map[string]fileRec, len(s.doc.Records[kind]))
	for id, rec := range s.doc.Records[kind] {
		ids = append(ids, id)
		copies[id] = rec
	}
	s.mu.Unlock()
	sort.Strings(ids)
	for _, id := range ids {
		if err := fn(id, []byte(copies[id].Data), copies[id].Version); err != nil {
			return err
		}
	}
	return nil
}

func containsNUL(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// write puts the whole document on disk: temporary file, sync, rename over the old file, sync of the folder. It says
// whether the file was replaced, which tells a failure before that from one after it.
func (s *Store) write() (replaced bool, err error) {
	raw, err := json.MarshalIndent(s.doc, "", " ")
	if err != nil {
		return false, kernel.Wrap(kernel.CodeInternal, err, "the identity file cannot be written")
	}
	_ = s.disk.Remove(tmpName)
	f, err := s.disk.Create(tmpName)
	if err != nil {
		return false, kernel.Wrap(kernel.CodeInternal, err, "cannot create the new "+FileName)
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		_ = f.Close()
		_ = s.disk.Remove(tmpName)
		return false, kernel.Wrap(kernel.CodeInternal, err, "cannot write the new "+FileName)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = s.disk.Remove(tmpName)
		return false, kernel.Wrap(kernel.CodeInternal, err, "cannot sync the new "+FileName)
	}
	if err := f.Close(); err != nil {
		_ = s.disk.Remove(tmpName)
		return false, kernel.Wrap(kernel.CodeInternal, err, "cannot close the new "+FileName)
	}
	if err := s.disk.Rename(tmpName, FileName); err != nil {
		_ = s.disk.Remove(tmpName)
		return false, kernel.Wrap(kernel.CodeInternal, err, "cannot put the new "+FileName+" in place")
	}
	if err := s.disk.SyncDir(); err != nil {
		return true, kernel.Wrap(kernel.CodeInternal, err, "cannot sync the data folder")
	}
	return true, nil
}
