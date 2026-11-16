package secrets_test

import (
	"sync"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// [REQ-403]
func TestWhatWasPutInComesOutOfOpenForEveryField(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "crm-db")
	o := r.open(t, "crm-db")
	sameText(t, field(t, o, "username"), testUser, "username")
	sameText(t, field(t, o, "password"), testPassword, "password")
	if o.SecretID() != "crm-db" || o.Kind() != secrets.KindPassword || o.Version() != 1 {
		t.Fatalf("opened: %s %s %d", o.SecretID(), o.Kind(), o.Version())
	}
	if _, ok := o.Value("token"); ok {
		t.Fatal("a password secret has a token field")
	}

	if _, err := r.svc.Create(ctx, secrets.CreateInput{ID: "crm-api", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": testToken}),
		Scope: []string{"crm"}, AllowedHosts: []secrets.Address{dbAddress}}); err != nil {
		t.Fatal(err)
	}
	sameText(t, field(t, r.open(t, "crm-api"), "token"), testToken, "token")
}

// [REQ-403]
func TestANewVersionIsWhatOpenGivesAndARetiredVersionDoesNotOpen(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "crm-db")
	r.clock.Advance(60e9)
	meta, err := r.svc.AddVersion(ctx, "crm-db", secrets.NewFields(map[string]string{"username": testUser, "password": "not-a-real-password-v2"}))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Version != 2 || len(meta.Versions) != 2 || !meta.Versions[1].CreatedAt.After(meta.Versions[0].CreatedAt) {
		t.Fatalf("meta after a new version: %+v", meta)
	}
	o := r.open(t, "crm-db")
	sameText(t, field(t, o, "password"), "not-a-real-password-v2", "version 2")
	if o.Version() != 2 {
		t.Fatalf("opened version %d", o.Version())
	}

	// Retiring the new version goes back to the old one; its ciphertext is gone from the record.
	if meta, err = r.svc.RetireVersion(ctx, "crm-db", 2); err != nil || meta.Version != 1 || meta.Versions[1].RetiredAt == nil {
		t.Fatalf("retire 2: %+v %v", meta, err)
	}
	if v := record(t, r, "crm-db").Versions[1]; v.Value != nil || v.WrappedKey != nil {
		t.Fatal("a retired version keeps its ciphertext")
	}
	sameText(t, field(t, r.open(t, "crm-db"), "password"), testPassword, "version 1 again")

	if meta, err = r.svc.RetireVersion(ctx, "crm-db", 1); err != nil || meta.Version != 0 {
		t.Fatalf("retire 1: %+v %v", meta, err)
	}
	_, err = r.svc.Open(ctx, "crm-db", okUse)
	wantCode(t, err, secrets.CodeVersionRetired)
	_, err = r.svc.RetireVersion(ctx, "crm-db", 1)
	wantCode(t, err, kernel.CodeConflict)
	_, err = r.svc.RetireVersion(ctx, "crm-db", 7)
	wantCode(t, err, kernel.CodeNotFound)
	_, err = r.svc.RetireVersion(ctx, "nothing", 1)
	wantCode(t, err, kernel.CodeNotFound)

	// A version added after the retirement of the others is the next number, not a reused one.
	if meta, err = r.svc.AddVersion(ctx, "crm-db", passwordFields()); err != nil || meta.Version != 3 {
		t.Fatalf("add after retire: %+v %v", meta, err)
	}
}

