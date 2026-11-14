package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

var athToken = regexp.MustCompile(`^pxm_[0-9a-f]{16}_[0-9a-f]{64}$`)

// athRun runs one command line and returns the exit code and both outputs.
func athRun(t *testing.T, d wiring, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := execute(args, &out, &errOut, d)
	return code, out.String(), errOut.String()
}

// athBootstrap makes the first administrator in a new data folder and returns the folder and the token.
func athBootstrap(t *testing.T) (dir, token string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "data")
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	code, out, errOut := athRun(t, d, "admin", "bootstrap", "-name", "root", "-store", "file", "-store-data-dir", dir)
	token = strings.TrimSpace(out)
	if code != exitOK || !athToken.MatchString(token) {
		t.Fatalf("bootstrap: %d %q %q", code, out, errOut)
	}
	return dir, token
}

// athSelfSigned makes a certificate for 127.0.0.1 and the client that trusts it.
func athSelfSigned(t *testing.T) (certPEM, keyPEM string, client *http.Client) {
	t.Helper()
	// A fixed stream: the key lives for one test and protects nothing, and a test that needs no system entropy is
	// the same on every run (REQ-001).
	stream := rand.NewChaCha8([32]byte{'p', 'r', 'a', 'x', 'i', 'u', 'm', ' ', 't', 'e', 's', 't', ' ', 'k', 'e', 'y'})
	key, err := ecdsa.GenerateKey(elliptic.P256(), stream)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "praxium test"},
		NotBefore: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(stream, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	client = &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})), client
}

// athServer is a running production server.
type athServer struct {
	endpoint string
	d        wiring
	stop     func()
	stderr   func() string
	done     chan int
	once     sync.Once
}

// athServe starts a production server on the data folder and waits until it answers.
func athServe(t *testing.T, dir string, files map[string]string, client *http.Client, extra ...string) *athServer {
	t.Helper()
	return athServeWith(t, []string{"-store", "file", "-store-data-dir", dir}, files, client, extra...)
}

