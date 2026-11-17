package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// [REQ-498] [REQ-474]
func TestSecretCmdFlowPutListShowRotateRetireDelete(t *testing.T) {
	e := newSecEnv(t)
	out := e.ok(secUser+"\n"+secPass+"\n", "put", "db_main", "-kind", "password", "-data-dir", e.dataDir, "-scope", "src_db", "-host", "db.example.test:5432")
	if !strings.Contains(out, "put the secret db_main in (password): version 1") || !strings.Contains(out, "made the master key file") {
		t.Errorf("put says:\n%s", out)
	}
	if list := e.ok("", "list", "-data-dir", e.dataDir); !strings.Contains(list, "db_main") || !strings.Contains(list, "db.example.test:5432") || !strings.Contains(list, "src_db") {
		t.Errorf("list says:\n%s", list)
	}
	show := e.ok("", "show", "db_main", "-data-dir", e.dataDir)
	for _, want := range []string{"kind:      password", "username, password", "in use:    version 1", "scope:     src_db", "key id:    mk-"} {
		if !strings.Contains(show, want) {
			t.Errorf("show lacks %q:\n%s", want, show)
		}
	}
	rotated := e.ok(secUser+"\n"+secPass2+"\n", "rotate", "db_main", "-data-dir", e.dataDir)
	if !strings.Contains(rotated, "added version 2") {
		t.Errorf("rotate says:\n%s", rotated)
	}
	if got := e.secOpen("db_main", "password"); got["password"] != secPass2 {
		t.Errorf("the open does not give the newest version")
	}
	retired := e.ok("", "retire", "db_main", "-version", "2", "-data-dir", e.dataDir)
	if !strings.Contains(retired, "version 1 is in use") {
		t.Errorf("retire says:\n%s", retired)
	}
	if got := e.secOpen("db_main", "password"); got["password"] != secPass {
		t.Errorf("after the newest is retired the open does not give the version before")
	}
	if del := e.ok("", "delete", "db_main", "-data-dir", e.dataDir); !strings.Contains(del, "deleted the secret db_main") {
		t.Errorf("delete says:\n%s", del)
	}
	if list := e.ok("", "list", "-data-dir", e.dataDir); !strings.Contains(list, "no secrets") {
		t.Errorf("list after delete says:\n%s", list)
	}
}

