//go:build !unix

package main

import (
	"errors"
	"os"
)

// errSyncDirUnsupported is what a system answers to the sync of a folder it cannot sync.
var errSyncDirUnsupported = errors.New("sync of a folder is not supported")

// lockFile on a system without a lock that the system drops at the end of a process: a LOCK file that holds the
// number of a process is the lock, best effort (two processes that start in the same instant can both pass). A
// process that died leaves it behind, and then the folder stays shut until a person removes LOCK (the message names
// the file). Linux and macOS never come here.
func lockFile(f *os.File) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() > 0 {
		return errors.New("LOCK holds the number of a process")
	}
	return nil
}

func unlockFile(f *os.File) { _ = os.Remove(f.Name()) }
