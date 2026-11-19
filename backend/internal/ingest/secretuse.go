package ingest

import (
	"sort"
)

// checkSecretUse checks where the placeholders of the bodies of the datasets of an HTTP source are given what they stand
// for: {{credential.<field>}} needs the secret of the authentication, {{auth.token}} the kind token_request. It also
// checks what no single field can: that the kind custom sends something, that a token sent only in templates is used, and
// that the fields a secret is asked for are of one kind of secret (ADR-0100).
func (s Source) checkSecretUse(c *collector) {
	if s.Adapter != AdapterHTTP || s.Connection == nil || s.Connection.Auth == nil {
		return
	}
	a := s.Connection.Auth
	u := secretUse{uses: len(a.Headers), fields: CredentialFields(*a, nil)}
	for _, h := range a.Headers {
		if src, ok := ParseHeaderFrom(h.From); ok && src.Token {
			u.tokenUsed = true
		}
	}
	for i, d := range s.Datasets {
		if d.Request != nil {
			s.checkBodyPlaceholders(c, a, i, &u)
		}
	}
	s.checkSecretNeeds(c, a, u)
}

// checkSecretNeeds checks what the authentication as a whole needs of the placeholders and the headers it has.
func (s Source) checkSecretNeeds(c *collector, a *Auth, u secretUse) {
	switch {
	case a.Kind == AuthCustom && u.uses == 0:
		c.add(ecAdAuthMissing, ptr("connection", "auth", "headers"), Params{"source": s.ID, "kind": a.Kind, "field": "headers"},
			"source %s: the kind custom sends nothing by itself; it needs an entry of auth.headers or a placeholder in the body of a request", s.ID)
	case a.Kind == AuthTokenRequest && a.Send == SendTemplate && !u.tokenUsed:
		c.add(ecAdTokenReq, ptr("connection", "auth", "send"), Params{"source": s.ID, "field": "auth.send", "reason": "is template, and no entry of auth.headers and no body has the token, so the token goes nowhere"},
			"source %s: auth.send is template, and no entry of auth.headers and no body has the token, so the token goes nowhere", s.ID)
	}
	if (a.Kind == AuthCustom || a.Kind == AuthTokenRequest) && mixesSecretKinds(u.fields) {
		c.add(ecAdElsewhere, ptr("connection", "auth"), Params{"source": s.ID, "field": "auth"},
			"source %s: the authentication asks for the token field and the fields username or password, which are of two kinds of secret; a secret has one kind", s.ID)
	}
}

// secretUse is what the placeholders and the headers of an authentication use, counted while the bodies are walked.
type secretUse struct {
	uses      int
	tokenUsed bool
	fields    []string
}

// checkBodyPlaceholders checks each placeholder of the body of dataset i against the authentication.
func (s Source) checkBodyPlaceholders(c *collector, a *Auth, i int, u *secretUse) {
	d := s.Datasets[i]
	walkPlaceholderPaths(d.Request.Body, nil, func(path []any, p Placeholder) {
		u.uses++
		at := ptr(append([]any{"datasets", i, "request", "body"}, path...)...)
		switch {
		case p.Token && a.Kind != AuthTokenRequest:
			c.add(ecAdElsewhere, at, Params{"source": s.ID, "field": "request.body"},
				"source %s, dataset %s: request.body has {{auth.token}}, and the authentication (%s) gives no token; only token_request does", s.ID, d.ID, a.Kind)
		case !p.Token && (a.Credential == nil || a.Kind == AuthNone):
			c.add(ecAdElsewhere, at, Params{"source": s.ID, "field": "request.body"},
				"source %s, dataset %s: request.body has {{credential.%s}}, and the authentication has no secret", s.ID, d.ID, p.Field)
		}
		u.tokenUsed = u.tokenUsed || p.Token
		if !p.Token && !containsString(u.fields, p.Field) {
			u.fields = append(u.fields, p.Field)
		}
	})
}

// mixesSecretKinds tells whether fields are those of a token secret and of a password secret together.
func mixesSecretKinds(fields []string) bool {
	token := containsString(fields, "token")
	password := containsString(fields, "username") || containsString(fields, "password")
	return token && password
}

// walkPlaceholderPaths calls f for every placeholder of a body with the path of it: the names and the numbers of the
// places that lead to it, in the order of the sorted names.
func walkPlaceholderPaths(v any, path []any, f func(path []any, p Placeholder)) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walkPlaceholderPaths(x[k], append(append([]any{}, path...), k), f)
		}
	case []any:
		for i, e := range x {
			walkPlaceholderPaths(e, append(append([]any{}, path...), i), f)
		}
	case string:
		if p, ok := ParsePlaceholder(x); ok {
			f(path, p)
		}
	}
}
