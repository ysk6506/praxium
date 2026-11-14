package filejournal

import (
	"io"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Report says what a recovery did.
type Report struct {
	// SnapshotSeq is the last record the snapshot that was loaded holds; 0 when none was loaded.
	SnapshotSeq uint64
	// Replayed is the number of records played after the snapshot, and LastRecord the number of the last one.
	Replayed   int
	LastRecord uint64
	// DroppedTail is true when a record cut by a crash was left out at the end of the log.
	DroppedTail bool
	// DroppedBytes is the size of what was left out at the end of the log.
	DroppedBytes int64
	// SkippedSnapshots names the snapshots that were damaged and passed over for an earlier one.
	SkippedSnapshots []string
}

type segInfo struct {
	name  string
	first uint64
}

// folder is the files of the data folder that belong to the journal.
type folder struct {
	segs  []segInfo // by first record, rising
	snaps []uint64  // newest first
	tmps  []string
}

func (j *Journal) list() (folder, error) {
	names, err := j.disk.List()
	if err != nil {
		return folder{}, kernel.Wrap(CodeIO, err, "cannot list the data folder")
	}
	var f folder
	for _, name := range names {
		kind, n, ok := parseName(name)
		switch {
		case !ok:
		case kind == "log":
			f.segs = append(f.segs, segInfo{name, n})
		case kind == "snap":
			f.snaps = append(f.snaps, n)
		default:
			f.tmps = append(f.tmps, name)
		}
	}
	sort.Slice(f.segs, func(a, b int) bool { return f.segs[a].first < f.segs[b].first })
	sort.Slice(f.snaps, func(a, b int) bool { return f.snaps[a] > f.snaps[b] })
	return f, nil
}

// Recover reads the folder: it loads the newest snapshot that is whole through load (the body of the snapshot, whose
// checksum has already been checked), then plays the records after it through apply in order. apply must not keep the
// payload. A record cut by a crash at the end of the log is left out; damage anywhere else, or records missing, is
// CodeCorrupt and nothing is opened. When it returns without an error the journal takes records and Report says
// what was done.
func (j *Journal) Recover(load func(body io.Reader) error, apply func(payload []byte) error) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	var rep Report
	if j.closed || j.recovered {
		return kernel.Errorf(CodeIO, "the journal is closed or was recovered already")
	}
	f, err := j.list()
	if err != nil {
		return err
	}
	for _, name := range f.tmps {
		_ = j.disk.Remove(name) // leftovers of a snapshot that never became one
	}
	base, err := j.loadSnapshot(f.snaps, load, &rep)
	if err != nil {
		return err
	}
	if err := j.replay(f.segs, base, apply, &rep); err != nil {
		return err
	}
	j.snapSeq, j.recovered = base, true
	rep.SnapshotSeq = base
	j.report = rep
	return nil
}

// loadSnapshot loads the newest whole snapshot. A damaged one is passed over (and reported); a format this build
// does not know, a read that failed, or a snapshot the loader refuses stops the recovery.
func (j *Journal) loadSnapshot(snaps []uint64, load func(io.Reader) error, rep *Report) (uint64, error) {
	for _, seq := range snaps {
		name := snapName(seq)
		if err := j.verifySnapshot(name, seq); err != nil {
			if kernel.CodeOf(err) != CodeCorrupt {
				return 0, err
			}
			rep.SkippedSnapshots = append(rep.SkippedSnapshots, name)
			continue
		}
		rc, err := j.disk.Open(name)
		if err != nil {
			return 0, kernel.Wrap(CodeIO, err, "cannot open "+name)
		}
		s, err := openSnapshotStream(name, rc)
		if err == nil {
			err = load(s)
		}
		rc.Close()
		if err != nil {
			return 0, err
		}
		return seq, nil
	}
	return 0, nil
}

// verifySnapshot reads a whole snapshot once to check its checksum before anything of it is loaded.
func (j *Journal) verifySnapshot(name string, seq uint64) error {
	rc, err := j.disk.Open(name)
	if err != nil {
		return kernel.Wrap(CodeIO, err, "cannot open "+name)
	}
	defer rc.Close()
	s, err := openSnapshotStream(name, rc)
	if err != nil {
		return err
	}
	if s.seq != seq {
		return kernel.Errorf(CodeCorrupt, "snapshot %s holds up to record %d, its name says %d", name, s.seq, seq)
	}
	if _, err := io.Copy(io.Discard, s); err != nil {
		return err
	}
	return nil
}

// relevant drops the segments that hold nothing after the snapshot: a segment ends where the next begins.
func relevant(segs []segInfo, base uint64) []segInfo {
	start := 0
	for start+1 < len(segs) && segs[start+1].first <= base+1 {
		start++
	}
	return segs[start:]
}

// replay plays the log after record base and decides where the next record goes.
func (j *Journal) replay(all []segInfo, base uint64, play func([]byte) error, rep *Report) error {
	apply := func(p []byte) error {
		rep.Replayed++
		return play(p)
	}
	segs := relevant(all, base)
	next := base + 1
	var last segScan
	for i, s := range segs {
		if (i == 0 && s.first > base+1) || (i > 0 && s.first != next) {
			return kernel.Errorf(CodeCorrupt, "records %d to %d are missing: %s starts at record %d", next, s.first-1, s.name, s.first)
		}
		rc, err := j.disk.Open(s.name)
		if err != nil {
			return kernel.Wrap(CodeIO, err, "cannot open "+s.name)
		}
		res, err := scanSegment(s.name, rc, s.first, base, apply)
		rc.Close()
		if err != nil {
			return err
		}
		if res.header != headerOK && i != len(segs)-1 {
			return kernel.Errorf(CodeCorrupt, "the header of %s is damaged and it is not the last segment", s.name)
		}
		j.pending += res.bytes
		next, last = max(next, res.next), res
		if res.torn {
			rep.DroppedTail = true
			rep.DroppedBytes += res.dropped
		}
	}
	rep.LastRecord = next - 1
	j.next = next
	j.chooseTail(segs, last, base)
	return nil
}

// chooseTail decides whether the next record goes after the last good record of the last segment, or starts a new
// segment. A segment that ended in a cut record is never appended to: the new record would sit behind garbage.
func (j *Journal) chooseTail(segs []segInfo, last segScan, base uint64) {
	if len(segs) == 0 {
		return
	}
	tail := segs[len(segs)-1]
	if last.header == headerOK && !last.torn && last.next >= base+1 {
		j.activeName = tail.name
		return
	}
	if tail.first == j.next {
		j.clear = tail.name
	}
}

// Report says what the recovery of this journal did.
func (j *Journal) Report() Report {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.report
}
