package ingest

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// A dataset that uses the result of another dataset (ADR-0111, E7): its request has variables {parent.<name>} that the values
// of the records of the parent dataset fill in, one request for each value or one for each batch of values. The parent is read
// first and to its end. This file is the form of it, the grammar of the variables, and the checks of the definition; the
// adapter reads the values and makes the requests.

// The defaults of a child dataset and the most a definition may set them to (ADR-0111). The server has ceilings of its own that no
// definition passes (httpsource.Ceilings).
const (
	DefaultChildRequests = 2000
	MaxChildRequests     = 50_000
	DefaultChildSeconds  = 30 * 60
	MaxChildSeconds      = 4 * 60 * 60
	DefaultBatchSize     = 100
	MaxBatchSize         = 1000
	// ParentValueMax is the most bytes a value of a parent may have, whatever the definition says.
	ParentValueMax = 256
	// OnNotFoundSkip says that a 404 for one value of the parent is a value that is passed over (the dataset is then read as what was
	// seen).
	OnNotFoundSkip = "skip"
	// BatchSeparators are the separators a batch of values may be joined with in the query.
	BatchSeparators = ",;|:"
)

// Parent says which dataset gives the values a child dataset asks with, and how many of them go in one request.
type Parent struct {
	// Dataset is the id of a dataset of the same source. Values says, for each name of a variable, the path of the value in a record
	// of the parent (a string or an integer).
	Dataset string            `json:"dataset"`
	Values  map[string]string `json:"values"`
	// Batch puts several values in one request (a query value or a whole value of the body).
	Batch *Batch `json:"batch,omitempty"`
	// MaxRequests and MaxSeconds are the most requests and the longest time one read of the dataset may take, parent included.
	MaxRequests int `json:"max_requests,omitempty"`
	MaxSeconds  int `json:"max_seconds,omitempty"`
}

// Batch is how many values go in one request and how they are joined in the query; Match is where the answer says which value a
// record is for (a path in a record of the child), so that a value the answer has no record for is known as not seen.
type Batch struct {
	Size      int    `json:"size,omitempty"`
	Separator string `json:"separator,omitempty"`
	Match     string `json:"match,omitempty"`
}

// Helper says that the dataset is read only to give values to another one: it has no columns, no key and no rows.
func (d Dataset) Helper() bool { return d.Collect != nil && !*d.Collect }

// IsChild says that the dataset asks with the values of another dataset.
func (d Dataset) IsChild() bool { return d.Parent != nil }

// ParentPiece is a part of a text with variables: a literal text, or the name of a variable ({parent.<name>}).
type ParentPiece struct {
	Text string
	Var  string
}

var parentVarRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

var errParentText = errors.New("a { or a } that is neither doubled nor the variable {parent.<name>}")

// ParseParentText reads a text of a request as literals and variables, from the left: {{ and }} stand for the brace itself and
// {parent.<name>} is a variable. Any other brace is an error. A text with no variable has the pieces of UnescapeBraces.
func ParseParentText(s string) ([]ParentPiece, error) {
	var out []ParentPiece
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			out = append(out, ParentPiece{Text: lit.String()})
			lit.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case (ch == '{' || ch == '}') && i+1 < len(s) && s[i+1] == ch:
			lit.WriteByte(ch)
			i++
		case ch == '{':
			name, end, ok := parentVarAt(s[i:])
			if !ok {
				return nil, errParentText
			}
			flush()
			out = append(out, ParentPiece{Var: name})
			i += end
		case ch == '}':
			return nil, errParentText
		default:
			lit.WriteByte(ch)
		}
	}
	flush()
	return out, nil
}

// parentVarAt reads {parent.<name>} at the start of s: the name, and the place of the closing brace.
func parentVarAt(s string) (string, int, bool) {
	end := strings.IndexByte(s, '}')
	if end < 0 || !strings.HasPrefix(s, "{parent.") {
		return "", 0, false
	}
	name := s[len("{parent."):end]
	return name, end, parentVarRe.MatchString(name)
}

