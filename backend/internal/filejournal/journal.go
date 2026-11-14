package filejournal

import (
	"sync"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Journal is the journal of one data folder. Open it, call Recover once, then Append and Checkpoint. It is safe for
// concurrent use, though the store above it appends from one place at a time.
type Journal struct {
	mu     sync.Mutex
	disk   Disk
	unlock func() error

	recovered bool
	report    Report
	closed    bool
	// failed is the first write error: the folder may hold half a record, so no further record may follow it in
	// this process. A new Open reads the folder again and finds the truth.
	failed error

	// next is the number of the next record.
	next uint64
	// snapSeq is the last record the newest snapshot holds.
	snapSeq uint64
	// pending is the size of the records since the newest snapshot.
	pending int64

	// active is the open segment; activeName names the segment the next record goes to. An empty name means the next
	// record starts a new segment (clear names a cut segment of the same name to remove first); a name without a file
	// open is a segment that exists and is opened for appending.
	active     File
	activeName string
	clear      string
	buf        []byte
}

// Open takes the lock of the folder and returns a journal that has not read it yet. A folder another live process has
// open is CodeLocked.
func Open(disk Disk) (*Journal, error) {
	unlock, err := disk.Lock()
	if err != nil {
		return nil, err
	}
	return &Journal{disk: disk, unlock: unlock}, nil
}

// usable is the check every write makes.
func (j *Journal) usable() error {
	switch {
	case j.closed:
		return kernel.Errorf(CodeIO, "the journal is closed")
	case j.failed != nil:
		return kernel.Wrap(CodeIO, j.failed, "the journal refuses writes after an earlier failure: restart to recover from the folder")
	case !j.recovered:
		return kernel.Errorf(CodeIO, "the journal has not been recovered yet")
	}
	return nil
}

// fail records a write error and returns it as the error of the journal.
func (j *Journal) fail(err error, what string) error {
	wrapped := kernel.Wrap(CodeIO, err, what)
	if j.failed == nil {
		j.failed = wrapped
	}
	return wrapped
}

// Append writes one record and returns when it is on stable storage: the file is synced, and for the first record of
// a new segment so is the folder. It returns the size of the records since the newest snapshot, so the caller can
// decide when to take the next one. After an error the record may or may not be in the folder, and the journal takes
// no more records.
func (j *Journal) Append(payload []byte) (int64, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.usable(); err != nil {
		return 0, err
	}
	if len(payload) > maxRecord {
		return 0, kernel.Errorf(kernel.CodeInvalidValue, "a record of %d bytes is larger than the limit of %d", len(payload), maxRecord)
	}
	fresh, err := j.openActive()
	if err != nil {
		return 0, err
	}
	j.buf = j.buf[:0]
	if fresh {
		j.buf = append(j.buf, putHeader(segMagic, j.next)...)
	}
	j.buf = appendFrame(j.buf, j.next, payload)
	n, err := j.active.Write(j.buf)
	if err == nil && n != len(j.buf) {
		err = kernel.Errorf(CodeIO, "short write: %d of %d bytes", n, len(j.buf))
	}
	if err != nil {
		return 0, j.fail(err, "cannot write a record")
	}
	if err := j.active.Sync(); err != nil {
		return 0, j.fail(err, "cannot sync the log")
	}
	if fresh {
		if err := j.disk.SyncDir(); err != nil {
			return 0, j.fail(err, "cannot sync the data folder")
		}
	}
	j.next++
	j.pending += int64(len(j.buf))
	return j.pending, nil
}

// openActive makes sure a segment is open for the next record and says whether it is new (so its header goes first).
func (j *Journal) openActive() (fresh bool, err error) {
	if j.active != nil {
		return false, nil
	}
	if j.activeName == "" {
		name := segName(j.next)
		if j.clear != "" {
			if err := j.disk.Remove(j.clear); err != nil {
				return false, j.fail(err, "cannot remove the cut segment "+j.clear)
			}
			if err := j.disk.SyncDir(); err != nil {
				return false, j.fail(err, "cannot sync the data folder")
			}
			j.clear = ""
		}
		f, err := j.disk.Create(name)
		if err != nil {
			return false, j.fail(err, "cannot create "+name)
		}
		j.active, j.activeName = f, name
		return true, nil
	}
	f, err := j.disk.OpenAppend(j.activeName)
	if err != nil {
		return false, j.fail(err, "cannot open "+j.activeName)
	}
	j.active = f
	return false, nil
}

// closeActive closes the open segment; the next record starts a new one.
func (j *Journal) closeActive() error {
	if j.active == nil {
		return nil
	}
	err := j.active.Close()
	j.active, j.activeName = nil, ""
	return err
}

// Close closes the open segment and gives the lock of the folder up. It takes no snapshot: the store above does that
// before it closes.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil
	}
	j.closed = true
	err := j.closeActive()
	if uerr := j.unlock(); err == nil {
		err = uerr
	}
	if err != nil {
		return kernel.Wrap(CodeIO, err, "cannot close the journal")
	}
	return nil
}
