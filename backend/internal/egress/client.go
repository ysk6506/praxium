package egress

import (
	"context"
	"io"
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

// Request is a request of a source. The package gives its own types so that the adapter that sends it does not
// import net/http and has no way to build a client, a transport or a request of its own.
type Request struct {
	// Method is "GET" when empty.
	Method string
	// URL is an absolute http or https URL. It is parsed and judged here: a URL with user information, an opaque
	// part, no host, or a host that is not a plain host is refused.
	URL    string
	Header map[string][]string
	Body   io.Reader
}

// Response is the answer of a source. The body is read once, and what goes wrong while reading it comes back as an
// *Error (the standard library's text of a broken read holds the addresses of both ends).
type Response struct {
	Status int
	Header map[string][]string
	Body   io.ReadCloser
	// Ended says that the end of the body is marked by the protocol: a length that is read in full, the last chunk, the end
	// of an HTTP/2 stream or the trailer of a compressed body. A body that is read to its end without such a mark may have
	// been cut where a connection closed, and the reader of a body that is made of lines (one record each) cannot tell by
	// the body whether it was.
	Ended bool
}

// Client is an HTTP client whose every connection goes through a Dialer. It reads no proxy from the
// environment, verifies every certificate, sends no request to an address with user information, and returns
// only *Error as an error (from the request and from reading the body), so that no URL and no address reaches a
// message.
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
	hc := &http.Client{Transport: guard{base: base}, Timeout: timeout, CheckRedirect: redirectRule(opt.MaxRedirects)}
	return &Client{hc: hc}, nil
}

// Do sends the request. A 3xx response that the client does not follow is an error of the kind KindRedirect.
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	hreq, err := build(ctx, req)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(hreq)
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
	return &Response{Status: resp.StatusCode, Header: resp.Header.Clone(), Body: safeBody{resp.Body}, Ended: endMarked(resp)}, nil
}

// endMarked says whether the protocol marks the end of the body of a response: a length, the chunked coding, HTTP/2, or a
// body that the transport unpacked (its trailer is a mark).
func endMarked(resp *http.Response) bool {
	if resp.ContentLength >= 0 || resp.Uncompressed || resp.ProtoMajor >= 2 {
		return true
	}
	for _, te := range resp.TransferEncoding {
		if strings.EqualFold(te, "chunked") {
			return true
		}
	}
	return false
}

// build parses the URL itself and makes the request. It refuses what url.Parse reads in a way the judgement does not.
func build(ctx context.Context, req Request) (*http.Request, error) {
	u, err := url.Parse(req.URL)
	if err != nil || u.Opaque != "" || u.User != nil || u.Host == "" {
		return nil, refused("", "the address is not an absolute URL with a host and no user information")
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	hreq, err := http.NewRequestWithContext(ctx, method, u.String(), req.Body)
	if err != nil {
		return nil, refused("", "the request cannot be made")
	}
	for name, values := range req.Header {
		for _, v := range values {
			hreq.Header.Add(name, v)
		}
	}
	return hreq, nil
}

// safeBody turns the errors of a read into *Error.
type safeBody struct{ rc io.ReadCloser }

func (b safeBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if err != nil && err != io.EOF {
		err = Sanitize(err)
	}
	return n, err
}

func (b safeBody) Close() error { return Sanitize(b.rc.Close()) }

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
// information or an opaque part in the address, and a Host header that is not the host of the address. It marks an
// http request as one without TLS, so that the dialer lets it go only to the loopback (see WithPlaintext).
type guard struct {
	base http.RoundTripper
}

func (g guard) RoundTrip(req *http.Request) (*http.Response, error) {
	u := req.URL
	if u == nil || u.User != nil || u.Host == "" || u.Opaque != "" {
		return nil, refused("", "the address has no host, or has user information or an opaque part")
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
		req = req.WithContext(WithPlaintext(req.Context()))
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