// athServeWith is athServe for any store flags. Files the map does not hold are read from the disk (a DSN file).
func athServeWith(t *testing.T, store []string, files map[string]string, client *http.Client, extra ...string) *athServer {
	t.Helper()
	d, listeners, stop := apiDeps(files, nil)
	mapped := d.env.ReadFile
	d.env.ReadFile = func(path string) ([]byte, error) {
		if data, err := mapped(path); err == nil {
			return data, nil
		}
		return os.ReadFile(path)
	}
	d.client = client
	errFile, errText := logFile(t)
	done := make(chan int, 1)
	args := append(append([]string{"api", "serve", "-addr", "127.0.0.1:0"}, store...), extra...)
	go func() { done <- execute(args, &bytes.Buffer{}, errFile, d) }()
	var ln net.Listener
	select {
	case ln = <-listeners:
	case code := <-done:
		t.Fatalf("the server exited with %d: %s", code, errText())
	case <-time.After(30 * time.Second):
		t.Fatalf("the server did not listen: %s", errText())
	}
	scheme := "http://"
	if files["/cert.pem"] != "" {
		scheme = "https://"
	}
	s := &athServer{endpoint: scheme + ln.Addr().String(), d: d, stop: stop, stderr: errText, done: done}
	t.Cleanup(s.shutdown)
	for attempt := 0; attempt < 1000; attempt++ {
		if code, _, _ := s.as("", "health"); code == 0 {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the server never became healthy: %s", errText())
	return nil
}

func (s *athServer) shutdown() {
	s.once.Do(func() {
		s.stop()
		select {
		case <-s.done:
		case <-time.After(10 * time.Second):
		}
	})
}

// as runs a client command with a token ("" for none).
func (s *athServer) as(token string, args ...string) (int, string, string) {
	all := []string{"--endpoint", s.endpoint}
	if token != "" {
		all = append(all, "--token", token)
	}
	var out, errOut bytes.Buffer
	code := execute(append(all, args...), &out, &errOut, s.d)
	return code, out.String(), errOut.String()
}

// [REQ-307]
func TestBootstrapMakesTheFirstAdministratorOnceAndPrintsItsTokenOnce(t *testing.T) {
	dir, token := athBootstrap(t)
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	code, out, errOut := athRun(t, d, "admin", "bootstrap", "-name", "second", "-store", "file", "-store-data-dir", dir)
	if code != exitFailure || out != "" || !strings.Contains(errOut, "administrator already exists") {
		t.Fatalf("a second bootstrap: %d %q %q", code, out, errOut)
	}
	if strings.Contains(errOut, token) {
		t.Fatal("the token is in an error")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	secret := token[strings.LastIndex(token, "_")+1:]
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), token) {
		t.Fatal("the file holds the secret in plain text")
	}
}

// [REQ-311]
func TestTheIdentityFileIsForItsOwnerOnlyAndALooseOneIsRefused(t *testing.T) {
	dir, _ := athBootstrap(t)
	path := filepath.Join(dir, "identity.json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, %v", info.Mode(), err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() == "identity.json.tmp" {
			t.Fatal("a temporary file was left behind")
		}
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	d, listeners, stop := apiDeps(nil, nil)
	defer stop()
	code, _, errOut := athRun(t, d, "api", "serve", "-addr", "127.0.0.1:0", "-store", "file", "-store-data-dir", dir)
	if code != exitFailure || !strings.Contains(errOut, "readable by other users") || !strings.Contains(errOut, "chmod 600") {
		t.Fatalf("a loose file: %d %q", code, errOut)
	}
	select {
	case <-listeners:
		t.Fatal("the server listened")
	default:
	}
}

// [REQ-309] [REQ-311]
func TestAProductionServerSignsInWithTokensAndKeepsThemAcrossARestart(t *testing.T) {
	dir, root := athBootstrap(t)
	srv := athServe(t, dir, nil, nil)
	if code, _, errOut := srv.as("", "whoami"); code == 0 || !strings.Contains(errOut, "auth.token_missing") {
		t.Fatalf("no token: %d %q", code, errOut)
	}
	if code, out, errOut := srv.as(root, "whoami"); code != 0 || !strings.Contains(out, "root") || !strings.Contains(out, "admin") {
		t.Fatalf("whoami: %d %q %q", code, out, errOut)
	}
	if code, _, errOut := srv.as(root, "admin", "user", "create", "ada", "--role", "viewer"); code != 0 {
		t.Fatalf("create user: %d %q", code, errOut)
	}
	code, out, errOut := srv.as(root, "admin", "token", "create", "--owner", "ada", "--role", "viewer", "--expires-in-days", "30", "--json")
	reader := ""
	if i := strings.Index(out, `"token": "`); code == 0 && i >= 0 {
		reader = out[i+len(`"token": "`):]
		reader = reader[:strings.Index(reader, `"`)]
	}
	if !athToken.MatchString(reader) {
		t.Fatalf("create token: %d %q %q", code, out, errOut)
	}
	if code, out, _ := srv.as(reader, "catalog", "roles"); code != 0 || !strings.Contains(out, "viewer") {
		t.Fatalf("the reader reads: %d %q", code, out)
	}
	if code, _, errOut := srv.as(reader, "admin", "user", "list"); code == 0 || !strings.Contains(errOut, "permission.role_missing") {
		t.Fatalf("the reader must not manage: %d %q", code, errOut)
	}
	_, list, _ := srv.as(root, "admin", "token", "list", "--owner", "ada", "--json")
	id := regexp.MustCompile(`"id": "(pxm_[0-9a-f]{16})"`).FindStringSubmatch(list)
	if id == nil {
		t.Fatalf("no token id in %s", list)
	}
	if code, _, errOut := srv.as(root, "admin", "token", "revoke", id[1]); code != 0 {
		t.Fatalf("revoke: %d %q", code, errOut)
	}
	if code, _, errOut := srv.as(reader, "whoami"); code == 0 || !strings.Contains(errOut, "auth.token_invalid") {
		t.Fatalf("a revoked token: %d %q", code, errOut)
	}
	srv.shutdown()
	again := athServe(t, dir, nil, nil)
	if code, _, _ := again.as(root, "whoami"); code != 0 {
		t.Fatalf("the administrator after a restart: %d", code)
	}
	if code, _, _ := again.as(reader, "whoami"); code == 0 {
		t.Fatal("a revoked token works after a restart")
	}
	if code, out, _ := again.as(root, "admin", "user", "list"); code != 0 || !strings.Contains(out, "ada") {
		t.Fatalf("the user after a restart: %d %q", code, out)
	}
	for _, text := range []string{srv.stderr(), again.stderr()} {
		if strings.Contains(text, root) || strings.Contains(text, reader) {
			t.Fatalf("a token is in the log: %s", text)
		}
		if !strings.Contains(text, `"event":"principal_created"`) && !strings.Contains(text, `"auth":"built-in"`) {
			t.Fatalf("the log shows neither the start nor the audit: %s", text)
		}
	}
}

// [REQ-312]
func TestProductionRefusesWhatItMustBeforeItOpensAnything(t *testing.T) {
	dir, _ := athBootstrap(t)
	tests := []struct {
		name string
		args []string
		vars map[string]string
		want string
	}{
		{"the memory store", []string{"-store", "memory"}, nil, "memory store"},
		{"the default store", nil, nil, "memory store"},
		{"a development flag", []string{"-store", "file", "-store-data-dir", dir, "-dev-roles", "admin"}, nil, "-dev-roles belongs to -mode development"},
		{"the development token variable", []string{"-store", "file", "-store-data-dir", dir}, map[string]string{envDevToken: fakeToken}, envDevToken},
		{"a certificate without a key", []string{"-store", "file", "-store-data-dir", dir, "-tls-cert", "/cert.pem"}, nil, "go together"},
		{"plain HTTP on every interface", []string{"-addr", "0.0.0.0:0", "-store", "file", "-store-data-dir", dir}, nil, "not a loopback address"},
		{"plain HTTP on a named host", []string{"-addr", "example.invalid:0", "-store", "file", "-store-data-dir", dir}, nil, "-tls-cert"},
		{"a certificate that cannot be read", []string{"-store", "file", "-store-data-dir", dir, "-tls-cert", "/nowhere", "-tls-key", "/nowhere"}, nil, "TLS certificate"},
		{"a certificate and key that do not match", []string{"-store", "file", "-store-data-dir", dir, "-tls-cert", "/junk", "-tls-key", "/junk"}, nil, "do not make a pair"},
		{"a store with no data folder", []string{"-store", "file"}, nil, "-store-data-dir"},
		{"an empty folder with no administrator", []string{"-store", "file", "-store-data-dir", "EMPTY"}, nil, "praxium admin bootstrap"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, listeners, stop := apiDeps(map[string]string{"/junk": "junk"}, tt.vars)
			defer stop()
			args := make([]string, 0, len(tt.args)+3)
			args = append(args, "api", "serve", "-mode", "production")
			for _, a := range tt.args {
				if a == "EMPTY" {
					a = t.TempDir()
				}
				args = append(args, a)
			}
			code, _, errOut := athRun(t, d, args...)
			if code != exitFailure || !strings.Contains(errOut, tt.want) {
				t.Fatalf("code %d, stderr %q, want it to mention %q", code, errOut, tt.want)
			}
			select {
			case <-listeners:
				t.Fatal("the server listened")
			default:
			}
		})
	}
}

// [REQ-312]
func TestProductionSpeaksTLSWhenItIsGivenACertificate(t *testing.T) {
	dir, root := athBootstrap(t)
	certPEM, keyPEM, client := athSelfSigned(t)
	srv := athServe(t, dir, map[string]string{"/cert.pem": certPEM, "/key.pem": keyPEM}, client, "-tls-cert", "/cert.pem", "-tls-key", "/key.pem")
	if code, out, errOut := srv.as(root, "whoami"); code != 0 || !strings.Contains(out, "root") {
		t.Fatalf("whoami over TLS: %d %q %q", code, out, errOut)
	}
	plain := strings.Replace(srv.endpoint, "https://", "http://", 1)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(plain + "/v1/health/live")
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatal("plain HTTP is answered on a TLS port")
		}
	}
	if !strings.Contains(srv.stderr(), `"tls":true`) {
		t.Fatalf("the start log must say TLS: %s", srv.stderr())
	}
}

