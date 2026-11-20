package httpsource

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// minMaskBytes is the length under which a value is not taken out of a text. A value of three bytes is a few letters that
// are in nearly every text, and taking them all out would leave nothing to read (ADR-0097).
const minMaskBytes = 4

// maskForms lists the shapes the values of a secret may come back in: as they are, escaped as a query or as a path of an
// address, and in base64 (the standard and the URL alphabet, with the padding and without). The result has no value
// twice, none under minMaskBytes, and the longest first, so that a value that holds another is taken out whole. A shape
// that is not here (hexadecimal, a part of a value, a value cut in pieces, a compressed one) is not found.
func maskForms(values []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if len(s) >= minMaskBytes && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, v := range values {
		if len(v) < minMaskBytes {
			continue
		}
		b := []byte(v)
		add(v)
		add(url.QueryEscape(v))
		add(url.PathEscape(v))
		add(base64.StdEncoding.EncodeToString(b))
		add(base64.RawStdEncoding.EncodeToString(b))
		add(base64.URLEncoding.EncodeToString(b))
		add(base64.RawURLEncoding.EncodeToString(b))
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// newReplacer makes what takes the texts out of another text, or nil when there are none.
func newReplacer(texts []string) *strings.Replacer {
	pairs := make([]string, 0, 2*len(texts))
	ordered := append([]string(nil), texts...)
	sort.SliceStable(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	for _, s := range ordered {
		if s != "" {
			pairs = append(pairs, s, ingest.MaskedValue)
		}
	}
	if len(pairs) == 0 {
		return nil
	}
	r := strings.NewReplacer(pairs...)
	// A replacer builds itself on its first use and keeps the texts it was given until then. Using it now means the texts are
	// never lying in it for anything that prints the struct that holds it with reflection.
	_ = r.Replace("")
	return r
}

// clean takes the values of the secrets out of a text and tells whether there was any.
func (m maskSet) clean(text string) (string, bool) {
	if m.repl == nil || text == "" {
		return text, false
	}
	out := m.repl.Replace(text)
	return out, out != text
}

// clean is a text of what the adapter gives out (a path, a header, a sentence) with the values of the open secrets taken out,
// the user name and the client id among them: it is for the records of what happened, which a person reads.
func (rn *run) clean(text string) string {
	out, hit := rn.signer.wide.clean(text)
	if hit {
		rn.cleaned++
	}
	return out
}

// cleanRow is a text of a record the source gave with the values of the secret's own fields taken out, and those only
// (ADR-0100): a row is data and its key is made of it, so a user name or a client id that the data has too must stay.
func (rn *run) cleanRow(text string) string {
	out, hit := rn.signer.mask.clean(text)
	if hit {
		rn.cleaned++
	}
	return out
}

// cleanValue takes the values of the open secrets out of a record the source gave, at every depth, before the record is
// made into a row. It changes the record, which is the adapter's own. A number that held one becomes the text it is.
func (rn *run) cleanValue(v any) any {
	switch x := v.(type) {
	case string:
		return rn.cleanRow(x)
	case json.Number:
		if out := rn.cleanRow(x.String()); out != x.String() {
			return out
		}
	case map[string]any:
		for k, e := range x {
			x[k] = rn.cleanValue(e)
		}
	case []any:
		for i, e := range x {
			x[i] = rn.cleanValue(e)
		}
	}
	return v
}

// cleanNames takes the values out of the names and values of a record of an exchange.
func (rn *run) cleanNames(list []ingest.NameValue) []ingest.NameValue {
	if len(list) == 0 {
		return list
	}
	out := make([]ingest.NameValue, len(list))
	for i, nv := range list {
		nv.Name, nv.Value = rn.clean(nv.Name), rn.clean(nv.Value)
		out[i] = nv
	}
	return out
}

// tokenLeft is the kind of the event that says a token was not given with its page.
const tokenLeft ingest.EventKind = "token_left"

// safeToken is the token of a page, or nothing when the cursor or the token holds a value of an open secret: the source put
// it there, and the token would carry it into the storage. The page goes without a token and a read that goes on from
// it starts again from the first page (ADR-0097). One event says so, in words that hold no value.
func (rn *run) safeToken(tok []byte, cursor string) []byte {
	if _, hit := rn.signer.mask.clean(cursor); !hit {
		if _, hit = rn.signer.mask.clean(string(tok)); !hit {
			return tok
		}
	}
	rn.emit(ingest.Event{Kind: tokenLeft, Reason: "the page has no token: the source put a value of the secret in the cursor"})
	return nil
}
