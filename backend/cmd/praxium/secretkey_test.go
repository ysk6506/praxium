package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-474]
func TestSecretKeyTheDefaultPlaceIsOutsideTheDataFolderUnderTheConfigurationFolder(t *testing.T) {
	e := newSecEnv(t)
	e.put("db_main")
	files := e.keyFiles()
	if len(files) != 1 {
		t.Fatalf("key files: %v", files)
	}
	id, err := os.ReadFile(filepath.Join(e.dataDir, folderIDFile))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(e.cfgDir, "praxium", "keys", strings.TrimSpace(string(id))+".key")
	if files[0] != want || !validFolderID(strings.TrimSpace(string(id))) {
		t.Errorf("the key is at %s, want %s", files[0], want)
	}
	if entries, _ := os.ReadDir(e.dataDir); len(entries) > 0 {
		for _, en := range entries {
			if strings.HasSuffix(en.Name(), ".key") {
				t.Errorf("a key file in the data folder: %s", en.Name())
			}
		}
	}
	if info, err := os.Stat(files[0]); err != nil || (info.Mode().Perm() != 0o600 && os.PathSeparator == '/') {
		t.Errorf("the key file has the mode %v (%v)", info.Mode().Perm(), err)
	}
}

// [REQ-474]
func TestSecretKeyAFileThatIsNamedComesBeforeTheDefaultPlace(t *testing.T) {
	e := newSecEnv(t)
	keyFile := filepath.Join(t.TempDir(), "keys", "mine.key")
	e.ok(secUser+"\n"+secPass+"\n", "put", "db_main", "-kind", "password", "-data-dir", e.dataDir, "-secrets-key-file", keyFile)
	if _, err := os.Stat(keyFile); err != nil {
		t.Errorf("the key was not made where it was named: %v", err)
	}
	if len(e.keyFiles()) != 0 {
		t.Errorf("a key was made in the configuration folder as well")
	}
	if _, err := os.Stat(filepath.Join(e.dataDir, folderIDFile)); err == nil {
		t.Errorf("a folder identifier was made though the key was named")
	}
	// without the name the key is not found (the default place has none), with it the secret changes
	e.fails(exitFailure, "", "set-scope", "db_main", "-scope", "a", "-data-dir", e.dataDir)
	e.ok("", "set-scope", "db_main", "-scope", "a", "-data-dir", e.dataDir, "-secrets-key-file", keyFile)
}

