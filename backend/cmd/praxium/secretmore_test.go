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
	base := []string{"put", "t1", "-kind", "token", "-data-dir", e.dataDir}
	cases := []struct {
		fields []string
		want   string
	}{
		{[]string{"token=" + file, "token=" + file}, "given twice"},
		{[]string{"to ken=" + dir}, "(a field)"},
		{[]string{"token=" + dir}, "cannot be read"},
		{[]string{"token=" + filepath.Join(dir, "none")}, "cannot be opened"},
		{[]string{"user=" + file}, "invalid field name"},
	}
	for _, c := range cases {
		args := append([]string{}, base...)
		for _, f := range c.fields {
			args = append(args, "-field", f)
		}
		msg := e.secFails(exitFailure, "", args...)
		if !strings.Contains(msg, c.want) || strings.Contains(msg, dir) || strings.Contains(msg, secToken) {
			t.Errorf("%v: %s", c.fields, msg)
		}
	}
	if safeName(strings.Repeat("a", 41)) != "(a field)" || safeName("ok_name-1") != "ok_name-1" {
		t.Errorf("safeName")
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
	if dir, err := (wiring{}).userConfigDir()(); err != nil && dir != "" {
		t.Errorf("the system's configuration folder: %q %v", dir, err)
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
	if err := k.checkOutside("bad\x00path"); err == nil {
		t.Errorf("a key path that cannot be resolved")
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
