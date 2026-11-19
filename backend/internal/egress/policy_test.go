package egress

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func pAddr(t *testing.T, text string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(text)
	if err != nil {
		t.Fatalf("test address %q: %v", text, err)
	}
	return a
}

func pPolicy(t *testing.T, p Policy) *compiled {
	t.Helper()
	c, err := compile(p)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return c
}

func pNoLocals() ([]netip.Addr, error) { return nil, nil }

// [REQ-658]
func TestDefaultPolicyAllowsPublicPrivateAndLoopbackAddresses(t *testing.T) {
	c := pPolicy(t, Policy{})
	allowed := []string{
		"8.8.8.8", "93.184.216.34", "10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.1.1", "127.0.0.1", "127.9.9.9", "::1",
		"100.64.0.1", "198.18.0.1", "fd12::1", "fec0::1", "2001:4860:4860::8888", "2606:4700::1111",
		"192.0.2.1", "2001:db8::1",
		"::ffff:8.8.8.8", "::ffff:10.0.0.1", "64:ff9b::808:808", "2002:808:808::1",
	}
	for _, text := range allowed {
		if why := c.judge(pAddr(t, text), 443, pNoLocals); why != "" {
			t.Errorf("%s is refused (%s), the design allows it", text, why)
		}
	}
}

// [REQ-658]
func TestEveryPolicyRefusesTheRangesOfTheDesign(t *testing.T) {
	refusedWhatever := []string{
		// unspecified and "this network"
		"0.0.0.0", "0.1.2.3", "0.255.255.255", "::", "::ffff:0.0.0.0",
		// link local and the metadata services of the clouds
		"169.254.169.254", "169.254.0.1", "169.254.255.255", "fe80::1", "fe80::1%eth0", "febf::1", "fd00:ec2::254", "fd00:ec2::23",
		"100.100.100.200", "168.63.129.16",
		// multicast and reserved
		"224.0.0.1", "239.255.255.250", "ff02::1", "ff0e::1", "255.255.255.255", "240.0.0.1", "192.0.0.192", "100::1",
		// IPv4 inside IPv6: mapped, compatible, translated, NAT64, 6to4, Teredo
		"::ffff:169.254.169.254", "::ffff:224.0.0.1", "::169.254.169.254", "::ffff:0:a9fe:a9fe", "64:ff9b::a9fe:a9fe",
		"64:ff9b::0.0.0.0", "2002:a9fe:a9fe::1", "2002:e000:1::1", "2001::1", "2001:0:4136:e378:8000:63bf:3fff:fdd2", "64:ff9b:1::1",
	}
	policies := map[string]Policy{
		"zero":    {},
		"strict":  {DenyPrivate: true},
		"allowed": {DenyPrivate: true, AllowRanges: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}},
	}
	for name, p := range policies {
		c := pPolicy(t, p)
		for _, text := range refusedWhatever {
			if why := c.judge(pAddr(t, text), 443, pNoLocals); why == "" {
				t.Errorf("policy %s lets %s through", name, text)
			}
		}
	}
}

// [REQ-658]
func TestRefusalGivesAReasonWithoutTheAddress(t *testing.T) {
	c := pPolicy(t, Policy{})
	why := c.judge(pAddr(t, "169.254.169.254"), 80, pNoLocals)
	if !strings.Contains(why, "link-local") || strings.Contains(why, "169") {
		t.Errorf("reason = %q", why)
	}
}

// [REQ-658]
func TestDenyPrivateRefusesLoopbackAndPrivateAddressesExceptTheOnesAllowed(t *testing.T) {
	c := pPolicy(t, Policy{DenyPrivate: true, AllowRanges: []netip.Prefix{
		netip.MustParsePrefix("10.1.0.0/16"), netip.MustParsePrefix("::ffff:127.0.0.0/104"), netip.MustParsePrefix("fd00:5::/32"),
	}})
	refusedNow := []string{
		"::1", "10.0.0.1", "10.2.0.1", "172.16.0.1", "172.31.255.255", "192.168.0.1", "100.64.0.1", "198.19.0.1", "fd00::1", "fec0::1",
		"::ffff:10.0.0.1", "64:ff9b::a00:1", "2002:a00:1::1",
	}
	for _, text := range refusedNow {
		if why := c.judge(pAddr(t, text), 5432, pNoLocals); why == "" {
			t.Errorf("%s is allowed under DenyPrivate", text)
		}
	}
	open := []string{"8.8.8.8", "2606:4700::1111", "10.1.2.3", "127.0.0.1", "fd00:5::7", "64:ff9b::808:808"}
	for _, text := range open {
		if why := c.judge(pAddr(t, text), 5432, pNoLocals); why != "" {
			t.Errorf("%s is refused (%s)", text, why)
		}
	}
}

// [REQ-658]
func TestAllowRangesNeverOpenWhatTheOperatorOrThePolicyDenied(t *testing.T) {
	all := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
	c := pPolicy(t, Policy{
		DenyPrivate: true,
		AllowRanges: all,
		Deny:        []netip.Prefix{netip.MustParsePrefix("10.9.0.0/16"), netip.MustParsePrefix("::ffff:10.8.0.0/112")},
		Self:        []netip.AddrPort{netip.MustParseAddrPort("10.0.0.5:8080")},
	})
	for _, text := range []string{"10.9.1.1", "10.8.1.1"} {
		if c.judge(pAddr(t, text), 443, pNoLocals) == "" {
			t.Errorf("%s is on the deny list and still allowed", text)
		}
	}
	if c.judge(pAddr(t, "10.0.0.5"), 8080, pNoLocals) == "" {
		t.Error("the server's own address is allowed")
	}
	if c.judge(pAddr(t, "10.0.0.5"), 5432, pNoLocals) != "" {
		t.Error("another port of the same address is refused")
	}
}

