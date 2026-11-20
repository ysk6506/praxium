package ingest

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

var authKinds = []string{AuthNone, AuthBearer, AuthBasic, AuthHeader, AuthTokenRequest, AuthCustom}

// The fields each kind of authentication reads, besides kind.
var authReads = map[string][]string{
	AuthNone:         {},
	AuthBearer:       {"credential"},
	AuthBasic:        {"credential"},
	AuthHeader:       {"credential", "header", "prefix"},
	AuthTokenRequest: {"credential", "request", "token_path", "token_from", "send", "send_header", "send_prefix", "headers"},
	AuthCustom:       {"credential", "headers"},
}

func (a Auth) fieldsSet() []namedSet {
	return []namedSet{{"credential", a.Credential != nil}, {"header", a.Header != ""}, {"prefix", a.Prefix != ""},
		{"request", a.Request != nil}, {"token_path", a.TokenPath != ""}, {"token_from", a.TokenFrom != ""}, {"send", a.Send != ""},
		{"send_header", a.SendHeader != ""}, {"send_prefix", a.SendPrefix != nil}, {"headers", len(a.Headers) > 0}}
}

// normalize checks the authentication and gives it with its defaults.
func (a Auth) normalize(c *collector, id string) *Auth {
	at := ptr("connection", "auth")
	if !containsString(authKinds, a.Kind) {
		c.add(ecAdAuthKind, at+ptr("kind"), Params{"source": id, "kind": clip(a.Kind, idClipAt), "supported": authKinds},
			"source %s: the kind of the authentication %q is none of none, bearer, basic, header, token_request, custom", id, clip(a.Kind, idClipAt))
		return &a
	}
	for _, f := range a.fieldsSet() {
		if f.set && !containsString(authReads[a.Kind], f.name) {
			c.add(ecAdAuthNot, at+ptr(f.name), Params{"source": id, "kind": a.Kind, "field": f.name},
				"source %s: auth.%s is not read by the kind %s", id, f.name, a.Kind)
		}
	}
	a.checkCredential(c, id, at)
	switch a.Kind {
	case AuthHeader:
		a.checkHeader(c, id, at)
	case AuthTokenRequest:
		a.checkTokenRequest(c, id, at)
	}
	a.checkAuthHeaders(c, id, at)
	return &a
}

func (a Auth) missing(c *collector, id, at, field string) {
	c.add(ecAdAuthMissing, at+ptr(field), Params{"source": id, "kind": a.Kind, "field": field}, "source %s: the kind %s needs auth.%s", id, a.Kind, field)
}

func (a Auth) checkCredential(c *collector, id, at string) {
	if a.Kind == AuthNone || !containsString(authReads[a.Kind], "credential") {
		return
	}
	if a.Credential == nil || a.Credential.SecretID == "" {
		a.missing(c, id, at, "credential")
		return
	}
	c.checkID(ecAdSecretID, at+ptr("credential", "secret_id"), "secret", a.Credential.SecretID, Params{"source": id, "field": "connection.auth.credential.secret_id"})
}

func (a Auth) checkHeader(c *collector, id, at string) {
	if a.Header == "" {
		a.missing(c, id, at, "header")
	} else if reason := headerProblem(a.Header, true); reason != "" {
		c.add(ecAdHeader, at+ptr("header"), Params{"source": id, "header": clip(a.Header, idClipAt), "reason": reason}, "source %s: the header %q %s", id, clip(a.Header, idClipAt), reason)
	}
	if reason := valueProblem(a.Prefix); reason != "" {
		c.add(ecAdHeader, at+ptr("prefix"), Params{"source": id, "header": "prefix", "reason": reason}, "source %s: auth.prefix %s", id, reason)
	}
}

func (a Auth) checkTokenRequest(c *collector, id, at string) {
	a.checkTokenFrom(c, id, at)
	a.checkSend(c, id, at)
	if a.SendHeader != "" {
		if reason := headerProblem(a.SendHeader, true); reason != "" {
			c.add(ecAdHeader, at+ptr("send_header"), Params{"source": id, "header": clip(a.SendHeader, idClipAt), "reason": reason}, "source %s: the header %q %s", id, clip(a.SendHeader, idClipAt), reason)
		}
	}
	if a.SendPrefix != nil {
		if reason := valueProblem(*a.SendPrefix); reason != "" {
			c.add(ecAdHeader, at+ptr("send_prefix"), Params{"source": id, "header": "send_prefix", "reason": reason}, "source %s: auth.send_prefix %s", id, reason)
		}
	}
	if a.Request == nil {
		a.missing(c, id, at, "request")
		return
	}
	a.Request.check(c, id, at+ptr("request"))
}

