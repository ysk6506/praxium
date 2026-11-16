package secretsfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// FileName is the file of the data folder that holds the secrets.
const FileName = "secrets.json"

const (
	format  = "praxium-secrets"
	version = 1
	tmpName = FileName + ".tmp"
)

// maxFileLen is the largest file that is read (a variable only so that a test need not make one that big).
var maxFileLen = 64 << 20

// Disk is the one folder the file lives in: plain file names, never paths. The composition root implements it over the
// operating system. The contract of an implementation:
//   - Create makes a file with the mode 0600, so only its owner can read it.
//   - ReadWhole refuses a file whose permissions are looser than 0600, and it limits the size while reading, so that a
//     file larger than the limit is never held whole.
//   - The data folder is locked by the process that has the Disk open, so that no other process writes the file.
//
// This package does not check these; the implementation that is added with the assembly does.
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
	// Sync makes everything written so far durable.
	Sync() error
	Close() error
}

type fileDoc struct {
	Format  string           `json:"format"`
	Version int              `json:"version"`
	Secrets []secrets.Record `json:"secrets"`
}

// Store is the secrets.Store that keeps its content in the file. It is the memory store of the secrets unit with a
// way to keep each write. The process that has it open is the only writer: the caller holds the lock of the data
// folder. A folder without the file is an empty store, and nothing is written until the first write.
type Store struct {
	*secrets.MemoryStore
}

var _ secrets.Store = (*Store)(nil)

// Open reads the file of the folder. A file that is not a secrets file of this format, of a version this program does
// not read, or damaged is an error that says why, and nothing is overwritten.
func Open(disk Disk) (*Store, error) {
	// A leftover of a write that never became the file.
	if err := disk.Remove(tmpName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot remove the leftover of an interrupted write of "+FileName)
	}
	records, err := readFile(disk)
	if err != nil {
		return nil, err
	}
	mem, err := secrets.OpenMemoryStore(records, func(all []secrets.Record) (bool, error) { return write(disk, all) })
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, unusable())
	}
	return &Store{MemoryStore: mem}, nil
}

func unusable() string {
	return FileName + " cannot be used; restore it from a copy, or move it away and put the secrets in again"
}

func readFile(disk Disk) ([]secrets.Record, error) {
	raw, err := disk.ReadWhole(FileName)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot read "+FileName)
	}
	if len(raw) > maxFileLen {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s is larger than %d bytes", FileName, maxFileLen)
	}
	var head struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(raw, &head); err != nil || head.Format != format {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s is not a secrets file of praxium; %s", FileName, "restore it from a copy, or move it away and put the secrets in again")
	}
	if head.Version != version {
		return nil, kernel.Errorf(kernel.CodeUnsupported, "%s has version %d; this praxium reads version %d (a newer praxium wrote it?)", FileName, head.Version, version)
	}
	var doc fileDoc
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s is damaged or has a part this praxium does not know; %s", FileName, "restore it from a copy, or move it away and put the secrets in again")
	}
	return doc.Secrets, nil
}

// write puts the whole file on disk: temporary file, sync, rename over the old file, sync of the folder. It says
// whether the file was replaced, which tells a failure before that from one after it.
func write(disk Disk, all []secrets.Record) (replaced bool, err error) {
	if all == nil {
		all = []secrets.Record{}
	}
	raw, err := json.MarshalIndent(fileDoc{Format: format, Version: version, Secrets: all}, "", " ")
	if err != nil {
		return false, kernel.Wrap(kernel.CodeInternal, err, FileName+" cannot be written")
	}
	raw = append(raw, '\n')
	_ = disk.Remove(tmpName)
	f, err := disk.Create(tmpName)
	if err != nil {
		return false, kernel.Wrap(kernel.CodeInternal, err, "cannot create the new "+FileName)
	}
	if _, err := f.Write(raw); err != nil {
		return false, abandon(disk, f, "cannot write the new "+FileName, err)
	}
	if err := f.Sync(); err != nil {
		return false, abandon(disk, f, "cannot sync the new "+FileName, err)
	}
	if err := f.Close(); err != nil {
		_ = disk.Remove(tmpName)
		return false, kernel.Wrap(kernel.CodeInternal, err, "cannot close the new "+FileName)
	}
	if err := disk.Rename(tmpName, FileName); err != nil {
		_ = disk.Remove(tmpName)
		return false, kernel.Wrap(kernel.CodeInternal, err, "cannot put the new "+FileName+" in place")
	}
	if err := disk.SyncDir(); err != nil {
		return true, kernel.Wrap(kernel.CodeInternal, err, "the new "+FileName+" is in place but the data folder could not be synced; it may not survive a crash")
	}
	return true, nil
}

func abandon(disk Disk, f File, message string, cause error) error {
	_ = f.Close()
	_ = disk.Remove(tmpName)
	return kernel.Wrap(kernel.CodeInternal, cause, message)
}