// [REQ-658]
func TestDenyListWorksOnTheEmbeddedAddressToo(t *testing.T) {
	c := pPolicy(t, Policy{Deny: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("2001:db8:ffff::/48")}})
	for _, text := range []string{"203.0.113.9", "::ffff:203.0.113.9", "64:ff9b::cb00:7109", "2002:cb00:7109::1", "2001:db8:ffff::1"} {
		if c.judge(pAddr(t, text), 443, pNoLocals) == "" {
			t.Errorf("%s is not refused", text)
		}
	}
	if c.judge(pAddr(t, "203.0.114.9"), 443, pNoLocals) != "" {
		t.Error("an address next to the denied range is refused")
	}
}

// [REQ-658]
func TestServersOwnAddressIsRefusedOnItsPortOnly(t *testing.T) {
	locals := func() ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.0.0.5"), netip.MustParseAddr("::ffff:10.0.0.6")}, nil
	}
	exact := pPolicy(t, Policy{Self: []netip.AddrPort{netip.MustParseAddrPort("[::ffff:192.168.7.7]:8080")}})
	if exact.judge(pAddr(t, "192.168.7.7"), 8080, locals) == "" {
		t.Error("a mapped Self address does not match its IPv4 form")
	}
	if exact.judge(pAddr(t, "192.168.7.7"), 8081, locals) != "" || exact.judge(pAddr(t, "192.168.7.8"), 8080, locals) != "" {
		t.Error("another port or another address is refused")
	}
	wide := pPolicy(t, Policy{Self: []netip.AddrPort{netip.MustParseAddrPort("0.0.0.0:8080")}})
	refusedTargets := []string{"127.0.0.1", "::1", "10.0.0.5", "10.0.0.6"}
	for _, text := range refusedTargets {
		if wide.judge(pAddr(t, text), 8080, locals) == "" {
			t.Errorf("%s:8080 is allowed though the server listens on every address", text)
		}
		if wide.judge(pAddr(t, text), 8081, locals) != "" {
			t.Errorf("%s:8081 is refused", text)
		}
	}
	if wide.judge(pAddr(t, "8.8.8.8"), 8080, locals) != "" {
		t.Error("an address that is not this machine's is refused")
	}
}

// [REQ-658]
func TestWhenTheLocalAddressesCannotBeListedAnOpenSelfEntryRefuses(t *testing.T) {
	wide := pPolicy(t, Policy{Self: []netip.AddrPort{netip.MustParseAddrPort("[::]:8080")}})
	failing := func() ([]netip.Addr, error) { return nil, errors.New("no interfaces") }
	if wide.judge(pAddr(t, "8.8.8.8"), 8080, failing) == "" {
		t.Error("an address is allowed though this machine's addresses are unknown")
	}
	if wide.judge(pAddr(t, "8.8.8.8"), 80, failing) != "" {
		t.Error("another port is refused")
	}
}

// [REQ-658]
func TestPolicyWithBadValuesIsRefused(t *testing.T) {
	bad := map[string]Policy{
		"host with a path":      {Hosts: []string{"a/b"}},
		"numeric host":          {Hosts: []string{"2130706433"}},
		"port zero":             {Ports: []uint16{443, 0}},
		"invalid deny":          {Deny: []netip.Prefix{{}}},
		"invalid allow":         {AllowRanges: []netip.Prefix{{}}},
		"short mapped deny":     {Deny: []netip.Prefix{netip.MustParsePrefix("::ffff:0.0.0.0/64")}},
		"invalid self":          {Self: []netip.AddrPort{{}}},
		"self without the port": {Self: []netip.AddrPort{netip.AddrPortFrom(netip.MustParseAddr("10.0.0.1"), 0)}},
	}
	for name, p := range bad {
		if _, err := compile(p); err == nil {
			t.Errorf("%s: no error", name)
		}
		if _, err := New(Config{Policy: p}); err == nil {
			t.Errorf("%s: New made a dialer", name)
		}
	}
}

// [REQ-658]
func TestHostAndPortLists(t *testing.T) {
	c := pPolicy(t, Policy{Hosts: []string{"API.Example.Test.", "10.0.0.5"}, Ports: []uint16{443, 8443}})
	if c.checkHost("api.example.test") != "" || c.checkHost("10.0.0.5") != "" {
		t.Error("a listed host is refused")
	}
	if c.checkHost("other.example.test") == "" || c.checkHost("10.0.0.6") == "" {
		t.Error("an unlisted host is allowed")
	}
	if c.checkPort(443) != "" || c.checkPort(8443) != "" || c.checkPort(80) == "" {
		t.Error("the port list is not applied")
	}
	open := pPolicy(t, Policy{})
	if open.checkHost("anything.test") != "" || open.checkPort(1) != "" {
		t.Error("an empty list restricts")
	}
}

// [REQ-658]
func TestAnAddressThatIsNotValidIsRefused(t *testing.T) {
	if pPolicy(t, Policy{}).judge(netip.Addr{}, 443, pNoLocals) == "" {
		t.Error("the zero address is allowed")
	}
}
