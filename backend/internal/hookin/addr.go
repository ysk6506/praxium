package hookin

import (
	"net/http"
	"net/netip"
	"strings"
)

// noAddress is the group of a call whose address could not be had.
const noAddress = "none"

// addrOf is where the caller comes from: the address of the connection, or, behind a proxy of the operator, the last item
// of the X-Forwarded-For lines of the call (a proxy appends the address it saw to what the caller sent, so the last item is
// the one the proxy wrote, and the lines before it are the caller's own words). Every line is read, joined as one list: a
// proxy that adds a line of its own and does not append to the caller's line is read the same way. When there is no such
// item, or it is not an address, the call has no address: the connection's own address is the proxy's and is not used, and
// a webhook that has a list of allowed addresses refuses a call that has none.
func (h *Handler) addrOf(r *http.Request) netip.Addr {
	raw := r.RemoteAddr
	if h.cfg.TrustProxy {
		list := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
		raw = strings.TrimSpace(list[strings.LastIndexByte(list, ',')+1:])
	}
	if ap, err := netip.ParseAddrPort(raw); err == nil {
		return ap.Addr().Unmap().WithZone("")
	}
	if a, err := netip.ParseAddr(strings.Trim(raw, "[]")); err == nil {
		return a.Unmap().WithZone("")
	}
	return netip.Addr{}
}

// addrGroup is the address as the entrance counts it: an IPv4 address as it is, an IPv6 address as its /64 (a network of
// one customer, of which one address after another is free), and noAddress for none.
func addrGroup(a netip.Addr) string {
	switch {
	case !a.IsValid():
		return noAddress
	case a.Is4():
		return a.String()
	}
	p, err := a.Prefix(64)
	if err != nil {
		return noAddress
	}
	return p.String()
}

// takeAddress takes one of the places of an address group, and says false when it has them all. It holds nothing but a count.
func (h *Handler) takeAddress(group string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.perAddr[group] >= h.cfg.PerAddressInFlight {
		return false
	}
	h.perAddr[group]++
	return true
}

func (h *Handler) dropAddress(group string) {
	h.mu.Lock()
	if h.perAddr[group]--; h.perAddr[group] <= 0 {
		delete(h.perAddr, group)
	}
	h.mu.Unlock()
}
