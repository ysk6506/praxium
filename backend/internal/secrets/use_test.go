package secrets_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// [REQ-405]
func TestASourceOutsideTheScopeIsRefused(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "crm-db")
	_, err := r.svc.Open(ctx, "crm-db", useFor("billing", dbAddress))
	wantCode(t, err, secrets.CodeScopeDenied)
	if strings.Contains(err.Error(), testPassword) || strings.Contains(err.Error(), testUser) {
		t.Fatal("the refusal repeats a value")
	}
	// Scope first: a source outside the scope is refused even for an address that is not allowed either.
	_, err = r.svc.Open(ctx, "crm-db", useFor("billing", secrets.Address{Host: "other.example.test", Port: 1}))
	wantCode(t, err, secrets.CodeScopeDenied)

	// A scope is changed, sorted and without repeats; the new source opens, the old one no longer does.
	meta, err := r.svc.SetScope(ctx, "crm-db", []string{"billing", "billing", "audit"})
	if err != nil || strings.Join(meta.Scope, ",") != "audit,billing" {
		t.Fatalf("set scope: %+v %v", meta, err)
	}
	if _, err := r.svc.Open(ctx, "crm-db", useFor("billing", dbAddress)); err != nil {
		t.Fatal(err)
	}
	_, err = r.svc.Open(ctx, "crm-db", okUse)
	wantCode(t, err, secrets.CodeScopeDenied)
	_, err = r.svc.SetScope(ctx, "crm-db", []string{"bad id"})
	wantCode(t, err, kernel.CodeInvalidValue)
	_, err = r.svc.SetScope(ctx, "nothing", nil)
	wantCode(t, err, kernel.CodeNotFound)
}

// [REQ-405]
func TestAHostOrAPortOutsideTheAllowedAddressesIsRefused(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "crm-db")
	for _, a := range []secrets.Address{
		{Host: "db.example.test", Port: 5433},
		{Host: "other.example.test", Port: 5432},
		{Host: "db.example.test.evil.test", Port: 5432},
		{Host: "xdb.example.test", Port: 5432},
		{Host: "example.test", Port: 5432},
		{Host: "10.0.0.1", Port: 5432},
	} {
		_, err := r.svc.Open(ctx, "crm-db", useFor("crm", a))
		if kernel.CodeOf(err) != secrets.CodeAddressDenied {
			t.Errorf("%+v: %v", a, err)
		}
	}
	// What is not an address at all is a mistake of the caller.
	for _, a := range []secrets.Address{{Host: "", Port: 5432}, {Host: "db.example.test", Port: 0}, {Host: "db.example.test:5432", Port: 5432}} {
		_, err := r.svc.Open(ctx, "crm-db", useFor("crm", a))
		wantCode(t, err, kernel.CodeInvalidValue)
	}
	_, err := r.svc.Open(ctx, "crm-db", secrets.Use{SourceID: "crm", Host: dbAddress.Host, Port: dbAddress.Port, Purpose: "play"})
	wantCode(t, err, kernel.CodeInvalidValue)
	_, err = r.svc.Open(ctx, "crm-db", secrets.Use{SourceID: "bad id", Host: dbAddress.Host, Port: dbAddress.Port, Purpose: secrets.PurposeRun})
	wantCode(t, err, kernel.CodeInvalidValue)

	// No allowed address, no use.
	if _, err := r.svc.SetAllowedHosts(ctx, "crm-db", nil); err != nil {
		t.Fatal(err)
	}
	_, err = r.svc.Open(ctx, "crm-db", okUse)
	wantCode(t, err, secrets.CodeAddressDenied)
	meta, err := r.svc.SetAllowedHosts(ctx, "crm-db", []secrets.Address{{Host: "B.Example.Test.", Port: 1}, {Host: "a.example.test", Port: 2}, {Host: "b.example.test", Port: 1}})
	if err != nil || len(meta.AllowedHosts) != 2 || meta.AllowedHosts[0].Host != "a.example.test" || meta.AllowedHosts[1].Host != "b.example.test" {
		t.Fatalf("set hosts: %+v %v", meta, err)
	}
	for _, a := range meta.AllowedHosts {
		if _, err := r.svc.Open(ctx, "crm-db", useFor("crm", a)); err != nil {
			t.Fatalf("%+v: %v", a, err)
		}
	}
	_, err = r.svc.SetAllowedHosts(ctx, "crm-db", []secrets.Address{{Host: "x y", Port: 1}})
	wantCode(t, err, kernel.CodeInvalidValue)
	_, err = r.svc.SetAllowedHosts(ctx, "nothing", nil)
	wantCode(t, err, kernel.CodeNotFound)
}

