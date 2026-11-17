package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/secretsfile"
)

func secSkipWithoutModes(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("ADR-0076: this system has no file modes of this kind")
	}
}

// [REQ-499]
func TestSecretDiskMakesFilesForTheOwnerOnly(t *testing.T) {
	secSkipWithoutModes(t)
	dir := t.TempDir()
	disk, err := openSecretDisk(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	f, err := disk.Create("secrets.json.tmp")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	info, err := os.Stat(filepath.Join(dir, "secrets.json.tmp"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the new file has the mode %v (%v), want 0600", info.Mode().Perm(), err)
	}
	if _, err := disk.Create("secrets.json.tmp"); !errors.Is(err, fs.ErrExist) {
		t.Errorf("Create over a name that exists: %v", err)
	}
}

// [REQ-499]
func TestSecretDiskRefusesToReadAFileOthersCanRead(t *testing.T) {
	secSkipWithoutModes(t)
	dir := t.TempDir()
	disk, err := openSecretDisk(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "secrets.json")
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604, 0o660, 0o666, 0o601} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		_, err := disk.ReadWhole("secrets.json")
		if err == nil || !strings.Contains(err.Error(), "chmod 600") || !strings.Contains(err.Error(), path) {
			t.Errorf("mode %04o: the read is not refused with the way to mend it: %v", mode, err)
		}
	}
	for _, mode := range []os.FileMode{0o600, 0o400} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := disk.ReadWhole("secrets.json"); err != nil {
			t.Errorf("mode %04o is refused: %v", mode, err)
		}
	}
	if _, err := disk.ReadWhole("missing.json"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a file that is not there: %v", err)
	}
}

// [REQ-499]
func TestSecretDiskLooksAtTheSizeBeforeItReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, make([]byte, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := readPrivateFile(path, 100); err != nil || len(data) != 100 {
		t.Errorf("a file of exactly the limit: %d bytes, %v", len(data), err)
	}
	if _, err := readPrivateFile(path, 99); err == nil || !strings.Contains(err.Error(), "larger than 99 bytes") {
		t.Errorf("a file over the limit: %v", err)
	}
	if maxSecretsFileBytes != 64<<20 {
		t.Errorf("the limit of secrets.json is not the one of the file store")
	}
}

// [REQ-499]
func TestSecretDiskDoesNotFollowLinks(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(other, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(dir, "secrets.json")); err != nil {
		t.Skip("ADR-0076: this system cannot make links: " + err.Error())
	}
	disk, err := openSecretDisk(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disk.ReadWhole("secrets.json"); err == nil || !strings.Contains(err.Error(), "not a plain file") {
		t.Errorf("a link is followed: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "folder.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := disk.ReadWhole("folder.json"); err == nil {
		t.Errorf("a folder is read")
	}
}

// [REQ-499]
func TestSecretDiskAWriteDoesNotWriteThroughALinkOfTheTemporaryName(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dir, "secrets.json.tmp")); err != nil {
		t.Skip("ADR-0076: this system cannot make links: " + err.Error())
	}
	e := &secEnv{t: t, w: wiring{deps: fixedDeps()}, dataDir: dir, cfgDir: t.TempDir()}
	e.w.configDir = func() (string, error) { return e.cfgDir, nil }
	e.secPut("db_main")
	if data, _ := os.ReadFile(victim); string(data) != "keep" {
		t.Errorf("the write went through the link")
	}
}

// [REQ-499]
func TestSecretDiskAWriteThatFailsLeavesNoTemporaryFile(t *testing.T) {
	e := newSecEnv(t)
	e.secPut("db_main")
	disk, err := openSecretDisk(e.dataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	store, err := secretsfile.Open(disk)
	if err != nil {
		t.Fatal(err)
	}
	// the new file cannot be put in place when its name is a folder that is not empty: the rename fails after the
	// new file was written and synced
	path := filepath.Join(e.dataDir, "secrets.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Delete(context.Background(), "db_main"); err == nil || !strings.Contains(err.Error(), "in place") {
		t.Fatalf("a rename that cannot be done did not fail the write: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Errorf("a write that failed left its temporary file")
	}
}

// [REQ-499]
func TestSecretDiskTheLeftoverOfAnInterruptedWriteIsRemovedByAWriterAndKeptByAReader(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "secrets.json.tmp")
	if err := os.WriteFile(tmp, []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, _ := openSecretDisk(dir, true)
	if _, err := secretsfile.Open(reader); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tmp); err != nil {
		t.Errorf("a reader removed the file of a write that may be going on")
	}
	if _, err := reader.Create("x"); err == nil {
		t.Errorf("a reader creates a file")
	}
	if err := reader.Rename("a", "b"); err == nil {
		t.Errorf("a reader renames a file")
	}
	writer, _ := openSecretDisk(dir, false)
	if _, err := secretsfile.Open(writer); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tmp); err == nil {
		t.Errorf("a writer kept the leftover")
	}
}

// [REQ-499]
func TestSecretDiskARewrittenFileStaysForTheOwnerOnly(t *testing.T) {
	secSkipWithoutModes(t)
	e := newSecEnv(t)
	e.secPut("db_main")
	e.secPut("db_two")
	info, err := os.Stat(filepath.Join(e.dataDir, "secrets.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("secrets.json has the mode %v (%v)", info.Mode().Perm(), err)
	}
	// loose as it is now, nothing reads it: not a list, not a change
	if err := os.Chmod(filepath.Join(e.dataDir, "secrets.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"list"}, {"show", "db_main"}, {"set-scope", "db_main", "-scope", "a"}} {
		msg := e.secFails(exitFailure, "", append(args, "-data-dir", e.dataDir)...)
		if !strings.Contains(msg, "chmod 600") {
			t.Errorf("%s with a loose file says: %s", args[0], msg)
		}
	}
}
