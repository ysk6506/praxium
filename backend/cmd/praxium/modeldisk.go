package main

import (
	"os"

	"github.com/ysk6506/praxium/backend/internal/definitionsfile"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// defDisk is the data folder as the file store of the definitions needs it (definitionsfile.Disk). A writer holds the
// lock of the folder; a reader (readOnly) holds nothing and cannot change anything: it reads the file whole, which
// the atomic rename of a write keeps from ever being half of one, and it does not even remove the leftover of an
// interrupted write, since that might be the temporary file of a write in progress.
type defDisk struct {
	fileDisk
	readOnly bool
}

var _ definitionsfile.Disk = defDisk{}

// ReadWhole returns a whole file of the folder.
func (d defDisk) ReadWhole(name string) ([]byte, error) { return os.ReadFile(d.path(name)) }

// Create makes a new file for writing that only its owner can read, and fails when the name exists.
func (d defDisk) Create(name string) (definitionsfile.File, error) {
	if d.readOnly {
		return nil, kernel.Errorf(kernel.CodeInternal, "the definitions are opened for reading only")
	}
	return os.OpenFile(d.path(name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
}

// Rename renames atomically, replacing the target.
func (d defDisk) Rename(from, to string) error {
	if d.readOnly {
		return kernel.Errorf(kernel.CodeInternal, "the definitions are opened for reading only")
	}
	return d.fileDisk.Rename(from, to)
}

// Remove deletes a file; a reader deletes nothing.
func (d defDisk) Remove(name string) error {
	if d.readOnly {
		return nil
	}
	return d.fileDisk.Remove(name)
}

// SyncDir makes the creations and renames durable.
func (d defDisk) SyncDir() error {
	if d.readOnly {
		return nil
	}
	return d.fileDisk.SyncDir()
}

// openDefinitionsDisk opens the folder for the definitions. A reader needs the folder to be there; a writer makes it.
func openDefinitionsDisk(dir string, readOnly bool) (defDisk, error) {
	if dir == "" {
		return defDisk{}, kernel.Errorf(kernel.CodeInvalidValue, "the definitions need a data folder")
	}
	if readOnly {
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() {
			return defDisk{}, kernel.Errorf(kernel.CodeNotFound, "the data folder %s does not exist", dir)
		}
		return defDisk{fileDisk: fileDisk{dir: dir}, readOnly: true}, nil
	}
	disk, err := openDataDir(dir)
	return defDisk{fileDisk: disk}, err
}
