package egress

import (
	"net/netip"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Policy says which connections the dialer may make. The zero value is the policy of the design: the ranges of
// refusedRanges are refused and nothing else is, so an address in a private range or on the loopback is allowed
// (the sources of a data centre are usually there).
type Policy struct {
	// Hosts, when not empty, lists the only names and IP literals that may be dialed (compared after
	// NormalizeHost). A source is bound to the one host of its definition this way.
	Hosts []string
	// Ports, when not empty, lists the only ports that may be dialed.
	Ports []uint16
	// Deny adds ranges to the refused ones (the flag of the operator). It also holds against AllowRanges.
	Deny []netip.Prefix
	// Self lists the address and port of this server's own API. An unspecified address (0.0.0.0 or ::) stands
	// for every address of this machine, the loopback included. It also holds against AllowRanges.
	Self []netip.AddrPort
	// DenyPrivate also refuses the loopback and the private ranges (10/8, 172.16/12, 192.168/16, 100.64/10,
	// 198.18/15, fc00::/7, fec0::/10), except the addresses in AllowRanges. It is the strict policy for a
	// deployment whose sources are all public.
	DenyPrivate bool
	// AllowRanges are the exceptions of DenyPrivate. They open only what DenyPrivate closed: a range of
	// refusedRanges, Deny and Self stay refused whatever is listed here.
	AllowRanges []netip.Prefix
	// AllowInsecure lets a source be reached without TLS (http, or sslmode=disable of PostgreSQL) when the host is
	// not the loopback. It never lets a certificate go unverified: nothing does.
	AllowInsecure bool
}

type rangeRule struct {
	prefix netip.Prefix
	reason string
}

func rules(reason string, prefixes ...string) []rangeRule {
	out := make([]rangeRule, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, rangeRule{netip.MustParsePrefix(p), reason})
	}
	return out
}

// refusedRanges are always refused, whatever the policy says. They are the addresses that a server must never
// reach on the word of a definition: the unspecified address, the multicast and the reserved ranges, the link-local
// ranges (the metadata service of the clouds is there), the metadata addresses outside them, and the ranges of
// transition mechanisms that carry an IPv4 address in a form the checks do not read (an embedded address is read
// separately, see embedded).
var refusedRanges = concat(
	rules("the address is unspecified", "0.0.0.0/8", "::/128"),
	rules("the address is multicast", "224.0.0.0/4", "ff00::/8"),
	rules("the address is reserved", "240.0.0.0/4", "192.0.0.0/24", "100::/64"),
	rules("the address is link-local", "169.254.0.0/16", "fe80::/10"),
	rules("the address is of a cloud metadata service", "100.100.100.200/32", "168.63.129.16/32", "fd00:ec2::/32"),
	rules("the address is of a tunnel mechanism", "2001::/32", "64:ff9b:1::/48"),
)

// privateRanges are refused only when the policy says DenyPrivate.
var privateRanges = rules("the address is loopback or private",
	"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"100.64.0.0/10", "198.18.0.0/15", "fc00::/7", "fec0::/10")

// embeddedRanges hold an IPv4 address in the last four bytes (the first three) or in bytes 2 to 5 (6to4). The
// IPv4 address inside is judged as well as the IPv6 address around it.
var (
	embeddedTail = rules("", "::/96", "64:ff9b::/96", "::ffff:0:0:0/96")
	embedded6to4 = netip.MustParsePrefix("2002::/16")
)

func concat(lists ...[]rangeRule) []rangeRule {
	var out []rangeRule
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// canonical drops the zone of an address (netip.Prefix.Contains is false for an address with a zone, which
// would let fe80::1%eth0 past a check) and turns an IPv4-mapped IPv6 address into the IPv4 address.
func canonical(a netip.Addr) netip.Addr {
	return a.WithZone("").Unmap()
}

// embedded returns the IPv4 address that an IPv6 address of a transition range carries.
func embedded(a netip.Addr) (netip.Addr, bool) {
	if !a.Is6() {
		return netip.Addr{}, false
	}
	b := a.As16()
	if a.Compare(netip.AddrFrom16([16]byte{15: 2})) < 0 {
		return netip.Addr{}, false // :: and ::1 carry no IPv4 address
	}
	if embedded6to4.Contains(a) {
		return netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}), true
	}
	for _, r := range embeddedTail {
		if r.prefix.Contains(a) {
			return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
		}
	}
	return netip.Addr{}, false
}

// compiled is a Policy after its checks, in the form the dialer looks things up in.
type compiled struct {
	hosts       map[string]bool
	ports       map[uint16]bool
	deny        []netip.Prefix
	allow       []netip.Prefix
	self        []netip.AddrPort
	denyPrivate bool
	insecure    bool
}

