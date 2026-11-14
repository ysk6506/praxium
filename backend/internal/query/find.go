package query

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// Filter operations of kg-queries.md 3.
const (
	OpEquals = "equals"
	OpIn     = "in"
	OpPrefix = "prefix"
	OpRange  = "range"
)

// AttrFilter is one condition on a versioned attribute. Values has one entry
// for equals and prefix, any number for in, and for range the lower and the
// upper bound (either may be empty).
type AttrFilter struct {
	Name   string
	Op     string
	Values []string
}

// IdentifierFilter finds the object an identifier names.
type IdentifierFilter struct {
	Scheme string
	Value  string
}

// FindInput is the input of find (kg-queries.md 3). Conditions are joined by and.
type FindInput struct {
	Common
	ObjectType string
	// IncludeSubtypes defaults to true.
	IncludeSubtypes *bool
	Trait           string
	Attributes      []AttrFilter
	Identifier      *IdentifierFilter
	// Text matches the display name and the string attributes the caller may read, ignoring case.
	Text string
	// ScopeID is refused: stored scopes do not exist yet.
	ScopeID string
}

// TypeCount is the number of matching objects of one type.
type TypeCount struct {
	ObjectType string `json:"object_type"`
	Count      int    `json:"count"`
}

// FindItem is one matching object.
type FindItem struct {
	ObjectID    string   `json:"object_id"`
	ObjectType  string   `json:"object_type"`
	DisplayName string   `json:"display_name"`
	MatchedOn   []string `json:"matched_on"`
}

// FindData is the body of the answer of find.
type FindData struct {
	CountsByType []TypeCount `json:"counts_by_type"`
	Items        []FindItem  `json:"items,omitempty"`
}

type attrSpec struct {
	no   int
	prop model.Property
}

type attrCond struct {
	AttrFilter
	byType map[int]attrSpec
}

type findPlan struct {
	types     map[int]bool
	traits    map[int]bool
	attrs     []attrCond
	idents    map[graph.ObjectID]bool
	identName string
	text      string
}

// Find answers find over the snapshot the input names.
func (e *Engine) Find(ctx context.Context, in FindInput, f ReadFilter) (Answer[FindData], error) {
	var zero Answer[FindData]
	if in.ScopeID != "" {
		return zero, kernel.Errorf(CodeScopeUnsupported, "scope_id needs stored scopes, which do not exist yet")
	}
	fp := fingerprint("find", in.ObjectType, in.IncludeSubtypes == nil || *in.IncludeSubtypes, in.Trait, attrParts(in.Attributes),
		identPart(in.Identifier), in.Text, in.View, in.KGVersionID, seqPart(in.ChangeSeq), in.Shape)
	r, err := e.open(ctx, in.Common, fp, f)
	if err != nil {
		return zero, err
	}
	plan, err := e.planFind(ctx, r, in)
	if err != nil {
		return zero, err
	}
	data, rc, err := e.runFind(ctx, r, plan, fp)
	if err != nil {
		return zero, err
	}
	env, err := e.envelope(ctx, r, rc)
	if err != nil {
		return zero, err
	}
	return Answer[FindData]{Meta: env, Data: data}, nil
}

func attrParts(fs []AttrFilter) []any {
	out := []any{}
	for _, a := range fs {
		vals := []any{}
		for _, v := range a.Values {
			vals = append(vals, v)
		}
		out = append(out, []any{a.Name, a.Op, vals})
	}
	return out
}

func identPart(i *IdentifierFilter) []any {
	if i == nil {
		return []any{}
	}
	return []any{i.Scheme, i.Value}
}

func seqPart(s *int64) string {
	if s == nil {
		return ""
	}
	return strconv.FormatInt(*s, 10)
}

