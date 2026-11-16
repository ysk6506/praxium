package secrets_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// [REQ-403]
func TestTheStoreHandsOutCopiesAndKeepsWhatWasPut(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	rec := record(t, r, "one")
	rec.Scope[0] = "changed"
	rec.Versions[0].Value.Data[0] ^= 0xff
	rec.AllowedHosts[0].Port = 1
	if again := record(t, r, "one"); again.Scope[0] != "crm" || again.AllowedHosts[0].Port != 5432 {
		t.Fatal("a change of a copy reached the store")
	}
	sameText(t, field(t, r.open(t, "one"), "password"), testPassword, "after changes of copies")
	if _, ok, err := r.store.Get(ctx, "missing"); ok || err != nil {
		t.Fatalf("get of a missing record: %v %v", ok, err)
	}
	if found, err := r.store.Delete(ctx, "missing"); found || err != nil {
		t.Fatalf("delete of a missing record: %v %v", found, err)
	}
}

// [REQ-403]
func TestWhatThePersistFunctionRefusesNeverHappens(t *testing.T) {
	var kept [][]string
	failing := false
	syncFailing := false
	persist := func(all []secrets.Record) (bool, error) {
		if failing {
			return false, errors.New("disk full")
		}
		var ids []string
		for _, r := range all {
			ids = append(ids, r.ID)
		}
		kept = append(kept, ids)
		if syncFailing {
			return true, errors.New("folder not synced")
		}
		return true, nil
	}
	store, err := secrets.OpenMemoryStore(nil, persist)
	if err != nil {
		t.Fatal(err)
	}
	r := newRigWith(t, store, newKey(99), 1)
	r.createPassword(t, "b")
	r.createPassword(t, "a")
	if len(kept) != 2 || len(kept[1]) != 2 || kept[1][0] != "a" || kept[1][1] != "b" {
		t.Fatalf("the states that were kept: %v", kept)
	}

	failing = true
	_, err = r.svc.Create(ctx, secrets.CreateInput{ID: "c", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": testToken})})
	wantCode(t, err, kernel.CodeInternal)
	_, err = r.svc.AddVersion(ctx, "a", passwordFields())
	wantCode(t, err, kernel.CodeInternal)
	wantCode(t, r.svc.Delete(ctx, "a"), kernel.CodeInternal)
	if _, err := r.svc.RetireVersion(ctx, "a", 1); kernel.CodeOf(err) != kernel.CodeInternal {
		t.Fatalf("retire: %v", err)
	}
	if list, _ := r.svc.List(ctx); len(list) != 2 {
		t.Fatalf("a refused write changed the store: %d secrets", len(list))
	}
	if meta, _ := r.svc.Get(ctx, "a"); len(meta.Versions) != 1 || meta.Version != 1 {
		t.Fatalf("a refused write changed a secret: %+v", meta)
	}

	// A file that is in place but not synced: the state is taken and the error is told.
	failing, syncFailing = false, true
	_, err = r.svc.AddVersion(ctx, "a", passwordFields())
	wantCode(t, err, kernel.CodeInternal)
	if meta, _ := r.svc.Get(ctx, "a"); len(meta.Versions) != 2 {
		t.Fatalf("the kept state was not taken: %+v", meta)
	}
}

// [REQ-403]
func TestAStoreReadFromRecordsRefusesWhatIsNotWhole(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	if _, err := r.svc.AddVersion(ctx, "one", passwordFields()); err != nil {
		t.Fatal(err)
	}
	good := record(t, r, "one")
	mutations := map[string]func(*secrets.Record){
		"an invalid id":              func(x *secrets.Record) { x.ID = "Bad Id" },
		"an unknown kind":            func(x *secrets.Record) { x.Kind = "certificate" },
		"a scope not sorted":         func(x *secrets.Record) { x.Scope = []string{"b", "a"} },
		"a scope with a repeat":      func(x *secrets.Record) { x.Scope = []string{"a", "a"} },
		"a scope with a bad id":      func(x *secrets.Record) { x.Scope = []string{"bad id"} },
		"an address not in the form": func(x *secrets.Record) { x.AllowedHosts = []secrets.Address{{Host: "DB.test", Port: 1}} },
		"addresses not sorted": func(x *secrets.Record) {
			x.AllowedHosts = []secrets.Address{{Host: "b.test", Port: 1}, {Host: "a.test", Port: 1}}
		},
		"a binding with another method": func(x *secrets.Record) { x.Binding.Method = "none" },
		"a binding with a short nonce":  func(x *secrets.Record) { x.Binding.Nonce = x.Binding.Nonce[:4] },
		"a binding with a short tag":    func(x *secrets.Record) { x.Binding.Data = x.Binding.Data[:4] },
		"a binding without a key id":    func(x *secrets.Record) { x.Binding.KeyID = "" },
		"no versions":                   func(x *secrets.Record) { x.Versions = nil },
		"versions out of order":         func(x *secrets.Record) { x.Versions[0], x.Versions[1] = x.Versions[1], x.Versions[0] },
		"a repeated version":            func(x *secrets.Record) { x.Versions[1].Version = 1 },
		"a version number of zero":      func(x *secrets.Record) { x.Versions[0].Version = 0 },
		"a live version without a key":  func(x *secrets.Record) { x.Versions[0].WrappedKey = nil },
		"a live version without value":  func(x *secrets.Record) { x.Versions[0].Value = nil },
		"a value with a key id":         func(x *secrets.Record) { x.Versions[0].Value.KeyID = "mk-x" },
		"a wrapped key without key id":  func(x *secrets.Record) { x.Versions[0].WrappedKey.KeyID = "" },
		"a retired version with data": func(x *secrets.Record) {
			t := x.Versions[0].CreatedAt
			x.Versions[0].RetiredAt = &t
		},
		"too many versions": func(x *secrets.Record) {
			for len(x.Versions) < 1001 {
				x.Versions = append(x.Versions, x.Versions[len(x.Versions)-1])
				x.Versions[len(x.Versions)-1].Version = len(x.Versions)
			}
		},
	}
	for what, mutate := range mutations {
		bad := good.Clone()
		mutate(&bad)
		if _, err := secrets.OpenMemoryStore([]secrets.Record{bad}, nil); kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Errorf("%s: %v", what, err)
		}
		if err := secrets.NewMemoryStore().Put(ctx, bad); kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Errorf("put of %s: %v", what, err)
		}
	}
	if _, err := secrets.OpenMemoryStore([]secrets.Record{good, good}, nil); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("a record twice: %v", err)
	}
	store, err := secrets.OpenMemoryStore([]secrets.Record{good}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.Get(ctx, "one"); !ok {
		t.Fatal("the record read is not there")
	}
}

