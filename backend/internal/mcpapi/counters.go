package mcpapi

import "sync/atomic"

// The numbers of REQ-504: how many requests each version rules answered, and how many were turned away for a version
// that could not be told (no header, or a version the server does not speak). They count from the start of the
// process and are shown by kg_whoami and in the log line of every request. "initialize" is the handshake of the older
// versions, which carries its own version in the body.
const (
	countInitialize        = "initialize"
	countNoVersionHeader   = "rejected_no_version_header"
	countUnsupportedHeader = "rejected_unsupported_version"
	countHeaderMismatch    = "rejected_header_mismatch"
)

type counters struct {
	by map[string]*atomic.Int64
}

func newCounters() *counters {
	c := &counters{by: map[string]*atomic.Int64{}}
	keys := append([]string{countInitialize, countNoVersionHeader, countUnsupportedHeader, countHeaderMismatch}, supportedVersions...)
	for _, k := range keys {
		c.by[k] = &atomic.Int64{}
	}
	return c
}

func (c *counters) add(key string) {
	if n, ok := c.by[key]; ok {
		n.Add(1)
	}
}

func (c *counters) snapshot() map[string]int64 {
	out := make(map[string]int64, len(c.by))
	for k, n := range c.by {
		out[k] = n.Load()
	}
	return out
}
