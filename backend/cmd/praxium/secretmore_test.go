package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/ysk6506/praxium/backend/internal/filejournal"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// secRunReader runs `praxium secret <args>` with any reader as the standard input.
func (e *secEnv) secRunReader(in io.Reader, args ...string) (int, string) {
	w := e.w
	w.stdin = in
	var out, errOut bytes.Buffer
	code := execute(append([]string{"secret"}, args...), &out, &errOut, w)
	e.outputs = append(e.outputs, out.String(), errOut.String())
	return code, errOut.String()
}

// [REQ-498]
func TestSecretMoreTheStandardInputIsNotAFileOfDevicesNorTooLongNorBroken(t *testing.T) {
	e := newSecEnv(t)
	args := []string{"put", "t1", "-kind", "token", "-data-dir", e.dataDir}
	if dev, err := os.Open(os.DevNull); err == nil {
		defer func() { _ = dev.Close() }()
		code, msg := e.secRunReader(dev, args...)
		if code != exitFailure || !strings.Contains(msg, "terminal") {
			t.Errorf("a device as the standard input: %d %s", code, msg)
		}
	}
	if code, msg := e.secRunReader(strings.NewReader(strings.Repeat("a", maxSecretStdin+1)), args...); code != exitFailure || !strings.Contains(msg, "longer than") {
		t.Errorf("a standard input that is too long: %d %s", code, msg)
	}
	if code, msg := e.secRunReader(iotest.ErrReader(errors.New("broken")), args...); code != exitFailure || !strings.Contains(msg, "cannot be read") {
		t.Errorf("a standard input that breaks: %d %s", code, msg)
	}
	if code, msg := e.secRunReader(strings.NewReader(""), args...); code != exitFailure || !strings.Contains(msg, "0") {
		t.Errorf("an empty standard input: %d %s", code, msg)
	}
	if _, err := os.Stat(filepath.Join(e.dataDir, "secrets.json")); err == nil {
		t.Errorf("a refused put made secrets.json")
	}
}

