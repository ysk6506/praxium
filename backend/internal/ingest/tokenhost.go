package ingest

import (
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// The address of the request that gives a token, and what it means that it is another host than the API (ADR-0106). A
// token request that goes to another origin than base_url is the authentication server's: the credentials go to it and to
// no other host, and what comes back, the token, goes to the API and to no other host. The definition is held to that here
// by where it may put a value of the secret; the adapter holds to it again when it builds a request (httpsource), and the
// secret holds the hosts it may be used for (secrets, authorize).

// origin is the scheme, the host and the port of an address, with the port of the scheme where none is written.
type origin struct{ scheme, host, port string }

// originOfText reads the origin of an address the way the client does for the forms of it that the client accepts: the scheme
// and the host in lower case, the host without the one dot that ends an absolute name and an IP address in its canonical
// form, and the port as a number (the one of the scheme where none is written). A host that is written another way than that
// is another origin only when the client takes it for another host; the token address has to be written in the canonical form
// (tokenHostProblem), so that the two readings of it cannot differ.
func originOfText(raw string) (origin, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return origin{}, false
	}
	port := "80"
	if strings.EqualFold(u.Scheme, "https") {
		port = "443"
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return origin{}, false
		}
		port = strconv.Itoa(n)
	}
	host := strings.ToLower(u.Hostname())
	if len(host) > 1 {
		host = strings.TrimSuffix(host, ".")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		host = ip.String()
	}
	return origin{strings.ToLower(u.Scheme), host, port}, true
}

// tokenCrossOrigin tells whether the token request goes to another origin than the base address. It is true when either
// address cannot be read: what cannot be told to be the same is not the same.
func tokenCrossOrigin(baseURL string, tr *TokenRequest) bool {
	if tr == nil || tr.URL == "" {
		return false
	}
	a, okA := originOfText(baseURL)
	b, okB := originOfText(tr.URL)
	return !(okA && okB && a == b)
}

// TokenGoesToAnotherOrigin tells whether the authentication of a definition asks for its token at another origin than the
// base address. A definition that does not say so, or cannot be read, is told apart the safe way: not the same.
func TokenGoesToAnotherOrigin(s Source) bool {
	if s.Adapter != AdapterHTTP || s.Connection == nil || s.Connection.Auth == nil || s.Connection.Auth.Kind != AuthTokenRequest {
		return false
	}
	return tokenCrossOrigin(s.Connection.BaseURL, s.Connection.Auth.Request)
}

// tokenURLProblem says why a text is no address of a token request, or "". It ends a sentence that starts with the field.
func tokenURLProblem(raw string) string {
	if hasControl(raw) {
		return "has a space or a control character"
	}
	if strings.ContainsAny(raw, "{}") {
		return "has a { or a }"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" {
		return "is not an address"
	}
	if reason := tokenURLPartsProblem(u); reason != "" {
		return reason
	}
	if reason := requestPathProblem(u.Path); reason != "" {
		return "has a path that " + reason
	}
	return ""
}

// tokenURLPartsProblem says what is wrong with the scheme, host, user, query and port of the address of a token request.
func tokenURLPartsProblem(u *url.URL) string {
	switch {
	case u.Scheme != "http" && u.Scheme != "https":
		return "has to begin with http:// or https://"
	case u.Hostname() == "":
		return "has no host"
	case u.User != nil:
		return "has a user name or a password in it; credentials come from a secret"
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return "has a query or a fragment; the token request has a path and nothing else"
	case !portOK(u.Port()):
		return "has a port that is not from 1 to 65535"
	}
	return tokenHostProblem(u)
}

// tokenHostProblem says why the host and the port of the address of a token request are not written in the one form the client
// reads them in, or "". The client reads a host as the canonical form of it (the one dot that ends a name is dropped, an IP
// address is written the way net/netip writes it) and refuses what it cannot read that way; an address that is written another
// way would be the same origin as base_url for the client and another for a reader of the text, and its ca_pem would be given
// and not used. The same checks as the client's are made here, since this package cannot import the client's.
func tokenHostProblem(u *url.URL) string {
	if p := u.Port(); p != "" && p != strconv.Itoa(portNumber(p)) {
		return "has a port that is written with a leading zero or a sign; write the number as it is, or leave the port out"
	}
	host := u.Hostname()
	switch {
	case host == "" || len(host) > 253:
		return "has a host that is empty or longer than 253 bytes"
	case strings.Contains(host, ":"):
		return ipHostProblem(host)
	}
	return nameHostProblem(host)
}

func portNumber(p string) int {
	n, _ := strconv.Atoi(p)
	return n
}

// ipHostProblem says why an IPv6 address is not written the canonical way.
func ipHostProblem(host string) string {
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is6() || ip.Zone() != "" {
		return "has an IPv6 address that is not one (a zone, as in %eth0, is not allowed)"
	}
	if ip.String() != strings.ToLower(host) {
		return "has an IPv6 address that is not written the canonical way (" + ip.String() + ")"
	}
	return ""
}

// hostChars are the characters a host name is made of (in lower case).
const hostChars = "abcdefghijklmnopqrstuvwxyz0123456789-._"