// [REQ-498]
func TestSecretCmdNoValueAndNoKeyInAnyOutputOrJSON(t *testing.T) {
	e := newSecEnv(t)
	e.put("db_main")
	e.ok("x\n"+secPass2+"\n", "rotate", "db_main", "-data-dir", e.dataDir)
	e.ok("", "retire", "db_main", "-version", "1", "-data-dir", e.dataDir)
	for _, args := range [][]string{
		{"list", "-data-dir", e.dataDir}, {"list", "-data-dir", e.dataDir, "--json"},
		{"show", "db_main", "-data-dir", e.dataDir}, {"show", "db_main", "-data-dir", e.dataDir, "--json"},
		{"set-scope", "db_main", "-scope", "src_a", "-data-dir", e.dataDir, "--json"},
	} {
		e.ok("", args...)
	}
	// errors of the commands that carry a value
	e.fails(exitFailure, secUser+"\n"+secPass+"\n", "put", "db_main", "-kind", "password", "-data-dir", e.dataDir)
	e.fails(exitFailure, secUser+"\n"+secPass+"\n", "put", "bad id", "-kind", "password", "-data-dir", e.dataDir)
	e.fails(exitFailure, secPass+"\n", "put", "t1", "-kind", "password", "-data-dir", e.dataDir)
	e.fails(exitFailure, secPass+"\n"+secPass+"\n"+secPass+"\n", "put", "t1", "-kind", "password", "-data-dir", e.dataDir)
	e.fails(exitFailure, "", "put", "t1", "-kind", "token", "-data-dir", e.dataDir, "-field", "token="+secPass)
	e.fails(exitUsage, "", "put", "t1", "-kind", "token", "-data-dir", e.dataDir, "-field", secPass)
	e.fails(exitUsage, "", "put", "t1", "-kind", "token", "-data-dir", e.dataDir, "-host", "postgres://u:"+secPass+"@db.example.test:5432/x")
	e.fails(exitUsage, "", "put", "t1", "-kind", "token", "-data-dir", e.dataDir, secPass)
	e.fails(exitUsage, "", "put", "t1", "-kind", "token", "-data-dir", e.dataDir, "-token", secPass)
	needles := append(secNeedles([]byte(secPass)), secNeedles([]byte(secPass2))...)
	needles = append(needles, secNeedles([]byte(secUser))...)
	needles = append(needles, secKeyNeedles(t, e.keyFiles()[0])...)
	secAssertAbsent(t, e.outputs, needles)
	raw, err := os.ReadFile(filepath.Join(e.dataDir, "secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	secAssertAbsent(t, []string{string(raw)}, secNeedles([]byte(secPass)))
}

// [REQ-498]
func TestSecretCmdTakesValuesFromFilesAndStripsOneLineBreak(t *testing.T) {
	e := newSecEnv(t)
	dir := t.TempDir()
	userFile, passFile := filepath.Join(dir, "u.txt"), filepath.Join(dir, "p.txt")
	if err := os.WriteFile(userFile, []byte(secUser+"\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passFile, []byte(secPass+" \n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.ok("", "put", "db_main", "-kind", "password", "-data-dir", e.dataDir, "-scope", "src_db", "-host", "db.example.test:5432",
		"-field", "username="+userFile, "-field", "password="+passFile)
	got := e.secOpen("db_main", "username", "password")
	if got["username"] != secUser || got["password"] != secPass+" \n" {
		t.Errorf("the values read from the files are not what the files hold minus one line break")
	}
	// a token on the standard input with no line break at its end
	e.ok(secToken, "put", "api_token", "-kind", "token", "-data-dir", e.dataDir, "-scope", "src_db", "-host", "db.example.test:5432")
	if got := e.secOpen("api_token", "token"); got["token"] != secToken {
		t.Errorf("the token of the standard input is not what was piped in")
	}
}

// [REQ-498]
func TestSecretCmdPutNeverChangesASecretAndRotateNeedsOne(t *testing.T) {
	e := newSecEnv(t)
	e.put("db_main")
	msg := e.fails(exitFailure, secUser+"\n"+secPass2+"\n", "put", "db_main", "-kind", "password", "-data-dir", e.dataDir)
	for _, want := range []string{"praxium secret rotate db_main", "(conflict)"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal lacks %q: %s", want, msg)
		}
	}
	if got := e.secOpen("db_main", "password"); got["password"] != secPass {
		t.Errorf("a refused put changed the secret")
	}
	if msg := e.fails(exitFailure, secUser+"\n"+secPass+"\n", "rotate", "nothing", "-data-dir", e.dataDir); !strings.Contains(msg, "(not_found)") {
		t.Errorf("rotate of a secret that is not there says: %s", msg)
	}
}

// [REQ-498]
func TestSecretCmdScopeAndHostsAreReplacedAndCanBeCleared(t *testing.T) {
	e := newSecEnv(t)
	e.put("db_main")
	e.ok("", "set-scope", "db_main", "-scope", "src_a,src_b", "-data-dir", e.dataDir)
	e.ok("", "set-hosts", "db_main", "-host", "DB2.example.test.:5433", "-host", "[::1]:5432,10.0.0.7:1", "-data-dir", e.dataDir)
	var shown struct {
		Scope []string          `json:"scope"`
		Hosts []secrets.Address `json:"allowed_hosts"`
	}
	if err := json.Unmarshal([]byte(e.ok("", "show", "db_main", "-data-dir", e.dataDir, "--json")), &shown); err != nil {
		t.Fatal(err)
	}
	if strings.Join(shown.Scope, ",") != "src_a,src_b" || len(shown.Hosts) != 3 || shown.Hosts[2].Host != "db2.example.test" {
		t.Errorf("scope %v hosts %v", shown.Scope, shown.Hosts)
	}
	if show := e.ok("", "show", "db_main", "-data-dir", e.dataDir); !strings.Contains(show, "[::1]:5432") {
		t.Errorf("an IPv6 address is shown without its brackets:\n%s", show)
	}
	e.ok("", "set-scope", "db_main", "-scope", "", "-data-dir", e.dataDir)
	e.ok("", "set-hosts", "db_main", "-host", "", "-data-dir", e.dataDir)
	if show := e.ok("", "show", "db_main", "-data-dir", e.dataDir); !strings.Contains(show, "no source may use it") || !strings.Contains(show, "no address allowed") {
		t.Errorf("cleared lists are not said:\n%s", show)
	}
	e.fails(exitUsage, "", "set-scope", "db_main", "-data-dir", e.dataDir)
	e.fails(exitUsage, "", "set-hosts", "db_main", "-data-dir", e.dataDir)
	if msg := e.fails(exitFailure, "", "set-scope", "db_main", "-scope", "bad id", "-data-dir", e.dataDir); !strings.Contains(msg, "(invalid_value)") {
		t.Errorf("a bad source id says: %s", msg)
	}
	if msg := e.fails(exitFailure, "", "set-hosts", "db_main", "-host", "a.example.test:0", "-data-dir", e.dataDir); !strings.Contains(msg, "invalid port") {
		t.Errorf("port 0 says: %s", msg)
	}
	if msg := e.fails(exitUsage, "", "set-hosts", "db_main", "-host", "no-port.example.test", "-data-dir", e.dataDir); !strings.Contains(msg, "host:port") {
		t.Errorf("a host without a port says: %s", msg)
	}
}

// [REQ-498]
func TestSecretCmdBadIdKindAndHostAreRefused(t *testing.T) {
	e := newSecEnv(t)
	in := secUser + "\n" + secPass + "\n"
	cases := []struct {
		args  []string
		code  int
		want  string
		noDir bool
	}{
		{args: []string{"put", "Bad", "-kind", "password"}, code: exitFailure, want: "invalid secret id"},
		{args: []string{"put", "ok_id", "-kind", "certificate"}, code: exitFailure, want: "unknown -kind"},
		{args: []string{"put", "ok_id"}, code: exitUsage, want: "-kind"},
		{args: []string{"put", "-kind", "token"}, code: exitUsage, want: "id of the secret first"},
		{args: []string{"put", "ok_id", "-kind", "password", "-host", "db.example.test:5432,[::1"}, code: exitUsage, want: "host:port"},
		{args: []string{"put", "ok_id", "-kind", "password", "-host", "*.example.test:5432"}, code: exitFailure, want: "(invalid_value)"},
		{args: []string{"put", "ok_id", "-kind", "password", "-scope", "bad id"}, code: exitFailure, want: "(invalid_value)"},
		{args: []string{"retire", "ok_id", "-version", "0"}, code: exitUsage, want: "-version"},
		{args: []string{"frobnicate"}, code: exitUsage, want: "no command", noDir: true},
		{args: []string{}, code: exitUsage, want: "secret needs", noDir: true},
		{args: []string{"list"}, code: exitUsage, want: "-data-dir", noDir: true},
	}
	for _, c := range cases {
		args := c.args
		if !c.noDir {
			args = append(append([]string{}, args...), "-data-dir", e.dataDir)
		}
		msg := e.fails(c.code, in, args...)
		if !strings.Contains(msg, c.want) {
			t.Errorf("%v: %q is not in %s", c.args, c.want, msg)
		}
	}
	if _, err := os.Stat(filepath.Join(e.dataDir, "secrets.json")); err == nil {
		t.Errorf("a refused command made secrets.json")
	}
	if len(e.keyFiles()) != 0 {
		t.Errorf("a refused put made a master key")
	}
}

// [REQ-498] [REQ-474]
func TestSecretCmdAServerThatHoldsTheFolderStopsTheChangesNotTheReads(t *testing.T) {
	e := newSecEnv(t)
	e.put("db_main")
	disk, err := openDataDir(e.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := disk.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unlock() }()
	for _, args := range [][]string{
		{"put", "other", "-kind", "token", "-data-dir", e.dataDir},
		{"rotate", "db_main", "-data-dir", e.dataDir}, {"retire", "db_main", "-version", "1", "-data-dir", e.dataDir},
		{"delete", "db_main", "-data-dir", e.dataDir}, {"set-scope", "db_main", "-scope", "a", "-data-dir", e.dataDir},
		{"set-hosts", "db_main", "-host", "a.example.test:1", "-data-dir", e.dataDir},
	} {
		msg := e.fails(exitFailure, secPass+"\n", args...)
		if !strings.Contains(msg, "in use by another praxium") || !strings.Contains(msg, "Stop it") {
			t.Errorf("%s while the folder is held says: %s", args[0], msg)
		}
	}
	e.ok("", "list", "-data-dir", e.dataDir)
	e.ok("", "show", "db_main", "-data-dir", e.dataDir)
}

// [REQ-498]
func TestSecretCmdReadsOfAFolderThatIsNotThereFail(t *testing.T) {
	e := newSecEnv(t)
	if msg := e.fails(exitFailure, "", "list", "-data-dir", filepath.Join(e.dataDir, "nope")); !strings.Contains(msg, "does not exist") {
		t.Errorf("%s", msg)
	}
	if _, err := os.Stat(filepath.Join(e.dataDir, "nope")); err == nil {
		t.Errorf("a read made the folder")
	}
}

// [REQ-498]
func TestSecretCmdHelpListsTheCommandsAndTheTopHelpNamesSecret(t *testing.T) {
	var top, help, put bytes.Buffer
	if code := execute([]string{"--help"}, &top, &bytes.Buffer{}, wiring{deps: fixedDeps()}); code != exitOK || !strings.Contains(top.String(), "secret ") {
		t.Errorf("the top help has no secret: %d", code)
	}
	if code := execute([]string{"secret", "--help"}, &help, &bytes.Buffer{}, wiring{deps: fixedDeps()}); code != exitOK {
		t.Fatalf("secret --help: %d", code)
	}
	for _, want := range []string{"put", "rotate", "list", "show", "retire", "delete", "set-scope", "set-hosts", "never read back", "terminal", "outside the data folder"} {
		if !strings.Contains(help.String(), want) {
			t.Errorf("secret --help lacks %q", want)
		}
	}
	if code := execute([]string{"secret", "put", "--help"}, &put, &bytes.Buffer{}, wiring{deps: fixedDeps()}); code != exitOK {
		t.Fatalf("secret put --help: %d", code)
	}
	for _, want := range []string{"-field", "-secrets-key-file", "-kind", "-scope", "-host", "example.test"} {
		if !strings.Contains(put.String(), want) {
			t.Errorf("secret put --help lacks %q", want)
		}
	}
	for _, banned := range []string{"-value", "-password ", "-token "} {
		if strings.Contains(put.String(), banned) {
			t.Errorf("secret put has a flag %q that would take a value", banned)
		}
	}
}

// [REQ-498]
func TestSecretCmdTheFlagsOfEveryCommandTakeNoValue(t *testing.T) {
	for name, spec := range secretCommands {
		fs := newSecretFlags(spec, name, &secretFlags{})
		fs.VisitAll(func(f *flag.Flag) {
			switch f.Name {
			case "data-dir", "secrets-key-file", "kind", "scope", "host", "field", "version", "json":
			default:
				t.Errorf("secret %s has the flag -%s, which no one has agreed to", name, f.Name)
			}
		})
	}
}
