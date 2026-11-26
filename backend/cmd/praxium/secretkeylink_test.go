package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The key files of these tests are made in temporary folders; the values are made up (secrethelper_test.go).

// sklVolume lays out a key file as the volume of secrets of an orchestrator does: the file named by the person is a
// link into "..data", and "..data" is a link to the folder of the moment that holds the plain file. It returns the
// named path and the plain file.
func sklVolume(t *testing.T, root string) (named, plain string) {
	t.Helper()
	vol := filepath.Join(root, "vol")
	stamp := filepath.Join(vol, "..2026_11_26_00_00_00.000000001")
	if err := os.MkdirAll(stamp, 0o700); err != nil {
		t.Fatal(err)
	}
	plain = filepath.Join(stamp, "master.key")
	if code := execute([]string{"secret", "keygen", "-secrets-key-file", plain}, os.Stderr, os.Stderr, wiring{deps: fixedDeps()}); code != exitOK {
		t.Fatalf("keygen: %d", code)
	}
	if err := os.Chmod(plain, 0o400); err != nil {
		t.Fatal(err)
	}
	named = filepath.Join(vol, "master.key")
	if err := os.Symlink(filepath.Base(stamp), filepath.Join(vol, "..data")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..data", "master.key"), named); err != nil {
		t.Fatal(err)
	}
	return named, plain
}

// sklKeyID is the id of the key that sealed a secret.
func sklKeyID(t *testing.T, e *secEnv, id string) string {
	t.Helper()
	var view struct {
		KeyID string `json:"key_id"`
	}
	if err := json.Unmarshal([]byte(e.secOk("", "show", id, "-data-dir", e.dataDir, "--json")), &view); err != nil || view.KeyID == "" {
		t.Fatalf("show: %v", err)
	}
	return view.KeyID
}

// [REQ-707]
// A named key file that is one link is followed to a plain file that only its owner reads, outside the data folder:
// the layout of a volume of secrets works for every command that uses the key, and nothing else is let through.
func TestSklANamedKeyFileThatIsOneLinkIsFollowedAndNothingMore(t *testing.T) {
	secSkipWithoutModes(t)
	e := newSecEnv(t)
	root := t.TempDir()
	named, plain := sklVolume(t, root)
	put := e.secOk(secUser+"\n"+secPass+"\n", "put", "db_main", "-kind", "password", "-data-dir", e.dataDir, "-secrets-key-file", named)
	if strings.Contains(put, "made the master key file") {
		t.Errorf("a key was made beside the one named: %s", put)
	}
	e.secOk("", "set-scope", "db_main", "-scope", "src_db", "-data-dir", e.dataDir, "-secrets-key-file", named)
	key, err := readMasterKey(plain)
	if err != nil || sklKeyID(t, e, "db_main") != key.ID() {
		t.Fatalf("the secret is not sealed by the key behind the link: %v", err)
	}
	// a link to the link is not followed twice
	twice := filepath.Join(root, "twice.key")
	if err := os.Symlink(named, twice); err != nil {
		t.Fatal(err)
	}
	if msg := e.secFails(exitFailure, "", "set-scope", "db_main", "-scope", "a", "-data-dir", e.dataDir, "-secrets-key-file", twice); !strings.Contains(msg, "a link to another link") {
		t.Errorf("a link to a link says: %s", msg)
	}
	// the target that others can read is refused, and neither the key nor its mode's fix names a value
	if err := os.Chmod(plain, 0o440); err != nil {
		t.Fatal(err)
	}
	msg := e.secFails(exitFailure, "", "set-scope", "db_main", "-scope", "a", "-data-dir", e.dataDir, "-secrets-key-file", named)
	if !strings.Contains(msg, "readable by other users") || !strings.Contains(msg, "(secret_master_key_invalid)") {
		t.Errorf("a loose target says: %s", msg)
	}
	if err := os.Chmod(plain, 0o400); err != nil {
		t.Fatal(err)
	}
	secAssertAbsent(t, e.outputs, secKeyNeedles(t, plain))
	// a link whose target is in the data folder, and a link in the data folder, are refused
	inside := filepath.Join(e.dataDir, "copy.key")
	raw, _ := os.ReadFile(plain)
	if err := os.WriteFile(inside, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	toInside := filepath.Join(root, "to-inside.key")
	inLink := filepath.Join(e.dataDir, "link.key")
	for _, link := range [][2]string{{inside, toInside}, {plain, inLink}} {
		if err := os.Symlink(link[0], link[1]); err != nil {
			t.Fatal(err)
		}
		if msg := e.secFails(exitFailure, "", "set-scope", "db_main", "-scope", "a", "-data-dir", e.dataDir, "-secrets-key-file", link[1]); !strings.Contains(msg, "inside the data folder") {
			t.Errorf("%s says: %s", link[1], msg)
		}
	}
	// a link to nothing is refused, and no key is made through it, even by the first put of a new folder
	dangling := filepath.Join(root, "dangling.key")
	if err := os.Symlink(filepath.Join(root, "nothing", "k.key"), dangling); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "data")
	if msg := e.secFails(exitFailure, secUser+"\n"+secPass+"\n", "put", "db_x", "-kind", "password", "-data-dir", other, "-secrets-key-file", dangling); !strings.Contains(msg, "never made through a link") ||
		!strings.Contains(msg, "(secret_master_key_missing)") {
		t.Errorf("a link to nothing says: %s", msg)
	}
	if _, err := os.Stat(filepath.Join(root, "nothing")); err == nil {
		t.Errorf("a key was made through a link")
	}
	// a link through a dead link, and a link through a plain file, cannot be read
	dead := filepath.Join(root, "dead")
	if err := os.Symlink(filepath.Join(root, "nowhere"), dead); err != nil {
		t.Fatal(err)
	}
	for want, target := range map[string]string{"points nowhere": filepath.Join(dead, "k.key"), "cannot look at": filepath.Join(plain, "k.key")} {
		link := filepath.Join(t.TempDir(), "k.key")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if msg := e.secFails(exitFailure, "", "set-scope", "db_main", "-scope", "a", "-data-dir", e.dataDir, "-secrets-key-file", link); !strings.Contains(msg, want) {
			t.Errorf("a link to %s says: %s", target, msg)
		}
	}
}