// [REQ-312]
func TestBehindAProxyAPlainNonLoopbackAddressStarts(t *testing.T) {
	dir, root := athBootstrap(t)
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	served := make(chan net.Addr, 1)
	d.listen = func(addr string) (net.Listener, error) {
		if addr != "0.0.0.0:0" {
			t.Errorf("addr %q", addr)
		}
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			served <- ln.Addr()
		}
		return ln, err
	}
	done := make(chan int, 1)
	errFile, errText := logFile(t)
	go func() {
		done <- execute([]string{"api", "serve", "-addr", "0.0.0.0:0", "-store", "file", "-store-data-dir", dir, "-behind-proxy"}, &bytes.Buffer{}, errFile, d)
	}()
	var addr net.Addr
	select {
	case addr = <-served:
	case code := <-done:
		t.Fatalf("exit %d: %s", code, errText())
	case <-time.After(10 * time.Second):
		t.Fatal("no listener")
	}
	s := &athServer{endpoint: "http://" + addr.String(), d: d, stop: stop, done: done}
	for attempt := 0; attempt < 1000; attempt++ {
		if code, out, _ := s.as(root, "whoami"); code == 0 && strings.Contains(out, "root") {
			s.shutdown()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("never answered")
}

// [REQ-307] [REQ-313]
func TestIssueTokenAtTheServerGetsAnAdministratorBackInAndNeedsTheFolderFree(t *testing.T) {
	dir, root := athBootstrap(t)
	srv := athServe(t, dir, nil, nil)
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	code, _, errOut := athRun(t, d, "admin", "issue-token", "-owner", "root", "-store", "file", "-store-data-dir", dir)
	if code != exitFailure || !strings.Contains(errOut, "another praxium has this data folder open") {
		t.Fatalf("beside a running server: %d %q", code, errOut)
	}
	srv.shutdown()
	code, out, errOut := athRun(t, d, "admin", "issue-token", "-owner", "root", "-store", "file", "-store-data-dir", dir, "-role", "admin", "-description", "recovery", "-expires-in-days", "7")
	fresh := strings.TrimSpace(out)
	if code != exitOK || !athToken.MatchString(fresh) || fresh == root || strings.Contains(errOut, fresh) {
		t.Fatalf("issue-token: %d %q %q", code, out, errOut)
	}
	if !strings.Contains(errOut, `"event":"token_issued"`) || !strings.Contains(errOut, "(local operator)") {
		t.Fatalf("the issue must be in the audit log: %s", errOut)
	}
	again := athServe(t, dir, nil, nil)
	if code, _, _ := again.as(fresh, "whoami"); code != 0 {
		t.Fatalf("the new token: %d", code)
	}
	again.shutdown()
	for _, args := range [][]string{{"-owner", "nobody"}, {"-owner", "root", "-role", "emperor"}} {
		code, out, errOut := athRun(t, d, append([]string{"admin", "issue-token", "-store", "file", "-store-data-dir", dir}, args...)...)
		if code != exitFailure || out != "" || errOut == "" {
			t.Fatalf("%v: %d %q %q", args, code, out, errOut)
		}
	}
}

// [REQ-313]
func TestTheOfflineAdminCommandsCheckTheirFlags(t *testing.T) {
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	dir := t.TempDir()
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"admin", "bootstrap"}, "-name is required"},
		{[]string{"admin", "bootstrap", "-name", "root"}, "-store file or -store postgres"},
		{[]string{"admin", "bootstrap", "-name", "root", "-store", "memory"}, "-store file or -store postgres"},
		{[]string{"admin", "bootstrap", "-name", "root", "-store", "file"}, "needs -store-data-dir"},
		{[]string{"admin", "bootstrap", "-name", "root", "-store", "postgres"}, "needs -store-dsn-file"},
		{[]string{"admin", "bootstrap", "-name", "Root", "-store", "file", "-store-data-dir", dir}, "name"},
		{[]string{"admin", "bootstrap", "-name", "root", "-store", "file", "-store-data-dir", dir, "-expires-in-days", "400"}, "1 to 365"},
		{[]string{"admin", "bootstrap", "-name", "root", "extra"}, "no arguments"},
		{[]string{"admin", "bootstrap", "-colour"}, "unknown flag"},
		{[]string{"admin", "issue-token"}, "-owner is required"},
	} {
		code, out, errOut := athRun(t, d, tt.args...)
		if code == exitOK || out != "" || !strings.Contains(errOut, tt.want) {
			t.Errorf("%v: %d %q %q, want %q", tt.args, code, out, errOut, tt.want)
		}
	}
	for _, command := range []string{"bootstrap", "issue-token"} {
		code, out, _ := athRun(t, d, "admin", command, "--help")
		if code != exitOK || !strings.HasPrefix(out, "usage: praxium admin "+command) || !strings.Contains(out, "-store-data-dir") {
			t.Errorf("%s --help: %d %q", command, code, out)
		}
	}
	if code, out, _ := athRun(t, d, "admin", "--help"); code != exitOK || !strings.Contains(out, "bootstrap") {
		t.Errorf("admin --help: %d %q", code, out)
	}
}

