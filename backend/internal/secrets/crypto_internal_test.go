package secrets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-403]
func TestADataKeyEncryptsOnceAndAnUnwrappedKeyNeverEncrypts(t *testing.T) {
	src := kernel.NewSeededBytes(1)
	dk := newDataKey(src)
	if _, err := dk.seal(src, []byte("plain"), []byte("aad")); err != nil {
		t.Fatal(err)
	}
	if _, err := dk.seal(src, []byte("plain"), []byte("aad")); kernel.CodeOf(err) != kernel.CodeInternal {
		t.Fatalf("a second encryption under one data key: %v", err)
	}
	master := GenerateMasterKey(src)
	wrapped, err := dk.wrap(src, master, []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	back, err := unwrapDataKey(master, wrapped, []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back.key, dk.key) {
		t.Fatal("the unwrapped key is another key")
	}
	if _, err := back.seal(src, []byte("x"), nil); kernel.CodeOf(err) != kernel.CodeInternal {
		t.Fatalf("an unwrapped key encrypted: %v", err)
	}
	if _, err := unwrapDataKey(master, wrapped, []byte("other aad")); kernel.CodeOf(err) != CodeIntegrity {
		t.Fatalf("unwrap under other authenticated data: %v", err)
	}
	dk.wipe()
	if !bytes.Equal(dk.key, make([]byte, keyLen)) {
		t.Fatal("wipe left the key")
	}
}

// [REQ-403]
func TestTheCipherRefusesWhatIsNotAKeyOrANonce(t *testing.T) {
	src := kernel.NewSeededBytes(2)
	if _, err := aeadSeal(src, make([]byte, 16), nil, nil); kernel.CodeOf(err) != kernel.CodeInternal {
		t.Fatalf("a key of 16 bytes: %v", err)
	}
	if _, err := aeadOpen(make([]byte, 31), sealed{}, nil); kernel.CodeOf(err) != kernel.CodeInternal {
		t.Fatalf("a key of 31 bytes: %v", err)
	}
	key := make([]byte, keyLen)
	src.Fill(key)
	s, err := aeadSeal(src, key, []byte("plain"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]sealed{
		"a short nonce": {nonce: s.nonce[:5], data: s.data},
		"a long nonce":  {nonce: append(append([]byte(nil), s.nonce...), 0), data: s.data},
		"no data":       {nonce: s.nonce},
		"short data":    {nonce: s.nonce, data: s.data[:3]},
	} {
		if _, err := aeadOpen(key, bad, []byte("aad")); kernel.CodeOf(err) != CodeIntegrity {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got, err := aeadOpen(key, s, []byte("aad")); err != nil || string(got) != "plain" {
		t.Fatalf("the good one: %v", err)
	}
}

// [REQ-403]
func TestTheEncodingOfFieldsRoundTripsAndRefusesWhatIsNotItsOwn(t *testing.T) {
	enc := encodeFields(NewFields(map[string]string{"username": "u", "password": "p\x00q"}))
	got, ok := decodeFields(enc)
	if !ok || string(got["username"]) != "u" || string(got["password"]) != "p\x00q" || len(got) != 2 {
		t.Fatalf("decode: %v %v", got, ok)
	}
	if !bytes.Equal(enc, encodeFields(NewFields(map[string]string{"password": "p\x00q", "username": "u"}))) {
		t.Fatal("the encoding depends on the order of the map")
	}
	for name, bad := range map[string][]byte{
		"empty":              nil,
		"a count only":       {0, 0, 0, 2},
		"a huge count":       {0, 0, 1, 0},
		"a cut name":         enc[:9],
		"a cut value":        enc[:len(enc)-1],
		"bytes after":        append(append([]byte(nil), enc...), 0),
		"a length too large": {0, 0, 0, 1, 0xff, 0xff, 0xff, 0xff},
	} {
		if _, ok := decodeFields(bad); ok {
			t.Errorf("%s was accepted", name)
		}
	}
	twice := []byte{0, 0, 0, 2, 0, 0, 0, 1, 'a', 0, 0, 0, 1, 'x', 0, 0, 0, 1, 'a', 0, 0, 0, 1, 'y'}
	if _, ok := decodeFields(twice); ok {
		t.Fatal("a repeated field name was accepted")
	}
}

// [REQ-403]
func TestWhatIsAuthenticatedCannotBeConfusedWithAnotherThing(t *testing.T) {
	// The same parts in another split are another message.
	a := newAAD("x").text("ab").text("c").b
	b := newAAD("x").text("a").text("bc").b
	if bytes.Equal(a, b) {
		t.Fatal("two splits of the same letters are the same authenticated data")
	}
	// A number is 64 bits: 1 and 2^32+1 are not the same bytes.
	if bytes.Equal(newAAD("x").number(1).b, newAAD("x").number(1<<32+1).b) {
		t.Fatal("a number of 32 bits and one of 33 bits give the same bytes")
	}
	inst := bytes.Repeat([]byte{7}, instanceLen)
	rec := Record{ID: "one", Instance: inst, Kind: KindToken, Binding: Sealed{Method: MethodAES256GCM, KeyID: "mk-a"}}
	// The three purposes never share authenticated data, even for the same secret and version.
	all := [][]byte{valueAAD("one", inst, 1), dataKeyAAD("one", inst, 1, "mk-a"), bindingAAD(rec)}
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			if bytes.Equal(all[i], all[j]) {
				t.Fatalf("purposes %d and %d share authenticated data", i, j)
			}
		}
	}
	other := bytes.Repeat([]byte{8}, instanceLen)
	if bytes.Equal(valueAAD("one", inst, 1), valueAAD("one", inst, 2)) || bytes.Equal(valueAAD("one", inst, 1), valueAAD("two", inst, 1)) ||
		bytes.Equal(valueAAD("one", inst, 1), valueAAD("one", other, 1)) {
		t.Fatal("the authenticated data of a value do not tell versions, secrets or instances apart")
	}
	if bytes.Equal(dataKeyAAD("one", inst, 1, "mk-a"), dataKeyAAD("one", inst, 1, "mk-b")) ||
		bytes.Equal(dataKeyAAD("one", inst, 1, "mk-a"), dataKeyAAD("one", other, 1, "mk-a")) {
		t.Fatal("the authenticated data of a wrapped key do not tell master keys or instances apart")
	}
}

// [REQ-403]
func TestTheBindingCoversEveryPartOfTheRecord(t *testing.T) {
	inst := bytes.Repeat([]byte{7}, instanceLen)
	retired := time.Unix(50, 0)
	base := Record{
		ID: "one", Instance: inst, Kind: KindToken, CreatedAt: time.Unix(10, 0), Scope: []string{"s"},
		AllowedHosts: []Address{{Host: "h.test", Port: 1}}, Binding: Sealed{Method: MethodAES256GCM, KeyID: "mk-a"},
		Versions: []VersionRecord{{Version: 1, CreatedAt: time.Unix(20, 0), Value: &Sealed{Method: MethodAES256GCM, Nonce: []byte{1}, Data: []byte{2}},
			WrappedKey: &Sealed{Method: MethodAES256GCM, KeyID: "mk-a", Nonce: []byte{3}, Data: []byte{4}}}},
	}
	changes := map[string]func(*Record){
		"the id":             func(r *Record) { r.ID = "two" },
		"the instance":       func(r *Record) { r.Instance = bytes.Repeat([]byte{8}, instanceLen) },
		"the kind":           func(r *Record) { r.Kind = KindPassword },
		"the creation time":  func(r *Record) { r.CreatedAt = time.Unix(11, 0) },
		"the scope":          func(r *Record) { r.Scope = []string{"t"} },
		"an address":         func(r *Record) { r.AllowedHosts = []Address{{Host: "h.test", Port: 2}} },
		"the binding key":    func(r *Record) { r.Binding.KeyID = "mk-b" },
		"the binding method": func(r *Record) { r.Binding.Method = "other" },
		"a version number":   func(r *Record) { r.Versions[0].Version = 2 },
		"a version time":     func(r *Record) { r.Versions[0].CreatedAt = time.Unix(21, 0) },
		"a retirement":       func(r *Record) { r.Versions[0].RetiredAt = &retired },
		"the value": func(r *Record) {
			r.Versions[0].Value = &Sealed{Method: MethodAES256GCM, Nonce: []byte{1}, Data: []byte{9}}
		},
		"the value method":   func(r *Record) { r.Versions[0].Value.Method = "other" },
		"the wrapped key":    func(r *Record) { r.Versions[0].WrappedKey.Data = []byte{9} },
		"the wrapped key id": func(r *Record) { r.Versions[0].WrappedKey.KeyID = "mk-b" },
		"another version":    func(r *Record) { r.Versions = append(r.Versions, r.Versions[0]) },
		"no version":         func(r *Record) { r.Versions = nil },
	}
	want := bindingAAD(base)
	for what, change := range changes {
		r := base.Clone()
		change(&r)
		if bytes.Equal(want, bindingAAD(r)) {
			t.Errorf("the binding does not cover %s", what)
		}
	}
}

// [REQ-403]
func TestAStoredVersionWhoseFieldsAreNotTheFieldsOfItsKindDoesNotOpen(t *testing.T) {
	for name, fields := range map[string]map[string]string{
		"a field of another name": {"username": "u", "other": "p"},
		"too many fields":         {"username": "u", "password": "p", "token": "t"},
		"too few fields":          {"username": "u"},
		"an empty value":          {"username": "u", "password": ""},
	} {
		store := NewMemoryStore()
		key := GenerateMasterKey(kernel.NewSeededBytes(4))
		svc, err := New(Config{Store: store, Keys: StaticKey(key), Clock: kernel.NewManualClock(time.Unix(0, 0)), Bytes: kernel.NewSeededBytes(4)})
		if err != nil {
			t.Fatal(err)
		}
		// The version is sealed correctly under the right key, but not by Create, which would have refused it.
		instance := bytes.Repeat([]byte{9}, instanceLen)
		v, err := svc.newVersion("one", instance, 1, NewFields(fields), key)
		if err != nil {
			t.Fatal(err)
		}
		rec := Record{ID: "one", Instance: instance, Kind: KindPassword, CreatedAt: time.Unix(10, 0), Scope: []string{"crm"}, AllowedHosts: []Address{{Host: "db.test", Port: 1}}, Versions: []VersionRecord{v}}
		if err := svc.bind(&rec, key); err != nil {
			t.Fatal(err)
		}
		if err := store.Put(context.Background(), rec); err != nil {
			t.Fatal(err)
		}
		_, err = svc.Open(context.Background(), "one", Use{SourceID: "crm", Host: "db.test", Port: 1, Purpose: PurposeRun})
		if kernel.CodeOf(err) != CodeIntegrity {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func sealedRecord(t *testing.T) (*Service, *MemoryStore, *MasterKey) {
	t.Helper()
	store := NewMemoryStore()
	key := GenerateMasterKey(kernel.NewSeededBytes(11))
	svc, err := New(Config{Store: store, Keys: StaticKey(key), Clock: kernel.NewManualClock(time.Unix(1000, 0)), Bytes: kernel.NewSeededBytes(12)})
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, key
}

// [REQ-403]
func TestEveryVersionHasItsOwnDataKey(t *testing.T) {
	svc, store, key := sealedRecord(t)
	in := CreateInput{ID: "one", Kind: KindToken, Fields: NewFields(map[string]string{"token": "not-a-real-token-for-tests"})}
	if _, err := svc.Create(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := svc.AddVersion(context.Background(), "one", in.Fields); err != nil {
			t.Fatal(err)
		}
	}
	rec, _, _ := store.Get(context.Background(), "one")
	seen := map[string]bool{}
	for _, v := range rec.Versions {
		dk, err := unwrapDataKey(key, sealed{nonce: v.WrappedKey.Nonce, data: v.WrappedKey.Data}, dataKeyAAD(rec.ID, rec.Instance, v.Version, key.id))
		if err != nil {
			t.Fatal(err)
		}
		if seen[string(dk.key)] {
			t.Fatal("two versions have the same data key")
		}
		seen[string(dk.key)] = true
	}
	if len(seen) != 4 {
		t.Fatalf("%d data keys for 4 versions", len(seen))
	}
}

// [REQ-405]
func TestTheUseIsCheckedBeforeAnythingIsDecrypted(t *testing.T) {
	svc, store, key := sealedRecord(t)
	ctx := context.Background()
	in := CreateInput{ID: "one", Kind: KindToken, Fields: NewFields(map[string]string{"token": "not-a-real-token-for-tests"}),
		Scope: []string{"crm"}, AllowedHosts: []Address{{Host: "db.test", Port: 1}}}
	if _, err := svc.Create(ctx, in); err != nil {
		t.Fatal(err)
	}
	// The ciphertext of the value is damaged and the binding is sealed again over the damaged record, so the binding
	// is good and only decrypting would fail.
	rec, _, _ := store.Get(ctx, "one")
	rec.Versions[0].Value.Data[0] ^= 1
	if err := svc.bind(&rec, key); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, rec); err != nil {
		t.Fatal(err)
	}
	use := Use{SourceID: "crm", Host: "db.test", Port: 1, Purpose: PurposeRun}
	if _, err := svc.Open(ctx, "one", use); kernel.CodeOf(err) != CodeIntegrity {
		t.Fatalf("an allowed use of the damaged version: %v", err)
	}
	other := use
	other.SourceID = "billing"
	if _, err := svc.Open(ctx, "one", other); kernel.CodeOf(err) != CodeScopeDenied {
		t.Fatalf("a source outside the scope reached the decryption: %v", err)
	}
	other = use
	other.Port = 2
	if _, err := svc.Open(ctx, "one", other); kernel.CodeOf(err) != CodeAddressDenied {
		t.Fatalf("an address outside the list reached the decryption: %v", err)
	}
}

// [REQ-403]
func TestAES256GCMGivesTheAnswersOfTheStandard(t *testing.T) {
	// Test case 14 of the GCM specification: a key of 256 zero bits, a nonce of 96 zero bits, one block of zeros.
	gcm, err := newAEAD(make([]byte, keyLen))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString("cea7403d4d606b6e074ec5d3baf39d18" + "d0d1c8a799996bf0265b98b5d48ab919")
	if got := gcm.Seal(nil, make([]byte, nonceLen), make([]byte, 16), nil); !bytes.Equal(got, want) {
		t.Fatal("AES-256-GCM does not give the answer of the specification")
	}
}

// [REQ-403]
func TestTheIDOfAMasterKeyComesFromTheKey(t *testing.T) {
	raw := bytes.Repeat([]byte{1}, keyLen)
	a, b := newMasterKey(raw), newMasterKey(raw)
	if a.id != b.id || a.id != keyID(raw) || len(a.id) != 19 {
		t.Fatalf("key id %q", a.id)
	}
	raw[0] = 2
	if newMasterKey(raw).id == a.id {
		t.Fatal("two keys have the same id")
	}
	// The id is of the purpose and the key: the plain hash of the key is another id.
	sum := sha256.Sum256(bytes.Repeat([]byte{1}, keyLen))
	if a.id == "mk-"+hex.EncodeToString(sum[:8]) {
		t.Fatal("the id is the plain hash of the key")
	}
}

// [REQ-404]
func TestWhatACallerGaveIsRepeatedInAnErrorOnlyWhenItIsSafe(t *testing.T) {
	for _, s := range []string{"db.example.test", "[2001:db8::1]", "host_1:5432", ""} {
		if !safeText(s) {
			t.Errorf("%q should be safe", s)
		}
	}
	for _, s := range []string{"postgres://app:pw@db.test:5432/crm", "a@b", "a b", "a/b", "a\n", "é", strings.Repeat("a", 129), "a=b", "a,b"} {
		if safeText(s) {
			t.Errorf("%q should not be safe", s)
		}
	}
	if msg := badText("host", "db.test"); msg != `invalid host "db.test"` {
		t.Fatalf("%q", msg)
	}
	if msg := badText("host", "postgres://app:pw@db.test/x"); strings.Contains(msg, "pw") || strings.Contains(msg, "app") {
		t.Fatalf("the message repeats the input: %q", msg)
	}
}

// [REQ-403]
func TestTheTimeOfARetirementIsBoundToTheNanosecond(t *testing.T) {
	at := time.Unix(50, 5)
	base := Record{
		ID: "one", Instance: bytes.Repeat([]byte{7}, instanceLen), Kind: KindToken, CreatedAt: time.Unix(10, 0),
		Binding: Sealed{Method: MethodAES256GCM, KeyID: "mk-a"},
		Versions: []VersionRecord{
			{Version: 1, CreatedAt: time.Unix(20, 0), RetiredAt: &at},
			{Version: 2, CreatedAt: time.Unix(30, 0), Value: &Sealed{Method: MethodAES256GCM, Nonce: []byte{1}, Data: []byte{2}},
				WrappedKey: &Sealed{Method: MethodAES256GCM, KeyID: "mk-a", Nonce: []byte{3}, Data: []byte{4}}},
		},
	}
	want := bindingAAD(base)
	for what, to := range map[string]time.Time{"another second": time.Unix(51, 5), "another nanosecond": time.Unix(50, 6), "whole seconds": time.Unix(50, 0)} {
		r := base.Clone()
		moved := to
		r.Versions[0].RetiredAt = &moved
		if bytes.Equal(want, bindingAAD(r)) {
			t.Errorf("the binding does not tell %s apart", what)
		}
	}
	r := base.Clone()
	r.Versions[0].RetiredAt = nil
	if bytes.Equal(want, bindingAAD(r)) {
		t.Error("the binding does not tell a retired version from a live one")
	}
}

// [REQ-403]
func TestATimeThatTheBindingCannotHoldIsRefusedOnRead(t *testing.T) {
	svc, store, _ := sealedRecord(t)
	in := CreateInput{ID: "one", Kind: KindToken, Fields: NewFields(map[string]string{"token": "not-a-real-token-for-tests"})}
	if _, err := svc.Create(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	good, _, _ := store.Get(context.Background(), "one")
	far := good.CreatedAt.AddDate(584, 0, 0)
	for what, change := range map[string]func(r *Record){
		"creation time":         func(r *Record) { r.CreatedAt = far },
		"version creation time": func(r *Record) { r.Versions[0].CreatedAt = far },
		"retirement time": func(r *Record) {
			r.Versions[0].Value, r.Versions[0].WrappedKey, r.Versions[0].RetiredAt = nil, nil, &far
		},
	} {
		r := good.Clone()
		change(&r)
		if err := r.Validate(); err == nil {
			t.Errorf("a %s 584 years away was accepted", what)
		}
	}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
}
