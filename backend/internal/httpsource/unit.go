package httpsource

import (
	"encoding/json"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// pval is a value of the parent: its text, and whether it is an integer, which a body gives as a number.
type pval struct {
	text string
	num  bool
}

func (v pval) json() any {
	if v.num {
		return json.Number(v.text)
	}
	return verbatim(v.text)
}

// unit is the values one request, or one series of pages, of the child is made with: a value of each name, or the values of one
// name for a batch.
type unit struct {
	names []string
	rows  [][]pval
	batch bool
	sep   string
	// seen is the texts of the match of the records of the answers, for a batch.
	seen map[string]bool
}

func (u *unit) index(name string) int { return slices.Index(u.names, name) }

// text is the value of a name as the text of a query or a path; a batch is its values joined.
func (u *unit) text(name string) string {
	i := u.index(name)
	if i < 0 {
		return ""
	}
	if !u.batch {
		return u.rows[0][i].text
	}
	parts := make([]string, len(u.rows))
	for n, r := range u.rows {
		parts[n] = r[i].text
	}
	return strings.Join(parts, u.sep)
}

// value is the value of a name as a value of a body: one, or an array of them for a batch.
func (u *unit) value(name string) any {
	i := u.index(name)
	if i < 0 {
		return ""
	}
	if !u.batch {
		return u.rows[0][i].json()
	}
	out := make([]any, len(u.rows))
	for n, r := range u.rows {
		out[n] = r[i].json()
	}
	return out
}

// fill puts the values in the query and the body of a request (ADR-0111): a query value that has a variable is made from its text,
// and it is not shown in the exchanges (the source gave it); a value of a body that is a variable is the value, as a value and
// not as a text that is read again.
func (u *unit) fill(p *params, req *ingest.Request) {
	names := make([]string, 0, len(req.Query))
	for k := range req.Query {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		raw := req.Query[k]
		if !ingest.HasParentVar(raw) {
			continue
		}
		name := unescape(k)
		p.query.Set(name, u.render(raw))
		for i := range p.shown {
			if p.shown[i].Name == name {
				p.shown[i] = shownValue(name, "", true)
			}
		}
	}
	if p.body != nil {
		p.body, _ = u.inBody(p.body).(map[string]any)
	}
}

// render makes a text of a query from its pieces.
func (u *unit) render(raw string) string {
	pieces, err := ingest.ParseParentText(raw)
	if err != nil {
		return unescape(raw)
	}
	var b strings.Builder
	for _, pc := range pieces {
		if pc.Var != "" {
			b.WriteString(u.text(pc.Var))
		} else {
			b.WriteString(pc.Text)
		}
	}
	return b.String()
}

func (u *unit) inBody(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = u.inBody(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = u.inBody(e)
		}
		return out
	case string:
		if names, ok := ingest.ParentVars(x); ok && len(names) == 1 && x == "{parent."+names[0]+"}" {
			return u.value(names[0])
		}
	}
	return v
}

// path makes the path of a request: the path of the base address and the path of the request, with the values in it. The text of a
// path is made twice, as the path (the values as they are) and as its escaped form (a value escaped as one step of a path), so
// that it is escaped once and never twice: a space is %20, a % is %25, a ; is %3B and a , is %2C.
func (u *unit) path(base, tmpl string) (string, string) {
	pieces, err := ingest.ParseParentText(tmpl)
	if err != nil {
		return base + unescape(tmpl), ""
	}
	p, r := base, escapedPath(base)
	for _, pc := range pieces {
		if pc.Var == "" {
			p += pc.Text
			r += escapedPath(pc.Text)
			continue
		}
		v := u.text(pc.Var)
		p += v
		r += url.PathEscape(v)
	}
	return p, r
}

func escapedPath(s string) string { return (&url.URL{Path: s}).EscapedPath() }

// parentLabel is a path as the definition writes it, with the variables kept as {parent.<name>}.
func parentLabel(tmpl string) string {
	pieces, err := ingest.ParseParentText(tmpl)
	if err != nil {
		return unescape(tmpl)
	}
	var b strings.Builder
	for _, pc := range pieces {
		if pc.Var != "" {
			b.WriteString("{parent." + pc.Var + "}")
		} else {
			b.WriteString(pc.Text)
		}
	}
	return b.String()
}