// checkTokenFrom checks where the token is read from: the path of token_path, or the whole body (token_from body, with no
// path). A token request that names neither is refused, as it always was.
func (a Auth) checkTokenFrom(c *collector, id, at string) {
	bad := func(field, reason string) {
		c.add(ecAdTokenReq, at+ptr(field), Params{"source": id, "field": "auth." + field, "reason": reason}, "source %s: auth.%s %s", id, field, reason)
	}
	switch a.TokenFrom {
	case "", TokenFromPath:
	case TokenFromBody:
		if a.TokenPath != "" {
			bad("token_path", "is not read when token_from is body; the whole body is the token")
		}
		return
	default:
		bad("token_from", "is none of path and body")
	}
	if a.TokenPath == "" {
		a.missing(c, id, at, "token_path")
	} else {
		c.checkPath(id, "", at+ptr("token_path"), "auth.token_path", a.TokenPath)
	}
}

// checkSend checks how the token is sent. Where the token goes when it is not in the header is checked with the datasets.
func (a Auth) checkSend(c *collector, id, at string) {
	switch a.Send {
	case "", SendHeader:
	case SendTemplate:
		for _, f := range []namedSet{{"send_header", a.SendHeader != ""}, {"send_prefix", a.SendPrefix != nil}} {
			if f.set {
				c.add(ecAdTokenReq, at+ptr(f.name), Params{"source": id, "field": "auth." + f.name, "reason": "is not read when send is template; the token goes where a placeholder or auth.headers puts it"},
					"source %s: auth.%s is not read when send is template", id, f.name)
			}
		}
	default:
		c.add(ecAdTokenReq, at+ptr("send"), Params{"source": id, "field": "auth.send", "reason": "is none of header and template"}, "source %s: auth.send is none of header and template", id)
	}
}

// maxAuthHeaders is the most headers auth.headers may name.
const maxAuthHeaders = 16

// loggedHeaders are headers that a proxy or a server keeps in the log of its accesses, so that a secret put in one is
// read by anyone who reads the log. The headers x-forwarded-* are the same (ADR-0100).
var loggedHeaders = []string{"user-agent", "referer", "referrer", "forwarded", "via", "x-request-id", "x-correlation-id", "x-real-ip", "traceparent", "tracestate", "from", "origin"}

// checkAuthHeaders checks the headers a value of the secret or the token goes in: a name the definition may write, one the
// logs of a server do not keep, a place the value comes from, and the text before it.
func (a Auth) checkAuthHeaders(c *collector, id, at string) {
	if len(a.Headers) > maxAuthHeaders {
		c.add(ecAdHeader, at+ptr("headers"), Params{"source": id, "header": "headers", "reason": "has more than 16 headers"}, "source %s: auth.headers has more than %d headers", id, maxAuthHeaders)
		return
	}
	seen := map[string]bool{}
	for i, h := range a.Headers {
		bad := func(field, name, reason string) {
			c.add(ecAdHeader, at+ptr("headers", i, field), Params{"source": id, "header": clip(name, idClipAt), "reason": reason}, "source %s: auth.headers %d, %s %q %s", id, i, field, clip(name, idClipAt), reason)
		}
		lower := strings.ToLower(h.Name)
		reason := headerProblem(h.Name, true)
		switch {
		case reason != "":
		case containsString(loggedHeaders, lower) || strings.HasPrefix(lower, "x-forwarded-"):
			reason = "is a header that servers and proxies keep in their logs; a secret does not go in it"
		case seen[lower]:
			reason = "is written twice, in two spellings"
		}
		seen[lower] = true
		if reason != "" {
			bad("name", h.Name, reason)
		}
		src, ok := ParseHeaderFrom(h.From)
		switch {
		case !ok:
			bad("from", h.From, "is neither token nor credential.<field>")
		case src.Token && a.Kind != AuthTokenRequest:
			bad("from", h.From, "is the token, which only the kind token_request has")
		}
		if reason := valueProblem(h.Prefix); reason != "" {
			bad("prefix", "prefix", reason)
		}
	}
}

