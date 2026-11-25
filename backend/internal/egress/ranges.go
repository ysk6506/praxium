package egress

import (
	"net/netip"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// MaxRanges is how many ranges a list of an operator may have.
const MaxRanges = 256

// ParseRanges reads the list that an operator gives for -source-plaintext-ranges and -source-deny-ranges: addresses and
// ranges (CIDR) of IP addresses separated by commas, such as "10.1.0.0/16,192.0.2.7,2001:db8::/48". A single address is
// the range of that address alone. Names are not taken: a name can be answered with other addresses later, and the
// decisions of the policy are made on the addresses a name was resolved to. An empty text is an empty list. An item that
// is empty, has a zone or is not an address or a range is an error that names the item (the operator wrote it), and so is
// a list of more than MaxRanges. A range has to be written as its first address: "10.1.2.3/16" is an error, not
// "10.1.0.0/16" (host bits are not dropped, which would widen what the operator wrote), "/0" is only "0.0.0.0/0" and
// "::/0", and a range that carries an IPv4 address in IPv6 notation has at least 96 bits.
func ParseRanges(list string) ([]netip.Prefix, error) {
	if strings.TrimSpace(list) == "" {
		return nil, nil
	}
	items := strings.Split(list, ",")
	if len(items) > MaxRanges {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a list of ranges has more than %d items", MaxRanges)
	}
	out := make([]netip.Prefix, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		p, err := parseRange(item)
		if err != nil {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "%q is not an IP address or a range such as 10.1.0.0/16 (a name is not taken: give the addresses)", clipText(item, 64))
		}
		out = append(out, p)
	}
	return out, nil
}

func parseRange(item string) (netip.Prefix, error) {
	if strings.Contains(item, "/") {
		p, err := netip.ParsePrefix(item)
		if err != nil || p.Addr().Zone() != "" {
			return netip.Prefix{}, errNotRange
		}
		// A range is written as its first address. Host bits are never dropped silently: an operator's typo would widen the
		// range ("10.1.2.3/8" is not "10.0.0.0/8"), and for a list that allows plaintext widening is the unsafe way. This
		// includes /0, which is only 0.0.0.0/0 or ::/0 as written.
		if p != p.Masked() {
			return netip.Prefix{}, errNotRange
		}
		// An address that carries an IPv4 address is a range of IPv4 addresses only from /96 on; a shorter one would name
		// IPv6 addresses that are not IPv4 at all, and is almost always a mistake.
		if p.Addr().Is4In6() && p.Bits() < 96 {
			return netip.Prefix{}, errNotRange
		}
		return p, nil
	}
	a, err := netip.ParseAddr(item)
	if err != nil || a.Zone() != "" {
		return netip.Prefix{}, errNotRange
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

var errNotRange = &Error{Kind: KindPolicy, Reason: "not a range"}

func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "…"
}
