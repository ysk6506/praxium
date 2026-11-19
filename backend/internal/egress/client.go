package egress

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClientOptions are the settings of an HTTP client of a source.
type ClientOptions struct {
	// CAPEM is the certificate authority of the source (see TLSConfig). Empty means the system's authorities.
	CAPEM string
	// Timeout bounds one request from the dial to the end of the body. Zero means 30 seconds.
	Timeout time.Duration
	// MaxRedirects is how many redirects to the same origin the client follows. Zero, the default of the design,
	// follows none and makes a 3xx an error. A redirect to another origin is never followed.
	MaxRedirects int
}

const (
	defaultRequestTimeout = 30 * time.Second
	maxHeaderBytes        = 1 << 20
)

// Client is an HTTP client whose every connection goes through a Dialer. It reads no proxy from the
// environment, verifies every certificate, sends no request to an address with user information, and returns
// only *Error as an error, so that no URL reaches a message.
type Client struct {
	hc *http.Client
}

// NewClient makes a Client on the dialer. The client keeps its own connections; close them with
// CloseIdleConnections when the work is done.
func NewClient(d *Dialer, opt ClientOptions) (*Client, error) {
	tlsCfg, err := TLSConfig(opt.CAPEM)
	if err != nil {
		return nil, err
	}
	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	base := &http.Transport{
		Proxy:                  nil,
		DialContext:            d.DialContext,
		TLSClientConfig:        tlsCfg,
		ForceAttemptHTTP2:      true,
		TLSHandshakeTimeout:    timeout,
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: maxHeaderBytes,
		IdleConnTimeout:        30 * time.Second,
		MaxIdleConns:           2,
		MaxIdleConnsPerHost:    2,
	}
	g := guard{base: base, dialer: d}
	hc := &http.Client{Transport: g, Timeout: timeout, CheckRedirect: redirectRule(opt.MaxRedirects)}
	return &Client{hc: hc}, nil
}

// Do sends the request. A 3xx response that the client does not follow is an error of the kind KindRedirect.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.hc.Do(req)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return nil, Sanitize(err)
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		_ = resp.Body.Close()
		return nil, &Error{Kind: KindRedirect, Reason: "the source answered with a redirect, which is not followed"}
	}
	return resp, nil
}

// CloseIdleConnections closes the connections that are kept for reuse.
func (c *Client) CloseIdleConnections() { c.hc.CloseIdleConnections() }

// redirectRule follows up to limit redirects to the origin of the first request, and no others.
func redirectRule(limit int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if limit <= 0 {
			return http.ErrUseLastResponse
		}
		if len(via) > limit {
			return &Error{Kind: KindRedirect, Reason: "the source redirected too many times"}
		}
		if !SameOrigin(via[0].URL, req.URL) {
			return &Error{Kind: KindRedirect, Reason: "the source redirected to another origin"}
		}
		return nil
	}
}

// guard refuses a request before it reaches the transport: a scheme other than http and https, user
// information in the address, and plain http to a host that is not the loopback (unless the policy allows it).
type guard struct {
	base   http.RoundTripper
	dialer *Dialer
}

func (g guard) RoundTrip(req *http.Request) (*http.Response, error) {
	u := req.URL
	if u == nil || u.User != nil || u.Host == "" {
		return nil, refused("", "the address has no host or has user information")
	}
	host, _, bad := NormalizeHost(u.Hostname())
	if bad != "" {
		return nil, refused("", bad)
	}
	if req.Host != "" && req.Host != u.Host {
		return nil, refused(host, "the Host header is not the host of the address")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !g.dialer.PlaintextAllowed(host) {
			return nil, refused(host, "plain http is allowed only to the loopback")
		}
	default:
		return nil, refused(host, "only http and https are allowed")
	}
	return g.base.RoundTrip(req)
}

// SameOrigin reports whether two URLs have the same scheme, host and port. Default ports (80, 443) count as written.
func SameOrigin(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	ha, _, badA := NormalizeHost(a.Hostname())
	hb, _, badB := NormalizeHost(b.Hostname())
	if badA != "" || badB != "" || ha != hb {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && portOf(a) == portOf(b)
}

func portOf(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

// ResolveSameOrigin reads an address that the source gave (the next page in a body or in a Link header) against
// the address of the request it came with. It is accepted only when the result has the origin of base, no user
// information and no backslash. The fragment is dropped. The error does not hold the address.
func ResolveSameOrigin(base *url.URL, ref string) (*url.URL, error) {
	if strings.ContainsAny(ref, "\\\x00\r\n") {
		return nil, refused("", "the next address holds a character that is not allowed")
	}
	u, err := url.Parse(ref)
	if err != nil {
		return nil, refused("", "the next address cannot be read")
	}
	next := base.ResolveReference(u)
	next.Fragment, next.RawFragment = "", ""
	if next.User != nil || !SameOrigin(base, next) {
		return nil, refused("", "the next address is not of the origin of the source")
	}
	return next, nil
}