// [REQ-307] [REQ-311]
func TestProductionOnPostgresBootstrapsServesAndKeepsTheTokens(t *testing.T) {
	dsn := dsnFile(t, pgstore.NewTestDatabase(t))
	store := []string{"-store", "postgres", "-store-dsn-file", dsn}
	d, listeners, stop := apiDeps(nil, nil)
	defer stop()
	d.env.ReadFile = os.ReadFile
	code, _, errOut := athRun(t, d, append([]string{"api", "serve", "-addr", "127.0.0.1:0"}, store...)...)
	if code != exitFailure || !strings.Contains(errOut, "no administrator") || !strings.Contains(errOut, "-store postgres -store-dsn-file "+dsn) {
		t.Fatalf("a database with no administrator: %d %q", code, errOut)
	}
	select {
	case <-listeners:
		t.Fatal("the server listened")
	default:
	}
	code, out, errOut := athRun(t, d, append([]string{"admin", "bootstrap", "-name", "root"}, store...)...)
	root := strings.TrimSpace(out)
	if code != exitOK || !athToken.MatchString(root) {
		t.Fatalf("bootstrap: %d %q %q", code, out, errOut)
	}
	if code, _, errOut = athRun(t, d, append([]string{"admin", "bootstrap", "-name", "other"}, store...)...); code != exitFailure || !strings.Contains(errOut, "already exists") {
		t.Fatalf("a second bootstrap: %d %q", code, errOut)
	}
	srv := athServeWith(t, store, nil, nil)
	if code, out, _ := srv.as(root, "whoami"); code != 0 || !strings.Contains(out, "root") {
		t.Fatalf("whoami: %d %q", code, out)
	}
	srv.shutdown()
	again := athServeWith(t, store, nil, nil)
	if code, _, _ := again.as(root, "whoami"); code != 0 {
		t.Fatalf("after a restart: %d", code)
	}
	if code, _, _ := athRun(t, d, "admin", "issue-token", "-owner", "root", "-store", "postgres", "-store-dsn-file", dsn); code != exitOK {
		t.Fatal("the issue of a token needs no lock on a database")
	}
}

// [REQ-308]
func TestTheSecretsOfTokensComeFromTheRandomSourceOfTheWiring(t *testing.T) {
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	d.random = func(n int) string { return strings.Repeat("ab", n) }
	dir := filepath.Join(t.TempDir(), "data")
	code, out, errOut := athRun(t, d, "admin", "bootstrap", "-name", "root", "-store", "file", "-store-data-dir", dir)
	want := "pxm_" + strings.Repeat("ab", 8) + "_" + strings.Repeat("ab", 32)
	if code != exitOK || strings.TrimSpace(out) != want {
		t.Fatalf("%d %q %q, want the token %s", code, out, errOut, want)
	}
}
