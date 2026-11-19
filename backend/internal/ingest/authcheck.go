package ingest

import (
	"sort"
	"strings"
)

var authKinds = []string{AuthNone, AuthBearer, AuthBasic, AuthHeader, AuthTokenRequest}

// The fields each kind of authentication reads, besides kind.
var authReads = map[string][]string{
	AuthNone:         {},
	AuthBearer:       {"credential"},
	AuthBasic:        {"credential"},
	AuthHeader:       {"credential", "header", "prefix"},
	AuthTokenRequest: {"credential", "request", "token_path", "send_header", "send_prefix"},
}

func (a Auth) fieldsSet() []namedSet {
	return []namedSet{{"credential", a.Credential != nil}, {"header", a.Header != ""}, {"prefix", a.Prefix != ""},
		{"request", a.Request != nil}, {"token_path", a.TokenPath != ""}, {"send_header", a.SendHeader != ""}, {"send_prefix", a.SendPrefix != nil}}
}

// normalize checks the authentication and gives it with its defaults.
func (a Auth) normalize(c *collector, id string) *Auth {
	at := ptr("connection", "auth")
	if !containsString(authKinds, a.Kind) {
		c.add(ecAdAuthKind, at+ptr("kind"), Params{"source": id, "kind": clip(a.Kind, idClipAt), "supported": authKinds},
			"source %s: the kind of the authentication %q is none of none, bearer, basic, header, token_request", id, clip(a.Kind, idClipAt))
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
	if a.TokenPath == "" {
		a.missing(c, id, at, "token_path")
	} else {
		c.checkPath(id, "", at+ptr("token_path"), "auth.token_path", a.TokenPath)
	}
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

func (r TokenRequest) check(c *collector, id, at string) {
	bad := func(field, reason string) {
		c.add(ecAdTokenReq, at+ptr(field), Params{"source": id, "field": "auth.request." + field, "reason": reason}, "source %s: auth.request.%s %s", id, field, reason)
	}
	if reason := requestPathProblem(r.Path); reason != "" {
		bad("path", reason)
	} else {
		c.checkText(id, at+ptr("path"), "auth.request.path", r.Path)
	}
	if r.Format != "json" && r.Format != "form" {
		bad("format", "is none of json and form")
	}
	names := make([]string, 0, len(r.Body))
	for k := range r.Body {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		v := r.Body[k]
		if k == "" {
			bad("body", "has a value with no name")
			continue
		}
		if m := placeholderRe.FindStringSubmatch(v); m != nil {
			continue
		}
		if strings.Contains(v, "{{credential.") {
			c.add(ecAdPlaceholder, at+ptr("body", k), Params{"source": id, "field": "auth.request.body." + clip(k, idClipAt), "value": clip(v, idClipAt)},
				"source %s: auth.request.body.%s has a {{credential.…}} inside a text; a placeholder is the whole value, {{credential.<field>}}", id, clip(k, idClipAt))
			continue
		}
		c.checkText(id, at+ptr("body", k), "auth.request.body."+clip(k, idClipAt), v)
	}
}
