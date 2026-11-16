package secrets

import (
	"bytes"
	"context"
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
	// The three purposes never share authenticated data, even for the same secret and version.
	rec := Record{ID: "one", Kind: KindToken}
	all := [][]byte{valueAAD("one", 1), dataKeyAAD("one", 1, "mk-a"), bindingAAD(rec, "mk-a")}
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			if bytes.Equal(all[i], all[j]) {
				t.Fatalf("purposes %d and %d share authenticated data", i, j)
			}
		}
	}
	if bytes.Equal(valueAAD("one", 1), valueAAD("one", 2)) || bytes.Equal(valueAAD("one", 1), valueAAD("two", 1)) {
		t.Fatal("the authenticated data do not tell versions or secrets apart")
	}
	if bytes.Equal(dataKeyAAD("one", 1, "mk-a"), dataKeyAAD("one", 1, "mk-b")) {
		t.Fatal("the authenticated data do not tell master keys apart")
	}
	r2 := rec
	r2.Scope = []string{"s"}
	r3 := rec
	r3.AllowedHosts = []Address{{Host: "h.test", Port: 1}}
	if bytes.Equal(bindingAAD(rec, "k"), bindingAAD(r2, "k")) || bytes.Equal(bindingAAD(rec, "k"), bindingAAD(r3, "k")) {
		t.Fatal("the binding does not cover the scope and the addresses")
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
		v, err := svc.newVersion("one", 1, NewFields(fields), key)
		if err != nil {
			t.Fatal(err)
		}
		rec := Record{ID: "one", Kind: KindPassword, Scope: []string{"crm"}, AllowedHosts: []Address{{Host: "db.test", Port: 1}}, Versions: []VersionRecord{v}}
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
