package ingest

import (
	"net/url"
	"sort"
	"strings"
)

// MaskedValue stands where a value was left out.
const MaskedValue = "***"

// sensitiveNames are the words that make a field name a secret's (sources-and-adapters.md 12, REQ-491). "key" alone is not
// one: a column called key is common and means nothing secret.
var sensitiveNames = []string{"password", "passwd", "secret", "token", "api_key", "apikey", "authorization", "credential"}

// SensitiveName tells whether a field name says its value is a secret: it has one of the words, in any case.
func SensitiveName(name string) bool {
	lower := strings.ToLower(name)
	for _, w := range sensitiveNames {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// MaskSample returns a copy of a value decoded from JSON in which the value of every object field with a sensitive name
// is left out, at any depth, and tells whether any was. The value it was given is not changed.
func MaskSample(v any) (any, bool) {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		masked := false
		for k, e := range x {
			if SensitiveName(k) {
				out[k], masked = MaskedValue, true
				continue
			}
			m, was := MaskSample(e)
			out[k], masked = m, masked || was
		}
		return out, masked
	case []any:
		out := make([]any, len(x))
		masked := false
		for i, e := range x {
			m, was := MaskSample(e)
			out[i], masked = m, masked || was
		}
		return out, masked
	}
	return v, false
}

// MaskPage returns a copy of a page whose rows have the value of a column with a sensitive name left out, and tells
// whether any was. A sample for a person is made from it.
func MaskPage(p Page) (Page, bool) {
	masked := false
	rows := make([]Row, len(p.Rows))
	for i, r := range p.Rows {
		f := make(map[string]string, len(r.Fields))
		for k, v := range r.Fields {
			if SensitiveName(k) && v != "" {
				v, masked = MaskedValue, true
			}
			f[k] = v
		}
		r.Fields = f
		rows[i] = r
	}
	p.Rows = rows
	return p, masked
}

// MaskText takes the values of secrets out of a text: each of them, wherever it is, is replaced. It is the second wall,
// for the sentence of a server that repeats a token it was given; the first is that the text is never made from such a
// sentence. The longer values go first, so that a value that holds another is taken out whole.
func MaskText(text string, secrets ...string) string {
	vals := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if s != "" {
			vals = append(vals, s)
		}
	}
	sort.SliceStable(vals, func(i, j int) bool { return len(vals[i]) > len(vals[j]) })
	for _, s := range vals {
		text = strings.ReplaceAll(text, s, MaskedValue)
	}
	return text
}

// NameValue is a header or a query parameter as a person sees it in a record of an exchange: the name, and the value
// unless it is one the person may not see.
type NameValue struct {
	Name   string `json:"name"`
	Value  string `json:"value,omitempty"`
	Masked bool   `json:"masked,omitempty"`
}

// shownHeaders are the response headers whose values are shown; the value of every other header is left out.
var shownHeaders = []string{"content-type", "content-length", "retry-after"}

// MaskHeaders lists the headers of a response by name, in name order, with the value only of those that are shown. A
// value that is left out is marked, so that a person sees that the header was there.
func MaskHeaders(h map[string][]string) []NameValue {
	out := make([]NameValue, 0, len(h))
	for name, vals := range h {
		nv := NameValue{Name: name}
		if lower := strings.ToLower(name); containsString(shownHeaders, lower) {
			nv.Value = strings.Join(vals, ", ")
		} else {
			nv.Masked = true
		}
		out = append(out, nv)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// MaskServerURL writes an address the server gave (the next page, a Link header) without what may sign it: no user and
// no fragment, and the value of each query parameter left out. A server signs an address with its query.
func MaskServerURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(an address that cannot be read)"
	}
	u.User, u.Fragment, u.RawFragment = nil, "", ""
	q := u.Query()
	names := make([]string, 0, len(q))
	for k := range q {
		names = append(names, k)
	}
	sort.Strings(names)
	for i, k := range names {
		names[i] = url.QueryEscape(k) + "=" + MaskedValue
	}
	u.RawQuery = strings.Join(names, "&")
	return u.String()
}
