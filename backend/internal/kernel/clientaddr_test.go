package kernel

import (
	"net/netip"
	"testing"
)

// [REQ-314] [REQ-862]
// Behind a declared proxy the address is the last item of all the lines of X-Forwarded-For, whatever the lines hold, and
// a line of the caller before it is not read.
func TestCadClientAddrReadsTheLastItemOfAllTheLines(t *testing.T) {
	cases := map[string]struct {
		remote    string
		forwarded []string
		want      string
		vouched   bool
	}{
		"the caller's line and the proxy's line":      {"192.0.2.10:4000", []string{"203.0.113.5", "198.51.100.1"}, "198.51.100.1", true},
		"one line with two items":                     {"192.0.2.10:4000", []string{"203.0.113.5, 198.51.100.1"}, "198.51.100.1", true},
		"the last item has a port":                    {"192.0.2.10:4000", []string{"203.0.113.5", "198.51.100.1:443"}, "198.51.100.1", true},
		"an IPv6 in brackets with a port":             {"192.0.2.10:4000", []string{"[2001:db8::7]:443"}, "2001:db8::7", true},
		"an IPv6 without brackets":                    {"192.0.2.10:4000", []string{"2001:db8::7"}, "2001:db8::7", true},
		"an IPv4 mapped into IPv6 is the IPv4":        {"192.0.2.10:4000", []string{"::ffff:198.51.100.1"}, "198.51.100.1", true},
		"a zone is dropped":                           {"192.0.2.10:4000", []string{"fe80::1%eth0"}, "fe80::1", true},
		"spaces around the item":                      {"192.0.2.10:4000", []string{"  198.51.100.9  "}, "198.51.100.9", true},
		"a line of only spaces is no item":            {"192.0.2.10:4000", []string{"198.51.100.1", "   "}, "192.0.2.10", false},
		"an empty line is no item":                    {"192.0.2.10:4000", []string{"198.51.100.1", ""}, "192.0.2.10", false},
		"a last item that is not an address":          {"192.0.2.10:4000", []string{"198.51.100.1, unknown"}, "192.0.2.10", false},
		"a trailing comma leaves an empty last item":  {"192.0.2.10:4000", []string{"198.51.100.1,"}, "192.0.2.10", false},
		"a network is not an address":                 {"192.0.2.10:4000", []string{"198.51.100.0/24"}, "192.0.2.10", false},
		"no line behind a proxy takes the connection": {"192.0.2.10:4000", nil, "192.0.2.10", false},
		"an unreadable connection behind a proxy":     {"nohost", nil, "", false},
		"a connection with no port, IPv6 in brackets": {"[2001:db8::1]", nil, "2001:db8::1", false},
	}
	for name, c := range cases {
		got, vouched := ClientAddr(c.remote, c.forwarded, true)
		if c.want == "" {
			if got.IsValid() || vouched {
				t.Errorf("%s: got %v, %v; want no address", name, got, vouched)
			}
			continue
		}
		if got != netip.MustParseAddr(c.want) || vouched != c.vouched {
			t.Errorf("%s: got %v, %v; want %s, %v", name, got, vouched, c.want, c.vouched)
		}
	}
}

// [REQ-314] [REQ-862]
// Without a declared proxy the header is not read at all, and an address that cannot be read is no address.
func TestCadUntrustedHeaderIsNotRead(t *testing.T) {
	got, vouched := ClientAddr("192.0.2.10:4000", []string{"203.0.113.9"}, false)
	if got != netip.MustParseAddr("192.0.2.10") || !vouched {
		t.Fatalf("without a proxy the connection is the address: %v, %v", got, vouched)
	}
	if got, vouched := ClientAddr("nohost", nil, false); got.IsValid() || vouched {
		t.Fatalf("an unreadable connection is no address: %v, %v", got, vouched)
	}
}

// [REQ-314]
// The key that counts a caller is the address as text, the host of the connection when there is no address, and the text
// of a connection's host is kept as it was written.
func TestCadCallerKeyIsTheAddressOrTheConnectionHost(t *testing.T) {
	cases := []struct {
		remote    string
		forwarded []string
		trust     bool
		want      string
	}{
		{"192.0.2.10:4000", []string{"198.51.100.1"}, true, "198.51.100.1"},
		{"192.0.2.10:4000", []string{"203.0.113.9", "198.51.100.1"}, true, "198.51.100.1"},
		{"192.0.2.10:4000", []string{"unknown"}, true, "192.0.2.10"},
		{"192.0.2.10:4000", nil, true, "192.0.2.10"},
		{"[2001:db8::1]:4000", nil, false, "2001:db8::1"},
		{"::1", nil, false, "::1"},
		{"nohost", nil, false, "nohost"},
	}
	for _, c := range cases {
		if got := CallerKey(c.remote, c.forwarded, c.trust); got != c.want {
			t.Errorf("CallerKey(%q, %q, %v) = %q, want %q", c.remote, c.forwarded, c.trust, got, c.want)
		}
	}
}

// [REQ-862]
// An IPv4 address is its own group and an IPv6 address is its /64; a missing address is the group "none".
func TestCadAddrGroupCountsAnIPv6NetworkAsOne(t *testing.T) {
	cases := map[string]string{
		"192.0.2.10":                       "192.0.2.10",
		"2001:db8:0:1::1":                  "2001:db8:0:1::/64",
		"2001:db8:0:1:ffff:ffff:ffff:ffff": "2001:db8:0:1::/64",
		"2001:db8:0:2::1":                  "2001:db8:0:2::/64",
	}
	for in, want := range cases {
		if got := AddrGroup(netip.MustParseAddr(in)); got != want {
			t.Errorf("AddrGroup(%s) = %q, want %q", in, got, want)
		}
	}
	if got := AddrGroup(netip.Addr{}); got != "none" {
		t.Errorf("no address is %q, want none", got)
	}
}
