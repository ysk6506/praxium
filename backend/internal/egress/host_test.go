package egress

import (
	"strings"
	"testing"
)

// [REQ-661]
func TestNormalizeHostAcceptsNamesAndPlainLiterals(t *testing.T) {
	cases := map[string]string{
		"Example.COM":           "example.com",
		"example.com.":          "example.com",
		"db_1.example.test":     "db_1.example.test",
		"localhost":             "localhost",
		"LOCALHOST.":            "localhost",
		"a":                     "a",
		"127.0.0.1":             "127.0.0.1",
		"10.1.2.3.":             "10.1.2.3",
		"::1":                   "::1",
		"2001:DB8::0:1":         "2001:db8::1",
		"::ffff:10.0.0.1":       "::ffff:10.0.0.1",
		"xn--bcher-kva.example": "xn--bcher-kva.example",
		"3com.example":          "3com.example",
	}
	for in, want := range cases {
		got, _, bad := NormalizeHost(in)
		if bad != "" || got != want {
			t.Errorf("NormalizeHost(%q) = %q, %q; want %q", in, got, bad, want)
		}
	}
}

// [REQ-661]
func TestNormalizeHostGivesTheAddressOfALiteralOnly(t *testing.T) {
	if _, ip, bad := NormalizeHost("10.1.2.3"); bad != "" || ip.String() != "10.1.2.3" {
		t.Errorf("literal: %v %q", ip, bad)
	}
	if _, ip, bad := NormalizeHost("[::1]"[1:4]); bad != "" || !ip.IsLoopback() {
		t.Errorf("v6 literal: %v %q", ip, bad)
	}
	if _, ip, bad := NormalizeHost("example.test"); bad != "" || ip.IsValid() {
		t.Errorf("name: %v %q", ip, bad)
	}
}

// [REQ-661]
func TestNormalizeHostRefusesWhatIsNotAPlainHost(t *testing.T) {
	long := strings.Repeat("a", 64) + ".test"
	tooLong := strings.Repeat("abcdefghi.", 26) + "test"
	bad := []string{
		"", ".", "..", ".a", "a..b", "a b", "a\tb", "a\x00b", "a\nb", "host/path", "host\\path", "user@host", "host?x", "host#x", "host%41",
		"host:80", "[::1]", "fe80::1%eth0", "é.test", "ＬＯＣＡＬ.test", "a,b", "a;b", "a'b",
		// numbers that a resolver reads as an IPv4 address
		"2130706433", "0x7f000001", "0x7f.1", "0X7F.0.0.1", "0177.0.0.1", "127.1", "127.0.1", "1.2.3", "1.2.3.4.5", "01.2.3.4", "256.1.1.1",
		"example.123", "0x", "0xzz.0x",
		// IPv6 forms that are not plain
		"::g", "1:2:3:4:5:6:7:8:9", "::1::", "1.2.3.4:5",
		long, tooLong, strings.Repeat("a", 300),
	}
	for _, in := range bad {
		if got, _, why := NormalizeHost(in); why == "" {
			t.Errorf("NormalizeHost(%q) = %q, no refusal", in, got)
		}
	}
}

// [REQ-661]
func TestParsePortReadsDigitsOnly(t *testing.T) {
	good := map[string]uint16{"1": 1, "80": 80, "443": 443, "65535": 65535, "00080": 80}
	for in, want := range good {
		if got, ok := parsePort(in); !ok || got != want {
			t.Errorf("parsePort(%q) = %d, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "0", "00000", "65536", "99999", "123456", "-1", "+80", "8 0", "http", "0x50", "1e2", "８０"} {
		if got, ok := parsePort(in); ok {
			t.Errorf("parsePort(%q) = %d", in, got)
		}
	}
}
