package egress

import (
	"context"
	"net"
	"net/netip"
	"time"
)

// Resolver turns a name into addresses. *net.Resolver is one; a test gives its own.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Config is what a Dialer is made from.
type Config struct {
	Policy Policy
	// Resolver resolves names. Nil means net.DefaultResolver.
	Resolver Resolver
	// DialTimeout bounds the resolving and the connecting of one dial together. Zero means 10 seconds.
	DialTimeout time.Duration
	// Interfaces lists the addresses of this machine, for a Self entry with an unspecified address. Nil means
	// the interface addresses of the system.
	Interfaces func() ([]netip.Addr, error)
}

const (
	defaultDialTimeout = 10 * time.Second
	maxAttempts        = 6
	attemptTimeout     = 5 * time.Second
)

// Dialer makes the connections of a source. It resolves the name itself, judges every address of the answer
// with the policy, and connects to the literal address it judged (see the package comment). It is safe for
// concurrent use. It has the methods that the PostgreSQL driver and net/http look for: Dial, DialTimeout and
// DialContext.
type Dialer struct {
	policy   *compiled
	resolver Resolver
	timeout  time.Duration
	locals   func() ([]netip.Addr, error)
	connect  func(ctx context.Context, network, address string) (net.Conn, error)
}

// New checks the policy and makes a Dialer.
func New(cfg Config) (*Dialer, error) {
	pol, err := compile(cfg.Policy)
	if err != nil {
		return nil, err
	}
	d := &Dialer{policy: pol, resolver: cfg.Resolver, timeout: cfg.DialTimeout, locals: cfg.Interfaces}
	if d.resolver == nil {
		d.resolver = net.DefaultResolver
	}
	if d.timeout <= 0 {
		d.timeout = defaultDialTimeout
	}
	if d.locals == nil {
		d.locals = systemAddrs
	}
	plain := &net.Dialer{KeepAlive: 30 * time.Second}
	d.connect = plain.DialContext
	return d, nil
}

func systemAddrs() ([]netip.Addr, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	out := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		if p, err := netip.ParsePrefix(a.String()); err == nil {
			out = append(out, p.Addr())
		}
	}
	return out, nil
}

// Dial connects to address ("host:port") over network ("tcp", "tcp4" or "tcp6").
func (d *Dialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

// DialTimeout is Dial with a limit on the whole dial. The shorter of it and the DialTimeout of the Config holds.
func (d *Dialer) DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return d.DialContext(ctx, network, address)
}

// DialContext resolves the host once, judges every address, and connects to the first address that answers.
// If the host is an IP literal there is no resolving. Any error is an *Error.
func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	want, err := wantFamily(network)
	if err != nil {
		return nil, err
	}
	host, ip, port, err := d.target(address)
	if err != nil {
		return nil, err
	}
	addrs, err := d.addresses(ctx, host, ip, port)
	if err != nil {
		return nil, err
	}
	return d.dialAny(ctx, host, filterFamily(addrs, want), port)
}

// PlaintextAllowed says whether a connection to host without TLS is allowed: the loopback always is, and any
// other host only when the policy says AllowInsecure. The HTTP client asks it for http; the PostgreSQL adapter
// asks it for sslmode=disable. A host that is not a valid host is not allowed.
func (d *Dialer) PlaintextAllowed(host string) bool {
	name, ip, bad := NormalizeHost(host)
	if bad != "" {
		return false
	}
	return d.policy.insecure || ip.IsLoopback() || name == "localhost"
}

func wantFamily(network string) (string, error) {
	switch network {
	case "tcp":
		return "", nil
	case "tcp4", "tcp6":
		return network, nil
	}
	return "", refused("", "only tcp connections are allowed")
}

// target splits and checks the address: the host, the port and the policy of both.
func (d *Dialer) target(address string) (string, netip.Addr, uint16, error) {
	rawHost, rawPort, err := net.SplitHostPort(address)
	if err != nil {
		return "", netip.Addr{}, 0, refused("", "the address is not host:port")
	}
	host, ip, bad := NormalizeHost(rawHost)
	if bad != "" {
		return "", netip.Addr{}, 0, refused("", bad)
	}
	port, ok := parsePort(rawPort)
	if !ok {
		return "", netip.Addr{}, 0, refused(host, "the port is not a number from 1 to 65535")
	}
	if why := d.policy.checkHost(host); why != "" {
		return "", netip.Addr{}, 0, refused(host, why)
	}
	if why := d.policy.checkPort(port); why != "" {
		return "", netip.Addr{}, 0, refused(host, why)
	}
	return host, ip, port, nil
}

// addresses resolves the host (once) and judges every address. One refused address refuses the whole answer.
func (d *Dialer) addresses(ctx context.Context, host string, ip netip.Addr, port uint16) ([]netip.Addr, error) {
	addrs := []netip.Addr{ip}
	if !ip.IsValid() {
		var err error
		if addrs, err = d.resolver.LookupNetIP(ctx, "ip", host); err != nil {
			e := Sanitize(err)
			if e.Kind == KindConnect {
				e = &Error{Kind: KindResolve, Reason: "the name did not resolve"}
			}
			e.Host = host
			return nil, e
		}
		if len(addrs) == 0 {
			return nil, &Error{Kind: KindResolve, Reason: "the name has no address", Host: host}
		}
	}
	for _, a := range addrs {
		if why := d.policy.judge(a, port, d.locals); why != "" {
			return nil, refused(host, why)
		}
	}
	return addrs, nil
}

func filterFamily(addrs []netip.Addr, network string) []netip.Addr {
	if network == "" {
		return addrs
	}
	out := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		if (network == "tcp4") == canonical(a).Is4() {
			out = append(out, a)
		}
	}
	return out
}

// dialAny connects to the addresses in turn, each for at most attemptTimeout, all within the time of the dial. What is dialed is the
// literal address that was judged, never the name.
func (d *Dialer) dialAny(ctx context.Context, host string, addrs []netip.Addr, port uint16) (net.Conn, error) {
	if len(addrs) == 0 {
		return nil, &Error{Kind: KindResolve, Reason: "the name has no address of the family asked for", Host: host}
	}
	if len(addrs) > maxAttempts {
		addrs = addrs[:maxAttempts]
	}
	var last error
	for _, a := range addrs {
		attempt, cancel := context.WithTimeout(ctx, attemptTimeout)
		a = canonical(a)
		network := "tcp6"
		if a.Is4() {
			network = "tcp4"
		}
		conn, err := d.connect(attempt, network, netip.AddrPortFrom(a, port).String())
		cancel()
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			last = ctx.Err()
			break
		}
		last = err
	}
	e := Sanitize(last)
	e.Host = host
	return nil, e
}
