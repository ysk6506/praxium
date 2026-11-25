package hookin

import (
	"net/http"
	"net/netip"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// noAddress is the group of a call whose address could not be had (kernel.AddrGroup's answer for no address).
const noAddress = "none"

// callerAddr is where the caller comes from, by the kernel's rule (kernel.ClientAddr): behind a proxy of the operator, the
// last item of all the X-Forwarded-For lines, the one the proxy wrote. vouched says that the address is that one, or the
// connection's own address without a proxy. Behind a proxy with no such item the address is the connection's, which is not
// vouched for: a webhook that has a list of allowed addresses refuses such a call, as it refuses a call that has no address.
func (h *Handler) callerAddr(r *http.Request) (addr netip.Addr, vouched bool) {
	return kernel.ClientAddr(r.RemoteAddr, r.Header.Values("X-Forwarded-For"), h.cfg.TrustProxy)
}

// addrOf is the address of the call alone (callerAddr without the vouching).
func (h *Handler) addrOf(r *http.Request) netip.Addr {
	a, _ := h.callerAddr(r)
	return a
}

// addrGroup is the address as the entrance counts it: an IPv4 address as it is, an IPv6 address as its /64, and noAddress
// for none (kernel.AddrGroup).
func addrGroup(a netip.Addr) string {
	return kernel.AddrGroup(a)
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
