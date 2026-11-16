package secrets_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// [REQ-403]
func TestASecretSealedByAnotherMasterKeyIsRefusedWithAWayOut(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	other := r.serviceWith(t, secrets.StaticKey(newKey(1234)))
	_, err := other.Open(ctx, "one", okUse)
	wantCode(t, err, secrets.CodeMasterKeyMismatch)
	for _, want := range []string{r.key.ID(), "put the secret in again", "key file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message lacks %q: %v", want, err)
		}
	}
	_, err = other.AddVersion(ctx, "one", passwordFields())
	wantCode(t, err, secrets.CodeMasterKeyMismatch)
	_, err = other.SetScope(ctx, "one", nil)
	wantCode(t, err, secrets.CodeMasterKeyMismatch)
	// Retiring and deleting need no key, and the right key still opens.
	sameText(t, field(t, r.open(t, "one"), "password"), testPassword, "the right key")
}

// [REQ-403]
func TestAKeyWithTheSameIdAndOtherBytesDoesNotAuthenticate(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	var f map[string]any
	if err := json.Unmarshal(newKey(1234).Encode(), &f); err != nil {
		t.Fatal(err)
	}
	f["key_id"] = r.key.ID()
	raw, _ := json.Marshal(f)
	same, err := secrets.ParseMasterKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.serviceWith(t, secrets.StaticKey(same)).Open(ctx, "one", okUse)
	wantCode(t, err, secrets.CodeIntegrity)
}

type failingKeys struct{ err error }

func (f failingKeys) MasterKey() (*secrets.MasterKey, error) { return nil, f.err }

type nilKeys struct{}

func (nilKeys) MasterKey() (*secrets.MasterKey, error) { return nil, nil }

// [REQ-403]
func TestWithoutAMasterKeyDescribingWorksAndTheRestSaysWhy(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	for name, keys := range map[string]secrets.KeyProvider{"no key": secrets.NoKey(), "a provider that gives nothing": nilKeys{}} {
		none := r.serviceWith(t, keys)
		_, err := none.Open(ctx, "one", okUse)
		wantCode(t, err, secrets.CodeMasterKeyMissing)
		if !strings.Contains(err.Error(), "key file") {
			t.Errorf("%s: the message names no way out: %v", name, err)
		}
		_, err = none.Create(ctx, secrets.CreateInput{ID: "new", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": testToken})})
		wantCode(t, err, secrets.CodeMasterKeyMissing)
		_, err = none.AddVersion(ctx, "one", passwordFields())
		wantCode(t, err, secrets.CodeMasterKeyMissing)
		_, err = none.SetScope(ctx, "one", nil)
		wantCode(t, err, secrets.CodeMasterKeyMissing)
		if list, err := none.List(ctx); err != nil || len(list) != 1 {
			t.Errorf("%s: list: %v", name, err)
		}
		if _, err := none.Get(ctx, "one"); err != nil {
			t.Errorf("%s: get: %v", name, err)
		}
	}
	// A key provider that fails gives its own error back.
	broken := r.serviceWith(t, failingKeys{err: kernel.Errorf(secrets.CodeMasterKeyInvalid, "the key file is damaged")})
	_, err := broken.Open(ctx, "one", okUse)
	wantCode(t, err, secrets.CodeMasterKeyInvalid)
	if _, err := broken.RetireVersion(ctx, "one", 1); err != nil {
		t.Fatalf("retire needs no key: %v", err)
	}
	if err := broken.Delete(ctx, "one"); err != nil {
		t.Fatalf("delete needs no key: %v", err)
	}
}

// [REQ-403]
func TestAMasterKeyFileThatCannotBeReadIsRefusedWithoutRepeatingIt(t *testing.T) {
	good := newKey(7).Encode()
	var f map[string]any
	if err := json.Unmarshal(good, &f); err != nil {
		t.Fatal(err)
	}
	edit := func(key string, value any) []byte {
		g := map[string]any{}
		for k, v := range f {
			g[k] = v
		}
		if value == nil {
			delete(g, key)
		} else {
			g[key] = value
		}
		out, _ := json.Marshal(g)
		return out
	}
	keyText := f["key"].(string)
	cases := map[string][]byte{
		"empty":              nil,
		"not JSON":           []byte("praxium"),
		"truncated":          good[:len(good)/2],
		"another format":     edit("format", "praxium-secrets"),
		"a newer version":    edit("version", 2),
		"version zero":       edit("version", 0),
		"another method":     edit("method", "chacha20-poly1305"),
		"a bad key id":       edit("key_id", "Bad Id"),
		"no key id":          edit("key_id", nil),
		"a short key":        edit("key", base64.StdEncoding.EncodeToString(make([]byte, 16))),
		"a long key":         edit("key", base64.StdEncoding.EncodeToString(append(make([]byte, 31), 1, 1))),
		"not base64":         edit("key", "not base64 at all!"),
		"a number as a key":  edit("key", 5),
		"a key of zeros":     edit("key", base64.StdEncoding.EncodeToString(make([]byte, 32))),
		"an unknown field":   edit("extra", "x"),
		"text after the key": append(append([]byte(nil), good...), []byte(`{"format":"x"}`)...),
		"too large":          append(append([]byte(nil), good...), []byte(strings.Repeat(" ", 5000))...),
	}
	for what, data := range cases {
		key, err := secrets.ParseMasterKey(data)
		if key != nil || kernel.CodeOf(err) != secrets.CodeMasterKeyInvalid {
			t.Errorf("%s: %v", what, err)
			continue
		}
		if strings.Contains(err.Error(), keyText) || !strings.Contains(err.Error(), "key file") {
			t.Errorf("%s: the message repeats the key or names no way out", what)
		}
	}
	// Whitespace around the one object is fine.
	if _, err := secrets.ParseMasterKey(append([]byte("\n  "), append(good, '\n', '\n')...)); err != nil {
		t.Fatal(err)
	}
}

// [REQ-403]
func TestAMasterKeyIsMadeFromRandomnessAndRoundTripsThroughItsFile(t *testing.T) {
	a, b := newKey(5), newKey(5)
	if a.ID() != b.ID() || string(a.Encode()) != string(b.Encode()) {
		t.Fatal("the same randomness gave another key")
	}
	if c := newKey(6); c.ID() == a.ID() || string(c.Encode()) == string(a.Encode()) {
		t.Fatal("another randomness gave the same key")
	}
	if err := secrets.ValidateID(a.ID()); err != nil || !strings.HasPrefix(a.ID(), "mk-") || len(a.ID()) != 19 {
		t.Fatalf("key id %q: %v", a.ID(), err)
	}
	r := newRig(t)
	r.createPassword(t, "one")
	read, err := secrets.ParseMasterKey(r.key.Encode())
	if err != nil || read.ID() != r.key.ID() {
		t.Fatalf("read back: %v", err)
	}
	o, err := r.serviceWith(t, secrets.StaticKey(read)).Open(ctx, "one", okUse)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	sameText(t, field(t, o, "password"), testPassword, "opened with the key read back")
}
