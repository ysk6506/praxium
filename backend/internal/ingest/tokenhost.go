package ingest

import (
	"net/url"
	"sort"
	"strings"
)

// The address of the request that gives a token, and what it means that it is another host than the API (ADR-0106). A
// token request that goes to another origin than base_url is the authentication server's: the credentials go to it and to
// no other host, and what comes back, the token, goes to the API and to no other host. The definition is held to that here
// by where it may put a value of the secret; the adapter holds to it again when it builds a request (httpsource), and the
// secret holds the hosts it may be used for (secrets, authorize).

// origin is the scheme, the host and the port of an address, with the port of the scheme where none is written.
type origin struct{ scheme, host, port string }

// originOfText reads the origin of an address as it is written: the scheme and the host in lower case, and the port. It
// does not normalize the host (a name that is written another way is another origin here), so that two addresses are the
// same origin here only when they are the same origin for the client too.
func originOfText(raw string) (origin, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return origin{}, false
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if strings.EqualFold(u.Scheme, "https") {
			port = "443"
		}
	}
	return origin{strings.ToLower(u.Scheme), strings.ToLower(u.Hostname()), port}, true
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
	return ""
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
