package filejournal

import (
	"bytes"
	"errors"
	"io"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

var errCrash = errors.New("simulated power cut")

type fakeFile struct {
	data   []byte
	synced int
}

type nsOp struct {
	kind string // "create", "remove", "rename"
	a, b string
	file *fakeFile
}

// fakeDisk is a data folder in memory that can be cut at any point. It keeps two views of the folder: what the
// process sees, and what a power cut would leave. A file's bytes are durable up to its last Sync; a creation, a
// rename or a removal is durable once SyncDir has run after it. Crash builds the disk of the next start.
type fakeDisk struct {
	files   map[string]*fakeFile
	durable map[string]*fakeFile
	pending []nsOp
	locked  bool
	dead    bool

	// tick counts the operations that change the folder (a Create, a Write, a Sync, a Rename, a SyncDir, a Remove).
	tick int
	// crashAt is the tick the power goes at (-1: never); with crashAfter the operation happens first.
	crashAt    int
	crashAfter bool
	// byteBudget is how many more bytes may be written before a write is cut (-1: no limit).
	byteBudget int
	written    int
}

func newFakeDisk() *fakeDisk {
	return &fakeDisk{files: map[string]*fakeFile{}, durable: map[string]*fakeFile{}, crashAt: -1, byteBudget: -1}
}

// step counts one operation and says whether it may go on, and whether the power goes right after it.
func (d *fakeDisk) step() (ok bool, cutAfter bool) {
	if d.dead {
		return false, false
	}
	t := d.tick
	d.tick++
	if t == d.crashAt {
		if d.crashAfter {
			return true, true
		}
		d.dead = true
		return false, false
	}
	return true, false
}

func (d *fakeDisk) finish(cutAfter bool) error {
	if cutAfter {
		d.dead = true
		return errCrash
	}
	return nil
}

func (d *fakeDisk) Lock() (func() error, error) {
	if d.locked {
		return nil, kernel.Errorf(CodeLocked, "locked")
	}
	d.locked = true
	return func() error { d.locked = false; return nil }, nil
}

func (d *fakeDisk) List() ([]string, error) {
	var out []string
	for n := range d.files {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

func (d *fakeDisk) Open(name string) (io.ReadCloser, error) {
	f, ok := d.files[name]
	if !ok {
		return nil, errors.New("no such file " + name)
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), f.data...))), nil
}

type fakeHandle struct {
	d    *fakeDisk
	f    *fakeFile
	name string
}

func (d *fakeDisk) Create(name string) (File, error) {
	ok, after := d.step()
	if !ok {
		return nil, errCrash
	}
	if _, exists := d.files[name]; exists {
		return nil, errors.New("exists " + name)
	}
	f := &fakeFile{}
	d.files[name] = f
	d.pending = append(d.pending, nsOp{kind: "create", a: name, file: f})
	return &fakeHandle{d, f, name}, d.finish(after)
}

func (d *fakeDisk) OpenAppend(name string) (File, error) {
	f, ok := d.files[name]
	if !ok {
		return nil, errors.New("no such file " + name)
	}
	return &fakeHandle{d, f, name}, nil
}

func (h *fakeHandle) Write(p []byte) (int, error) {
	d := h.d
	ok, after := d.step()
	if !ok {
		return 0, errCrash
	}
	if d.byteBudget >= 0 && d.written+len(p) > d.byteBudget {
		n := d.byteBudget - d.written
		h.f.data = append(h.f.data, p[:n]...)
		d.written += n
		d.dead = true
		return n, errCrash
	}
	h.f.data = append(h.f.data, p...)
	d.written += len(p)
	return len(p), d.finish(after)
}

func (h *fakeHandle) Sync() error {
	ok, after := h.d.step()
	if !ok {
		return errCrash
	}
	h.f.synced = len(h.f.data)
	return h.d.finish(after)
}

func (h *fakeHandle) Close() error { return nil }

func (d *fakeDisk) Rename(from, to string) error {
	ok, after := d.step()
	if !ok {
		return errCrash
	}
	f, exists := d.files[from]
	if !exists {
		return errors.New("no such file " + from)
	}
	d.files[to] = f
	delete(d.files, from)
	d.pending = append(d.pending, nsOp{kind: "rename", a: from, b: to, file: f})
	return d.finish(after)
}

func (d *fakeDisk) SyncDir() error {
	ok, after := d.step()
	if !ok {
		return errCrash
	}
	d.durable = map[string]*fakeFile{}
	for n, f := range d.files {
		d.durable[n] = f
	}
	d.pending = nil
	return d.finish(after)
}

func (d *fakeDisk) Remove(name string) error {
	ok, after := d.step()
	if !ok {
		return errCrash
	}
	f, exists := d.files[name]
	if !exists {
		return errors.New("no such file " + name)
	}
	delete(d.files, name)
	d.pending = append(d.pending, nsOp{kind: "remove", a: name, file: f})
	return d.finish(after)
}

// tailMode is what a power cut does with the bytes of a file that were written but not synced.
type tailMode int

const (
	tailLost    tailMode = iota // none of them reach the disk
	tailPrefix                  // a prefix of them does (the length is drawn)
	tailAll                     // all of them do
	tailZeros                   // the file is as long as written, but the unsynced part is zeros
	tailGarbage                 // the file is as long as written, but the unsynced part is noise
	tailModes
)

// crash returns the folder the next start finds. keepOps is how many of the creations, renames and removals since
// the last SyncDir reached the disk (they reach it in order); the unsynced bytes follow mode. Noise and lengths
// come from rng, a kernel generator, so a run repeats.
func (d *fakeDisk) crash(keepOps int, mode tailMode, rng kernel.Rng) *fakeDisk {
	ns := map[string]*fakeFile{}
	for n, f := range d.durable {
		ns[n] = f
	}
	for i, op := range d.pending {
		if i >= keepOps {
			break
		}
		switch op.kind {
		case "create":
			ns[op.a] = op.file
		case "remove":
			delete(ns, op.a)
		case "rename":
			delete(ns, op.a)
			ns[op.b] = op.file
		}
	}
	out := newFakeDisk()
	seen := map[*fakeFile]*fakeFile{}
	for n, f := range ns {
		c, ok := seen[f]
		if !ok {
			c = &fakeFile{data: survive(f, mode, rng)}
			c.synced = len(c.data)
			seen[f] = c
		}
		out.files[n] = c
		out.durable[n] = c
	}
	return out
}

func survive(f *fakeFile, mode tailMode, rng kernel.Rng) []byte {
	keep := append([]byte(nil), f.data[:f.synced]...)
	rest := f.data[f.synced:]
	switch mode {
	case tailLost:
	case tailPrefix:
		if len(rest) > 0 {
			keep = append(keep, rest[:rng.Uint64()%uint64(len(rest)+1)]...)
		}
	case tailAll:
		keep = append(keep, rest...)
	case tailZeros:
		keep = append(keep, make([]byte, len(rest))...)
	case tailGarbage:
		for range rest {
			keep = append(keep, byte(rng.Uint64()))
		}
	}
	return keep
}
