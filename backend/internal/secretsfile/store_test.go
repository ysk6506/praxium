package secretsfile

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The values are made up for the tests. A failing test never prints one.
const (
	testUser     = "demo-user"
	testPassword = "not-a-real-password-for-tests"
)

var ctx = context.Background()

var dbAddress = secrets.Address{Host: "db.example.test", Port: 5432}

var okUse = secrets.Use{SourceID: "crm", Host: dbAddress.Host, Port: dbAddress.Port, Purpose: secrets.PurposeRun}

func fields(password string) secrets.Fields {
	return secrets.NewFields(map[string]string{"username": testUser, "password": password})
}

func key() *secrets.MasterKey { return secrets.GenerateMasterKey(kernel.NewSeededBytes(99)) }

func open(t *testing.T, disk Disk) *Store {
	t.Helper()
	st, err := Open(disk)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return st
}

func newService(t *testing.T, st secrets.Store, k *secrets.MasterKey) *secrets.Service {
	t.Helper()
	svc, err := secrets.New(secrets.Config{Store: st, Keys: secrets.StaticKey(k), Clock: kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)), Bytes: kernel.NewSeededBytes(1)})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func create(t *testing.T, svc *secrets.Service, id string) {
	t.Helper()
	if _, err := svc.Create(ctx, secrets.CreateInput{ID: id, Kind: secrets.KindPassword, Fields: fields(testPassword),
		Scope: []string{"crm"}, AllowedHosts: []secrets.Address{dbAddress}}); err != nil {
		t.Fatal(err)
	}
}

func wantCode(t *testing.T, err error, want kernel.Code) {
	t.Helper()
	if err == nil || kernel.CodeOf(err) != want {
		t.Fatalf("want code %s, got %v", want, err)
	}
}

func versions(t *testing.T, st secrets.Store, id string) int {
	t.Helper()
	rec, ok, err := st.Get(ctx, id)
	if err != nil || !ok {
		t.Fatalf("get %q: %v %v", id, ok, err)
	}
	return len(rec.Versions)
}

// [REQ-403]
func TestAFolderWithoutTheFileIsEmptyAndNothingIsWrittenUntilTheFirstWrite(t *testing.T) {
	disk := NewMemDisk()
	st := open(t, disk)
	if list, _ := st.List(ctx); len(list) != 0 || len(disk.Names()) != 0 {
		t.Fatalf("an empty folder: %d secrets, files %v", len(list), disk.Names())
	}
	create(t, newService(t, st, key()), "crm-db")
	if names := disk.Names(); len(names) != 1 || names[0] != FileName {
		t.Fatalf("files after a write: %v", names)
	}
}

