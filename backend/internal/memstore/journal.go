package memstore

import (
	"io"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Journal is the port that makes the store durable (ADR-0034). A store with a journal is the same state machine as
// one without: every write that changes what is stored is first appended to the journal as one record and applied
// to memory only after the append has returned, and a start replays the journal to the state it had. The journal
// knows bytes only; the file implementation is adapter.filejournal, which this unit does not import: the composition
// root joins them.
type Journal interface {
	// Recover loads the newest whole snapshot through load, then plays the records after it, in order, through apply.
	// apply must be done with the payload when it returns.
	Recover(load func(body io.Reader) error, apply func(payload []byte) error) error
	// Append makes one record durable and returns the size of the records since the newest snapshot.
	Append(record []byte) (pending int64, err error)
	// Checkpoint writes a snapshot of the state as of the last appended record through write.
	Checkpoint(write func(w io.Writer) error) error
	// Close gives the journal up; the store takes no further writes.
	Close() error
}

// DefaultCheckpointBytes is the size of the log since the last snapshot at which a store takes the next one.
const DefaultCheckpointBytes = 128 << 20

// Options are the settings of a store with a journal.
type Options struct {
	// CheckpointBytes is the size of the log at which the store takes a snapshot; 0 means the default.
	CheckpointBytes int64
}

// journal is what the store keeps for its journal; all zero for a store without one.
type journal struct {
	j       Journal
	jErr    error
	closed  bool
	pending int64
	limit   int64
	ckptErr error
}

// Open returns the store that the journal holds: it recovers the journal into an empty store. On an error the journal
// is closed, so the folder is free again.
func Open(j Journal, opts Options) (*Store, error) {
	s := New()
	if err := j.Recover(s.loadSnapshot, s.replay); err != nil {
		_ = j.Close()
		return nil, err
	}
	s.j = j
	s.limit = opts.CheckpointBytes
	if s.limit <= 0 {
		s.limit = DefaultCheckpointBytes
	}
	return s, nil
}

// persist appends the record of a write before the write is applied. After the first failure the store takes no more
// writes: the journal may hold a record that memory does not, and only a restart reads the truth.
func (s *Store) persist(kind byte, v any) error {
	if s.j == nil {
		return nil
	}
	if s.jErr != nil {
		return kernel.Wrap(kernel.CodeInternal, s.jErr, "the store takes no more writes: its journal failed or was closed, restart to recover")
	}
	rec, err := encodeRecord(kind, v)
	if err != nil {
		return err
	}
	pending, err := s.j.Append(rec)
	if err != nil {
		s.jErr = err
		return kernel.Wrap(kernel.CodeInternal, err, "the journal could not make the write durable; the write was not applied")
	}
	s.pending = pending
	return nil
}

// maybeCheckpoint takes a snapshot when the log has grown to the limit. The write lock is held, so the state stands
// still. A failed snapshot leaves the log whole; it is kept in CheckpointError and not tried again by itself.
func (s *Store) maybeCheckpoint() {
	if s.j == nil || s.jErr != nil || s.ckptErr != nil || s.pending < s.limit {
		return
	}
	s.ckptErr = s.j.Checkpoint(s.writeSnapshot)
}

// Checkpoint takes a snapshot now. Readers go on; writers wait.
func (s *Store) Checkpoint() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.j == nil {
		return nil
	}
	err := s.j.Checkpoint(s.writeSnapshot)
	return err
}

// CheckpointError is the error of the last snapshot the store took by itself, or nil.
func (s *Store) CheckpointError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ckptErr
}

// Close takes a last snapshot, so the next start reads one file instead of replaying a log, and closes the journal.
// The store takes no writes afterwards. Without a journal it does nothing.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.j == nil || s.closed {
		return nil
	}
	s.closed = true
	var err error
	if s.jErr == nil {
		err = s.j.Checkpoint(s.writeSnapshot)
	}
	if cerr := s.j.Close(); err == nil {
		err = cerr
	}
	s.jErr = kernel.Errorf(kernel.CodeInternal, "the store is closed")
	return err
}

// replay applies one record of the journal to the state.
func (s *Store) replay(rec []byte) error {
	if len(rec) < 2 || rec[0] != recordVersion {
		return kernel.Errorf(kernel.CodeUnsupported, "a journal record has a format version this build does not know: want %d", recordVersion)
	}
	switch rec[1] {
	case kindCreateKG:
		var w wCreate
		if err := decodeRecord(rec, &w); err != nil {
			return err
		}
		d := &ids{}
		kg := d.kgRow(w.KG)
		if d.err != nil {
			return d.err
		}
		_, err := s.createKG(kg, false)
		return err
	case kindChunk:
		return s.replayChunk(rec)
	case kindAbandon:
		var w wAbandon
		if err := decodeRecord(rec, &w); err != nil {
			return err
		}
		d := &ids{}
		st, err := s.state(d.kg(w.KG))
		if err == nil && d.err == nil {
			st.abandon(d.head(w.Head))
		}
		return firstErr(err, d.err)
	case kindSeal:
		var w wSeal
		if err := decodeRecord(rec, &w); err != nil {
			return err
		}
		d := &ids{}
		st, err := s.state(d.kg(w.KG))
		if err != nil || d.err != nil {
			return firstErr(err, d.err)
		}
		v, head := d.versionRow(w.Version), d.head(w.Head)
		if d.err != nil {
			return d.err
		}
		if err := st.checkSeal(v); err != nil {
			return err
		}
		st.seal(v, w.Order, head)
		return nil
	case kindRestore:
		var w wRestore
		if err := decodeRecord(rec, &w); err != nil {
			return err
		}
		d := &ids{}
		st, err := s.state(d.kg(w.KG))
		if err == nil && d.err == nil {
			st.restore(w.ToSeq, d.head(w.Head), w.Now)
		}
		return firstErr(err, d.err)
	}
	return kernel.Errorf(kernel.CodeUnsupported, "a journal record has kind %d, which this build does not know", rec[1])
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func (s *Store) replayChunk(rec []byte) error {
	var wire wChunk
	if err := decodeRecord(rec, &wire); err != nil {
		return err
	}
	w, err := fromWireChunk(wire)
	if err != nil {
		return err
	}
	st, err := s.state(w.KG)
	if err != nil {
		return err
	}
	seq, err := st.checkChunk(w)
	if err != nil {
		return err
	}
	st.commitChunk(w, seq)
	return nil
}
