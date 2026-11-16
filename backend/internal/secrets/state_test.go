package secrets_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// A file changed by hand is judged against the whole record. The three scenarios below are the ones a reviewer
// found when the tag covered only the scope and the addresses: each mixes pieces of states that were each genuine.

// [REQ-403]
func TestAnOldScopeAndAddressesWithANewValueDoNotOpen(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	old := record(t, r, "one")
	// The owner narrows the use and changes the value.
	if _, err := r.svc.SetScope(ctx, "one", []string{"billing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.AddVersion(ctx, "one", secrets.NewFields(map[string]string{"username": testUser, "password": testPassword + "-2"})); err != nil {
		t.Fatal(err)
	}
	mixed := record(t, r, "one")
	mixed.Scope, mixed.AllowedHosts, mixed.Binding = old.Scope, old.AllowedHosts, old.Binding
	openFails(t, r, "one", mixed, secrets.CodeIntegrity, "old scope, addresses and binding with the new value")
	// The old scope with the current binding does not open either.
	mixed = record(t, r, "one")
	mixed.Scope = old.Scope
	openFails(t, r, "one", mixed, secrets.CodeIntegrity, "old scope with the current binding")
}

// [REQ-403]
func TestARetiredVersionPutBackDoesNotOpen(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	if _, err := r.svc.AddVersion(ctx, "one", passwordFields()); err != nil {
		t.Fatal(err)
	}
	before := record(t, r, "one")
	if _, err := r.svc.RetireVersion(ctx, "one", 1); err != nil {
		t.Fatal(err)
	}
	// The old ciphertext of version 1 comes back, with and without the old binding.
	back := record(t, r, "one")
	back.Versions[0] = before.Versions[0]
	openFails(t, r, "one", back, secrets.CodeIntegrity, "the old version put back, current binding")
	// Dropping the newest version to open the older one is refused too.
	cut := record(t, r, "one")
	cut.Versions = before.Versions[:1]
	openFails(t, r, "one", cut, secrets.CodeIntegrity, "the newest version dropped")
}

// [REQ-403]
func TestASecretMadeAgainUnderTheSameIDDoesNotMixWithTheOldOne(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	first := record(t, r, "one")
	if err := r.svc.Delete(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	r.createPassword(t, "one")
	second := record(t, r, "one")
	if string(first.Instance) == string(second.Instance) {
		t.Fatal("two secrets with one id share an instance")
	}
	cases := map[string]func(a, b *secrets.Record){
		"old version, new binding":      func(a, b *secrets.Record) { b.Versions = a.Versions },
		"old binding, new version":      func(a, b *secrets.Record) { b.Binding = a.Binding },
		"old value, new wrapped key":    func(a, b *secrets.Record) { b.Versions[0].Value = a.Versions[0].Value },
		"old wrapped key, new value":    func(a, b *secrets.Record) { b.Versions[0].WrappedKey = a.Versions[0].WrappedKey },
		"old instance, new everything":  func(a, b *secrets.Record) { b.Instance = a.Instance },
		"old record whole, new binding": func(a, b *secrets.Record) { a.Binding = b.Binding; *b = *a },
	}
	for what, mix := range cases {
		a, b := first.Clone(), second.Clone()
		mix(&a, &b)
		openFails(t, r, "one", b, secrets.CodeIntegrity, what)
	}
}

// [REQ-403]
func TestWhatIsPastedFromAnotherSecretDoesNotOpen(t *testing.T) {
	cases := map[string]func(one, two *secrets.Record){
		"the binding":  func(one, two *secrets.Record) { one.Binding = two.Binding },
		"the versions": func(one, two *secrets.Record) { one.Versions = two.Versions },
		"the instance": func(one, two *secrets.Record) { one.Instance = two.Instance },
		"the wrapped key": func(one, two *secrets.Record) {
			one.Versions[0].WrappedKey = two.Versions[0].WrappedKey
		},
	}
	for what, paste := range cases {
		r := oneAndTwo(t)
		one, two := record(t, r, "one"), record(t, r, "two")
		paste(&one, &two)
		openFails(t, r, "one", one, secrets.CodeIntegrity, what)
	}
}

// [REQ-405]
func TestAnEmptyScopeAndAnEmptyListOfAddressesMeanNobody(t *testing.T) {
	r := newRig(t)
	create := func(id string, scope []string, hosts []secrets.Address) {
		t.Helper()
		if _, err := r.svc.Create(ctx, secrets.CreateInput{ID: id, Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": testToken}),
			Scope: scope, AllowedHosts: hosts}); err != nil {
			t.Fatal(err)
		}
	}
	create("noscope", nil, []secrets.Address{dbAddress})
	create("nohosts", []string{"crm"}, nil)
	_, err := r.svc.Open(ctx, "noscope", okUse)
	wantCode(t, err, secrets.CodeScopeDenied)
	_, err = r.svc.Open(ctx, "nohosts", okUse)
	wantCode(t, err, secrets.CodeAddressDenied)
	// Emptying what was set is the same.
	r.createPassword(t, "one")
	if _, err := r.svc.SetScope(ctx, "one", []string{}); err != nil {
		t.Fatal(err)
	}
	_, err = r.svc.Open(ctx, "one", okUse)
	wantCode(t, err, secrets.CodeScopeDenied)
	if _, err := r.svc.SetScope(ctx, "one", []string{"crm"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.SetAllowedHosts(ctx, "one", []secrets.Address{}); err != nil {
		t.Fatal(err)
	}
	_, err = r.svc.Open(ctx, "one", okUse)
	wantCode(t, err, secrets.CodeAddressDenied)
}

// [REQ-403]
func TestEveryWriteChecksTheTagFirst(t *testing.T) {
	writes := map[string]func(r *rig) error{
		"add version": func(r *rig) error { _, err := r.svc.AddVersion(ctx, "one", passwordFields()); return err },
		"retire":      func(r *rig) error { _, err := r.svc.RetireVersion(ctx, "one", 1); return err },
		"scope":       func(r *rig) error { _, err := r.svc.SetScope(ctx, "one", []string{"crm"}); return err },
		"addresses":   func(r *rig) error { _, err := r.svc.SetAllowedHosts(ctx, "one", nil); return err },
	}
	for what, write := range writes {
		r := newRig(t)
		r.createPassword(t, "one")
		rec := record(t, r, "one")
		rec.Versions[0].Value.Data = flip(rec.Versions[0].Value.Data)
		put(t, r, rec)
		wantCode(t, write(r), secrets.CodeIntegrity)
		if got := record(t, r, "one"); !reflect.DeepEqual(got, rec) {
			t.Errorf("%s: a refused write changed the record", what)
		}
	}
}

// [REQ-403]
func TestAWriteBindsTheNewStateSoTheNextOpenWorks(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	steps := []func() error{
		func() error { _, err := r.svc.AddVersion(ctx, "one", passwordFields()); return err },
		func() error { _, err := r.svc.RetireVersion(ctx, "one", 1); return err },
		func() error { _, err := r.svc.SetScope(ctx, "one", []string{"crm", "other"}); return err },
		func() error { _, err := r.svc.SetAllowedHosts(ctx, "one", []secrets.Address{dbAddress}); return err },
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatal(err)
		}
		o, err := r.svc.Open(ctx, "one", okUse)
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		o.Close()
	}
	if _, err := r.svc.RetireVersion(ctx, "one", 2); err != nil {
		t.Fatal(err)
	}
	_, err := r.svc.Open(ctx, "one", okUse)
	wantCode(t, err, secrets.CodeVersionRetired)
}

// [REQ-403]
func TestVersionNumbersThatCannotBeRightAreRefusedOnRead(t *testing.T) {
	bad := map[string]func(rec *secrets.Record){
		"zero":            func(rec *secrets.Record) { rec.Versions[0].Version = 0 },
		"negative":        func(rec *secrets.Record) { rec.Versions[0].Version = -1 },
		"past the limit":  func(rec *secrets.Record) { rec.Versions[0].Version = 1 << 21 },
		"wraps at 32 bit": func(rec *secrets.Record) { rec.Versions[0].Version = 1<<32 + 1 },
		"twice": func(rec *secrets.Record) {
			rec.Versions = append(rec.Versions, rec.Versions[0])
		},
		"descending": func(rec *secrets.Record) {
			second := rec.Versions[0]
			second.Version = 2
			rec.Versions = []secrets.VersionRecord{second, rec.Versions[0]}
		},
	}
	for what, change := range bad {
		r := newRig(t)
		r.createPassword(t, "one")
		rec := record(t, r, "one")
		change(&rec)
		if err := r.store.Put(ctx, rec); err == nil {
			if _, err := r.svc.Open(ctx, "one", okUse); err == nil {
				t.Errorf("%s: a record with such a version number opened", what)
			}
		}
	}
}

// [REQ-406]
func TestThePortsThatAreHandedOutAreNarrow(t *testing.T) {
	r := newRig(t)
	w, o, d := r.svc.Writer(), r.svc.Opener(), r.svc.Describer()
	if _, ok := any(w).(secrets.Opener); ok {
		t.Error("the writer can be asserted to open")
	}
	if _, ok := any(w).(secrets.Describer); ok {
		t.Error("the writer can be asserted to describe")
	}
	if _, ok := any(o).(secrets.Writer); ok {
		t.Error("the opener can be asserted to write")
	}
	if _, ok := any(o).(secrets.Describer); ok {
		t.Error("the opener can be asserted to describe")
	}
	if _, ok := any(d).(secrets.Writer); ok {
		t.Error("the describer can be asserted to write")
	}
	if _, ok := any(d).(secrets.Opener); ok {
		t.Error("the describer can be asserted to open")
	}
	if _, ok := any(o).(*secrets.Service); ok {
		t.Error("the opener is the service")
	}
	// They still work.
	if _, err := w.Create(ctx, secrets.CreateInput{ID: "one", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": testToken}),
		Scope: []string{"crm"}, AllowedHosts: []secrets.Address{dbAddress}}); err != nil {
		t.Fatal(err)
	}
	op, err := o.Open(ctx, "one", okUse)
	if err != nil {
		t.Fatal(err)
	}
	op.Close()
	if list, err := d.List(ctx); err != nil || len(list) != 1 {
		t.Fatalf("list: %v", err)
	}
}

// [REQ-405]
func TestAnOpenedSecretIsClosedSafely(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	o, err := r.svc.Open(ctx, "one", secrets.Use{SourceID: "crm", Host: "DB.Example.Test.", Port: 5432, Purpose: secrets.PurposeRun})
	if err != nil {
		t.Fatal(err)
	}
	if o.Address() != dbAddress {
		t.Fatalf("the address is %+v, not the one that was allowed", o.Address())
	}
	pw, _ := o.Value("password")
	sameText(t, pw.Reveal(), testPassword, "before close")
	o.Close()
	o.Close()
	if pw.Reveal() != "" {
		t.Fatal("a closed value is not empty")
	}
	if again, _ := o.Value("password"); again.Reveal() != "" {
		t.Fatal("a value read after close is not empty")
	}
	var zero secrets.Opened
	zero.Close()
	var none *secrets.Opened
	none.Close()
}

// [REQ-404]
func TestFieldsFromJSONRefuseNamesThatCollideAndNeverRepeatTheBody(t *testing.T) {
	var ok secrets.Fields
	if err := json.Unmarshal([]byte(`{"username":"a","password":"b"}`), &ok); err != nil || len(ok.Names()) != 2 {
		t.Fatalf("a good body: %v", err)
	}
	for _, body := range []string{
		`{"password":"hidden-1","password":"hidden-2"}`, `{"password":"hidden-1","Password":"hidden-2"}`,
		`{"password":5}`, `{"password":"hidden-1"} {"x":"hidden-3"}`, `["hidden-1"]`, `{"password":"hidden-1"`, `"hidden-1"`,
	} {
		var f secrets.Fields
		err := json.Unmarshal([]byte(body), &f)
		if err == nil {
			t.Errorf("%q was accepted", body)
			continue
		}
		if strings.Contains(err.Error(), "hidden") {
			t.Errorf("the error repeats the body of %q", body)
		}
	}
}

// [REQ-404]
func TestAnErrorRepeatsWhatACallerGaveOnlyWhenItIsSafe(t *testing.T) {
	r := newRig(t)
	const url = "postgres://app:hunter-demo@db.example.test:5432/crm"
	in := secrets.CreateInput{ID: "one", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": testToken}), Scope: []string{"crm"}}
	cases := map[string]func() error{
		"allowed host": func() error {
			in.AllowedHosts = []secrets.Address{{Host: url, Port: 1}}
			_, err := r.svc.Create(ctx, in)
			return err
		},
		"use host": func() error {
			r.createPassword(t, "two")
			_, err := r.svc.Open(ctx, "two", secrets.Use{SourceID: "crm", Host: url, Port: 1, Purpose: secrets.PurposeRun})
			return err
		},
		"scope": func() error {
			in.AllowedHosts = nil
			in.Scope = []string{url}
			_, err := r.svc.Create(ctx, in)
			return err
		},
		"field name": func() error {
			in.Scope = nil
			in.Fields = secrets.NewFields(map[string]string{url: "x"})
			_, err := r.svc.Create(ctx, in)
			return err
		},
		"kind": func() error {
			in.Fields = secrets.NewFields(map[string]string{"token": testToken})
			in.Kind = secrets.Kind(url)
			_, err := r.svc.Create(ctx, in)
			return err
		},
		"purpose": func() error {
			_, err := r.svc.Open(ctx, "two", secrets.Use{SourceID: "crm", Host: "db.example.test", Port: 5432, Purpose: secrets.Purpose(url)})
			return err
		},
	}
	for what, run := range cases {
		err := run()
		if kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Errorf("%s: %v", what, err)
			continue
		}
		for _, part := range []string{"hunter-demo", "app:", "postgres"} {
			if strings.Contains(err.Error(), part) {
				t.Errorf("%s: the error repeats part of what was given", what)
			}
		}
	}
	// A safe text is told back, which is what makes the message useful.
	in.Kind = secrets.KindToken
	in.Scope = []string{"bad source!"}
	if _, err := r.svc.Create(ctx, in); err == nil || strings.Contains(err.Error(), "bad source!") {
		t.Fatalf("a text with a space and a mark was repeated: %v", err)
	}
	in.AllowedHosts = []secrets.Address{{Host: "db_1.example.test:x", Port: 0}}
	in.Scope = nil
	if _, err := r.svc.Create(ctx, in); err == nil {
		t.Fatal("a port of zero was accepted")
	}
}

// [REQ-406]
func TestTheNarrowPortsDoTheirWholeWork(t *testing.T) {
	r := newRig(t)
	w, d := r.svc.Writer(), r.svc.Describer()
	in := secrets.CreateInput{ID: "one", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": testToken}),
		Scope: []string{"crm"}, AllowedHosts: []secrets.Address{dbAddress}}
	if _, err := w.Create(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := w.AddVersion(ctx, "one", in.Fields); err != nil {
		t.Fatal(err)
	}
	if _, err := w.SetScope(ctx, "one", []string{"crm", "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.SetAllowedHosts(ctx, "one", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := w.RetireVersion(ctx, "one", 1); err != nil {
		t.Fatal(err)
	}
	meta, err := d.Get(ctx, "one")
	if err != nil || len(meta.Scope) != 2 {
		t.Fatalf("get: %v", err)
	}
	if err := w.Delete(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Get(ctx, "one"); err == nil {
		t.Fatal("a deleted secret is still described")
	}
}
