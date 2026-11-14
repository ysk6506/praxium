package ingest

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// memStateDisk is a folder in memory that can be made to fail at one step of a write.
type memStateDisk struct {
	files    map[string][]byte
	failAt   string
	synced   []string
	renamed  []string
	dirSyncs int
}

func newMemStateDisk() *memStateDisk { return &memStateDisk{files: map[string][]byte{}} }

func (d *memStateDisk) fails(step string) error {
	if d.failAt == step {
		return errors.New("injected failure at " + step)
	}
	return nil
}

func (d *memStateDisk) ReadWhole(name string) ([]byte, error) {
	data, ok := d.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return data, nil
}

func (d *memStateDisk) Create(name string) (StateFile, error) {
	if err := d.fails("create"); err != nil {
		return nil, err
	}
	if _, ok := d.files[name]; ok {
		return nil, fs.ErrExist
	}
	d.files[name] = nil
	return &memStateFile{disk: d, name: name}, nil
}

func (d *memStateDisk) Rename(from, to string) error {
	if err := d.fails("rename"); err != nil {
		return err
	}
	d.renamed = append(d.renamed, from+">"+to)
	d.files[to] = d.files[from]
	delete(d.files, from)
	return nil
}

func (d *memStateDisk) Remove(name string) error {
	if _, ok := d.files[name]; !ok {
		return fs.ErrNotExist
	}
	delete(d.files, name)
	return nil
}

func (d *memStateDisk) SyncDir() error {
	d.dirSyncs++
	return d.fails("syncdir")
}

type memStateFile struct {
	disk *memStateDisk
	name string
	buf  bytes.Buffer
}

func (f *memStateFile) Write(p []byte) (int, error) {
	if err := f.disk.fails("write"); err != nil {
		return 0, err
	}
	return f.buf.Write(p)
}

func (f *memStateFile) Sync() error {
	f.disk.synced = append(f.disk.synced, f.name)
	return f.disk.fails("sync")
}

func (f *memStateFile) Close() error {
	f.disk.files[f.name] = f.buf.Bytes()
	return nil
}

func savedState(run int) *State {
	s := newState()
	s.Run = run
	s.Identity["k"] = "obj_1"
	return s
}

// [REQ-291]
func TestFileStateStoreKeepsAStateAcrossOpens(t *testing.T) {
	disk := newMemStateDisk()
	first, err := OpenFileStateStore(disk)
	if err != nil || first.KG() != "" || first.Sources() != 0 {
		t.Fatalf("a folder without a file must give an empty store: %v", err)
	}
	if err := first.Bind("kg_1"); err != nil {
		t.Fatal(err)
	}
	if err := first.Save(context.Background(), "src_a", savedState(3)); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(disk.synced, ","); got != stateTmpName+","+stateTmpName {
		t.Fatalf("every write must be synced before the rename, got %q", got)
	}
	if _, left := disk.files[stateTmpName]; left {
		t.Fatal("the temporary file must be gone after the rename")
	}
	again, err := OpenFileStateStore(disk)
	if err != nil || again.KG() != "kg_1" || again.Sources() != 1 {
		t.Fatalf("the second open must see what was saved: %v", err)
	}
	st, err := again.Load(context.Background(), "src_a")
	if err != nil || st.Run != 3 || st.Identity["k"] != "obj_1" {
		t.Fatalf("the state must come back as it was saved: %+v %v", st, err)
	}
	if st, err := again.Load(context.Background(), "src_other"); err != nil || st.Run != 0 {
		t.Fatalf("a source that never ran starts empty: %+v %v", st, err)
	}
}

// [REQ-291]
func TestFileStateStoreLeavesTheOldFileWhenAWriteFails(t *testing.T) {
	for _, step := range []string{"create", "write", "sync", "rename"} {
		disk := newMemStateDisk()
		store, err := OpenFileStateStore(disk)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Save(context.Background(), "src_a", savedState(1)); err != nil {
			t.Fatal(err)
		}
		before := string(disk.files[StateFileName])
		disk.failAt = step
		if err := store.Save(context.Background(), "src_a", savedState(2)); err == nil {
			t.Fatalf("%s: a failed write must be an error", step)
		}
		if string(disk.files[StateFileName]) != before {
			t.Fatalf("%s: the old file must stay as it was", step)
		}
		if _, left := disk.files[stateTmpName]; left {
			t.Fatalf("%s: the temporary file must be removed", step)
		}
		disk.failAt = ""
		if st, err := store.Load(context.Background(), "src_a"); err != nil || st.Run != 1 {
			t.Fatalf("%s: after a failed save the state in memory is the old one: %+v %v", step, st, err)
		}
	}
	disk := newMemStateDisk()
	store, _ := OpenFileStateStore(disk)
	disk.failAt = "syncdir"
	if err := store.Bind("kg_1"); err == nil {
		t.Fatal("a folder that cannot be synced is an error")
	}
}

// [REQ-291]
func TestFileStateStoreRefusesAFileItDoesNotKnow(t *testing.T) {
	cases := map[string]struct {
		content string
		code    kernel.Code
	}{
		"not json":      {"hello", kernel.CodeInvalidValue},
		"another file":  {`{"format":"other","version":1}`, kernel.CodeInvalidValue},
		"newer version": {`{"format":"praxium-ingest-state","version":2,"kg_id":"kg_1"}`, kernel.CodeUnsupported},
	}
	for name, c := range cases {
		disk := newMemStateDisk()
		disk.files[StateFileName] = []byte(c.content)
		if _, err := OpenFileStateStore(disk); err == nil || kernel.CodeOf(err) != c.code {
			t.Errorf("%s: got %v, want code %s", name, err, c.code)
		}
		if string(disk.files[StateFileName]) != c.content {
			t.Errorf("%s: the file must not be touched", name)
		}
	}
}
