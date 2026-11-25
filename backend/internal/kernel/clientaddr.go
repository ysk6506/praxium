package kernel

import (
	"net/netip"
	"strings"
)

// noAddressGroup is the group of a caller whose address could not be had.
const noAddressGroup = "none"

// ClientAddr is the address a call comes from, read from the connection and, behind a proxy the operator declared
// (trustProxy), from X-Forwarded-For. forwarded is every line of that header, in order. The proxy appends the address it
// saw to what the caller sent, so the last item of all the lines together is the one the proxy wrote; the items before it
// are the caller's own words and are not read. vouched says that the address is that one, or, without a proxy, the
// connection's address. Behind a proxy with no such item (no header, or an item that is not an address, such as "unknown")
// the address is the connection's, which is the proxy's own or that of a call that did not pass a proxy: a caller cannot
// make either up. A zero address and false mean that no address could be read.
func ClientAddr(remote string, forwarded []string, trustProxy bool) (addr netip.Addr, vouched bool) {
	if !trustProxy {
		addr = readAddr(remote)
		return addr, addr.IsValid()
	}
	list := strings.Join(forwarded, ",")
	if a := readAddr(list[strings.LastIndexByte(list, ',')+1:]); a.IsValid() {
		return a, true
	}
	return readAddr(remote), false
}

// AddrGroup is an address as a limit counts it: an IPv4 address as it is, an IPv6 address as its /64 (a network of one
// customer, of which one address after another is free), and "none" for no address.
func AddrGroup(a netip.Addr) string {
	switch {
	case !a.IsValid():
		return noAddressGroup
	case a.Is4():
		return a.String()
	}
	p, _ := a.Prefix(64) // a valid IPv6 address with no zone has a prefix of 64 bits
	return p.String()
}

// CallerKey is where a call comes from as a key: the text of the address ClientAddr reads, or, when it reads none, the
// host part of the connection's address as it was written.
func CallerKey(remote string, forwarded []string, trustProxy bool) string {
	if a, _ := ClientAddr(remote, forwarded, trustProxy); a.IsValid() {
		return a.String()
	}
	return remoteHost(remote)
}

// readAddr reads "host:port", "[host]:port", "host" or "[host]" as an address: the port is cut off, an IPv4 address mapped
// into IPv6 is the IPv4 address, and a zone is dropped. Anything else reads as no address.
func readAddr(raw string) netip.Addr {
	raw = strings.TrimSpace(raw)
	if ap, err := netip.ParseAddrPort(raw); err == nil {
		return ap.Addr().Unmap().WithZone("")
	}
	if a, err := netip.ParseAddr(strings.Trim(raw, "[]")); err == nil {
		return a.Unmap().WithZone("")
	}
	return netip.Addr{}
}

// remoteHost is the host part of "host:port" or "[host]:port", and the text as it is when there is no port.
func remoteHost(addr string) string {
	if strings.HasPrefix(addr, "[") {
		if end := strings.Index(addr, "]"); end > 0 {
			return addr[1:end]
		}
	}
	if i := strings.LastIndex(addr, ":"); i >= 0 && strings.Count(addr, ":") == 1 {
		return addr[:i]
	}
	return addr
}
