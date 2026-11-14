package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/filejournal"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/memstore"
)

// openFileStore opens the store of a data folder on the real disk.
func openFileStore(t *testing.T, dir string, ckpt int64) *memstore.Store {
	t.Helper()
	disk, err := openDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	j, err := filejournal.Open(disk)
	if err != nil {
		t.Fatal(err)
	}
	s, err := memstore.Open(j, memstore.Options{CheckpointBytes: ckpt})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// load puts a small world in a store and returns the KG.
func load(t *testing.T, s graph.Store, sites int) graph.KG {
	t.Helper()
	ctx := context.Background()
	fx, err := graph.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	clock := kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(3))
	kg, err := graph.NewTestKG(ctx, s, fx.Reg, ids, clock, graph.KGSpec{Name: "prod", Role: graph.RoleProduction})
	if err != nil {
		t.Fatal(err)
	}
	a, err := graph.NewApplier(s, fx.Reg, graph.NewTestKeyProvider("disk"), clock, ids, graph.Options{Semantic: 1, ChunkRows: 5})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := a.Acquire(ctx, kg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Apply(ctx, kg.ID, lease, fx.World(sites, 2, 3)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Seal(ctx, kg.ID, lease, graph.SealSync); err != nil {
		t.Fatal(err)
	}
	return kg
}

func headOf(t *testing.T, s graph.Store, kg graph.KGID) graph.Head {
	t.Helper()
	h, err := s.Head(context.Background(), kg)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// A store on a real folder keeps its KG across a close and across a kill, with and without snapshots.
// [REQ-271]
func TestAFileStoreKeepsItsKGInARealFolder(t *testing.T) {
	for _, ckpt := range []int64{0, 1} {
		dir := filepath.Join(t.TempDir(), "data", "kg")
		s := openFileStore(t, dir, ckpt)
		kg := load(t, s, 2)
		want := headOf(t, s, kg.ID)
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		s = openFileStore(t, dir, ckpt)
		got := headOf(t, s, kg.ID)
		if got.AppliedSeq != want.AppliedSeq || got.Hash.Sum != want.Hash.Sum {
			t.Fatalf("checkpoint %d: head %+v, want %+v", ckpt, got, want)
		}
		if vs, _ := s.Versions(context.Background(), kg.ID); len(vs) != 1 {
			t.Fatalf("%d versions", len(vs))
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// A process that is killed leaves the log as it was: the next start reads it, and a record cut at the end is dropped.
// [REQ-272]
func TestAFileStoreAfterAKillAndACutRecord(t *testing.T) {
	dir := t.TempDir()
	s := openFileStore(t, dir, 0)
	kg := load(t, s, 1)
	want := headOf(t, s, kg.ID)
	// Kill: the process ends without Close. Its lock goes with it; here the files are copied and the copy opened.
	copyDir := t.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(e.Name(), "log-") {
			b = append(b, bytes.Repeat([]byte{0xEE}, 50)...)
		}
		if err := os.WriteFile(filepath.Join(copyDir, e.Name()), b, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	s2 := openFileStore(t, copyDir, 0)
	if got := headOf(t, s2, kg.ID); got.AppliedSeq != want.AppliedSeq || got.Hash.Sum != want.Hash.Sum {
		t.Fatalf("head after a kill %+v, want %+v", got, want)
	}
	if err := s2.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

// Two stores cannot share a folder; a lock file that a dead process left behind does not shut the folder.
// [REQ-273]
func TestTheLockOfADataFolder(t *testing.T) {
	dir := t.TempDir()
	disk, err := openDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	j, err := filejournal.Open(disk)
	if err != nil {
		t.Fatal(err)
	}
	_, err = filejournal.Open(disk)
	if kernel.CodeOf(err) != filejournal.CodeLocked || !strings.Contains(err.Error(), "in use by another process") {
		t.Fatalf("a second open: %v", err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	// A dead process: the file is there with its number, nobody holds the lock.
	if err := os.WriteFile(filepath.Join(dir, lockFileName), []byte("99999\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	j2, err := filejournal.Open(disk)
	if err != nil {
		t.Fatalf("a lock file nobody holds shut the folder: %v", err)
	}
	if err := j2.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := openDataDir(""); err == nil {
		t.Fatal("an empty folder name was accepted")
	}
	file := filepath.Join(dir, "afile")
	if err := os.WriteFile(file, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := openDataDir(filepath.Join(file, "inside")); err == nil {
		t.Fatal("a folder below a file was accepted")
	}
}

// The calls of the disk do what the journal asks of them.
// [REQ-271]
func TestTheFileDiskDoesWhatTheJournalAsks(t *testing.T) {
	dir := t.TempDir()
	d, err := openDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	f, err := d.Create("a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Create("a"); err == nil {
		t.Fatal("Create replaced a file")
	}
	g, err := d.OpenAppend("a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Write([]byte("two")); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := d.Rename("a", "b"); err != nil {
		t.Fatal(err)
	}
	if err := d.SyncDir(); err != nil {
		t.Fatal(err)
	}
	names, err := d.List()
	if err != nil || len(names) != 1 || names[0] != "b" {
		t.Fatalf("list %v %v", names, err)
	}
	r, err := d.Open("b")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if string(got) != "onetwo" {
		t.Fatalf("content %q", got)
	}
	if err := d.Remove("b"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.OpenAppend("b"); err == nil {
		t.Fatal("OpenAppend made a file")
	}
	if _, err := d.Open("b"); err == nil {
		t.Fatal("Open found a removed file")
	}
	if err := (fileDisk{dir: filepath.Join(dir, "gone")}).SyncDir(); err == nil {
		t.Fatal("a folder that is not there was synced")
	}
	if _, err := (fileDisk{dir: filepath.Join(dir, "gone")}).List(); err == nil {
		t.Fatal("a folder that is not there was listed")
	}
	if _, err := (fileDisk{dir: filepath.Join(dir, "gone")}).Lock(); kernel.CodeOf(err) != filejournal.CodeIO {
		t.Fatalf("lock in a folder that is not there: %v", err)
	}
}
