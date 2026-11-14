package filejournal

import "io"

// File is a file opened for appending.
type File interface {
	io.Writer
	// Sync makes everything written so far durable. After an error nothing about the file may be assumed.
	Sync() error
	Close() error
}

// Disk is the data folder as the journal needs it, and nothing more: the names are plain file names inside the
// folder, never paths. The composition root implements it over the operating system; the tests implement it over
// memory and make it fail.
type Disk interface {
	// Lock takes the exclusive lock of the folder and returns the function that gives it up. It fails at once
	// (CodeLocked) while a live process holds it. A lock that a process that has died held must not keep the folder
	// shut: the real disk uses a lock the operating system takes back when the process ends.
	Lock() (unlock func() error, err error)
	// List returns the names of the files of the folder, in any order.
	List() ([]string, error)
	// Open opens a file for reading.
	Open(name string) (io.ReadCloser, error)
	// Create makes a new file for appending and fails if the name exists.
	Create(name string) (File, error)
	// OpenAppend opens an existing file for appending at its end.
	OpenAppend(name string) (File, error)
	// Rename renames a file atomically, replacing the target if there is one.
	Rename(from, to string) error
	// SyncDir makes the creations, renames and removals made so far durable.
	SyncDir() error
	// Remove deletes a file.
	Remove(name string) error
}