func (r TokenRequest) check(c *collector, id, at string) {
	bad := func(field, reason string) {
		c.add(ecAdTokenReq, at+ptr(field), Params{"source": id, "field": "auth.request." + field, "reason": reason}, "source %s: auth.request.%s %s", id, field, reason)
	}
	if reason := requestPathProblem(r.Path); reason != "" {
		bad("path", reason)
	} else {
		c.checkText(id, at+ptr("path"), "auth.request.path", r.Path)
	}
	basic := false
	switch r.CredentialsIn {
	case "", CredentialsInBody:
	case CredentialsInBasic:
		basic = true
	default:
		bad("credentials_in", "is none of body and basic")
	}
	if !(basic && len(r.Body) == 0 && r.Format == "") && r.Format != "json" && r.Format != "form" {
		bad("format", "is none of json and form")
	}
	names := make([]string, 0, len(r.Body))
	for k := range r.Body {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if k == "" {
			bad("body", "has a value with no name")
			continue
		}
		r.checkValue(c, id, []any{"body", k}, r.Body[k], basic, 0)
	}
}

// maxTokenBodyDepth is how deep the body of a token request may nest.
const maxTokenBodyDepth = 8

// checkValue checks a value of the body of the token request, at any depth. A value that is exactly {{credential.<field>}}
// is the secret's, unless the credentials are in the header; one with {{credential. or {{auth.token}} inside a text, and
// the token (which does not exist yet), are refused.
func (r TokenRequest) checkValue(c *collector, id string, path []any, v any, basic bool, depth int) {
	at := ptr(append([]any{"connection", "auth", "request"}, path...)...)
	label := "auth.request." + dotted(path)
	switch x := v.(type) {
	case string:
		r.checkText(c, id, at, label, x, basic)
	case map[string]any, []any:
		if r.Format == "form" {
			c.add(ecAdTokenReq, at, Params{"source": id, "field": label, "reason": "has a list or an object; a form has texts only"}, "source %s: %s has a list or an object; a form has texts only", id, label)
			return
		}
		if depth >= maxTokenBodyDepth {
			c.add(ecAdTokenReq, at, Params{"source": id, "field": label, "reason": "nests deeper than 8 levels"}, "source %s: %s nests deeper than %d levels", id, label, maxTokenBodyDepth)
			return
		}
		r.checkNested(c, id, path, x, basic, depth)
	default:
		if r.Format == "form" {
			c.add(ecAdTokenReq, at, Params{"source": id, "field": label, "reason": "is not a text; a form has texts only"}, "source %s: %s is not a text; a form has texts only", id, label)
		}
	}
}

func (r TokenRequest) checkNested(c *collector, id string, path []any, v any, basic bool, depth int) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if k == "" {
				c.add(ecAdTokenReq, ptr(append([]any{"connection", "auth", "request"}, path...)...), Params{"source": id, "field": "auth.request." + dotted(path), "reason": "has a value with no name"},
					"source %s: auth.request.%s has a value with no name", id, dotted(path))
				continue
			}
			r.checkValue(c, id, append(append([]any{}, path...), k), x[k], basic, depth+1)
		}
	case []any:
		for i, e := range x {
			r.checkValue(c, id, append(append([]any{}, path...), i), e, basic, depth+1)
		}
	}
}

// checkText checks a text of the body of the token request.
func (r TokenRequest) checkText(c *collector, id, at, label, v string, basic bool) {
	if p, ok := ParsePlaceholder(v); ok {
		switch {
		case p.Token:
			c.add(ecAdElsewhere, at, Params{"source": id, "field": label},
				"source %s: %s is {{auth.token}}, and the request that asks for the token has no token yet", id, label)
		case basic:
			c.add(ecAdTokenReq, at, Params{"source": id, "field": label, "reason": "has a {{credential.…}} while credentials_in is basic; the credentials are in the Authorization header"},
				"source %s: %s has a {{credential.…}} while credentials_in is basic; the credentials are in the Authorization header", id, label)
		}
		return
	}
	if hasPlaceholderInside(v) {
		c.add(ecAdPlaceholder, at, Params{"source": id, "field": label, "value": clip(v, idClipAt)},
			"source %s: %s has a {{credential.…}} or {{auth.token}} inside a text; a placeholder is the whole value, {{credential.<field>}}", id, label)
		return
	}
	c.checkText(id, at, label, v)
}

// dotted writes a path of a body as the names of a field are written.
func dotted(path []any) string {
	parts := make([]string, len(path))
	for i, p := range path {
		switch v := p.(type) {
		case int:
			parts[i] = strconv.Itoa(v)
		default:
			parts[i] = clip(fmt.Sprint(v), idClipAt)
		}
	}
	return strings.Join(parts, ".")
}