// [REQ-474]
func TestSecretKeyInsideTheDataFolderIsRefused(t *testing.T) {
	e := newSecEnv(t)
	if err := os.MkdirAll(filepath.Join(e.dataDir, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	linked := os.Symlink(e.dataDir, link) == nil
	places := []string{
		filepath.Join(e.dataDir, "a.key"),
		filepath.Join(e.dataDir, "sub", "a.key"),
		filepath.Join(e.dataDir, "sub", "..", "a.key"),
		filepath.Join(e.dataDir, "not-yet", "deeper", "a.key"),
		e.dataDir,
	}
	if linked {
		places = append(places, filepath.Join(link, "a.key"))
	}
	for _, place := range places {
		msg := e.fails(exitFailure, secUser+"\n"+secPass+"\n", "put", "db_main", "-kind", "password", "-data-dir", e.dataDir, "-secrets-key-file", place)
		if !strings.Contains(msg, "inside the data folder") {
			t.Errorf("%s: %s", place, msg)
		}
	}
	if _, err := os.Stat(filepath.Join(e.dataDir, "a.key")); err == nil {
		t.Errorf("a key was made inside the data folder")
	}
	// a place beside the data folder is fine
	beside := e.dataDir + "-keys"
	e.ok(secUser+"\n"+secPass+"\n", "put", "db_main", "-kind", "password", "-data-dir", e.dataDir, "-secrets-key-file", filepath.Join(beside, "a.key"))
}

// [REQ-474]
func TestSecretKeyTheDefaultPlaceInsideTheDataFolderIsRefused(t *testing.T) {
	e := newSecEnv(t)
	e.cfgDir = filepath.Join(e.dataDir, "config")
	msg := e.fails(exitFailure, secUser+"\n"+secPass+"\n", "put", "db_main", "-kind", "password", "-data-dir", e.dataDir)
	if !strings.Contains(msg, "inside the data folder") {
		t.Errorf("%s", msg)
	}
}

// [REQ-474]
func TestSecretKeyIsMadeOnlyWhenTheFirstSecretIsPutIn(t *testing.T) {
	e := newSecEnv(t)
	// reads and refused puts make none
	if err := os.MkdirAll(e.dataDir, 0o750); err != nil {
		t.Fatal(err)
	}
	e.ok("", "list", "-data-dir", e.dataDir)
	e.fails(exitFailure, secUser+"\n", "put", "db_main", "-kind", "password", "-data-dir", e.dataDir)
	if len(e.keyFiles()) != 0 {
		t.Fatalf("a refused put made a key")
	}
	e.put("db_main")
	keys := e.keyFiles()
	if len(keys) != 1 {
		t.Fatalf("key files: %v", keys)
	}
	before, _ := os.ReadFile(keys[0])
	// more puts and changes reuse it
	e.ok(secToken, "put", "t1", "-kind", "token", "-data-dir", e.dataDir)
	e.ok("a\nb\n", "rotate", "db_main", "-data-dir", e.dataDir)
	if after, _ := os.ReadFile(keys[0]); string(after) != string(before) || len(e.keyFiles()) != 1 {
		t.Errorf("a key that is there was replaced or another was made")
	}
	// a secret that is there and no key: nothing makes one
	if err := os.Remove(keys[0]); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"put", "t2", "-kind", "token"}, {"rotate", "db_main"}, {"retire", "db_main", "-version", "1"}, {"set-scope", "db_main", "-scope", "a"}, {"set-hosts", "db_main", "-host", "a.example.test:1"}} {
		stdin := "x\ny\n"
		if args[0] == "put" {
			stdin = "x\n"
		}
		msg := e.fails(exitFailure, stdin, append(args, "-data-dir", e.dataDir)...)
		if !strings.Contains(msg, "(secret_master_key_missing)") || !strings.Contains(msg, "put the key file back") || !strings.Contains(msg, "-secrets-key-file") {
			t.Errorf("%s without its key says: %s", args[0], msg)
		}
	}
	if len(e.keyFiles()) != 0 {
		t.Errorf("a command made a key while secrets were stored")
	}
	// reading and deleting need no key; after the last delete a new key is made at the next first put
	e.ok("", "list", "-data-dir", e.dataDir)
	e.ok("", "show", "db_main", "-data-dir", e.dataDir)
	e.ok("", "delete", "db_main", "-data-dir", e.dataDir)
	e.ok("", "delete", "t1", "-data-dir", e.dataDir)
	e.put("db_new")
	if len(e.keyFiles()) != 1 {
		t.Errorf("the first put into an empty store made no key")
	}
}

