package egress

import (
	"context"
	"net/netip"
	"strings"
	"testing"
)

func hxbRanges(t *testing.T, text string) []netip.Prefix {
	t.Helper()
	out, err := ParseRanges(text)
	if err != nil {
		t.Fatalf("ParseRanges(%q): %v", text, err)
	}
	return out
}

// [REQ-820] A connection without TLS goes to the loopback and to the addresses of the plaintext ranges, and to no other: an
// empty list is the loopback alone, an IPv4 address inside an IPv6 one is judged as IPv4, and an address that carries
// another inside it in a transition range has to have both in the list.
func TestHxbPlaintextRangesDecideByTheAddress(t *testing.T) {
	list := hxbRanges(t, "10.1.0.0/16, 192.0.2.7 ,2001:db8::/32")
	c, err := compile(Policy{PlaintextRanges: list})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := compile(Policy{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		addr      string
		inList    bool
		inNoRange bool
	}{
		{"127.0.0.1", true, true},
		{"127.9.9.9", true, true},
		{"::1", true, true},
		{"::ffff:127.0.0.1", true, true},
		{"10.1.2.3", true, false},
		{"::ffff:10.1.2.3", true, false},
		{"10.2.0.1", false, false},
		{"192.0.2.7", true, false},
		{"192.0.2.8", false, false},
		{"2001:db8::1", true, false},
		{"2001:db9::1", false, false},
		// 6to4 of 10.1.2.3: the address inside is in the list and the one around it is not, so it does not pass
		{"2002:a01:203::1", false, false},
		{"93.184.216.34", false, false},
	} {
		ip := pAddr(t, tc.addr)
		if got := c.plaintextAllowed(ip); got != tc.inList {
			t.Errorf("%s with the list: %v, want %v", tc.addr, got, tc.inList)
		}
		if got := empty.plaintextAllowed(ip); got != tc.inNoRange {
			t.Errorf("%s with no range: %v, want %v", tc.addr, got, tc.inNoRange)
		}
	}
}

// [REQ-820] The decision is made on the addresses the name was resolved to, all of them: a name that has one address
// outside the list is refused, whatever the name looks like, and what is dialed is the address that was judged.
func TestHxbPlaintextRangesAreJudgedOnTheResolvedAddresses(t *testing.T) {
	res := &dtResolver{answers: map[string][][]netip.Addr{
		"in.example.test":        {dtAddrs(t, "10.1.2.3")},
		"out.example.test":       {dtAddrs(t, "10.9.9.9")},
		"mixed.example.test":     {dtAddrs(t, "10.1.2.3", "10.9.9.9")},
		"localhost":              {dtAddrs(t, "10.9.9.9")},
		"meta.example.test":      {dtAddrs(t, "169.254.169.254")},
		"loop.example.test":      {dtAddrs(t, "127.0.0.1")},
		"mappedin.example.test":  {dtAddrs(t, "::ffff:10.1.2.3")},
		"rebinding.example.test": {dtAddrs(t, "10.1.2.3"), dtAddrs(t, "10.9.9.9")},
	}}
	d, seen := dtDialer(t, Config{Resolver: res, Policy: Policy{PlaintextRanges: hxbRanges(t, "10.1.0.0/16")}}, nil)
	ctx := WithPlaintext(context.Background())
	for _, name := range []string{"in.example.test", "loop.example.test", "mappedin.example.test", "rebinding.example.test"} {
		conn, err := d.DialContext(ctx, "tcp", name+":80")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		_ = conn.Close()
	}
	if got := (*seen)[0].address; got != "10.1.2.3:80" {
		t.Errorf("dialed %q for the name in the list", got)
	}
	before := len(*seen)
	for _, name := range []string{"out.example.test", "mixed.example.test", "localhost", "meta.example.test"} {
		_, err := d.DialContext(ctx, "tcp", name+":80")
		e := dtKind(t, err)
		if e.Kind != KindPolicy || e.Reason != reasonAddress || strings.Contains(err.Error(), "10.9.9.9") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(*seen) != before {
		t.Error("a name that was refused was connected to")
	}
	// the same names with TLS are not asked about the ranges at all
	conn, err := d.DialContext(context.Background(), "tcp", "out.example.test:443")
	if err != nil {
		t.Fatalf("with TLS: %v", err)
	}
	_ = conn.Close()
}

// [REQ-820] A range that covers every address does not open what the policy always refuses: the link-local range (the
// metadata service of the clouds), the multicast range and the unspecified address, nor the deny list and this server's own API.
func TestHxbPlaintextRangesOfEveryAddressKeepTheRefusedRanges(t *testing.T) {
	res := &dtResolver{answers: map[string][][]netip.Addr{
		"meta.example.test":  {dtAddrs(t, "169.254.169.254")},
		"multi.example.test": {dtAddrs(t, "224.0.0.1")},
		"zero.example.test":  {dtAddrs(t, "0.0.0.0")},
		"deny.example.test":  {dtAddrs(t, "10.1.2.3")},
		"self.example.test":  {dtAddrs(t, "10.5.5.5")},
		"fine.example.test":  {dtAddrs(t, "93.184.216.34")},
		"fine6.example.test": {dtAddrs(t, "2606:2800:220:1::1")},
	}}
	pol := Policy{
		PlaintextRanges: hxbRanges(t, "0.0.0.0/0,::/0"),
		Deny:            hxbRanges(t, "10.1.0.0/16"),
		Self:            []netip.AddrPort{netip.MustParseAddrPort("10.5.5.5:8080")},
	}
	d, _ := dtDialer(t, Config{Resolver: res, Policy: pol}, nil)
	ctx := WithPlaintext(context.Background())
	for _, name := range []string{"meta.example.test", "multi.example.test", "zero.example.test", "deny.example.test"} {
		if _, err := d.DialContext(ctx, "tcp", name+":80"); dtKind(t, err).Kind != KindPolicy {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := d.DialContext(ctx, "tcp", "self.example.test:8080"); dtKind(t, err).Kind != KindPolicy {
		t.Errorf("self: %v", err)
	}
	for _, name := range []string{"fine.example.test", "fine6.example.test"} {
		conn, err := d.DialContext(ctx, "tcp", name+":80")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		_ = conn.Close()
	}
}

// [REQ-820] The two ways to allow a connection without TLS are not given together, and a range that is not valid is refused
// when the policy is made. A policy of the old bool alone allows every address, as it did.
func TestHxbAllowInsecureAndPlaintextRangesAreNotGivenTogether(t *testing.T) {
	if _, err := New(Config{Policy: Policy{AllowInsecure: true, PlaintextRanges: hxbRanges(t, "10.0.0.0/8")}}); err == nil {
		t.Error("both were accepted")
	}
	if _, err := New(Config{Policy: Policy{PlaintextRanges: []netip.Prefix{{}}}}); err == nil {
		t.Error("a range that is not valid was accepted")
	}
	c, err := compile(Policy{AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"10.9.9.9", "93.184.216.34", "2606:2800:220:1::1", "2002:a01:203::1"} {
		if !c.plaintextAllowed(pAddr(t, a)) {
			t.Errorf("AllowInsecure does not allow %s", a)
		}
	}
	// a mapped range is written as the IPv4 range that the addresses are compared in
	mapped, err := compile(Policy{PlaintextRanges: []netip.Prefix{netip.MustParsePrefix("::ffff:10.1.0.0/112")}})
	if err != nil {
		t.Fatal(err)
	}
	if !mapped.plaintextAllowed(pAddr(t, "10.1.2.3")) || mapped.plaintextAllowed(pAddr(t, "10.2.2.3")) {
		t.Error("a mapped range does not act as the IPv4 range")
	}
}

// [REQ-820] The Tracker of PostgreSQL (sslmode=disable) is judged by the same ranges.
func TestHxbTheTrackerOfAPlaintextConnectionIsJudgedByTheRanges(t *testing.T) {
	res := &dtResolver{answers: map[string][][]netip.Addr{
		"db.example.test":    {dtAddrs(t, "10.1.2.3")},
		"other.example.test": {dtAddrs(t, "10.9.9.9")},
	}}
	d, seen := dtDialer(t, Config{Resolver: res, Policy: Policy{PlaintextRanges: hxbRanges(t, "10.1.0.0/16")}}, nil)
	tr := d.Tracker(TrackerConfig{Plaintext: true, Budget: 1 << 20})
	conn, err := tr.Dial("tcp", "db.example.test:5432")
	if err != nil {
		t.Fatalf("in the list: %v", err)
	}
	_ = conn.Close()
	if _, err := tr.Dial("tcp", "other.example.test:5432"); dtKind(t, err).Kind != KindPolicy {
		t.Errorf("outside the list: %v", err)
	}
	if len(*seen) != 1 {
		t.Errorf("%d connections", len(*seen))
	}
}

// [REQ-820] The list an operator writes is read as addresses and ranges, nothing else.
func TestHxbParseRangesTakesAddressesAndRangesOnly(t *testing.T) {
	got, err := ParseRanges(" 10.1.0.0/16,192.0.2.7 , 2001:db8::7,10.1.2.0/24")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.1.0.0/16", "192.0.2.7/32", "2001:db8::7/128", "10.1.2.0/24"}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Errorf("item %d is %s, want %s", i, got[i], want[i])
		}
	}
	if none, err := ParseRanges("  "); err != nil || none != nil {
		t.Errorf("an empty list: %v %v", none, err)
	}
	for _, bad := range []string{"db.example.test", "10.1.0.0/33", "10.1.0.0/", "10.1.0.0/16,", ",", "10.1.0.0/16,,10.2.0.0/16", "fe80::1%eth0", "fe80::1%eth0/64", "10.1.0", "*", "0.0.0.0/0 ::/0", "10.1.0.0/16;10.2.0.0/16"} {
		if _, err := ParseRanges(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	long := strings.Repeat("10.0.0.1,", MaxRanges) + "10.0.0.2"
	if _, err := ParseRanges(long); err == nil {
		t.Error("a list that is too long was accepted")
	}
	if _, err := ParseRanges("0.0.0.0/0,::/0"); err != nil {
		t.Errorf("the spelling of every address: %v", err)
	}
}

// [REQ-820] A range is written as its first address: host bits are an error that names the item, never dropped (that would
// widen what the operator wrote), /0 is only the two spellings of every address, and a range of IPv6 notation that carries
// IPv4 addresses has at least 96 bits.
func TestHxbParseRangesRefusesHostBitsAndTooWideMappedRanges(t *testing.T) {
	for _, bad := range []string{
		"10.1.2.3/16", "10.1.2.3/8", "10.1.2.3/0", "10.0.0.1/31", "1.2.3.4/0", "::1/0", "2001:db8::1/32",
		"::ffff:10.0.0.0/8", "::ffff:0.0.0.0/95", "::ffff:0:0/0", "::ffff:10.1.2.3/100",
	} {
		_, err := ParseRanges("192.0.2.7," + bad)
		if err == nil {
			t.Errorf("%q was accepted", bad)
			continue
		}
		if !strings.Contains(err.Error(), bad) {
			t.Errorf("the error for %q does not name the item: %v", bad, err)
		}
	}
	for _, ok := range []string{"10.0.0.0/8", "10.1.2.3/32", "0.0.0.0/0", "::/0", "::ffff:10.0.0.0/104", "::ffff:10.1.2.3/128", "2001:db8::/32"} {
		if _, err := ParseRanges(ok); err != nil {
			t.Errorf("%q was refused: %v", ok, err)
		}
	}
}
