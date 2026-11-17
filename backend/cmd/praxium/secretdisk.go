package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"runtime"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secretsfile"
)

// maxSecretsFileBytes is the most that is read of secrets.json. The file store of the secrets reads no more than this
// either; the disk checks it first, so a file that is larger is never held whole in memory.
const maxSecretsFileBytes = 64 << 20

// secretDisk is the data folder as the file store of the secrets needs it (secretsfile.Disk, ADR-0076). It keeps the
// contract of that interface:
//   - Create makes a file with the mode 0600, so only its owner can read it, and fails when the name exists.
//   - ReadWhole refuses a file that is not a plain file (a link, a folder), and a file that group or other users can
//     read, and says how to mend it. It looks at the size before it reads and reads no more than the limit. (Where
//     the system has no such modes, Windows, the mode is not looked at: the same rule as the built-in sign-in's.)
//   - A writer holds the lock of the data folder (Lock, taken by whoever opens the disk to write).
//
// A reader (readOnly) holds no lock and can change nothing: not even the leftover of an interrupted write is removed,
// since it may be the temporary file of a write that is going on.
type secretDisk struct {
	fileDisk
	readOnly bool
}

var _ secretsfile.Disk = secretDisk{}

func (d secretDisk) refuseWrite() error {
	return kernel.Errorf(kernel.CodeInternal, "the secrets are opened for reading only")
}

// ReadWhole returns the whole file, after checking what it is, who may read it and how large it is.
func (d secretDisk) ReadWhole(name string) ([]byte, error) {
	return readPrivateFile(d.path(name), maxSecretsFileBytes)
}

// Create makes a new file for writing, for its owner only, and fails when the name exists.
func (d secretDisk) Create(name string) (secretsfile.File, error) {
	if d.readOnly {
		return nil, d.refuseWrite()
	}
	return os.OpenFile(d.path(name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
}

// Rename renames atomically, replacing the target.
func (d secretDisk) Rename(from, to string) error {
	if d.readOnly {
		return d.refuseWrite()
	}
	return d.fileDisk.Rename(from, to)
}

// Remove deletes a file; a reader deletes nothing.
func (d secretDisk) Remove(name string) error {
	if d.readOnly {
		return nil
	}
	return d.fileDisk.Remove(name)
}

// SyncDir makes the creations and renames durable.
func (d secretDisk) SyncDir() error {
	if d.readOnly {
		return nil
	}
	return d.fileDisk.SyncDir()
}

// openSecretDisk opens the folder for the secrets. A reader needs the folder to be there; a writer makes it.
func openSecretDisk(dir string, readOnly bool) (secretDisk, error) {
	if dir == "" {
		return secretDisk{}, kernel.Errorf(kernel.CodeInvalidValue, "the secrets need a data folder: give -data-dir <folder>")
	}
	if readOnly {
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() {
			return secretDisk{}, kernel.Errorf(kernel.CodeNotFound, "the data folder %s does not exist", dir)
		}
		return secretDisk{fileDisk: fileDisk{dir: dir}, readOnly: true}, nil
	}
	disk, err := openDataDir(dir)
	return secretDisk{fileDisk: disk}, err
}

// readPrivateFile reads a file that only its owner may read. The file is looked at before it is opened (a link or a
// folder is refused) and again after (the file opened is the file looked at, and its mode and size are those of the
// open file), so the file cannot be swapped in between. A file that is not there is an error that is fs.ErrNotExist.
func readPrivateFile(path string, limit int64) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s is not a plain file (a link or a folder is not followed); put the file itself there", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, info) || !info.Mode().IsRegular() {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s changed while it was being opened; try again", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s is readable by other users (mode %04o): run chmod 600 %s, then try again", path, info.Mode().Perm(), path)
	}
	if info.Size() > limit {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s is larger than %d bytes", path, limit)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s is larger than %d bytes", path, limit)
	}
	return data, nil
}

// isNotExist says whether an error is a file that is not there.
func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