// [REQ-707]
// The webhook key named with -secrets-key-file is found through one link as the master key is.
func TestSklTheWebhookKeyIsFoundThroughOneLinkToo(t *testing.T) {
	e := newSecEnv(t)
	real := filepath.Join(t.TempDir(), "hooks.key")
	e.secOk("", "webhook-issue", "crm/alerts", "-kind", "hmac", "-data-dir", e.dataDir, "-secrets-key-file", real)
	link := filepath.Join(t.TempDir(), "hooks-link.key")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	e.secOk("", "webhook-rotate", "crm/alerts", "-data-dir", e.dataDir, "-secrets-key-file", link)
	twice := filepath.Join(t.TempDir(), "twice.key")
	if err := os.Symlink(link, twice); err != nil {
		t.Fatal(err)
	}
	if msg := e.secFails(exitFailure, "", "webhook-rotate", "crm/alerts", "-data-dir", e.dataDir, "-secrets-key-file", twice); !strings.Contains(msg, "a link to another link") {
		t.Errorf("a link to a link says: %s", msg)
	}
}

// [REQ-706]
// keygen makes the key file alone, for its owner only, never over what is there and never inside the data folder it is
// given; the secrets put in with it are sealed by its key, and no output holds the key.
func TestSklKeygenMakesTheKeyFileAloneAndNeverOverAnother(t *testing.T) {
	secSkipWithoutModes(t)
	e := newSecEnv(t)
	path := filepath.Join(t.TempDir(), "keys", "deeper", "master.key")
	out := e.secOk("", "keygen", "-secrets-key-file", path)
	key, err := readMasterKey(path)
	if err != nil || !strings.Contains(out, "made the master key file "+path) || !strings.Contains(out, "key id "+key.ID()) {
		t.Fatalf("keygen: %v %s", err, out)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the key file is not for its owner only: %v", info)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the folder made for it: %v", info)
	}
	before, _ := os.ReadFile(path)
	if msg := e.secFails(exitFailure, "", "keygen", "-secrets-key-file", path); !strings.Contains(msg, "is there already") || !strings.Contains(msg, "(conflict)") {
		t.Errorf("a second keygen says: %s", msg)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Errorf("the key file changed")
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("a temporary file stays: %d files", len(entries))
	}
	link := filepath.Join(filepath.Dir(path), "link.key")
	if err := os.Symlink(filepath.Join(filepath.Dir(path), "none.key"), link); err != nil {
		t.Fatal(err)
	}
	if msg := e.secFails(exitFailure, "", "keygen", "-secrets-key-file", link); !strings.Contains(msg, "is there already") {
		t.Errorf("keygen over a link says: %s", msg)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "none.key")); err == nil {
		t.Errorf("a key was made through a link")
	}
	e.secOk(secUser+"\n"+secPass+"\n", "put", "db_main", "-kind", "password", "-data-dir", e.dataDir, "-secrets-key-file", path)
	if sklKeyID(t, e, "db_main") != key.ID() {
		t.Errorf("the secret is not sealed by the key keygen made")
	}
	if msg := e.secFails(exitFailure, "", "keygen", "-secrets-key-file", filepath.Join(e.dataDir, "k.key"), "-data-dir", e.dataDir); !strings.Contains(msg, "inside the data folder") {
		t.Errorf("a place inside the data folder says: %s", msg)
	}
	if msg := e.secFails(exitFailure, "", "keygen", "-secrets-key-file", filepath.Join(t.TempDir(), "k.key"), "-data-dir", filepath.Join(t.TempDir(), "none")); !strings.Contains(msg, "does not exist") {
		t.Errorf("a data folder that is not there says: %s", msg)
	}
	other := filepath.Join(t.TempDir(), "other.key")
	var view struct {
		KeyFile string `json:"key_file"`
		KeyID   string `json:"key_id"`
	}
	if err := json.Unmarshal([]byte(e.secOk("", "keygen", "-secrets-key-file", other, "-data-dir", e.dataDir, "--json")), &view); err != nil || view.KeyFile != other || view.KeyID == "" || view.KeyID == key.ID() {
		t.Errorf("keygen --json: %+v %v", view, err)
	}
	secAssertAbsent(t, e.outputs, secKeyNeedles(t, path))
	secAssertAbsent(t, e.outputs, secKeyNeedles(t, other))
}

