package definitionsfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"

	"github.com/ysk6506/praxium/backend/internal/definitions"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// FileName is the file of the data folder that holds the definitions.
const FileName = "definitions.json"

const (
	format  = "praxium-definitions"
	version = 1
	tmpName = FileName + ".tmp"
)

// maxFileLen is the largest file that is read (a variable only so that a test need not make one that big).
var maxFileLen = 512 << 20

// Disk is the one folder the file lives in: plain file names, never paths. The composition root implements it over the
// operating system; Create must make a file only its owner can read.
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

// Store is the definitions Store that keeps its content in the file. It is the memory store of the definitions unit
// with a way to keep each write, so the rules of a write are the same as there. The process that has it open is the only
// writer: the caller holds the lock of the data folder. A folder without the file is an empty store, and nothing is
// written until the first write.
type Store struct {
	*definitions.MemoryStore
}

var _ definitions.Store = (*Store)(nil)

// Open reads the file of the folder. A file that is not a definitions file of this format, of a version this program
// does not read, or damaged is an error that says why, and nothing is overwritten.
func Open(disk Disk) (*Store, error) {
	// A leftover of a write that never became the file.
	if err := disk.Remove(tmpName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot remove the leftover of an interrupted write of "+FileName)
	}
	data, err := readFile(disk)
	if err != nil {
		return nil, err
	}
	mem, err := definitions.OpenMemoryStore(data, func(d definitions.Data) (bool, error) { return write(disk, d) })
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, FileName+" cannot be used; move it away to start over")
	}
	return &Store{MemoryStore: mem}, nil
}

func readFile(disk Disk) (definitions.Data, error) {
	raw, err := disk.ReadWhole(FileName)
	if errors.Is(err, fs.ErrNotExist) {
		return definitions.Data{}, nil
	}
	if err != nil {
		return definitions.Data{}, kernel.Wrap(kernel.CodeInternal, err, "cannot read "+FileName)
	}
	if len(raw) > maxFileLen {
		return definitions.Data{}, kernel.Errorf(kernel.CodeInvalidValue, "%s is larger than %d bytes", FileName, maxFileLen)
	}
	var head struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(raw, &head); err != nil || head.Format != format {
		return definitions.Data{}, kernel.Errorf(kernel.CodeInvalidValue, "%s is not a definitions file of praxium; move it away to start over", FileName)
	}
	if head.Version != version {
		return definitions.Data{}, kernel.Errorf(kernel.CodeUnsupported, "%s has version %d; this praxium reads version %d (a newer praxium wrote it?)", FileName, head.Version, version)
	}
	return decode(raw)
}

// write puts the whole file on disk: temporary file, sync, rename over the old file, sync of the folder. It says whether
// the file was replaced, which tells a failure before that from one after it.
func write(disk Disk, d definitions.Data) (replaced bool, err error) {
	raw, err := encode(d)
	if err != nil {
		return false, kernel.Wrap(kernel.CodeInternal, err, FileName+" cannot be written")
	}
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

// section writes a list with one element to a line, so the file reads and compares well.
func section(out *bytes.Buffer, name string, list []json.RawMessage, last bool) {
	out.WriteString(`"` + name + `":[`)
	for i, el := range list {
		if i > 0 {
			out.WriteByte(',')
		}
		out.WriteString("\n ")
		out.Write(el)
	}
	out.WriteString("\n]")
	if !last {
		out.WriteByte(',')
	}
	out.WriteByte('\n')
}