// [REQ-405]
func TestTheSameHostWrittenInAnotherWayIsTheSameHost(t *testing.T) {
	cases := []struct{ allowed, used string }{
		{"db.example.test", "DB.Example.TEST"},
		{"DB.EXAMPLE.TEST", "db.example.test"},
		{"db.example.test.", "db.example.test"},
		{"db.example.test", "db.example.test."},
		{"[2001:DB8::1]", "2001:db8:0:0:0:0:0:1"},
		{"2001:db8::1", "[2001:0db8:0000:0000:0000:0000:0000:0001]"},
		{"10.0.0.1", "::ffff:10.0.0.1"},
		{"[::ffff:10.0.0.1]", "10.0.0.1"},
		{"::1", "[0:0:0:0:0:0:0:1]"},
		{"a_b.example.test", "A_B.example.test"},
	}
	for _, c := range cases {
		r := newRig(t)
		if _, err := r.svc.Create(ctx, secrets.CreateInput{ID: "crm-db", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": testToken}),
			Scope: []string{"crm"}, AllowedHosts: []secrets.Address{{Host: c.allowed, Port: 443}}}); err != nil {
			t.Fatalf("allowed %q: %v", c.allowed, err)
		}
		o, err := r.svc.Open(ctx, "crm-db", useFor("crm", secrets.Address{Host: c.used, Port: 443}))
		if err != nil {
			t.Fatalf("%q against %q: %v", c.used, c.allowed, err)
		}
		o.Close()
	}
}

// [REQ-405]
func TestHostsThatLookAlikeAreDifferentHosts(t *testing.T) {
	r := newRig(t)
	if _, err := r.svc.Create(ctx, secrets.CreateInput{ID: "crm-db", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": testToken}),
		Scope: []string{"crm"}, AllowedHosts: []secrets.Address{{Host: "db.example.test", Port: 443}, {Host: "2001:db8::1", Port: 443}, {Host: "10.0.0.1", Port: 443}}}); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"db.example.test.example.test", "ddb.example.test", "db-example.test", "2001:db8::2", "2001:db8::10", "10.0.0.2", "10.0.0.10", "::ffff:10.0.0.2"} {
		_, err := r.svc.Open(ctx, "crm-db", useFor("crm", secrets.Address{Host: host, Port: 443}))
		if kernel.CodeOf(err) != secrets.CodeAddressDenied {
			t.Errorf("%q: %v", host, err)
		}
	}
}

// [REQ-405]
func TestTheFormsOfAHostThatAreRefusedOrWrittenInTheOneForm(t *testing.T) {
	valid := map[string]string{
		"DB.Example.Test":    "db.example.test",
		"db.example.test.":   "db.example.test",
		"localhost":          "localhost",
		"a-b.c1.test":        "a-b.c1.test",
		"_dmarc.example.io":  "_dmarc.example.io",
		"xn--bcher-kva.test": "xn--bcher-kva.test",
		"192.168.1.10":       "192.168.1.10",
		"[2001:DB8:0:0::1]":  "2001:db8::1",
		"2001:DB8::1":        "2001:db8::1",
		"::ffff:192.0.2.1":   "192.0.2.1",
		"[::1]":              "::1",
		"1abc.test":          "1abc.test",
		"example.1a":         "example.1a",
	}
	for raw, want := range valid {
		got, err := secrets.NormalizeHost(raw)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", raw, got, err, want)
		}
	}
	invalid := []string{
		"", " ", "a b", " db.test", "db.test ", "db.test:80", "db.test/x", "http://db.test", "user@db.test", "*.db.test", "db..test",
		".", "..", ".db.test", "-db.test", "db-.test", "db.test..", "bücher.test", "db.te\nst", "db.test\x00",
		"127.1", "0x7f.1", "0x7f.0x1", "2130706433", "1.2.3", "010.0.0.1", "10.0.0.1.", "1.2.3.4.5", "256.1.1.1", "999.999.999.999",
		"fe80::1%eth0", "fe80::1%", "[fe80::1%eth0]", "[1.2.3.4]", "[::1", "::1]", "[]", "[db.test]", "1:2:3", ":::", "[::1]:80",
		strings.Repeat("a", 64) + ".test", strings.Repeat("a.", 130) + "test",
	}
	for _, raw := range invalid {
		if got, err := secrets.NormalizeHost(raw); kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Errorf("%q should be refused, got %q, %v", raw, got, err)
		}
	}
}