// ParentVars lists the names of the variables of a text, in order, and whether the text reads (a text that has a brace that is
// no variable and not doubled does not).
func ParentVars(s string) ([]string, bool) {
	pieces, err := ParseParentText(s)
	if err != nil {
		return nil, false
	}
	var names []string
	for _, p := range pieces {
		if p.Var != "" {
			names = append(names, p.Var)
		}
	}
	return names, true
}

// parentVars is what a dataset of a child lets its request use: the names it declared, and which of them were used.
type parentVars struct {
	names map[string]bool
	batch string
	used  map[string]bool
}

func (d Dataset) newParentVars() *parentVars {
	if d.Parent == nil {
		return nil
	}
	pv := &parentVars{names: map[string]bool{}, used: map[string]bool{}}
	for n := range d.Parent.Values {
		pv.names[n] = true
	}
	if d.Parent.Batch != nil && len(d.Parent.Values) == 1 {
		for n := range d.Parent.Values {
			pv.batch = n
		}
	}
	return pv
}

// The places of a request a variable of a parent may be in.
const (
	placeNone = iota
	placePath
	placeQuery
	placeBody
)

// checkTextP is checkText for a place where a variable of the parent may be. A text without a variable is checked as before.
func (c *collector) checkTextP(source, pointer, field, s string, place int) bool {
	pv := c.pv
	if pv == nil || place == placeNone || hasPlaceholderInside(s) || !strings.Contains(s, "{parent.") {
		return c.checkText(source, pointer, field, s)
	}
	pieces, err := ParseParentText(s)
	if err != nil {
		return c.checkText(source, pointer, field, s)
	}
	ok := true
	whole := len(pieces) == 1 && pieces[0].Var != ""
	for _, p := range pieces {
		if p.Var == "" {
			continue
		}
		pv.used[p.Var] = true
		if !c.checkParentVar(source, pointer, field, p.Var, place, whole) {
			ok = false
		}
	}
	return ok
}

// checkParentVar checks one variable of a text: it is declared, and it is in a place that may have it.
func (c *collector) checkParentVar(source, pointer, field, name string, place int, whole bool) bool {
	pv := c.pv
	var reason, why string
	switch {
	case !pv.names[name]:
		c.add(ecAdParentTemplateUnknown, pointer, Params{"source": source, "dataset": c.dataset, "field": field, "name": name},
			"source %s, dataset %s: %s has {parent.%s}, which parent.values does not declare", source, c.dataset, field, name)
		return false
	case name == pv.batch && (place == placePath || !whole):
		reason, why = "a batch of values is a whole value of the query or of the body, and never a part of a text or of the path", "has the batch {parent.%s} where only a whole value of the query or of the body may have it"
	case place == placeBody && !whole:
		reason, why = "a value of the body that has a variable has nothing else in it", "has a {parent.%s} with other text; in a body it is the whole value"
	default:
		return true
	}
	c.add(ecAdParentTemplatePlace, pointer, Params{"source": source, "dataset": c.dataset, "field": field, "reason": reason},
		"source %s, dataset %s: %s "+why, source, c.dataset, field, name)
	return false
}

