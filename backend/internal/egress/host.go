package egress

import (
	"net/netip"
	"strconv"
	"strings"
)

// NormalizeHost checks a host the way the dialer does and returns its canonical form: lower case, without the
// one trailing dot of an absolute name. An IP literal comes back in the canonical form of net/netip together
// with the address. A host that is not a plain name or a plain IP literal is refused with a reason:
//
//   - only the ASCII letters, digits, '-', '.' and '_' of a name (and ':' of an IPv6 literal) are allowed, so no
//     userinfo, path, port, zone, escape, space or control character gets through;
//   - a name whose last label is a number (a decimal, an octal or a hexadecimal "IP address" such as
//     2130706433, 0177.0.0.1, 0x7f.1 or 127.1) is refused unless it is a dotted-quad that net/netip accepts,
//     because the resolvers of the system read such a name as an IPv4 address in a form the checks do not
//     recognise;
//   - a name with an empty or a too long label, or longer than 253 bytes, is refused.
func NormalizeHost(host string) (string, netip.Addr, string) {
	if len(host) == 0 || len(host) > 255 {
		return "", netip.Addr{}, "the host is empty or too long"
	}
	host = strings.ToLower(host)
	if len(host) > 1 && strings.HasSuffix(host, ".") {
		host = host[:len(host)-1]
	}
	if strings.Contains(host, ":") {
		return literalV6(host)
	}
	return normalizeName(host)
}

func normalizeName(host string) (string, netip.Addr, string) {
	for i := 0; i < len(host); i++ {
		if !nameByte(host[i]) {
			return "", netip.Addr{}, "the host holds a character that a host name does not have"
		}
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return "", netip.Addr{}, "the host has an empty or a too long label"
		}
	}
	if numericLabel(labels[len(labels)-1]) {
		ip, err := netip.ParseAddr(host)
		if err != nil || !ip.Is4() {
			return "", netip.Addr{}, "the host is a number that is not a dotted IPv4 address"
		}
		return ip.String(), ip, ""
	}
	if len(host) > 253 {
		return "", netip.Addr{}, "the host is empty or too long"
	}
	return host, netip.Addr{}, ""
}

func literalV6(host string) (string, netip.Addr, string) {
	for i := 0; i < len(host); i++ {
		c := host[i]
		if c != ':' && c != '.' && !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return "", netip.Addr{}, "the host holds a character that an IP address does not have"
		}
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is6() {
		return "", netip.Addr{}, "the host is not an IPv6 address"
	}
	return ip.String(), ip, ""
}

func nameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_'
}

// numericLabel tells a label that a resolver may read as a number: all decimal digits, or 0x and hexadecimal digits.
func numericLabel(label string) bool {
	if strings.HasPrefix(label, "0x") {
		label = label[2:]
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
				return false
			}
		}
		return true
	}
	for i := 0; i < len(label); i++ {
		if label[i] < '0' || label[i] > '9' {
			return false
		}
	}
	return len(label) > 0
}

// parsePort reads a port written as decimal digits only (no sign, no service name) between 1 and 65535.
func parsePort(text string) (uint16, bool) {
	if text == "" || len(text) > 5 {
		return 0, false
	}
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(text, 10, 16)
	if err != nil || n == 0 {
		return 0, false
	}
	return uint16(n), true
}
