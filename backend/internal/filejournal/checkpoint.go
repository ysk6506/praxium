package filejournal

import (
	"io"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Checkpoint writes a snapshot of everything up to the last record. write is given the writer of the snapshot's body
// and must write the state as it is after that record; the caller keeps the state still meanwhile. The snapshot is
// written to a temporary file, synced, renamed, and the folder is synced, in that order, so a crash leaves either the
// old files or the new one. Then the log segment is closed (the next record starts a new one) and the files the
// previous snapshot no longer needs are removed: the journal keeps the two newest snapshots and the log after the
// older of them, so a snapshot that turns out damaged can be replaced by the one before it and the log. A failed
// Checkpoint leaves the log whole and the journal usable.
func (j *Journal) Checkpoint(write func(w io.Writer) error) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.usable(); err != nil {
		return err
	}
	seq := j.next - 1
	if seq == 0 || seq == j.snapSeq {
		return nil
	}
	if err := j.writeSnapshot(seq, write); err != nil {
		return err
	}
	j.snapSeq, j.pending = seq, 0
	if err := j.closeActive(); err != nil {
		return j.fail(err, "cannot close the log segment")
	}
	j.retain()
	return nil
}

func (j *Journal) writeSnapshot(seq uint64, write func(io.Writer) error) (err error) {
	tmp := tmpName(seq)
	_ = j.disk.Remove(tmp) // debris of an earlier attempt, if any
	f, err := j.disk.Create(tmp)
	if err != nil {
		return kernel.Wrap(CodeIO, err, "cannot create "+tmp)
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = j.disk.Remove(tmp)
		}
	}()
	w, err := newSnapWriter(f, seq)
	if err != nil {
		return kernel.Wrap(CodeIO, err, "cannot write "+tmp)
	}
	if err = write(w); err != nil {
		return err
	}
	if err = w.finish(); err != nil {
		return kernel.Wrap(CodeIO, err, "cannot write "+tmp)
	}
	if err = f.Sync(); err != nil {
		return kernel.Wrap(CodeIO, err, "cannot sync "+tmp)
	}
	if err = f.Close(); err != nil {
		return kernel.Wrap(CodeIO, err, "cannot close "+tmp)
	}
	if err = j.disk.Rename(tmp, snapName(seq)); err != nil {
		return kernel.Wrap(CodeIO, err, "cannot rename "+tmp)
	}
	if err = j.disk.SyncDir(); err != nil {
		return kernel.Wrap(CodeIO, err, "cannot sync the data folder")
	}
	return nil
}

// retain removes what the two newest snapshots no longer need. Removing is best effort: a file that stays is read
// past by the next recovery and removed by the next checkpoint.
func (j *Journal) retain() {
	f, err := j.list()
	if err != nil {
		return
	}
	prev := uint64(0)
	for i, seq := range f.snaps {
		switch {
		case i == 1:
			prev = seq
		case i > 1:
			_ = j.disk.Remove(snapName(seq))
		}
	}
	for i := 0; i+1 < len(f.segs); i++ {
		if f.segs[i+1].first <= prev+1 {
			_ = j.disk.Remove(f.segs[i].name)
		}
	}
	_ = j.disk.SyncDir()
}