// [REQ-706]
// The command line of keygen: the key file is needed, nothing else is taken, and the help says how it is used.
func TestSklKeygenCommandLine(t *testing.T) {
	e := newSecEnv(t)
	for _, args := range [][]string{{"keygen"}, {"keygen", "-secrets-key-file", " "}, {"keygen", "-secrets-key-file", "k.key", "extra"}, {"keygen", "-bogus"}} {
		if msg := e.secFails(exitUsage, "", args...); !strings.Contains(msg, secretKeygenUsage) {
			t.Errorf("%v: %s", args, msg)
		}
	}
	code, out, _ := e.secRun("", "keygen", "--help")
	if code != exitOK || !strings.HasPrefix(out, secretKeygenUsage) || !strings.Contains(out, "example") {
		t.Errorf("help: %d %s", code, out)
	}
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if msg := e.secFails(exitFailure, "", "keygen", "-secrets-key-file", filepath.Join(file, "sub", "k.key")); !strings.Contains(msg, "cannot make the folder") {
		t.Errorf("a file on the way says: %s", msg)
	}
	dead := filepath.Join(t.TempDir(), "dead")
	if err := os.Symlink(filepath.Join(t.TempDir(), "nowhere"), dead); err != nil {
		t.Fatal(err)
	}
	if msg := e.secFails(exitFailure, "", "keygen", "-secrets-key-file", filepath.Join(dead, "k.key")); !strings.Contains(msg, "points nowhere") {
		t.Errorf("a dead link on the way says: %s", msg)
	}
}