// checkParentBlock checks and fills the defaults of the parent block of a dataset, and what it says about the dataset.
func (d Dataset) checkParentBlock(c *collector, source, at string) Dataset {
	if d.Parent == nil {
		if d.OnNotFound != "" {
			c.add(ecAdParentInvalid, at+ptr("on_not_found"), Params{"source": source, "dataset": d.ID, "field": "on_not_found", "reason": "is for a dataset that has a parent"},
				"source %s, dataset %s: on_not_found is for a dataset that has a parent", source, d.ID)
		}
		return d
	}
	p := *d.Parent
	p.Values = map[string]string{}
	for k, v := range d.Parent.Values {
		p.Values[k] = v
	}
	bad := func(field, reason string) {
		c.add(ecAdParentInvalid, at+ptr("parent", field), Params{"source": source, "dataset": d.ID, "field": "parent." + field, "reason": reason},
			"source %s, dataset %s: parent.%s %s", source, d.ID, field, reason)
	}
	p.checkNames(bad)
	c.rangeOf(&p.MaxRequests, rangeSpec{"max_requests", DefaultChildRequests, MaxChildRequests}, at, d.ID, source)
	c.rangeOf(&p.MaxSeconds, rangeSpec{"max_seconds", DefaultChildSeconds, MaxChildSeconds}, at, d.ID, source)
	if d.OnNotFound != "" && d.OnNotFound != OnNotFoundSkip {
		c.add(ecAdParentInvalid, at+ptr("on_not_found"), Params{"source": source, "dataset": d.ID, "field": "on_not_found", "reason": "is not skip"},
			"source %s, dataset %s: on_not_found is skip, or not given for a 404 that makes the dataset not complete", source, d.ID)
	}
	if d.Helper() {
		bad("dataset", "is given for a dataset that is only read for its values (collect false); it cannot have a parent")
	}
	if p.Batch != nil {
		b := *p.Batch
		p.Batch = &b
		d.checkBatch(c, source, at, &p)
	}
	d.Parent = &p
	return d
}

// checkNames checks the dataset the values are of and the names and paths of the values.
func (p Parent) checkNames(bad func(field, reason string)) {
	if p.Dataset == "" {
		bad("dataset", "is empty; name the dataset of this source that gives the values")
	}
	if len(p.Values) == 0 {
		bad("values", "is empty; name the values of a record of the parent that the request uses")
	}
	names := make([]string, 0, len(p.Values))
	for k := range p.Values {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if !parentVarRe.MatchString(k) || strings.TrimSpace(p.Values[k]) == "" {
			bad("values", "has a name that is not lower-case letters, digits and _ (starting with a letter), or a path that is empty")
			break
		}
	}
}

// rangeSpec is a number of the parent block: its name, its default and the most it may be.
type rangeSpec struct {
	name    string
	def, hi int
}

// rangeOf gives a number its default when it is not given and refuses one that is outside 1 to the most.
func (c *collector) rangeOf(v *int, sp rangeSpec, at, id, source string) {
	name, def, hi := sp.name, sp.def, sp.hi
	switch {
	case *v == 0:
		*v = def
	case *v < 0 || *v > hi:
		c.add(ecAdParentInvalid, at+ptr("parent", name), Params{"source": source, "dataset": id, "field": "parent." + name, "reason": "is outside the range it may have"},
			"source %s, dataset %s: parent.%s is %d, and it is from 1 to %d", source, id, name, *v, hi)
		*v = def
	}
}

func (d Dataset) checkBatch(c *collector, source, at string, p *Parent) {
	b := p.Batch
	bad := func(field, reason string) {
		c.add(ecAdBatchInvalid, at+ptr("parent", "batch", field), Params{"source": source, "dataset": d.ID, "field": "parent.batch." + field, "reason": reason},
			"source %s, dataset %s: parent.batch.%s %s", source, d.ID, field, reason)
	}
	if b.Size == 0 {
		b.Size = DefaultBatchSize
	}
	if b.Size < 1 || b.Size > MaxBatchSize {
		bad("size", "is from 1 to "+strconv.Itoa(MaxBatchSize))
		b.Size = DefaultBatchSize
	}
	if b.Separator == "" {
		b.Separator = ","
	}
	if len(b.Separator) != 1 || !strings.Contains(BatchSeparators, b.Separator) {
		bad("separator", "is one of "+BatchSeparators)
	}
	if len(p.Values) != 1 {
		bad("size", "needs parent.values to have exactly one name, the one a batch of values is called by")
	}
	if b.Match == "" {
		bad("match", "is needed: the path in a record of the answer of the value it is for, so that a value with no record is not taken for one that is gone")
		return
	}
	_, err := ParseColumnPath(b.Match, d.OpensArray())
	c.pathProblem(source, d.ID, at+ptr("parent", "batch", "match"), "parent.batch.match", b.Match, err)
}

