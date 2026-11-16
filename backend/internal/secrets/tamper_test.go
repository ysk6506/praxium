package secrets_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// openFails puts the record in the store and says that opening it fails with the code, without a value in the error.
func openFails(t *testing.T, r *rig, id string, rec secrets.Record, want kernel.Code, what string) {
	t.Helper()
	put(t, r, rec)
	_, err := r.svc.Open(ctx, id, okUse)
	if kernel.CodeOf(err) != want {
		t.Errorf("%s: want %s, got %v", what, want, err)
		return
	}
	if strings.Contains(err.Error(), testPassword) || strings.Contains(err.Error(), testUser) {
		t.Errorf("%s: the error repeats a value", what)
	}
}

func oneAndTwo(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	r.createPassword(t, "one")
	r.createPassword(t, "two")
	return r
}

// [REQ-403]
func TestACiphertextPastedFromAnotherSecretDoesNotOpen(t *testing.T) {
	for _, what := range []string{"value", "wrapped key", "both"} {
		r := oneAndTwo(t)
		one, two := record(t, r, "one"), record(t, r, "two")
		switch what {
		case "value":
			one.Versions[0].Value = two.Versions[0].Value
		case "wrapped key":
			one.Versions[0].WrappedKey = two.Versions[0].WrappedKey
		default:
			one.Versions[0].Value, one.Versions[0].WrappedKey = two.Versions[0].Value, two.Versions[0].WrappedKey
		}
		openFails(t, r, "one", one, secrets.CodeIntegrity, what)
	}
}

// [REQ-403]
func TestACiphertextPastedFromAnotherVersionOfTheSameSecretDoesNotOpen(t *testing.T) {
	for _, what := range []string{"value", "wrapped key", "both"} {
		r := newRig(t)
		r.createPassword(t, "one")
		if _, err := r.svc.AddVersion(ctx, "one", secrets.NewFields(map[string]string{"username": "other-user", "password": "not-a-real-password-v2"})); err != nil {
			t.Fatal(err)
		}
		rec := record(t, r, "one")
		v1, v2 := rec.Versions[0], &rec.Versions[1]
		switch what {
		case "value":
			v2.Value = v1.Value
		case "wrapped key":
			v2.WrappedKey = v1.WrappedKey
		default:
			v2.Value, v2.WrappedKey = v1.Value, v1.WrappedKey
		}
		openFails(t, r, "one", rec, secrets.CodeIntegrity, what)
	}
}

// [REQ-403]
func TestOneFlippedByteAnywhereInTheCiphertextDoesNotOpen(t *testing.T) {
	changes := map[string]func(*secrets.Record){
		"value data":        func(r *secrets.Record) { r.Versions[0].Value.Data = flip(r.Versions[0].Value.Data) },
		"value nonce":       func(r *secrets.Record) { r.Versions[0].Value.Nonce = flip(r.Versions[0].Value.Nonce) },
		"wrapped key data":  func(r *secrets.Record) { r.Versions[0].WrappedKey.Data = flip(r.Versions[0].WrappedKey.Data) },
		"wrapped key nonce": func(r *secrets.Record) { r.Versions[0].WrappedKey.Nonce = flip(r.Versions[0].WrappedKey.Nonce) },
		"binding data":      func(r *secrets.Record) { r.Binding.Data = flip(r.Binding.Data) },
		"binding nonce":     func(r *secrets.Record) { r.Binding.Nonce = flip(r.Binding.Nonce) },
		"value tag":         func(r *secrets.Record) { d := r.Versions[0].Value.Data; d[len(d)-1] ^= 0x80 },
	}
	for what, change := range changes {
		r := newRig(t)
		r.createPassword(t, "one")
		rec := record(t, r, "one")
		change(&rec)
		openFails(t, r, "one", rec, secrets.CodeIntegrity, what)
	}
}

// [REQ-403] [REQ-405]
func TestAScopeOrAnAddressEditedByHandDoesNotOpen(t *testing.T) {
	changes := map[string]func(*secrets.Record){
		"a source added to the scope": func(r *secrets.Record) { r.Scope = []string{"crm", "intruder"} },
		"the scope emptied":           func(r *secrets.Record) { r.Scope = []string{} },
		"the scope changed":           func(r *secrets.Record) { r.Scope = []string{"intruder"} },
		"an address added": func(r *secrets.Record) {
			r.AllowedHosts = append(r.AllowedHosts, secrets.Address{Host: "evil.example.test", Port: 1})
		},
		"the port changed":      func(r *secrets.Record) { r.AllowedHosts[0].Port = 5433 },
		"the host changed":      func(r *secrets.Record) { r.AllowedHosts[0].Host = "evil.example.test" },
		"the addresses emptied": func(r *secrets.Record) { r.AllowedHosts = []secrets.Address{} },
		"the kind changed":      func(r *secrets.Record) { r.Kind = secrets.KindToken },
	}
	for what, change := range changes {
		r := newRig(t)
		r.createPassword(t, "one")
		rec := record(t, r, "one")
		change(&rec)
		openFails(t, r, "one", rec, secrets.CodeIntegrity, what)
	}
}

// [REQ-403] [REQ-405]
func TestAChangeNeverMakesAFileEditedByHandGenuine(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	rec := record(t, r, "one")
	rec.Scope = []string{"crm", "intruder"}
	put(t, r, rec)
	_, err := r.svc.SetScope(ctx, "one", []string{"crm", "intruder"})
	wantCode(t, err, secrets.CodeIntegrity)
	_, err = r.svc.SetAllowedHosts(ctx, "one", nil)
	wantCode(t, err, secrets.CodeIntegrity)
	_, err = r.svc.AddVersion(ctx, "one", passwordFields())
	wantCode(t, err, secrets.CodeIntegrity)
	if got := record(t, r, "one"); len(got.Scope) != 2 || len(got.Versions) != 1 {
		t.Fatal("a refused change altered the record")
	}
	// Describing never needs the key, so it shows what is stored; the open is what refuses.
	if meta, err := r.svc.Get(ctx, "one"); err != nil || len(meta.Scope) != 2 {
		t.Fatalf("describe: %+v %v", meta, err)
	}
}

// [REQ-403]
func TestAnotherMasterKeyIdIsToldApartFromATamperedFile(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	rec := record(t, r, "one")
	rec.Binding.KeyID = "mk-other"
	openFails(t, r, "one", rec, secrets.CodeMasterKeyMismatch, "binding key id")
	rec = record(t, r, "one")
	rec.Binding.KeyID = r.key.ID()
	rec.Versions[0].WrappedKey.KeyID = "mk-other"
	openFails(t, r, "one", rec, secrets.CodeMasterKeyMismatch, "wrapped key id")
}