// [REQ-498]
func TestSecretMoreFieldFilesThatAreWrong(t *testing.T) {
	e := newSecEnv(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "v.txt")
	if err := os.WriteFile(file, []byte(secToken), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	linked := os.Symlink(file, link) == nil
	base := []string{"put", "t1", "-kind", "token", "-data-dir", e.dataDir}
	cases := []struct {
		fields []string
		code   int
		want   string
	}{
		{[]string{"token=" + file, "token=" + file}, exitFailure, "given twice"},
		{[]string{"to ken=" + dir}, exitUsage, "a -field is not"},
		{[]string{"token=" + dir}, exitFailure, "not a plain file"},
		{[]string{"token=" + filepath.Join(dir, "none")}, exitFailure, "cannot be opened"},
		{[]string{"username=" + file}, exitFailure, "not a field of this kind of secret"},
		{[]string{"user=" + file}, exitUsage, "a -field is not"},
	}
	if linked {
		cases = append(cases, struct {
			fields []string
			code   int
			want   string
		}{[]string{"token=" + link}, exitFailure, "not a plain file"})
	}
	for _, c := range cases {
		args := append([]string{}, base...)
		for _, f := range c.fields {
			args = append(args, "-field", f)
		}
		msg := e.secFails(c.code, "", args...)
		if !strings.Contains(msg, c.want) || strings.Contains(msg, dir) || strings.Contains(msg, secToken) {
			t.Errorf("%v: %s", c.fields, msg)
		}
	}
}

// [REQ-498]
func TestSecretMoreValueFilesThatAreNotPlainFilesAreRefused(t *testing.T) {
	e := newSecEnv(t)
	args := []string{"put", "t1", "-kind", "token", "-data-dir", e.dataDir, "-field"}
	for _, device := range []string{os.DevNull, "/dev/tty", "/dev/stdin"} {
		if _, err := os.Lstat(device); err != nil {
			continue
		}
		msg := e.secFails(exitFailure, "", append(append([]string{}, args...), "token="+device)...)
		if !strings.Contains(msg, "not a plain file") && !strings.Contains(msg, "cannot be opened") {
			t.Errorf("%s: %s", device, msg)
		}
		if strings.Contains(msg, device) {
			t.Errorf("the path of the file is in the message: %s", msg)
		}
	}
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscallMkfifo(fifo); err == nil {
		msg := e.secFails(exitFailure, "", append(append([]string{}, args...), "token="+fifo)...)
		if !strings.Contains(msg, "not a plain file") {
			t.Errorf("a pipe: %s", msg)
		}
	}
}

// [REQ-498]
func TestSecretMoreValuesWithControlCharactersOrAMarkAreRefused(t *testing.T) {
	e := newSecEnv(t)
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	args := []string{"put", "t1", "-kind", "token", "-data-dir", e.dataDir}
	bad := map[string]string{
		"nul": "ab\x00cd" + "\n", "tab": "ab\tcd\n", "inner": "ab\ncd\n", "two": secToken + "\n\n", "del": "ab\x7fcd\n",
		"cr": "ab\rcd\n", "bom": "\xef\xbb\xbf" + secToken + "\n",
	}
	for name, text := range bad {
		msg := e.secFails(exitFailure, "", append(append([]string{}, args...), "-field", "token="+write(name, text))...)
		if !strings.Contains(msg, "the field token") || strings.Contains(msg, "cd") || strings.Contains(msg, secToken) {
			t.Errorf("%s: %s", name, msg)
		}
		if msg := e.secFails(exitFailure, text, args...); !strings.Contains(msg, "the field token") && !strings.Contains(msg, "line(s)") {
			t.Errorf("standard input %s: %s", name, msg)
		}
	}
	if _, err := os.Stat(filepath.Join(e.dataDir, "secrets.json")); err == nil {
		t.Errorf("a refused value made secrets.json")
	}
	// one line break at the end, LF or CRLF, is not part of the value; a space is
	for name, text := range map[string]string{"lf": secToken + "\n", "crlf": secToken + "\r\n", "none": secToken} {
		id := "t_" + name
		e.secOk("", "put", id, "-kind", "token", "-data-dir", e.dataDir, "-scope", "src_db", "-host", "db.example.test:5432", "-field", "token="+write("ok_"+name, text))
		if got := e.secOpen(id, "token"); got["token"] != secToken {
			t.Errorf("%s: the value is not what the file holds minus one line break", name)
		}
	}
	e.secOk(secToken+"\r\n", "put", "t_stdin", "-kind", "token", "-data-dir", e.dataDir, "-scope", "src_db", "-host", "db.example.test:5432")
	if got := e.secOpen("t_stdin", "token"); got["token"] != secToken {
		t.Errorf("a CRLF line on the standard input is not the value without it")
	}
}

// [REQ-498]
func TestSecretMoreWhatWasTypedIsNeverRepeatedByAFlagError(t *testing.T) {
	e := newSecEnv(t)
	e.secPut("db_main")
	typed := "FakeTok3nValueXyz=="
	for _, args := range [][]string{
		{"put", "t2", "-kind", "token", "-data-dir", e.dataDir, "-field", typed},
		{"put", "t2", "-kind", "token", "-data-dir", e.dataDir, "-field", "fakeuser=fakepw99"},
		{"put", "t1", "-kind", "token", "-data-dir", e.dataDir, "-" + typed},
		{"put", "t1", "-kind", "token", "-data-dir", e.dataDir, "-FakeTok3nValueXyz"},
		{"retire", "db_main", "-data-dir", e.dataDir, "-version", "FakeTok3nValueXyz"},
		{"list", "-data-dir", e.dataDir, "-json=FakeTok3nValueXyz"},
		{"put", "t1", "-kind", "token", "-data-dir", e.dataDir, "-field"},
		{"put", "t1", "-kind", "token", "-data-dir", e.dataDir, "-field", "token=" + typed + "/none"},
		{"put", "t1", "-kind", "token", "-data-dir", e.dataDir, "-scope", "FakeTok3nValueXyz=="},
		{"put", "t1", "-kind", "token", "-data-dir", e.dataDir, "-host", "FakeTok3nValueXyz=="},
		{"put", "t1", "-kind", "token", "-data-dir", e.dataDir, typed},
	} {
		code, _, msg := e.secRun("", args...)
		if code == exitOK {
			t.Errorf("%v: no failure", args)
		}
		if strings.Contains(msg, "FakeTok3n") || strings.Contains(msg, "fakeuser") || strings.Contains(msg, "fakepw99") {
			t.Errorf("%v: what was typed is in the message: %s", args, msg)
		}
	}
}

// [REQ-498]
func TestSecretMoreAllVersionsRetiredAndTheSmallAnswers(t *testing.T) {
	e := newSecEnv(t)
	if err := os.MkdirAll(e.dataDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if out := e.secOk("", "list", "-data-dir", e.dataDir, "--json"); !strings.Contains(out, `"secrets": []`) {
		t.Errorf("an empty list in JSON: %s", out)
	}
	e.secPut("db_main")
	out := e.secOk("", "retire", "db_main", "-version", "1", "-data-dir", e.dataDir)
	if !strings.Contains(out, "no version is left") {
		t.Errorf("retire of the last version says: %s", out)
	}
	if show := e.secOk("", "show", "db_main", "-data-dir", e.dataDir); !strings.Contains(show, "every version is retired") {
		t.Errorf("show says: %s", show)
	}
	if list := e.secOk("", "list", "-data-dir", e.dataDir); !strings.Contains(list, "none (all retired)") || !strings.Contains(list, "1 (1 retired)") {
		t.Errorf("list says: %s", list)
	}
	if msg := e.secFails(exitFailure, "", "retire", "db_main", "-version", "1", "-data-dir", e.dataDir); !strings.Contains(msg, "(conflict)") {
		t.Errorf("a version retired twice: %s", msg)
	}
	if msg := e.secFails(exitFailure, "", "show", "nope", "-data-dir", e.dataDir); !strings.Contains(msg, "(not_found)") {
		t.Errorf("show of a secret that is not there: %s", msg)
	}
	if msg := e.secFails(exitFailure, "", "delete", "nope", "-data-dir", e.dataDir); !strings.Contains(msg, "(not_found)") {
		t.Errorf("delete of a secret that is not there: %s", msg)
	}
	if del := e.secOk("", "delete", "db_main", "-data-dir", e.dataDir, "--json"); !strings.Contains(del, `"deleted": true`) {
		t.Errorf("delete in JSON: %s", del)
	}
}

// [REQ-499]
func TestSecretMoreTheDiskAndTheAssemblyAnswerSmallThings(t *testing.T) {
	if _, err := openSecretDisk("", false); err == nil {
		t.Errorf("a disk with no folder")
	}
	reader, err := openSecretDisk(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	if reader.SyncDir() != nil || reader.Remove("x") != nil {
		t.Errorf("a reader syncs and removes nothing without failing")
	}
	other := kernel.Errorf(kernel.CodeInternal, "disk")
	if secretLocked("d", other) != other {
		t.Errorf("another failure goes through as it is")
	}
	if kernel.CodeOf(secretLocked("d", kernel.Errorf(filejournal.CodeLocked, "held"))) != filejournal.CodeLocked {
		t.Errorf("a lock that is held keeps its code")
	}
	e := newSecEnv(t) // hides HOME and the like in its own temporary folder
	got, err := (wiring{}).userConfigDir()()
	want, wantErr := os.UserConfigDir()
	if got != want || (err == nil) != (wantErr == nil) {
		t.Errorf("without a wiring the configuration folder is the system's: %q %v, want %q %v", got, err, want, wantErr)
	}
	if want != "" && !strings.HasPrefix(want, filepath.Dir(e.dataDir)) {
		t.Errorf("the tests do not hide the real configuration folder: %s", want)
	}
}

// secKeysFor is a key provider of the folder of the environment, for the tests of its small answers.
func secKeysFor(t *testing.T, e *secEnv) *fileKeys {
	t.Helper()
	disk, err := openSecretDisk(e.dataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	return &fileKeys{loc: secretLocation{dataDir: e.dataDir}, disk: disk, configDir: func() (string, error) { return e.cfgDir, nil }, bytes: productBytes()}
}

// [REQ-474]
func TestSecretMoreTheKeyProviderIsCarefulWhenItCannotTell(t *testing.T) {
	e := newSecEnv(t)
	if err := os.MkdirAll(e.dataDir, 0o750); err != nil {
		t.Fatal(err)
	}
	k := secKeysFor(t, e)
	k.mayCreate = func(context.Context) (bool, error) { return false, errors.New("the store cannot be read") }
	if _, err := k.MasterKey(); err == nil || len(e.secKeyFiles()) != 0 {
		t.Errorf("a key was made though the store could not be read: %v", err)
	}
	k.mayCreate = nil
	if err := os.Mkdir(filepath.Join(e.dataDir, folderIDFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := k.MasterKey(); kernel.CodeOf(err) != "secret_master_key_invalid" {
		t.Errorf("an identifier that cannot be read: %v", err)
	}
}

// [REQ-474]
func TestSecretMoreTheFolderIdentifierIsMadeOnceAndSaysWhatFailed(t *testing.T) {
	e := newSecEnv(t)
	if err := os.MkdirAll(e.dataDir, 0o750); err != nil {
		t.Fatal(err)
	}
	k := secKeysFor(t, e)
	first, err := k.makeFolderID(filepath.Join(e.dataDir, folderIDFile))
	if err != nil || !validFolderID(first) {
		t.Fatalf("%q %v", first, err)
	}
	again, err := k.makeFolderID(filepath.Join(e.dataDir, folderIDFile))
	if err != nil || again != first {
		t.Errorf("an identifier that is there was replaced: %q, %q (%v)", first, again, err)
	}
	if _, err := k.makeFolderID(filepath.Join(e.dataDir, "no", "such", folderIDFile)); err == nil {
		t.Errorf("an identifier made in a folder that is not there")
	}
}

// [REQ-474]
func TestSecretMorePathsThatCannotBeResolvedAreRefused(t *testing.T) {
	e := newSecEnv(t)
	k := secKeysFor(t, e)
	if _, err := normalizePath(""); err == nil {
		t.Errorf("an empty key path")
	}
	if _, _, err := createMasterKey("bad\x00path", productBytes()); err == nil {
		t.Errorf("a key path that cannot be used")
	}
	k.loc.dataDir = "bad\x00dir"
	if err := k.checkOutside(filepath.Join(t.TempDir(), "k.key")); err == nil {
		t.Errorf("a data folder that cannot be resolved")
	}
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := createMasterKey(filepath.Join(file, "sub", "k.key"), productBytes()); err == nil {
		t.Errorf("a key made in a folder that cannot be made")
	}
}

// [REQ-498]
func TestSecretMoreTheIdOfASecretHasTheGrammarOfTheIdOfASource(t *testing.T) {
	samples := []string{"a", "db_main", "a-b_c9", "A", "1a", "_a", "-a", "a.b", "a b", "a:b", "", strings.Repeat("a", 64), strings.Repeat("a", 65), "é", "a\n"}
	for _, id := range samples {
		if (secrets.ValidateID(id) == nil) != ingest.ValidID(id) {
			t.Errorf("the grammar of a secret id and of a source id differ for a sample of %d bytes", len(id))
		}
	}
}

// [REQ-474]
func TestSecretMorePathsAreMadeWholeWithLinksFollowedInOrder(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "a", "b"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "a", "b"), filepath.Join(base, "ln")); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	got, err := normalizePath(base + sep + "ln" + sep + ".." + sep + "." + sep + "k.key")
	if want := filepath.Join(base, "a", "k.key"); err != nil || got != want {
		t.Errorf("link/.. is %q (%v), want %q", got, err, want)
	}
	t.Chdir(base)
	if got, err := normalizePath(filepath.Join("a", "b", "k.key")); err != nil || got != filepath.Join(base, "a", "b", "k.key") {
		t.Errorf("a relative path is %q (%v)", got, err)
	}
	if err := os.Symlink(filepath.Join(base, "nowhere"), filepath.Join(base, "dead")); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizePath(filepath.Join("dead", "k.key")); err == nil {
		t.Errorf("a link that points nowhere, in the middle of a path")
	}
	if got, err := normalizePath(filepath.Join("new", "folder", "k.key")); err != nil || got != filepath.Join(base, "new", "folder", "k.key") {
		t.Errorf("parts that are not there yet: %q (%v)", got, err)
	}
}

// [REQ-474]
func TestSecretMoreAFileIsReadOnlyWhenItIsPlainSmallAndPrivate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readFileChecked(path, 5, false, "the file"); err == nil || !strings.Contains(err.Error(), "larger than 5") {
		t.Errorf("a file over the limit: %v", err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := readFileChecked(path, 100, true, "the key"); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("a file that others can read: %v", err)
	}
	if _, err := readFileChecked(path, 100, false, "the file"); err != nil {
		t.Errorf("a plain file: %v", err)
	}
}

// [REQ-499]
func TestSecretMoreAnEmptyListIsAnEmptyListInJSON(t *testing.T) {
	e := newSecEnv(t)
	e.secPut("db_main")
	e.secOk("", "delete", "db_main", "-data-dir", e.dataDir)
	if out := e.secOk("", "list", "--json", "-data-dir", e.dataDir); !strings.Contains(out, "[]") {
		t.Errorf("an empty list in JSON: %s", out)
	}
	if out := e.secOk("", "list", "-data-dir", e.dataDir); strings.TrimSpace(out) == "" {
		t.Errorf("an empty list says nothing")
	}
}

// [REQ-474]
func TestSecretMoreOnlyLowerCaseHexMakesAFolderIdentifier(t *testing.T) {
	ok := folderIDPrefix + strings.Repeat("a1", 16)
	if !validFolderID(ok) {
		t.Fatalf("%q", ok)
	}
	for _, bad := range []string{"", folderIDPrefix, folderIDPrefix + strings.Repeat("A1", 16), folderIDPrefix + strings.Repeat("g1", 16), ok + "0", "x-" + strings.Repeat("a1", 16)} {
		if validFolderID(bad) {
			t.Errorf("%q passes", bad)
		}
	}
}