// nameHostProblem says why a name or an IPv4 address is not written the canonical way: ASCII letters, digits, - and _ in labels
// of 1 to 63 bytes that are separated by dots, with no dot at the end; and an IPv4 address as four numbers and no other form
// (127.1, 2130706433 and 0x7f.0.0.1 are names the resolver of the system reads as addresses).
func nameHostProblem(host string) string {
	lower := strings.ToLower(host)
	switch {
	case strings.Trim(lower, hostChars) != "":
		return "has a host with a character that a host name does not have (only ASCII letters, digits, - and _ between dots)"
	case strings.HasSuffix(lower, "."):
		return "has a host that ends in a dot; write the name without it"
	}
	labels := strings.Split(lower, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return "has a host with an empty label or a label of more than 63 bytes"
		}
	}
	if numericLabel(labels[len(labels)-1]) {
		if ip, err := netip.ParseAddr(lower); err != nil || !ip.Is4() || ip.String() != lower {
			return "has a host that is a number and not an IPv4 address of four numbers"
		}
	}
	return ""
}

// numericLabel tells a label that a resolver may read as a number: all decimal digits, or 0x and hexadecimal digits.
func numericLabel(label string) bool {
	if digits, hex := strings.CutPrefix(label, "0x"); hex {
		return strings.Trim(digits, "0123456789abcdef") == ""
	}
	return label != "" && strings.Trim(label, "0123456789") == ""
}

// checkAddress checks where the token is asked for: a path under the base address, or a whole address, and what belongs
// to the one or the other.
func (r TokenRequest) checkAddress(c *collector, id, at string, bad func(field, reason string)) {
	switch {
	case r.Path != "" && r.URL != "":
		bad("url", "is given together with path; the token request has one address, a path under base_url or a whole address")
	case r.URL != "":
		if reason := tokenURLProblem(r.URL); reason != "" {
			bad("url", reason)
		}
		r.checkCA(c, id, at)
	default:
		if reason := requestPathProblem(r.Path); reason != "" {
			bad("path", reason)
		} else {
			c.checkText(id, at+ptr("path"), "auth.request.path", r.Path)
		}
		if r.CAPEM != "" {
			bad("ca_pem", "is read when the address is a url; a token request with a path goes to the host of base_url, and the ca_pem of the connection applies")
		}
	}
	checkHeaderTexts(c, id, []any{"connection", "auth", "request", "headers"}, r.Headers)
}

func (r TokenRequest) checkCA(c *collector, id, at string) {
	if r.CAPEM == "" {
		return
	}
	if reason := caProblem(r.CAPEM); reason != "" {
		c.add(ecAdCAPEM, at+ptr("ca_pem"), Params{"source": id, "reason": reason}, "source %s: auth.request.ca_pem %s", id, reason)
	}
}

// checkExpiresIn checks where the answer says how long the token holds.
func (a Auth) checkExpiresIn(c *collector, id, at string) {
	if a.ExpiresInPath == "" {
		return
	}
	if a.TokenFrom == TokenFromBody {
		c.add(ecAdTokenUse, at+ptr("expires_in_path"), Params{"source": id, "field": "auth.expires_in_path", "reason": "is not read when token_from is body; the whole body is the token and has no other value"},
			"source %s: auth.expires_in_path is not read when token_from is body", id)
		return
	}
	c.checkPath(id, "", at+ptr("expires_in_path"), "auth.expires_in_path", a.ExpiresInPath)
}

// checkHeaderTexts checks the headers of a connection or of a token request, which are names and texts and no secret.
func checkHeaderTexts(c *collector, id string, at []any, headers map[string]string) {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	seen := map[string]bool{}
	for _, name := range names {
		reason := headerProblem(name, false)
		if reason == "" && seen[strings.ToLower(name)] {
			reason = "is written twice, in two spellings"
		}
		if reason == "" {
			reason = valueProblem(headers[name])
		}
		if reason == "" && hasPlaceholderInside(headers[name]) {
			reason = "has a {{credential.…}} or {{auth.token}}; a header of the connection is a text, and a secret goes in a header through auth.headers"
		}
		seen[strings.ToLower(name)] = true
		if reason != "" {
			c.add(ecAdHeader, ptr(append(append([]any{}, at...), name)...), Params{"source": id, "header": clip(name, idClipAt), "reason": reason},
				"source %s: the header %q %s", id, clip(name, idClipAt), reason)
		}
	}
}

// checkTokenOrigin checks what the address of the token request means for the rest of the definition. A token that is asked
// for at another origin is asked for with the secret and used at the API without it: no entry of auth.headers and no body
// of a request of the API has a value of the secret then (the datasets are checked with the placeholders, checkSecretUse),
// and the ca_pem of the token request is for that other origin only.
func (s Source) checkTokenOrigin(c *collector) {
	if s.Adapter != AdapterHTTP || s.Connection == nil || s.Connection.Auth == nil {
		return
	}
	a := s.Connection.Auth
	if a.Kind != AuthTokenRequest || a.Request == nil || a.Request.URL == "" {
		return
	}
	if !tokenCrossOrigin(s.Connection.BaseURL, a.Request) {
		if a.Request.CAPEM != "" {
			c.add(ecAdTokenReq, ptr("connection", "auth", "request", "ca_pem"), Params{"source": s.ID, "field": "auth.request.ca_pem", "reason": "is read for another origin than base_url; for this origin the ca_pem of the connection applies"},
				"source %s: auth.request.ca_pem is read for another origin than base_url", s.ID)
		}
		return
	}
	for i, h := range a.Headers {
		if src, ok := ParseHeaderFrom(h.From); ok && !src.Token {
			c.add(ecAdHeaderFrom, ptr("connection", "auth", "headers", i, "from"), Params{"source": s.ID, "header": clip(h.Name, idClipAt), "reason": "is a value of the secret, and the token is asked for at another host than base_url; the secret goes to that host only, and the API gets the token"},
				"source %s: auth.headers %d sends a value of the secret to the API, and the token is asked for at another host than base_url", s.ID, i)
		}
	}
}
