package filejournal

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// state is what the tests keep in the journal: a list of byte strings. A snapshot is the list, and a record is one
// more item, so what a recovery must give back is a list that can be compared with what was written.
type state [][]byte

func encodeState(s state) []byte {
	var out []byte
	for _, p := range s {
		out = binary.BigEndian.AppendUint32(out, uint32(len(p)))
		out = append(out, p...)
	}
	return out
}

func decodeState(b []byte) state {
	var s state
	for len(b) >= 4 {
		n := int(binary.BigEndian.Uint32(b))
		s = append(s, append([]byte(nil), b[4:4+n]...))
		b = b[4+n:]
	}
	return s
}

func (s state) equal(o state) bool {
	if len(s) != len(o) {
		return false
	}
	for i := range s {
		if !bytes.Equal(s[i], o[i]) {
			return false
		}
	}
	return true
}

// recoverState opens the folder and reads it back into a state.
func recoverState(d Disk) (*Journal, state, Report, error) {
	j, err := Open(d)
	if err != nil {
		return nil, nil, Report{}, err
	}
	var s state
	err = j.Recover(func(r io.Reader) error {
		b, err := io.ReadAll(r)
		s = decodeState(b)
		return err
	}, func(p []byte) error {
		s = append(s, append([]byte(nil), p...))
		return nil
	})
	if err != nil {
		_ = j.Close()
		return nil, nil, j.Report(), err
	}
	return j, s, j.Report(), nil
}

// payloads makes n records of different sizes, the same every run.
func payloads(n int, seed uint64) state {
	rng := kernel.NewSeededRng(seed)
	out := make(state, n)
	for i := range out {
		p := make([]byte, 1+int(rng.Uint64()%40))
		for k := range p {
			p[k] = byte(rng.Uint64())
		}
		out[i] = p
	}
	return out
}

// plan is a run of appends with a checkpoint after some of them.
type plan struct {
	items state
	ckpt  map[int]bool
}

// run plays the plan on a journal that was recovered empty. acked is how many appends came back without an error;
// the run stops at the first error, and the item in flight at that moment is items[acked] (if there is one).
func (p plan) run(j *Journal) (acked int) {
	var kept state
	for i, it := range p.items {
		if _, err := j.Append(it); err != nil {
			return i
		}
		kept = append(kept, it)
		acked = i + 1
		if p.ckpt[i] {
			snap := append(state(nil), kept...)
			if err := j.Checkpoint(func(w io.Writer) error {
				_, err := w.Write(encodeState(snap))
				return err
			}); err != nil {
				return acked
			}
		}
	}
	return acked
}

// openEmpty opens a fresh folder and recovers it.
func openEmpty(t *testing.T, d Disk) *Journal {
	t.Helper()
	j, s, _, err := recoverState(d)
	if err != nil {
		t.Fatalf("recover an empty folder: %v", err)
	}
	if len(s) != 0 {
		t.Fatalf("an empty folder recovered %d items", len(s))
	}
	return j
}

// mustRecover recovers a folder that must be whole.
func mustRecover(t *testing.T, d Disk) (*Journal, state, Report) {
	t.Helper()
	j, s, rep, err := recoverState(d)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	return j, s, rep
}

func wantCode(t *testing.T, err error, code kernel.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("want error %s, got none", code)
	}
	if got := kernel.CodeOf(err); got != code {
		t.Fatalf("want error %s, got %s: %v", code, got, err)
	}
}