// [REQ-403]
func TestTheSameValueTwiceGivesDifferentCiphertextAndTheSameRandomnessGivesTheSameBytes(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	r.createPassword(t, "two")
	a, b := record(t, r, "one").Versions[0], record(t, r, "two").Versions[0]
	if string(a.Value.Data) == string(b.Value.Data) || string(a.WrappedKey.Data) == string(b.WrappedKey.Data) ||
		string(a.Value.Nonce) == string(b.Value.Nonce) {
		t.Fatal("the same value gave the same ciphertext, key or nonce")
	}
	if _, err := r.svc.AddVersion(ctx, "one", passwordFields()); err != nil {
		t.Fatal(err)
	}
	if v2 := record(t, r, "one").Versions[1]; string(v2.Value.Data) == string(a.Value.Data) {
		t.Fatal("the same value as a new version gave the same ciphertext")
	}

	// A fixed source of randomness, a fixed clock and a fixed key repeat the stored bytes.
	again := newRig(t)
	again.createPassword(t, "one")
	again.createPassword(t, "two")
	if !equalRecords(record(t, r, "two"), record(t, again, "two")) {
		t.Fatal("the same randomness gave other bytes")
	}
	other := newRigWith(t, secrets.NewMemoryStore(), newKey(99), 2)
	other.createPassword(t, "one")
	if string(record(t, other, "one").Versions[0].Value.Data) == string(a.Value.Data) {
		t.Fatal("another randomness gave the same ciphertext")
	}
}

func equalRecords(a, b secrets.Record) bool {
	x, y := a.Versions[0], b.Versions[0]
	return string(x.Value.Data) == string(y.Value.Data) && string(x.Value.Nonce) == string(y.Value.Nonce) &&
		string(x.WrappedKey.Data) == string(y.WrappedKey.Data) && string(a.Binding.Data) == string(b.Binding.Data)
}

// [REQ-403]
func TestEveryNonceOfAStoreIsDifferent(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "crm-db")
	for i := 0; i < 20; i++ {
		if _, err := r.svc.AddVersion(ctx, "crm-db", passwordFields()); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	rec := record(t, r, "crm-db")
	nonces := [][]byte{rec.Binding.Nonce}
	for _, v := range rec.Versions {
		nonces = append(nonces, v.Value.Nonce, v.WrappedKey.Nonce)
	}
	for _, n := range nonces {
		if seen[string(n)] {
			t.Fatal("a nonce repeats")
		}
		seen[string(n)] = true
	}
}

// [REQ-403]
func TestWhatIsPutInIsCheckedAgainstTheKind(t *testing.T) {
	r := newRig(t)
	type input struct {
		in     secrets.CreateInput
		values map[string]string
	}
	good := func() input {
		return input{in: secrets.CreateInput{ID: "crm-db", Kind: secrets.KindPassword}, values: map[string]string{"username": testUser, "password": testPassword}}
	}
	bad := func(mutate func(*input)) error {
		x := good()
		mutate(&x)
		x.in.Fields = secrets.NewFields(x.values)
		_, err := r.svc.Create(ctx, x.in)
		return err
	}
	cases := map[string]func(*input){
		"unknown kind":       func(x *input) { x.in.Kind = "certificate" },
		"empty kind":         func(x *input) { x.in.Kind = "" },
		"field not in kind":  func(x *input) { x.values["token"] = testToken },
		"missing field":      func(x *input) { delete(x.values, "password") },
		"empty value":        func(x *input) { x.values["password"] = "" },
		"no fields":          func(x *input) { x.values = nil },
		"value too long":     func(x *input) { x.values["password"] = string(make([]byte, 16<<10+1)) },
		"value not UTF-8":    func(x *input) { x.values["password"] = "\xff\xfe" },
		"empty id":           func(x *input) { x.in.ID = "" },
		"scope with a space": func(x *input) { x.in.Scope = []string{"bad id"} },
		"bad host":           func(x *input) { x.in.AllowedHosts = []secrets.Address{{Host: "a b", Port: 1}} },
		"port zero":          func(x *input) { x.in.AllowedHosts = []secrets.Address{{Host: "a.test", Port: 0}} },
		"port too big":       func(x *input) { x.in.AllowedHosts = []secrets.Address{{Host: "a.test", Port: 65536}} },
	}
	for name, mutate := range cases {
		err := bad(mutate)
		if kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Errorf("%s: %v", name, err)
		}
	}
	if list, _ := r.svc.List(ctx); len(list) != 0 {
		t.Fatal("a refused secret was stored")
	}
	if _, err := r.svc.AddVersion(ctx, "crm-db", passwordFields()); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Fatalf("a version for a secret that is not there: %v", err)
	}
	r.createPassword(t, "crm-db")
	_, err := r.svc.AddVersion(ctx, "crm-db", secrets.NewFields(map[string]string{"token": testToken}))
	wantCode(t, err, kernel.CodeInvalidValue)
	_, err = r.svc.Create(ctx, secrets.CreateInput{ID: "crm-db", Kind: secrets.KindPassword, Fields: passwordFields()})
	wantCode(t, err, kernel.CodeConflict)
	var zero secrets.Fields
	_, err = r.svc.AddVersion(ctx, "crm-db", zero)
	wantCode(t, err, kernel.CodeInvalidValue)
}