// [REQ-403]
func TestWhatWasWrittenOpensAfterTheFileIsOpenedAgain(t *testing.T) {
	disk := NewMemDisk()
	k := key()
	svc := newService(t, open(t, disk), k)
	create(t, svc, "crm-db")
	create(t, svc, "crm-api")
	if _, err := svc.AddVersion(ctx, "crm-db", fields("not-a-real-password-v2")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetScope(ctx, "crm-api", []string{"crm", "billing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RetireVersion(ctx, "crm-api", 1); err != nil {
		t.Fatal(err)
	}
	before, _ := svc.List(ctx)

	again := newService(t, open(t, disk), k)
	after, _ := again.List(ctx)
	x, _ := json.Marshal(before)
	y, _ := json.Marshal(after)
	if string(x) != string(y) {
		t.Fatal("the reopened store describes other secrets")
	}
	o, err := again.Open(ctx, "crm-db", okUse)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	pw, _ := o.Value("password")
	if o.Version() != 2 || pw.Reveal() != "not-a-real-password-v2" {
		t.Fatal("the reopened store opens another version")
	}
	_, err = again.Open(ctx, "crm-api", okUse)
	wantCode(t, err, secrets.CodeVersionRetired)
}

// [REQ-403] [REQ-404]
func TestTheFileHoldsNoValueInTheClearAndHasTheFormatAndTheVersion(t *testing.T) {
	disk := NewMemDisk()
	k := key()
	create(t, newService(t, open(t, disk), k), "crm-db")
	raw, ok := disk.Bytes(FileName)
	if !ok {
		t.Fatal("no file")
	}
	for _, v := range []string{testPassword, testUser, string(k.Encode())} {
		for _, form := range []string{v, hex.EncodeToString([]byte(v)), base64.StdEncoding.EncodeToString([]byte(v)), base64.RawStdEncoding.EncodeToString([]byte(v))} {
			if bytes.Contains(raw, []byte(form)) {
				t.Fatal("the file shows a value")
			}
		}
	}
	var doc struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
		Secrets []struct {
			ID       string `json:"id"`
			Versions []struct {
				Version    int
				WrappedKey struct {
					Method string `json:"method"`
					KeyID  string `json:"key_id"`
				} `json:"wrapped_key"`
			}
		} `json:"secrets"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Format != "praxium-secrets" || doc.Version != 1 || len(doc.Secrets) != 1 {
		t.Fatalf("the file head: %q %d", doc.Format, doc.Version)
	}
	wrapped := doc.Secrets[0].Versions[0].WrappedKey
	if wrapped.Method != "aes-256-gcm" || wrapped.KeyID != k.ID() {
		t.Fatalf("the wrapped key: %+v", wrapped)
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatal("the file does not end in a newline")
	}
}

// [REQ-403]
func TestAFileThatIsNotUsableIsRefusedWithAReasonAndNothingIsOverwritten(t *testing.T) {
	good := NewMemDisk()
	create(t, newService(t, open(t, good), key()), "crm-db")
	goodBytes, _ := good.Bytes(FileName)
	edit := func(mutate func(map[string]any)) []byte {
		var m map[string]any
		if err := json.Unmarshal(goodBytes, &m); err != nil {
			t.Fatal(err)
		}
		mutate(m)
		out, _ := json.Marshal(m)
		return out
	}
	firstSecret := func(m map[string]any) map[string]any { return m["secrets"].([]any)[0].(map[string]any) }
	cases := map[string]struct {
		data []byte
		code kernel.Code
	}{
		"not JSON":                       {[]byte("praxium"), kernel.CodeInvalidValue},
		"another format":                 {[]byte(`{"format":"praxium-definitions","version":1}`), kernel.CodeInvalidValue},
		"an empty file":                  {nil, kernel.CodeInvalidValue},
		"a newer version":                {edit(func(m map[string]any) { m["version"] = 2 }), kernel.CodeUnsupported},
		"an unknown field":               {edit(func(m map[string]any) { m["extra"] = 1 }), kernel.CodeInvalidValue},
		"a secret with an unknown field": {edit(func(m map[string]any) { firstSecret(m)["extra"] = 1 }), kernel.CodeInvalidValue},
		"a secret whose id is bad":       {edit(func(m map[string]any) { firstSecret(m)["id"] = "Bad Id" }), kernel.CodeInvalidValue},
		"a secret whose kind is unknown": {edit(func(m map[string]any) { firstSecret(m)["kind"] = "certificate" }), kernel.CodeInvalidValue},
		"a nonce that is not base64":     {edit(func(m map[string]any) { firstSecret(m)["binding"].(map[string]any)["nonce"] = "!!" }), kernel.CodeInvalidValue},
		"a secret twice":                 {edit(func(m map[string]any) { m["secrets"] = append(m["secrets"].([]any), firstSecret(m)) }), kernel.CodeInvalidValue},
		"a nonce of the wrong length":    {edit(func(m map[string]any) { firstSecret(m)["binding"].(map[string]any)["nonce"] = "AAAA" }), kernel.CodeInvalidValue},
	}
	for name, c := range cases {
		disk := NewMemDisk()
		disk.Put(FileName, c.data)
		_, err := Open(disk)
		if kernel.CodeOf(err) != c.code || err == nil {
			t.Errorf("%s: want %s, got %v", name, c.code, err)
			continue
		}
		if !strings.Contains(err.Error(), FileName) || !strings.Contains(err.Error(), "secrets.json") {
			t.Errorf("%s: the message does not name the file: %v", name, err)
		}
		if after, _ := disk.Bytes(FileName); !bytes.Equal(after, c.data) {
			t.Errorf("%s: the file was overwritten", name)
		}
	}
	// A file that is too large.
	saved := maxFileLen
	maxFileLen = 10
	defer func() { maxFileLen = saved }()
	disk := NewMemDisk()
	disk.Put(FileName, goodBytes)
	_, err := Open(disk)
	wantCode(t, err, kernel.CodeInvalidValue)
}

// [REQ-403]
func TestTheDiskFailingAtAnyStepLeavesTheOldFileOrTheNewOneAndTheStoreAgreesWithIt(t *testing.T) {
	for step, wantNew := range map[string]bool{"create": false, "write": false, "sync": false, "close": false, "rename": false, "syncdir": true} {
		disk := NewMemDisk()
		k := key()
		st := open(t, disk)
		svc := newService(t, st, k)
		create(t, svc, "crm-db")
		oldBytes, _ := disk.Bytes(FileName)

		disk.Fail[step] = errors.New("disk failure")
		_, err := svc.AddVersion(ctx, "crm-db", fields("not-a-real-password-v2"))
		wantCode(t, err, kernel.CodeInternal)
		delete(disk.Fail, step)

		want := 1
		if wantNew {
			want = 2
		}
		if got := versions(t, st, "crm-db"); got != want {
			t.Errorf("%s: the store holds %d versions, want %d", step, got, want)
		}
		if again := open(t, disk); versions(t, again, "crm-db") != want {
			t.Errorf("%s: the file holds another state than the store", step)
		}
		if after, _ := disk.Bytes(FileName); !wantNew && !bytes.Equal(after, oldBytes) {
			t.Errorf("%s: the old file changed", step)
		}
		for _, name := range disk.Names() {
			if name != FileName {
				t.Errorf("%s: a leftover file %q", step, name)
			}
		}
		// The store still works after the failure.
		if _, err := svc.AddVersion(ctx, "crm-db", fields("not-a-real-password-v3")); err != nil {
			t.Errorf("%s: a write after the failure: %v", step, err)
		}
	}
}

// [REQ-403]
func TestAProcessThatDiedWhileWritingLeavesTheOldFileAndTheLeftoverIsRemoved(t *testing.T) {
	disk := NewMemDisk()
	k := key()
	create(t, newService(t, open(t, disk), k), "crm-db")
	oldBytes, _ := disk.Bytes(FileName)
	// The process died with half of the new file in the temporary file, before the rename.
	disk.Put(tmpName, oldBytes[:len(oldBytes)/2])
	st := open(t, disk)
	if _, ok := disk.Bytes(tmpName); ok {
		t.Fatal("the leftover was not removed")
	}
	if after, _ := disk.Bytes(FileName); !bytes.Equal(after, oldBytes) || versions(t, st, "crm-db") != 1 {
		t.Fatal("the old file or the old state is gone")
	}
	o, err := newService(t, st, k).Open(ctx, "crm-db", okUse)
	if err != nil {
		t.Fatal(err)
	}
	o.Close()

	// The disk that cannot remove the leftover, or read the file, is an error and nothing is overwritten.
	disk.Fail["remove"] = errors.New("denied")
	_, err = Open(disk)
	wantCode(t, err, kernel.CodeInternal)
	delete(disk.Fail, "remove")
	disk.Fail["read"] = errors.New("denied")
	_, err = Open(disk)
	wantCode(t, err, kernel.CodeInternal)
}

// [REQ-403]
func TestAnotherKeyOpensNothingFromTheFile(t *testing.T) {
	disk := NewMemDisk()
	create(t, newService(t, open(t, disk), key()), "crm-db")
	other := newService(t, open(t, disk), secrets.GenerateMasterKey(kernel.NewSeededBytes(5)))
	_, err := other.Open(ctx, "crm-db", okUse)
	wantCode(t, err, secrets.CodeMasterKeyMismatch)
	if list, err := other.List(ctx); err != nil || len(list) != 1 {
		t.Fatalf("describing without the right key: %v", err)
	}
}

// [REQ-403]
func TestTheMemoryDiskBehavesLikeADisk(t *testing.T) {
	d := NewMemDisk()
	if _, err := d.ReadWhole("x"); err == nil {
		t.Fatal("a file that is not there")
	}
	f, _ := d.Create("x")
	if _, err := d.Create("x"); err == nil {
		t.Fatal("create over a file")
	}
	_, _ = f.Write([]byte("ab"))
	_ = f.Sync()
	_ = f.Close()
	if err := d.Rename("nope", "y"); err == nil {
		t.Fatal("rename of nothing")
	}
	if err := d.Rename("x", "y"); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.ReadWhole("y"); string(got) != "ab" {
		t.Fatal("the content of the renamed file")
	}
	if err := d.Remove("x"); err == nil {
		t.Fatal("remove of nothing")
	}
	if d.SyncDir() != nil {
		t.Fatal("sync")
	}
}
