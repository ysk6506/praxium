package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ysk6506/praxium/backend/internal/filejournal"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// lockFileName is the file in the data folder that the lock is taken on.
const lockFileName = "LOCK"

// fileDisk is the data folder of the built-in file store on the operating system (ADR-0034): the one place that
// gives filejournal its disk, as the root gives the file adapter of the ingestion pipeline its opener.
type fileDisk struct{ dir string }

var _ filejournal.Disk = fileDisk{}

// openDataDir makes sure the folder exists and returns its disk. It does not take the lock; the journal does.
func openDataDir(dir string) (fileDisk, error) {
	if dir == "" {
		return fileDisk{}, kernel.Errorf(kernel.CodeInvalidValue, "the file store needs a data folder")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fileDisk{}, kernel.Wrap(kernel.CodeInvalidValue, err, "cannot create the data folder")
	}
	return fileDisk{dir: dir}, nil
}

func (d fileDisk) path(name string) string { return filepath.Join(d.dir, name) }

// Lock takes the lock of the folder: an exclusive lock on the LOCK file that the operating system keeps for as long
// as the process holds the file open and drops when the process ends, however it ends. A process that died has
// therefore left no lock behind (lock_unix.go; lock_other.go for the systems without such a lock).
func (d fileDisk) Lock() (func() error, error) {
	f, err := os.OpenFile(d.path(lockFileName), os.O_CREATE|os.O_RDWR, 0o640)
	if err != nil {
		return nil, kernel.Wrap(filejournal.CodeIO, err, "cannot open the lock file")
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, kernel.Wrap(filejournal.CodeLocked, err,
			fmt.Sprintf("the data folder %s is in use by another process (a lock on %s is held)", d.dir, lockFileName))
	}
	// The process number is for the people who look into the folder; the lock is the operating system's.
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(fmt.Sprintf("%d\n", os.Getpid())), 0)
	return func() error {
		unlockFile(f)
		return f.Close()
	}, nil
}

// List returns the names of the files of the folder.
func (d fileDisk) List() ([]string, error) {
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// Open opens a file for reading.
func (d fileDisk) Open(name string) (io.ReadCloser, error) { return os.Open(d.path(name)) }

// Create makes a new file and fails when the name exists.
func (d fileDisk) Create(name string) (filejournal.File, error) {
	return os.OpenFile(d.path(name), os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0o640)
}

// OpenAppend opens an existing file for appending.
func (d fileDisk) OpenAppend(name string) (filejournal.File, error) {
	return os.OpenFile(d.path(name), os.O_WRONLY|os.O_APPEND, 0o640)
}

// Rename renames atomically, replacing the target.
func (d fileDisk) Rename(from, to string) error { return os.Rename(d.path(from), d.path(to)) }

// Remove deletes a file.
func (d fileDisk) Remove(name string) error { return os.Remove(d.path(name)) }

// SyncDir makes the creations, renames and removals durable by syncing the folder itself.
func (d fileDisk) SyncDir() error {
	f, err := os.Open(d.dir)
	if err != nil {
		return err
	}
	err = f.Sync()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	// Some systems cannot sync a folder and say so; the file syncs still hold.
	if errors.Is(err, errSyncDirUnsupported) {
		return nil
	}
	return err
}