func compile(p Policy) (*compiled, error) {
	c := &compiled{denyPrivate: p.DenyPrivate, insecure: p.AllowInsecure}
	if len(p.Hosts) > 0 {
		c.hosts = map[string]bool{}
		for _, h := range p.Hosts {
			name, _, bad := NormalizeHost(h)
			if bad != "" {
				return nil, kernel.Errorf(kernel.CodeInvalidValue, "egress: a host of the policy is not valid: %s", bad)
			}
			c.hosts[name] = true
		}
	}
	if len(p.Ports) > 0 {
		c.ports = map[uint16]bool{}
		for _, port := range p.Ports {
			if port == 0 {
				return nil, kernel.Errorf(kernel.CodeInvalidValue, "egress: port 0 is not a port of the policy")
			}
			c.ports[port] = true
		}
	}
	var err error
	if c.deny, err = prefixes("Deny", p.Deny); err != nil {
		return nil, err
	}
	if c.allow, err = prefixes("AllowRanges", p.AllowRanges); err != nil {
		return nil, err
	}
	for _, s := range p.Self {
		if !s.IsValid() || s.Port() == 0 {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "egress: an address of Self is not valid")
		}
		c.self = append(c.self, netip.AddrPortFrom(canonical(s.Addr()), s.Port()))
	}
	return c, nil
}

// prefixes validates ranges and writes an IPv4-mapped range as the IPv4 range, which is the form addresses are
// compared in.
func prefixes(field string, in []netip.Prefix) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(in))
	for _, p := range in {
		if !p.IsValid() {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "egress: a range of %s is not valid", field)
		}
		addr := p.Addr().WithZone("")
		bits := p.Bits()
		if addr.Is4In6() {
			if bits < 96 {
				return nil, kernel.Errorf(kernel.CodeInvalidValue, "egress: a mapped range of %s must be at least /96", field)
			}
			addr, bits = addr.Unmap(), bits-96
		}
		out = append(out, netip.PrefixFrom(addr, bits).Masked())
	}
	return out, nil
}

func within(list []netip.Prefix, ip netip.Addr) bool {
	for _, p := range list {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func ruleReason(list []rangeRule, ip netip.Addr) string {
	for _, r := range list {
		if r.prefix.Contains(ip) {
			return r.reason
		}
	}
	return ""
}

// checkHost says why a host is not allowed, or returns "".
func (c *compiled) checkHost(host string) string {
	if c.hosts != nil && !c.hosts[host] {
		return "the host is not one the source may use"
	}
	return ""
}

func (c *compiled) checkPort(port uint16) string {
	if c.ports != nil && !c.ports[port] {
		return "the port is not one the source may use"
	}
	return ""
}

// judge says why a connection to ip and port is refused, or returns "". locals lists the addresses of this
// machine, for a Self entry with an unspecified address.
func (c *compiled) judge(ip netip.Addr, port uint16, locals func() ([]netip.Addr, error)) string {
	if !ip.IsValid() {
		return "the address is not valid"
	}
	ip = canonical(ip)
	candidates := []netip.Addr{ip}
	if inner, ok := embedded(ip); ok {
		candidates = append(candidates, inner)
	}
	for _, cand := range candidates {
		if why := c.judgeOne(cand, port, locals); why != "" {
			return why
		}
	}
	return ""
}

func (c *compiled) judgeOne(ip netip.Addr, port uint16, locals func() ([]netip.Addr, error)) string {
	if why := ruleReason(refusedRanges, ip); why != "" {
		return why
	}
	if why := c.selfReason(ip, port, locals); why != "" {
		return why
	}
	if within(c.deny, ip) {
		return "the address is on the deny list of the operator"
	}
	if c.denyPrivate && ruleReason(privateRanges, ip) != "" && !within(c.allow, ip) {
		return "the address is loopback or private and the policy does not allow it"
	}
	return ""
}

func (c *compiled) selfReason(ip netip.Addr, port uint16, locals func() ([]netip.Addr, error)) string {
	for _, s := range c.self {
		if s.Port() != port {
			continue
		}
		if s.Addr() == ip {
			return "the address is this server's own API"
		}
		if !s.Addr().IsUnspecified() {
			continue
		}
		if ip.IsLoopback() {
			return "the address is this server's own API"
		}
		own, err := locals()
		if err != nil {
			return "the addresses of this machine could not be listed"
		}
		for _, l := range own {
			if canonical(l) == ip {
				return "the address is this server's own API"
			}
		}
	}
	return ""
}
