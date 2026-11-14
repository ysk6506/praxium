package identityfile

import (
	"bytes"
	"io/fs"
	"sync"
)

// MemDisk is a Disk in memory, for the tests of this store and of what runs on it. It can be told to fail a step, and
// it remembers the mode a file was asked for.
type MemDisk struct {
	mu    sync.Mutex
	files map[string][]byte
	// Fail names the steps that fail: "create", "write", "sync", "close", "rename", "syncdir", "read" or "remove".
	Fail map[string]error
}

var _ Disk = (*MemDisk)(nil)

// NewMemDisk makes an empty folder.
func NewMemDisk() *MemDisk { return &MemDisk{files: map[string][]byte{}, Fail: map[string]error{}} }

func (d *MemDisk) failure(step string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.Fail[step]
}

// Put puts a file in the folder, as a test that starts from a file on disk does.
func (d *MemDisk) Put(name string, data []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.files[name] = append([]byte(nil), data...)
}

// Bytes returns a file of the folder, and whether it is there.
func (d *MemDisk) Bytes(name string) ([]byte, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	data, ok := d.files[name]
	return append([]byte(nil), data...), ok
}

// Names lists the files of the folder.
func (d *MemDisk) Names() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for name := range d.files {
		out = append(out, name)
	}
	return out
}

// ReadWhole returns a file.
func (d *MemDisk) ReadWhole(name string) ([]byte, error) {
	if err := d.failure("read"); err != nil {
		return nil, err
	}
	data, ok := d.Bytes(name)
	if !ok {
		return nil, fs.ErrNotExist
	}
	return data, nil
}

// Create makes a new file.
func (d *MemDisk) Create(name string) (File, error) {
	if err := d.failure("create"); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.files[name]; ok {
		return nil, fs.ErrExist
	}
	d.files[name] = nil
	return &memFile{disk: d, name: name}, nil
}

// Rename moves a file over another.
func (d *MemDisk) Rename(from, to string) error {
	if err := d.failure("rename"); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	data, ok := d.files[from]
	if !ok {
		return fs.ErrNotExist
	}
	d.files[to] = data
	delete(d.files, from)
	return nil
}

// Remove deletes a file.
func (d *MemDisk) Remove(name string) error {
	if err := d.failure("remove"); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.files[name]; !ok {
		return fs.ErrNotExist
	}
	delete(d.files, name)
	return nil
}

// SyncDir does nothing but may fail.
func (d *MemDisk) SyncDir() error { return d.failure("syncdir") }

type memFile struct {
	disk *MemDisk
	name string
	buf  bytes.Buffer
}

func (f *memFile) Write(p []byte) (int, error) {
	if err := f.disk.failure("write"); err != nil {
		return 0, err
	}
	return f.buf.Write(p)
}

func (f *memFile) Sync() error { return f.disk.failure("sync") }

func (f *memFile) Close() error {
	f.disk.mu.Lock()
	f.disk.files[f.name] = append([]byte(nil), f.buf.Bytes()...)
	f.disk.mu.Unlock()
	return f.disk.failure("close")
}