// checkParents checks what the parents of the datasets say of the other datasets: that the parent is a dataset of the source that
// is no child, that no dataset is its own parent, and that the paths of the values read a record of the parent. It is the check
// that needs more than one dataset, so it is made when all of them are read.
func (s Source) checkParents(c *collector) {
	byID := map[string]int{}
	for i, d := range s.Datasets {
		byID[d.ID] = i
	}
	for i, d := range s.Datasets {
		if d.Parent == nil || d.Parent.Dataset == "" {
			continue
		}
		at := ptr("datasets", i, "parent", "dataset")
		params := Params{"source": s.ID, "dataset": d.ID, "parent": clip(d.Parent.Dataset, idClipAt)}
		j, ok := byID[d.Parent.Dataset]
		switch {
		case d.Parent.Dataset == d.ID:
			c.add(ecAdParentCycle, at, params, "source %s: dataset %s is its own parent", s.ID, d.ID)
		case !ok:
			c.add(ecAdParentUnknown, at, params, "source %s: dataset %s has the parent %q, which is no dataset of this source", s.ID, d.ID, clip(d.Parent.Dataset, idClipAt))
		case s.Datasets[j].Parent != nil && s.Datasets[j].Parent.Dataset == d.ID:
			c.add(ecAdParentCycle, at, params, "source %s: datasets %s and %s are each other's parent", s.ID, d.ID, d.Parent.Dataset)
		case s.Datasets[j].Parent != nil:
			c.add(ecAdParentDepth, at, params, "source %s: the parent %s of dataset %s has a parent of its own; one level is read", s.ID, d.Parent.Dataset, d.ID)
		default:
			s.checkParentPaths(c, i, s.Datasets[j])
		}
	}
}

// checkParentPaths checks the paths of the values of dataset i against the records of its parent.
func (s Source) checkParentPaths(c *collector, i int, parent Dataset) {
	d := s.Datasets[i]
	outer := parent.OpensArray()
	names := make([]string, 0, len(d.Parent.Values))
	for k := range d.Parent.Values {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		p := d.Parent.Values[k]
		_, err := ParseColumnPath(p, outer)
		c.pathProblem(s.ID, d.ID, ptr("datasets", i, "parent", "values", k), "parent.values."+clip(k, idClipAt), p, err)
	}
}

// ParentValuePaths gives the paths of the values of a child, read as paths of a record of its parent.
func (d Dataset) ParentValuePaths(parent Dataset) (map[string]ColumnPath, error) {
	out := make(map[string]ColumnPath, len(d.Parent.Values))
	for k, p := range d.Parent.Values {
		cp, err := ParseColumnPath(p, parent.OpensArray())
		if err != nil {
			return nil, err
		}
		out[k] = cp
	}
	return out, nil
}

// MatchPath is the path of the value a record of a batch's answer is for.
func (d Dataset) MatchPath() (ColumnPath, error) {
	return ParseColumnPath(d.Parent.Batch.Match, d.OpensArray())
}

// checkParentUsed says which declared values the request never uses.
func (d Dataset) checkParentUsed(c *collector, source, at string) {
	pv := c.pv
	if pv == nil {
		return
	}
	names := make([]string, 0, len(pv.names))
	for n := range pv.names {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !pv.used[n] {
			c.add(ecAdParentValueUnused, at+ptr("parent", "values", n), Params{"source": source, "dataset": d.ID, "name": n},
				"source %s, dataset %s: parent.values declares %q and no place of the request has {parent.%s}", source, d.ID, n, n)
		}
	}
}

// HasParentVar says whether a text has a variable of the parent in it.
func HasParentVar(s string) bool {
	names, ok := ParentVars(s)
	return ok && len(names) > 0
}
