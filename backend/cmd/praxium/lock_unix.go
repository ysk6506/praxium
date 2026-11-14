//go:build unix

package main

import (
	"os"
	"syscall"
)

// errSyncDirUnsupported is what a system answers to the sync of a folder it cannot sync.
var errSyncDirUnsupported error = syscall.EINVAL

// lockFile takes an exclusive advisory lock without waiting; the system drops it when the process ends.
func lockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }

func unlockFile(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
