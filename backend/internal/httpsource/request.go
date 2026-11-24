package httpsource

import (
	"bytes"
	"net/url"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// plan is one request as the adapter means to send it, before the credentials are put on it. The method is GET or POST
// and nothing else; build, below, is the one place where a request is made (REQ-411).
type plan struct {
	method string
	url    *url.URL
	// tmpl is the body as a tree with its placeholders not put in yet (nil for no body), and form says it is sent as a form
	// and not as JSON. The body is made when the request is built, so that a request sent again after the token was
	// asked for once more carries the new one.
	tmpl map[string]any
	form bool
	// token marks the request that asks for a token: it is signed in with nothing (but the Basic header, when basic is
	// set), and not retried.
	token bool
	basic bool
	// shown lists the query as a person may see it in a record of the exchange: the names, and the values that are the
	// definition's own.
	shown []ingest.NameValue
}

// retryable says whether the request may be sent again when the source fails in a passing way: a GET only (ADR-0090,
// sources-and-adapters.md 7.1; a POST that reads is not sent twice).
func (p plan) retryable() bool { return p.method == ingest.MethodGet && !p.token }

// params are the parts of a request that the pagination adds to: the query and, for a POST, the body.
type params struct {
	query url.Values
	body  map[string]any
	shown []ingest.NameValue
}

func unescape(s string) string { return ingest.UnescapeBraces(s) }

// verbatim is a text of a body that the source gave (a cursor): it is sent as it is, and no placeholder or doubled brace
// of it is read.
type verbatim string

// newParams starts from what the definition wrote, with the braces that stand for themselves written as themselves.
func newParams(req *ingest.Request) *params {
	p := &params{query: url.Values{}}
	names := make([]string, 0, len(req.Query))
	for k := range req.Query {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		name, value := unescape(k), unescape(req.Query[k])
		p.query.Set(name, value)
		p.shown = append(p.shown, shownValue(name, value, false))
	}
	if req.Body != nil {
		p.body = copyBody(req.Body).(map[string]any)
	}
	return p
}

// shownValue is a name and value of the query as a person may see them: the value of a name that says it is a secret's,
// and a value the source gave (a cursor), are left out.
func shownValue(name, value string, fromSource bool) ingest.NameValue {
	if fromSource || ingest.SensitiveName(name) {
		return ingest.NameValue{Name: name, Masked: true}
	}
	return ingest.NameValue{Name: name, Value: value}
}

// copyBody copies a body of the definition as it is written, so that the request may add to the copy. The placeholders and
// the doubled braces are read when the request is built (signer.fill).
func copyBody(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = copyBody(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = copyBody(e)
		}
		return out
	}
	return v
}

// dataPlan is the request that reads the dataset at a position: the request of the definition with the parameters of the
// pagination for that position, or the address the source gave for it.
func (rn *run) dataPlan(ds ingest.Dataset, at pos) (plan, error) {
	req := ds.Request
	p := newParams(req)
	rn.pager.apply(p, at)
	pl := plan{method: req.Method, shown: p.shown}
	if pl.method == "" {
		pl.method = ingest.MethodGet
	}
	if at.link != nil {
		u := *at.link
		pl.url = &u
		pl.shown = serverQuery(&u)
	} else {
		u := *rn.origin
		u.Path += unescape(req.Path)
		u.RawQuery = p.query.Encode()
		pl.url = &u
	}
	if pl.method == ingest.MethodPost && p.body != nil {
		pl.tmpl = p.body
	}
	return pl, nil
}

// serverQuery lists the parameters of an address the source gave: the names, never the values (a server signs an
// address with its query).
func serverQuery(u *url.URL) []ingest.NameValue {
	q := u.Query()
	names := make([]string, 0, len(q))
	for k := range q {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]ingest.NameValue, 0, len(names))
	for _, k := range names {
		out = append(out, shownValue(k, "", true))
	}
	return out
}

// build makes the request of the egress package from a plan: the headers of the connection, what the request accepts and
// sends, and the credentials, with the body made from its template. It is the one place where a request is made, and it
// uses the two methods GET and POST. It fails, with nothing made, when a value of the secret or the token cannot go where
// the definition puts it (a line break in a header, a field the secret does not have).
func (rn *run) build(p plan) (egress.Request, error) {
	h := map[string][]string{}
	for name, value := range rn.set.headers {
		h[name] = []string{unescape(value)}
	}
	if !hasHeader(h, "Accept") {
		h["Accept"] = []string{"application/json"}
	}
	body, ctype, err := rn.signer.bodyOf(p)
	if err != nil {
		return egress.Request{}, err
	}
	if ctype != "" {
		setHeader(h, "Content-Type", ctype)
	}
	switch {
	case !p.token:
		if err := rn.signer.apply(h); err != nil {
			return egress.Request{}, err
		}
	case p.basic:
		if err := rn.signer.setBasic(h); err != nil {
			return egress.Request{}, err
		}
	}
	req := egress.Request{Method: ingest.MethodGet, URL: p.url.String(), Header: h}
	if p.method == ingest.MethodPost {
		req.Method = ingest.MethodPost
		req.Body = bytes.NewReader(body)
	}
	return req, nil
}

func hasHeader(h map[string][]string, name string) bool {
	for k := range h {
		if strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}

// setHeader sets a header and takes out the others of the same name that differ only in case.
func setHeader(h map[string][]string, name, value string) {
	for k := range h {
		if strings.EqualFold(k, name) {
			delete(h, k)
		}
	}
	h[name] = []string{value}
}
