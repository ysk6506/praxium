package ingest

import (
	"regexp"
	"sort"
	"strings"
)

// The one grammar of the places where a secret or the token of an HTTP source goes into a request (ADR-0100). A value of
// a JSON body that is exactly {{credential.<field>}} or {{auth.token}} is a placeholder; a header gets its value from
// auth.headers, in a structure and not in a text; no placeholder is a part of a text, and none is in an address. The
// adapter reads these places with the functions here, so that what a definition means is decided in one place.

// tokenPlaceholder is the text of the placeholder of the token the source gave.
const tokenPlaceholder = "{{auth.token}}"

// Placeholder is what a value of a body stands for: a field of the secret, or the token the source gave.
type Placeholder struct {
	// Token is true for {{auth.token}}; Field is then empty.
	Token bool
	// Field is the field of the secret for {{credential.<field>}}.
	Field string
}

// ParsePlaceholder tells whether a whole text is a placeholder: {{credential.<field>}} or {{auth.token}}. A text that has
// one inside it is no placeholder.
func ParsePlaceholder(text string) (Placeholder, bool) {
	if text == tokenPlaceholder {
		return Placeholder{Token: true}, true
	}
	if m := placeholderRe.FindStringSubmatch(text); m != nil {
		return Placeholder{Field: m[1]}, true
	}
	return Placeholder{}, false
}

// hasPlaceholderInside tells whether a text has the writing of a placeholder in it, whole or not.
func hasPlaceholderInside(s string) bool {
	return strings.Contains(s, "{{credential.") || strings.Contains(s, tokenPlaceholder)
}

var credentialFromRe = regexp.MustCompile(`^credential\.([a-z][a-z_]*)$`)

// HeaderSource is where the value of an auth.headers entry comes from.
type HeaderSource struct {
	// Token is true for the token the source gave; Field is the field of the secret otherwise.
	Token bool
	Field string
}

// ParseHeaderFrom reads the from of an auth.headers entry: token, or credential.<field>.
func ParseHeaderFrom(from string) (HeaderSource, bool) {
	if from == "token" {
		return HeaderSource{Token: true}, true
	}
	if m := credentialFromRe.FindStringSubmatch(from); m != nil {
		return HeaderSource{Field: m[1]}, true
	}
	return HeaderSource{}, false
}

// WalkPlaceholders calls f for every value of a body that is a placeholder, at any depth. The names of a body are no
// places for one.
func WalkPlaceholders(body any, f func(Placeholder)) {
	switch x := body.(type) {
	case map[string]any:
		for _, e := range x {
			WalkPlaceholders(e, f)
		}
	case []any:
		for _, e := range x {
			WalkPlaceholders(e, f)
		}
	case string:
		if p, ok := ParsePlaceholder(x); ok {
			f(p)
		}
	}
}

// PlaceholdersOf lists the placeholders of a body in the order they are met (a map is walked by its sorted names).
func PlaceholdersOf(body any) []Placeholder {
	var out []Placeholder
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for _, k := range sortedKeys(x) {
				walk(x[k])
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		case string:
			if p, ok := ParsePlaceholder(x); ok {
				out = append(out, p)
			}
		}
	}
	walk(body)
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CredentialFields lists the fields of the secret that an authentication and the body of one request name, each once, in
// the order of the first use: the fields of auth.headers, of the body of the token request, and of the body of the data
// request (nil for none). The adapter opens the secret and needs these fields in it.
func CredentialFields(a Auth, dataBody map[string]any) []string {
	var out []string
	add := func(f string) {
		if f != "" && !containsString(out, f) {
			out = append(out, f)
		}
	}
	for _, h := range a.Headers {
		if s, ok := ParseHeaderFrom(h.From); ok {
			add(s.Field)
		}
	}
	if a.Request != nil {
		for _, p := range PlaceholdersOf(mapAny(a.Request.Body)) {
			add(p.Field)
		}
		if a.Request.CredentialsIn == CredentialsInBasic {
			add("username")
			add("password")
		}
	}
	for _, p := range PlaceholdersOf(mapAny(dataBody)) {
		add(p.Field)
	}
	return out
}

// mapAny gives a map as the value it is, with a nil map as a nil value (so that a walk of it has nothing to walk).
func mapAny(m map[string]any) any {
	if m == nil {
		return nil
	}
	return m
}
