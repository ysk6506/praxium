package httpsource

import (
	"encoding/json"
	"net/url"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// The body of a request is made here from its template when the request is built (ADR-0100). The values a placeholder
// stands for come from the signer (auth.go), which is the one place that takes a value out of a secret; this file holds
// the walk of the tree and the encoding, and never a value of its own.

// fill gives a body with its placeholders put in. A text that is exactly a placeholder becomes the value as a value, whatever
// it holds, and the other texts of the definition are what they are with their doubled braces written as one. A text the
// source gave (verbatim) is left as it is: no placeholder of it is read. It makes the tree anew, so the definition's own is
// never changed. unescapeNames says whether the doubled braces of the names are written as one.
func (s *signer) fill(v any, unescapeNames bool) (any, error) {
	return s.fillIn(v, unescapeNames, false)
}

// fillIn is fill for the body of a request that is the token request (forToken) or one of the API: which secret a value is
// taken from, and whether the token may be in it, is the request's (valueIn).
func (s *signer) fillIn(v any, unescapeNames, forToken bool) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			name := k
			if unescapeNames {
				name = ingest.UnescapeBraces(k)
			}
			filled, err := s.fillIn(e, unescapeNames, forToken)
			if err != nil {
				return nil, err
			}
			out[name] = filled
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			filled, err := s.fillIn(e, unescapeNames, forToken)
			if err != nil {
				return nil, err
			}
			out[i] = filled
		}
		return out, nil
	case verbatim:
		return string(x), nil
	case string:
		if p, ok := ingest.ParsePlaceholder(x); ok {
			return s.valueIn(p, forToken)
		}
		return ingest.UnescapeBraces(x), nil
	}
	return v, nil
}

// bodyOf makes the body of a request from its template, and says its content type. A form is a flat map of texts. No
// template is no body.
func (s *signer) bodyOf(p plan) ([]byte, string, error) {
	if p.tmpl == nil {
		return nil, "", nil
	}
	filled, err := s.fillIn(p.tmpl, !p.token, p.token)
	if err != nil {
		return nil, "", err
	}
	tree, _ := filled.(map[string]any)
	if p.form {
		form := url.Values{}
		for k, v := range tree {
			text, ok := v.(string)
			if !ok {
				return nil, "", ingest.NewSourceError(ingest.SrcInternal, "a form of the token request has a value that is not a text", nil)
			}
			form.Set(k, text)
		}
		return []byte(form.Encode()), "application/x-www-form-urlencoded", nil
	}
	body, err := json.Marshal(tree)
	if err != nil {
		return nil, "", ingest.NewSourceError(ingest.SrcInternal, "the body of the request could not be made", nil)
	}
	return body, "application/json", nil
}