// [REQ-403]
func TestTheSyntaxOfAnID(t *testing.T) {
	long := "a"
	for len(long) < 64 {
		long += "b"
	}
	for _, id := range []string{"a", "crm-db", "crm_db_2", "a-", long} {
		if err := secrets.ValidateID(id); err != nil {
			t.Errorf("%q should be valid: %v", id, err)
		}
	}
	for _, id := range []string{"", "1abc", "-abc", "_abc", "Abc", "abC", "ab c", "ab.c", "ab/c", "é", long + "b", "ab\n"} {
		if err := secrets.ValidateID(id); kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Errorf("%q should be invalid: %v", id, err)
		}
	}
	r := newRig(t)
	if _, err := r.svc.Get(ctx, "Bad"); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("get with a bad id: %v", err)
	}
	if _, err := r.svc.Get(ctx, "good"); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Fatalf("get of a missing secret: %v", err)
	}
}

// [REQ-403]
func TestDeleteRemovesTheSecretAndListIsInTheOrderOfTheIDs(t *testing.T) {
	r := newRig(t)
	for _, id := range []string{"zeta", "alpha", "mid"} {
		r.createPassword(t, id)
	}
	list, err := r.svc.List(ctx)
	if err != nil || len(list) != 3 || list[0].ID != "alpha" || list[1].ID != "mid" || list[2].ID != "zeta" {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := r.svc.Delete(ctx, "mid"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Get(ctx, "mid"); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Fatalf("after delete: %v", err)
	}
	wantCode(t, r.svc.Delete(ctx, "mid"), kernel.CodeNotFound)
	wantCode(t, r.svc.Delete(ctx, "Bad"), kernel.CodeInvalidValue)
	_, err = r.svc.Open(ctx, "mid", okUse)
	wantCode(t, err, kernel.CodeNotFound)
}

// [REQ-403]
func TestWritesFromManyGoroutinesAllLand(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "shared")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.svc.AddVersion(ctx, "shared", passwordFields()); err != nil {
				t.Error(err)
			}
			o, err := r.svc.Open(ctx, "shared", okUse)
			if err != nil {
				t.Error(err)
				return
			}
			o.Close()
		}()
	}
	wg.Wait()
	meta, err := r.svc.Get(ctx, "shared")
	if err != nil || len(meta.Versions) != 13 || meta.Version != 13 {
		t.Fatalf("versions after the writes: %+v %v", meta, err)
	}
}

// [REQ-403]
func TestAServiceNeedsAllItsParts(t *testing.T) {
	good := secrets.Config{Store: secrets.NewMemoryStore(), Keys: secrets.NoKey(), Clock: kernel.NewManualClock(epoch), Bytes: kernel.NewSeededBytes(1)}
	if _, err := secrets.New(good); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*secrets.Config){
		func(c *secrets.Config) { c.Store = nil },
		func(c *secrets.Config) { c.Keys = nil },
		func(c *secrets.Config) { c.Clock = nil },
		func(c *secrets.Config) { c.Bytes = nil },
	} {
		c := good
		mutate(&c)
		if _, err := secrets.New(c); kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Fatalf("a config with a missing part: %v", err)
		}
	}
}