func (e *Engine) planFind(ctx context.Context, r *read, in FindInput) (*findPlan, error) {
	p := &findPlan{text: strings.ToLower(in.Text)}
	candidates, err := e.planTypes(p, in)
	if err != nil {
		return nil, err
	}
	if err := e.planTrait(p, in.Trait, candidates); err != nil {
		return nil, err
	}
	for _, a := range in.Attributes {
		c, err := e.planAttr(a, candidates, r.filter)
		if err != nil {
			return nil, err
		}
		p.attrs = append(p.attrs, c)
	}
	if in.Identifier != nil {
		if err := e.planIdentifier(ctx, r, p, *in.Identifier); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// planTypes fills the type set of the plan and returns the types a condition on attributes may name.
func (e *Engine) planTypes(p *findPlan, in FindInput) ([]*typeInfo, error) {
	if in.ObjectType == "" {
		return e.types.all, nil
	}
	root, ok := e.types.byName[in.ObjectType]
	if !ok {
		return nil, inputf("object_type", "no object type %q", in.ObjectType)
	}
	p.types = map[int]bool{}
	sub := in.IncludeSubtypes == nil || *in.IncludeSubtypes
	var candidates []*typeInfo
	for _, t := range e.types.all {
		if t.typ.ID == root.typ.ID || sub && t.ancestors[root.typ.ID] {
			p.types[t.typ.TypeNo] = true
			candidates = append(candidates, t)
		}
	}
	return candidates, nil
}

func (e *Engine) planTrait(p *findPlan, name string, candidates []*typeInfo) error {
	if name == "" {
		return nil
	}
	tr, ok := e.reg.TraitByName(name)
	if !ok {
		return inputf("trait", "no trait %q", name)
	}
	p.traits = map[int]bool{}
	for _, t := range candidates {
		if t.traits[tr.ID] {
			p.traits[t.typ.TypeNo] = true
		}
	}
	return nil
}

// checkOp refuses an operation outside the four, or one with the wrong number of values.
func checkOp(a AttrFilter) error {
	switch a.Op {
	case OpEquals, OpPrefix:
		if len(a.Values) != 1 {
			return inputf("attributes", "%s on %q takes one value", a.Op, a.Name)
		}
	case OpIn:
		if len(a.Values) == 0 {
			return inputf("attributes", "in on %q takes at least one value", a.Name)
		}
	case OpRange:
		if len(a.Values) != 2 || a.Values[0] == "" && a.Values[1] == "" {
			return inputf("attributes", "range on %q takes a lower and an upper bound, one of them may be empty", a.Name)
		}
	default:
		return kernel.Errorf(CodeFilterOp, "operation %q is not equals, in, prefix or range", a.Op)
	}
	return nil
}

func (e *Engine) planAttr(a AttrFilter, candidates []*typeInfo, f ReadFilter) (attrCond, error) {
	c := attrCond{AttrFilter: a, byType: map[int]attrSpec{}}
	if err := checkOp(a); err != nil {
		return c, err
	}
	for _, t := range candidates {
		prop, ok := t.byName[a.Name]
		if !ok {
			continue
		}
		if prop.EffectiveHistory() == model.HistoryVolatile {
			return c, kernel.Errorf(CodeVolatileFilter, "attribute %q is volatile; filters on volatile attributes are not served yet", a.Name)
		}
		if SensitivityRank(prop.EffectiveSensitivity()) > SensitivityRank(f.Floor()) {
			return c, kernel.Errorf(CodeHiddenFilter, "attribute %q is above what the caller may read", a.Name)
		}
		c.byType[t.typ.TypeNo] = attrSpec{no: prop.AttrNo, prop: prop}
	}
	if len(c.byType) == 0 {
		return c, kernel.Errorf(CodeFilterField, "no attribute %q on the chosen types", a.Name)
	}
	return c, nil
}

func (e *Engine) planIdentifier(ctx context.Context, r *read, p *findPlan, id IdentifierFilter) error {
	scheme, ok := e.reg.IdentifierSchemeByName(id.Scheme)
	if !ok {
		return inputf("identifier.scheme", "no identifier scheme %q; the schemes are: %s", id.Scheme, strings.Join(e.reg.IdentifierSchemeNames(), ", "))
	}
	recs, err := r.snap.Identifiers(ctx, scheme.ID.String(), tidyIdentifier(scheme, id.Value))
	if err != nil {
		return err
	}
	p.idents = map[graph.ObjectID]bool{}
	p.identName = scheme.Name
	for _, rec := range recs {
		p.idents[rec.Object] = true
	}
	return nil
}

// findScan is the state of one pass over the snapshot: the counts of every
// match, and the items of the page after the cursor.
type findScan struct {
	counts map[string]int
	items  []FindItem
	total  int
	more   bool
	after  string
	limit  int
	paged  bool
}

func (e *Engine) runFind(ctx context.Context, r *read, p *findPlan, fp string) (FindData, ResultComplete, error) {
	sc := &findScan{counts: map[string]int{}, limit: r.c.Limit, paged: r.c.Shape == ShapeItems}
	if r.cur != nil {
		sc.after = r.cur.After
	}
	var after graph.ObjectID
	for {
		page, err := r.snap.ScanObjects(ctx, after, scanPage)
		if err != nil {
			return FindData{}, ResultComplete{}, err
		}
		for _, row := range page {
			after = row.ID
			e.scanRow(r, p, sc, row)
		}
		if len(page) < scanPage {
			break
		}
	}
	data := FindData{CountsByType: sortedCounts(sc.counts), Items: sc.items}
	if sc.paged && sc.items == nil {
		data.Items = []FindItem{}
	}
	rc := ResultComplete{Total: sc.total, Returned: len(sc.items)}
	if sc.more {
		reason := "limit"
		rc.Truncated, rc.Reason = true, &reason
		next := cursor{Gen: r.info.Generation, Seq: r.info.Seq, Version: versionOf(r.info), Query: fp, After: sc.items[len(sc.items)-1].ObjectID}.encode()
		rc.NextCursor = &next
	}
	return data, rc, nil
}

// scanRow judges one row: whether it matches, whether the caller may see it, and whether it falls on the page.
func (e *Engine) scanRow(r *read, p *findPlan, sc *findScan, row graph.ObjectRow) {
	matched, ok := e.matchFind(r, p, row)
	if !ok {
		return
	}
	if !e.judge(r, row).Visible {
		r.hid.reached[row.ID.String()] = true
		return
	}
	sc.total++
	info := e.types.byNo[row.TypeNo]
	sc.counts[info.typ.Name]++
	if !sc.paged || row.ID.String() <= sc.after {
		return
	}
	if len(sc.items) >= sc.limit {
		sc.more = true
		return
	}
	sc.items = append(sc.items, FindItem{ObjectID: row.ID.String(), ObjectType: info.typ.Name, DisplayName: row.Name, MatchedOn: matched})
	r.use.add(row.MinGrade, "", "", "")
}

func versionOf(info graph.SnapshotInfo) string {
	if info.Version.IsZero() {
		return ""
	}
	return info.Version.String()
}

func sortedCounts(m map[string]int) []TypeCount {
	out := make([]TypeCount, 0, len(m))
	for t, n := range m {
		out = append(out, TypeCount{ObjectType: t, Count: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ObjectType < out[j].ObjectType })
	return out
}

// matchFind judges one row against the plan and says on what it matched.
func (e *Engine) matchFind(r *read, p *findPlan, row graph.ObjectRow) ([]string, bool) {
	if p.types != nil && !p.types[row.TypeNo] || p.traits != nil && !p.traits[row.TypeNo] {
		return nil, false
	}
	if p.idents != nil && !p.idents[row.ID] {
		return nil, false
	}
	var cap model.Sensitivity
	capKnown := false
	capOf := func() model.Sensitivity {
		if !capKnown {
			cap, capKnown = e.judge(r, row).Cap, true
		}
		return cap
	}
	for _, c := range p.attrs {
		if !e.attrMatches(r, c, row, capOf) {
			return nil, false
		}
	}
	if p.text != "" && !e.textMatches(r, p.text, row, capOf()) {
		return nil, false
	}
	return matchedOn(p), true
}

// matchedOn names the conditions of the plan; a row that got this far met all of them.
func matchedOn(p *findPlan) []string {
	on := []string{}
	if p.types != nil {
		on = append(on, "object_type")
	}
	if p.traits != nil {
		on = append(on, "trait")
	}
	for _, c := range p.attrs {
		on = append(on, "attribute:"+c.Name)
	}
	if p.idents != nil {
		on = append(on, "identifier:"+p.identName)
	}
	if p.text != "" {
		on = append(on, "text")
	}
	return on
}

func (e *Engine) attrMatches(r *read, c attrCond, row graph.ObjectRow, capOf func() model.Sensitivity) bool {
	spec, ok := c.byType[row.TypeNo]
	if !ok {
		return false
	}
	a, has := row.Attrs[spec.no]
	if !has || !r.designed(a.Basis) {
		return false
	}
	if SensitivityRank(spec.prop.EffectiveSensitivity()) > SensitivityRank(capOf()) {
		return false
	}
	return compare(c, valueText(a.Value))
}

func (e *Engine) textMatches(r *read, text string, row graph.ObjectRow, cap model.Sensitivity) bool {
	if strings.Contains(strings.ToLower(row.Name), text) {
		return true
	}
	info := e.types.byNo[row.TypeNo]
	for no, a := range row.Attrs {
		s, isText := a.Value.(string)
		prop := info.byAttr[no]
		if !isText || !r.designed(a.Basis) || SensitivityRank(prop.EffectiveSensitivity()) > SensitivityRank(cap) {
			continue
		}
		if strings.Contains(strings.ToLower(s), text) {
			return true
		}
	}
	return false
}