// faultyStore is a store whose steps fail when told to.
type faultyStore struct {
	*secrets.MemoryStore
	get, list, put, del bool
}

var errDisk = errors.New("disk failure")

func (f *faultyStore) Get(c context.Context, id string) (secrets.Record, bool, error) {
	if f.get {
		return secrets.Record{}, false, errDisk
	}
	return f.MemoryStore.Get(c, id)
}

func (f *faultyStore) List(c context.Context) ([]secrets.Record, error) {
	if f.list {
		return nil, errDisk
	}
	return f.MemoryStore.List(c)
}

func (f *faultyStore) Put(c context.Context, rec secrets.Record) error {
	if f.put {
		return errDisk
	}
	return f.MemoryStore.Put(c, rec)
}

func (f *faultyStore) Delete(c context.Context, id string) (bool, error) {
	if f.del {
		return false, errDisk
	}
	return f.MemoryStore.Delete(c, id)
}

// [REQ-403]
func TestAStoreThatFailsGivesAnInternalErrorAndNeverAValue(t *testing.T) {
	store := &faultyStore{MemoryStore: secrets.NewMemoryStore()}
	svc, err := secrets.New(secrets.Config{Store: store, Keys: secrets.StaticKey(newKey(3)), Clock: kernel.NewManualClock(epoch), Bytes: kernel.NewSeededBytes(3)})
	if err != nil {
		t.Fatal(err)
	}
	create := secrets.CreateInput{ID: "one", Kind: secrets.KindPassword, Fields: passwordFields(), Scope: []string{"crm"}, AllowedHosts: []secrets.Address{dbAddress}}
	if _, err := svc.Create(ctx, create); err != nil {
		t.Fatal(err)
	}
	store.get, store.list, store.put, store.del = true, true, true, true
	check := func(what string, err error) {
		t.Helper()
		if kernel.CodeOf(err) != kernel.CodeInternal || strings.Contains(err.Error(), testPassword) {
			t.Errorf("%s: %v", what, err)
		}
	}
	create.ID = "two"
	_, err = svc.Create(ctx, create)
	check("create, get fails", err)
	_, err = svc.Get(ctx, "one")
	check("get", err)
	_, err = svc.List(ctx)
	check("list", err)
	_, err = svc.Open(ctx, "one", okUse)
	check("open", err)
	_, err = svc.AddVersion(ctx, "one", passwordFields())
	check("add version", err)
	store.get = false
	_, err = svc.Create(ctx, create)
	check("create, put fails", err)
	_, err = svc.SetScope(ctx, "one", nil)
	check("set scope", err)
	check("delete", svc.Delete(ctx, "one"))
}
