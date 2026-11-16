package secrets_test

import (
	"context"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// Every value here is made up for the tests. A failing test never prints a value: it says which comparison failed.
const (
	testUser     = "demo-user"
	testPassword = "not-a-real-password-for-tests"
	testToken    = "not-a-real-token-for-tests"
)

var ctx = context.Background()

var epoch = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

type rig struct {
	svc   *secrets.Service
	store *secrets.MemoryStore
	clock *kernel.ManualClock
	key   *secrets.MasterKey
}

func newKey(seed uint64) *secrets.MasterKey {
	return secrets.GenerateMasterKey(kernel.NewSeededBytes(seed))
}

func newRig(t *testing.T) *rig {
	t.Helper()
	return newRigWith(t, secrets.NewMemoryStore(), newKey(99), 1)
}

func newRigWith(t *testing.T, store *secrets.MemoryStore, key *secrets.MasterKey, seed uint64) *rig {
	t.Helper()
	clock := kernel.NewManualClock(epoch)
	svc, err := secrets.New(secrets.Config{Store: store, Keys: secrets.StaticKey(key), Clock: clock, Bytes: kernel.NewSeededBytes(seed)})
	if err != nil {
		t.Fatal(err)
	}
	return &rig{svc: svc, store: store, clock: clock, key: key}
}

// serviceWith is another service over the same store, with another key or the same one.
func (r *rig) serviceWith(t *testing.T, keys secrets.KeyProvider) *secrets.Service {
	t.Helper()
	svc, err := secrets.New(secrets.Config{Store: r.store, Keys: keys, Clock: r.clock, Bytes: kernel.NewSeededBytes(5)})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

var dbAddress = secrets.Address{Host: "db.example.test", Port: 5432}

func passwordFields() secrets.Fields {
	return secrets.NewFields(map[string]string{"username": testUser, "password": testPassword})
}

func (r *rig) createPassword(t *testing.T, id string) secrets.Meta {
	t.Helper()
	meta, err := r.svc.Create(ctx, secrets.CreateInput{
		ID: id, Kind: secrets.KindPassword, Fields: passwordFields(),
		Scope: []string{"crm"}, AllowedHosts: []secrets.Address{dbAddress},
	})
	if err != nil {
		t.Fatal(err)
	}
	return meta
}

func useFor(source string, a secrets.Address) secrets.Use {
	return secrets.Use{SourceID: source, Host: a.Host, Port: a.Port, Purpose: secrets.PurposeRun}
}

var okUse = useFor("crm", dbAddress)

func (r *rig) open(t *testing.T, id string) *secrets.Opened {
	t.Helper()
	o, err := r.svc.Open(ctx, id, okUse)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(o.Close)
	return o
}

func wantCode(t *testing.T, err error, want kernel.Code) {
	t.Helper()
	if err == nil || kernel.CodeOf(err) != want {
		t.Fatalf("want code %s, got %v", want, err)
	}
}

// field returns the text of a field of an opened secret.
func field(t *testing.T, o *secrets.Opened, name string) string {
	t.Helper()
	v, ok := o.Value(name)
	if !ok {
		t.Fatalf("the opened secret has no field %q", name)
	}
	return v.Reveal()
}

// sameText compares two texts without printing either.
func sameText(t *testing.T, got, want, what string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: the value is not the one that was put in", what)
	}
}

func record(t *testing.T, r *rig, id string) secrets.Record {
	t.Helper()
	rec, ok, err := r.store.Get(ctx, id)
	if err != nil || !ok {
		t.Fatalf("record %q: %v %v", id, ok, err)
	}
	return rec
}

func put(t *testing.T, r *rig, rec secrets.Record) {
	t.Helper()
	if err := r.store.Put(ctx, rec); err != nil {
		t.Fatal(err)
	}
}

func flip(b []byte) []byte {
	out := append([]byte(nil), b...)
	out[len(out)/2] ^= 0x01
	return out
}
