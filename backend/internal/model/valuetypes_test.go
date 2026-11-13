package model_test

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

func TestValueTypesCheckAndTidy(t *testing.T) {
	// Identity uses the tidy form from the value type, so the rule is written once.
	tests := []struct {
		name  string
		vt    string
		in    string
		want  string
		valid bool
	}{
		{"ipv4 drops leading zeros", "vt.ipv4_address", "192.000.002.010", "192.0.2.10", true},
		{"ipv4 as it is", "vt.ipv4_address", "192.0.2.10", "192.0.2.10", true},
		{"ipv4 with three numbers", "vt.ipv4_address", "192.0.2", "", false},
		{"ipv4 above 255", "vt.ipv4_address", "192.0.2.256", "", false},
		{"ipv4 with a letter", "vt.ipv4_address", "192.0.2.x", "", false},
		{"ipv4 with a sign", "vt.ipv4_address", "192.0.2.-1", "", false},
		{"mac with colons", "vt.mac_address", "00:1A:2B:3C:4D:5E", "001a2b3c4d5e", true},
		{"mac with hyphens", "vt.mac_address", "00-1a-2b-3c-4d-5e", "001a2b3c4d5e", true},
		{"mac too short", "vt.mac_address", "00:1a:2b", "", false},
		{"mac with a non-hex digit", "vt.mac_address", "00:1a:2b:3c:4d:5g", "", false},
		{"serial number trimmed and upper-cased", "vt.serial_number", "  sn-1001 a ", "SN-1001 A", true},
		{"serial number with only separators is kept", "vt.serial_number", " -- ", "--", true},
		{"serial number empty", "vt.serial_number", "   ", "", false},
		{"fqdn lower case without the dot", "vt.fqdn", "SRV-001.Example.TEST.", "srv-001.example.test", true},
		{"fqdn empty", "vt.fqdn", ".", "", false},
		{"fqdn label with a hyphen at the start", "vt.fqdn", "-a.example.test", "", false},
		{"fqdn label with a hyphen at the end", "vt.fqdn", "a-.example.test", "", false},
		{"fqdn with an underscore", "vt.fqdn", "a_b.example.test", "", false},
		{"fqdn with an empty label", "vt.fqdn", "a..example.test", "", false},
		{"fqdn with a label of 64 letters", "vt.fqdn", string(make([]byte, 64)) + ".test", "", false},
		{"built-in value type keeps the value", "vt.string", " Any Text ", " Any Text ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := model.NormalizeValue(vtID(tt.vt), tt.in)
			if tt.valid != (err == nil) {
				t.Fatalf("NormalizeValue(%s, %q) error = %v, want valid %v", tt.vt, tt.in, err, tt.valid)
			}
			if !tt.valid && kernel.CodeOf(err) != model.CodeValueInvalid {
				t.Fatalf("code = %s, want %s", kernel.CodeOf(err), model.CodeValueInvalid)
			}
			if got != tt.want {
				t.Fatalf("NormalizeValue(%s, %q) = %q, want %q", tt.vt, tt.in, got, tt.want)
			}
		})
	}
}

func TestFQDNLimits(t *testing.T) {
	// A label has 1 to 63 characters and a name at most 253.
	label := func(n int) string {
		out := make([]byte, n)
		for i := range out {
			out[i] = 'a'
		}
		return string(out)
	}
	ok, _ := model.NormalizeValue(vtID("vt.fqdn"), label(63)+".test")
	if ok != label(63)+".test" {
		t.Errorf("a label of 63 letters is rejected")
	}
	long := label(63) + "." + label(63) + "." + label(63) + "." + label(63) + ".test"
	if _, err := model.NormalizeValue(vtID("vt.fqdn"), long); err == nil {
		t.Errorf("a name of %d characters is accepted", len(long))
	}
	if _, err := model.NormalizeValue(vtID("vt.fqdn"), label(64)+".test"); err == nil {
		t.Errorf("a label of 64 letters is accepted")
	}
}

// [REQ-100]
func TestBaseValueTypes(t *testing.T) {
	// Each of the ten base kinds is a built-in value type with a stable id.
	for _, kind := range model.BaseKinds() {
		if id := model.BaseValueTypeID(kind); id.String() != "vt."+string(kind) {
			t.Errorf("BaseValueTypeID(%s) = %s", kind, id)
		}
	}
}
