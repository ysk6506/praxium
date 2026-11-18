package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The values of these tests are made up. Nothing here touches the user's configuration folder: the folder is given
// by the wiring, and it is a temporary one.
const (
	secUser  = "svc_demo_user"
	secPass  = "zq-fake-pw-7731"
	secPass2 = "zq-fake-pw-9902"
	secToken = "tok-fake-4410-abcdef"
)

// secEnv is a data folder and a configuration folder in a temporary folder, and the wiring that points at them.
type secEnv struct {
	t       *testing.T
	w       wiring
	dataDir string
	cfgDir  string
	// outputs is everything the commands printed, for the search of what must not be there.
	outputs []string
}

func newSecEnv(t *testing.T) *secEnv {
	t.Helper()
	root := t.TempDir()
	// the real configuration folder of the person who runs the tests stays out of reach, even for a command that is
	// run without the wiring's own folder (a package-wide TestMain would be part of every locked test's fingerprint)
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "AppData", "USERPROFILE"} {
		t.Setenv(name, filepath.Join(root, "home"))
	}
	e := &secEnv{t: t, dataDir: filepath.Join(root, "data"), cfgDir: filepath.Join(root, "config")}
	e.w = wiring{deps: fixedDeps()}
	e.w.configDir = func() (string, error) { return e.cfgDir, nil }
	return e
}

// run runs `praxium secret <args>` with stdin as the standard input.
func (e *secEnv) secRun(stdin string, args ...string) (code int, out, errOut string) {
	e.t.Helper()
	w := e.w
	w.stdin = strings.NewReader(stdin)
	var o, er bytes.Buffer
	code = execute(append([]string{"secret"}, args...), &o, &er, w)
	e.outputs = append(e.outputs, o.String(), er.String())
	return code, o.String(), er.String()
}

// ok runs a command that must succeed and returns what it printed.
func (e *secEnv) secOk(stdin string, args ...string) string {
	e.t.Helper()
	code, out, errOut := e.secRun(stdin, args...)
	if code != exitOK {
		e.t.Fatalf("praxium secret %v: code %d: %s", args[0], code, errOut)
	}
	return out
}

// fails runs a command that must fail with the code and returns its error text.
func (e *secEnv) secFails(want int, stdin string, args ...string) string {
	e.t.Helper()
	code, out, errOut := e.secRun(stdin, args...)
	if code != want {
		e.t.Fatalf("praxium secret %v: code %d, want %d: %s %s", args, code, want, out, errOut)
	}
	return errOut
}

func (e *secEnv) secPut(id string) {
	e.t.Helper()
	e.secOk(secUser+"\n"+secPass+"\n", "put", id, "-kind", "password", "-data-dir", e.dataDir, "-scope", "src_db", "-host", "db.example.test:5432")
}

// keyFiles lists the key files under the configuration folder.
func (e *secEnv) secKeyFiles() []string {
	files, _ := filepath.Glob(filepath.Join(e.cfgDir, "praxium", "keys", "*"+keyFileSuffix))
	return files
}

// keyFileFields reads the key file and gives the strings its bytes are written as, to look for them in the outputs.
func secKeyNeedles(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return secNeedles(secDecode(t, f.Key))
}

func secDecode(t *testing.T, b64 string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// secNeedles are the forms a byte string could be printed in: the bytes, the hex, the base64 (standard and URL).
func secNeedles(raw []byte) []string {
	return []string{string(raw), hex.EncodeToString(raw), base64.StdEncoding.EncodeToString(raw),
		base64.RawStdEncoding.EncodeToString(raw), base64.URLEncoding.EncodeToString(raw), base64.RawURLEncoding.EncodeToString(raw)}
}

// secAssertAbsent fails when any text holds any needle.
func secAssertAbsent(t *testing.T, texts []string, needles []string) {
	t.Helper()
	for _, text := range texts {
		for _, n := range needles {
			if n != "" && strings.Contains(text, n) {
				t.Errorf("a text holds a value or a key that must not be there (%d bytes of it)", len(n))
			}
		}
	}
}

// secOpen opens a secret through the assembly, as the ingestion service will, and returns its fields' values.
func (e *secEnv) secOpen(id string, fields ...string) map[string]string {
	e.t.Helper()
	st, err := openSecretStore(e.w, secretLocation{dataDir: e.dataDir}, secretChange)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	opened, err := st.svc.Opener().Open(context.Background(), id, secrets.Use{SourceID: "src_db", Host: "db.example.test", Port: 5432, Purpose: secrets.PurposeCheck})
	if err != nil {
		e.t.Fatal(err)
	}
	defer opened.Close()
	out := map[string]string{}
	for _, name := range fields {
		v, _ := opened.Value(name)
		out[name] = v.Reveal()
	}
	return out
}