// [REQ-474]
func TestSecretKeyThatIsThereIsNeverOverwrittenNotEvenByTwoCommandsAtOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "one.key")
	const workers = 8
	ids := make([]string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, _, err := createMasterKey(path, kernel.SystemBytes{})
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = key.ID()
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] || id == "" {
			t.Fatalf("the commands did not end with one key: %v", ids)
		}
	}
	onDisk, err := readMasterKey(path)
	if err != nil || onDisk.ID() != ids[0] {
		t.Errorf("the key on disk is not the one they all use: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("a temporary file stays: %d files", len(entries))
	}
	before, _ := os.ReadFile(path)
	if _, made, err := createMasterKey(path, kernel.SystemBytes{}); err != nil || made {
		t.Errorf("a key that is there was made again: made=%v %v", made, err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Errorf("the key file changed")
	}
}

// [REQ-474]
func TestSecretKeyWithLooseModeOrOfAnotherKindIsRefused(t *testing.T) {
	secSkipWithoutModes(t)
	e := newSecEnv(t)
	e.put("db_main")
	key := e.keyFiles()[0]
	if err := os.Chmod(key, 0o644); err != nil {
		t.Fatal(err)
	}
	msg := e.fails(exitFailure, "", "set-scope", "db_main", "-scope", "a", "-data-dir", e.dataDir)
	if !strings.Contains(msg, "chmod 600") || !strings.Contains(msg, "(secret_master_key_invalid)") {
		t.Errorf("a loose key says: %s", msg)
	}
	if err := os.Chmod(key, 0o600); err != nil {
		t.Fatal(err)
	}
	e.ok("", "set-scope", "db_main", "-scope", "a", "-data-dir", e.dataDir)
	// a link to the key is not followed
	real := key + ".real"
	if err := os.Rename(key, real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, key); err != nil {
		t.Skip("no links here")
	}
	if msg := e.fails(exitFailure, "", "set-scope", "db_main", "-scope", "a", "-data-dir", e.dataDir); !strings.Contains(msg, "not a plain file") {
		t.Errorf("a linked key says: %s", msg)
	}
	// a file that is no key says so without repeating what is in it
	_ = os.Remove(key)
	if err := os.WriteFile(key, []byte("not a key "+secPass), 0o600); err != nil {
		t.Fatal(err)
	}
	msg = e.fails(exitFailure, "", "set-scope", "db_main", "-scope", "a", "-data-dir", e.dataDir)
	if !strings.Contains(msg, "(secret_master_key_invalid)") || strings.Contains(msg, secPass) {
		t.Errorf("a damaged key says: %s", msg)
	}
}

// [REQ-474]
func TestSecretKeyAnotherKeyIsTheMismatchOfTheUnit(t *testing.T) {
	e := newSecEnv(t)
	e.put("db_main")
	other := filepath.Join(t.TempDir(), "other.key")
	e.ok(secToken, "put", "t1", "-kind", "token", "-data-dir", filepath.Join(t.TempDir(), "elsewhere"), "-secrets-key-file", other)
	msg := e.fails(exitFailure, "", "set-scope", "db_main", "-scope", "a", "-data-dir", e.dataDir, "-secrets-key-file", other)
	if !strings.Contains(msg, "(secret_master_key_mismatch)") {
		t.Errorf("another key says: %s", msg)
	}
}

// [REQ-474]
func TestSecretKeyIsFoundAgainWhenTheFolderMoves(t *testing.T) {
	e := newSecEnv(t)
	e.put("db_main")
	moved := filepath.Join(filepath.Dir(e.dataDir), "moved", "deeper")
	if err := os.MkdirAll(filepath.Dir(moved), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(e.dataDir, moved); err != nil {
		t.Fatal(err)
	}
	e.dataDir = moved
	e.ok("x\ny\n", "rotate", "db_main", "-data-dir", e.dataDir)
	if got := e.secOpen("db_main", "password"); got["password"] != "y" {
		t.Errorf("the moved folder does not open with its key")
	}
	if len(e.keyFiles()) != 1 {
		t.Errorf("a moved folder got a second key: %v", e.keyFiles())
	}
}

// [REQ-474]
func TestSecretKeyIdentifierIsRandomAndNotMadeFromThePath(t *testing.T) {
	e := newSecEnv(t)
	e.put("a")
	first, _ := os.ReadFile(filepath.Join(e.dataDir, folderIDFile))
	if err := os.RemoveAll(e.dataDir); err != nil {
		t.Fatal(err)
	}
	e.put("a")
	second, _ := os.ReadFile(filepath.Join(e.dataDir, folderIDFile))
	if string(first) == string(second) || !validFolderID(strings.TrimSpace(string(second))) {
		t.Errorf("the identifier of a folder at the same path is the same again: it comes from the path")
	}
	if len(e.keyFiles()) != 2 {
		t.Errorf("the folders did not get their own keys: %v", e.keyFiles())
	}
	// a damaged identifier and a lost one with secrets there: the key has to be named
	if err := os.WriteFile(filepath.Join(e.dataDir, folderIDFile), []byte("junk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if msg := e.fails(exitFailure, "", "set-scope", "a", "-scope", "x", "-data-dir", e.dataDir); !strings.Contains(msg, "-secrets-key-file") {
		t.Errorf("a damaged identifier says: %s", msg)
	}
	_ = os.Remove(filepath.Join(e.dataDir, folderIDFile))
	if msg := e.fails(exitFailure, "", "set-scope", "a", "-scope", "x", "-data-dir", e.dataDir); !strings.Contains(msg, "(secret_master_key_missing)") {
		t.Errorf("a lost identifier says: %s", msg)
	}
	if _, err := os.Stat(filepath.Join(e.dataDir, folderIDFile)); err == nil {
		t.Errorf("a change made a new identifier for a folder that holds secrets")
	}
}

// [REQ-474]
func TestSecretKeyNoConfigurationFolderAsksForTheFile(t *testing.T) {
	e := newSecEnv(t)
	e.w.configDir = func() (string, error) { return "", errors.New("no home") }
	msg := e.fails(exitFailure, secUser+"\n"+secPass+"\n", "put", "db_main", "-kind", "password", "-data-dir", e.dataDir)
	if !strings.Contains(msg, "-secrets-key-file <file>") || !strings.Contains(msg, "(secret_master_key_missing)") {
		t.Errorf("%s", msg)
	}
	keyFile := filepath.Join(t.TempDir(), "k.key")
	e.ok(secUser+"\n"+secPass+"\n", "put", "db_main", "-kind", "password", "-data-dir", e.dataDir, "-secrets-key-file", keyFile)
}
