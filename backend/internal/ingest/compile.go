package ingest

import (
	"fmt"
	"slices"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// Plan is a source and a mapping checked against the registry: every name has become the thing it names. The
// run uses the plan and nothing else of the definitions.
type Plan struct {
	Source  Source
	Mapping Mapping
	reg     *model.Registry
	refs    []Refinement
	feeds   []*feedPlan
	links   []*linkPlan
	// columns of every dataset and refinement by id.
	columns map[string][]string
	// roots is the dataset a refinement comes from, through all the refinements before it.
	roots map[string]string
}

type valueSpec struct {
	prop     model.Property
	base     model.BaseKind
	volatile bool
}

type attrPlan struct {
	valueSpec
	column string
	rank   int
}

type keyPart struct {
	kind   graph.PartKind
	ref    string
	vt     model.ValueTypeID
	column string
}

type keyPlan struct {
	name     string
	priority int
	parts    []keyPart
	scheme   model.IdentifierSchemeID
}

type identPlan struct {
	scheme model.IdentifierSchemeID
	vt     model.ValueTypeID
	column string
}

type feedPlan struct {
	feed    Feed
	index   int
	typ     model.ObjectType
	attrs   []attrPlan
	keys    []keyPlan
	idents  []identPlan
	basis   model.Basis
	grade   model.Grade
	nameCol string
}

type linkPlan struct {
	def      LinkDef
	lt       model.LinkType
	from, to *feedPlan
	keyType  model.ValueTypeID
	basis    model.Basis
}

func bad(format string, args ...any) error {
	return kernel.Errorf(kernel.CodeInvalidValue, format, args...)
}

// Compile checks the mapping against the source and the registry and returns the plan. It is the one place the
// definitions are judged: a name the standard model does not know, a key it does not have or a link it does not
// allow is an error here and never a surprise in a run.
func Compile(reg *model.Registry, src Source, m Mapping) (*Plan, error) {
	p := &Plan{Source: src, Mapping: m, reg: reg, columns: map[string][]string{}, roots: map[string]string{}}
	for _, d := range src.Datasets {
		p.roots[d.ID] = d.ID
		for _, c := range d.Columns {
			p.columns[d.ID] = append(p.columns[d.ID], c.Name)
		}
	}
	if err := p.compileRefinements(); err != nil {
		return nil, err
	}
	if len(m.Feeds) == 0 {
		return nil, bad("the mapping has no feed")
	}
	byID := map[string]*feedPlan{}
	for i, f := range m.Feeds {
		fp, err := p.compileFeed(f, i)
		if err != nil {
			return nil, err
		}
		if byID[f.ID] != nil {
			return nil, bad("feed %s appears twice", f.ID)
		}
		byID[f.ID] = fp
		p.feeds = append(p.feeds, fp)
	}
	return p, p.compileLinks(byID)
}

func (p *Plan) compileRefinements() error {
	for _, r := range p.Mapping.Refinements {
		cols, ok := p.columns[r.Input]
		if !ok {
			return bad("refinement %s reads %q, which is no dataset or earlier refinement", r.ID, r.Input)
		}
		if r.ID == "" || p.columns[r.ID] != nil {
			return bad("refinement id %q is empty or already used", r.ID)
		}
		for i, op := range r.Ops {
			if err := checkOp(op, cols); err != nil {
				return bad("refinement %s, operation %d: %v", r.ID, i+1, err)
			}
		}
		p.columns[r.ID], p.roots[r.ID] = cols, p.roots[r.Input]
		p.refs = append(p.refs, r)
	}
	return nil
}

func (p *Plan) compileFeed(f Feed, index int) (*feedPlan, error) {
	cols, ok := p.columns[f.Dataset]
	if !ok {
		return nil, bad("feed %s reads %q, which is no dataset or refinement", f.ID, f.Dataset)
	}
	typ, ok := p.reg.ObjectTypeByName(f.ObjectType)
	if !ok {
		return nil, bad("feed %s: %q is no object type of the model", f.ID, f.ObjectType)
	}
	fp := &feedPlan{feed: f, index: index, typ: typ, grade: model.GradeFact}
	fp.basis = model.Basis(f.Basis)
	if !fp.basis.Valid() {
		return nil, bad("feed %s: basis %q is none of configured, observed, declared, derived", f.ID, f.Basis)
	}
	if f.Approximate {
		fp.grade = model.GradeAssumed
	}
	if f.RetiresObjects && !f.CreatesObjects {
		return nil, bad("feed %s retires objects it does not create", f.ID)
	}
	props := map[string]model.Property{}
	for _, pr := range p.reg.EffectiveProperties(typ.ID) {
		props[pr.Name] = pr
	}
	if err := p.compileAttrs(fp, props, cols); err != nil {
		return nil, err
	}
	if err := p.compileKeys(fp, props, cols); err != nil {
		return nil, err
	}
	return fp, p.compileIdentifiers(fp, cols)
}

func (p *Plan) compileAttrs(fp *feedPlan, props map[string]model.Property, cols []string) error {
	for _, a := range fp.feed.Attributes {
		pr, ok := props[a.Attribute]
		if !ok {
			return bad("feed %s: type %s has no attribute %q", fp.feed.ID, fp.typ.Name, a.Attribute)
		}
		if !slices.Contains(cols, a.Column) {
			return bad("feed %s: attribute %s reads column %q, which dataset %s does not have", fp.feed.ID, a.Attribute, a.Column, fp.feed.Dataset)
		}
		vt, ok := p.reg.ValueType(pr.ValueType)
		if !ok || !supportedBase(vt.Base) {
			return kernel.Errorf(kernel.CodeUnsupported, "feed %s: attribute %s has a value of kind %s, which a flat file cell does not give", fp.feed.ID, a.Attribute, vt.Base)
		}
		fp.attrs = append(fp.attrs, attrPlan{valueSpec: valueSpec{prop: pr, base: vt.Base, volatile: pr.EffectiveHistory() == model.HistoryVolatile}, column: a.Column, rank: a.Rank})
		if a.Attribute == "name" {
			fp.nameCol = a.Column
		}
	}
	return nil
}

func supportedBase(b model.BaseKind) bool {
	switch b {
	case model.BaseString, model.BaseEnum, model.BaseInteger, model.BaseNumber, model.BaseBoolean, model.BaseTime, model.BaseQuantity:
		return true
	}
	return false
}

// compileKeys finds, for every identity map of the feed, the key of the standard model it stands for.
func (p *Plan) compileKeys(fp *feedPlan, props map[string]model.Property, cols []string) error {
	if len(fp.feed.Identity) == 0 {
		return bad("feed %s has no identity: no row could be told from another", fp.feed.ID)
	}
	registry := p.reg.IdentityKeys(fp.typ.ID)
	for _, im := range fp.feed.Identity {
		kp, err := p.keyFor(fp, im, registry, props, cols)
		if err != nil {
			return err
		}
		fp.keys = append(fp.keys, kp)
	}
	sort.SliceStable(fp.keys, func(i, j int) bool { return fp.keys[i].priority < fp.keys[j].priority })
	return nil
}

func (p *Plan) keyFor(fp *feedPlan, im IdentityMap, registry []model.IdentityKey, props map[string]model.Property, cols []string) (keyPlan, error) {
	if (im.Scheme == "") == (len(im.Components) == 0) {
		return keyPlan{}, bad("feed %s: an identity key names a scheme or lists components, one of the two", fp.feed.ID)
	}
	if im.Scheme != "" {
		return p.schemeKey(fp, im, registry, cols)
	}
	names := make([]string, len(im.Components))
	parts := make([]keyPart, len(im.Components))
	for i, c := range im.Components {
		pr, ok := props[c.Property]
		if !ok || !slices.Contains(cols, c.Column) {
			return keyPlan{}, bad("feed %s: identity component %s/%s names a property the type lacks or a column the dataset lacks", fp.feed.ID, c.Property, c.Column)
		}
		names[i] = c.Property
		parts[i] = keyPart{kind: graph.PartProperty, ref: pr.ID.String(), vt: pr.ValueType, column: c.Column}
	}
	for _, k := range registry {
		if k.Scheme.IsZero() && p.sameProperties(k, names) {
			return keyPlan{name: keyName(im, fmt.Sprint(names)), priority: im.Priority, parts: parts}, nil
		}
	}
	return keyPlan{}, bad("feed %s: type %s has no identity key made of the properties %v, in that order", fp.feed.ID, fp.typ.Name, names)
}

func (p *Plan) schemeKey(fp *feedPlan, im IdentityMap, registry []model.IdentityKey, cols []string) (keyPlan, error) {
	sch, ok := p.reg.IdentifierSchemeByName(im.Scheme)
	if !ok {
		return keyPlan{}, bad("feed %s: %q is no identifier scheme of the model", fp.feed.ID, im.Scheme)
	}
	if !slices.Contains(cols, im.Column) {
		return keyPlan{}, bad("feed %s: identity %s reads column %q, which the dataset lacks", fp.feed.ID, im.Scheme, im.Column)
	}
	for _, k := range registry {
		if k.Scheme == sch.ID && len(k.Components) == 0 {
			return keyPlan{name: keyName(im, im.Scheme), priority: im.Priority, scheme: sch.ID,
				parts: []keyPart{{kind: graph.PartScheme, ref: sch.ID.String(), vt: sch.Normalizer, column: im.Column}}}, nil
		}
	}
	return keyPlan{}, bad("feed %s: scheme %s is not an identity key of type %s", fp.feed.ID, im.Scheme, fp.typ.Name)
}

func keyName(im IdentityMap, fallback string) string {
	if im.KeyName != "" {
		return im.KeyName
	}
	return fallback
}

// sameProperties tells whether the key is made of exactly these properties, in this order.
func (p *Plan) sameProperties(k model.IdentityKey, names []string) bool {
	if len(k.Components) != len(names) {
		return false
	}
	for i, c := range k.Components {
		if c.Property.IsZero() {
			return false
		}
		ref, ok := p.reg.Property(c.Property)
		if !ok || ref.Property.Name != names[i] {
			return false
		}
	}
	return true
}

func (p *Plan) compileIdentifiers(fp *feedPlan, cols []string) error {
	for _, im := range fp.feed.Identifiers {
		sch, ok := p.reg.IdentifierSchemeByName(im.Scheme)
		if !ok || !slices.Contains(cols, im.Column) {
			return bad("feed %s: identifier %s/%s names a scheme the model lacks or a column the dataset lacks", fp.feed.ID, im.Scheme, im.Column)
		}
		fp.idents = append(fp.idents, identPlan{scheme: sch.ID, vt: sch.Normalizer, column: im.Column})
	}
	return nil
}

func (p *Plan) compileLinks(feeds map[string]*feedPlan) error {
	for _, d := range p.Mapping.Links {
		lp, err := p.compileLink(d, feeds)
		if err != nil {
			return err
		}
		p.links = append(p.links, lp)
	}
	return nil
}

func (p *Plan) compileLink(d LinkDef, feeds map[string]*feedPlan) (*linkPlan, error) {
	lt, ok := p.reg.LinkTypeByName(d.LinkType)
	if !ok {
		return nil, bad("link definition %s: %q is no link type of the model", d.ID, d.LinkType)
	}
	if d.Condition != ConditionSameKey {
		return nil, kernel.Errorf(kernel.CodeUnsupported, "link definition %s: condition %q is not in this build (same_key)", d.ID, d.Condition)
	}
	from, to := feeds[d.From.Feed], feeds[d.To.Feed]
	if from == nil || to == nil {
		return nil, bad("link definition %s names a feed that does not exist", d.ID)
	}
	if _, ok := p.reg.BindingFor(from.typ.ID, to.typ.ID, lt.ID); !ok {
		return nil, bad("link definition %s: %s does not allow a link from %s to %s", d.ID, lt.Name, from.typ.Name, to.typ.Name)
	}
	if len(d.From.Columns) == 0 || len(d.From.Columns) != len(d.To.Columns) {
		return nil, bad("link definition %s: both ends need the same number of key columns, at least one", d.ID)
	}
	if err := checkColumns(p.columns[from.feed.Dataset], d.From.Columns, d.ID); err != nil {
		return nil, err
	}
	if err := checkColumns(p.columns[to.feed.Dataset], d.To.Columns, d.ID); err != nil {
		return nil, err
	}
	lp := &linkPlan{def: d, lt: lt, from: from, to: to, basis: model.BasisDerived}
	if d.Basis != "" {
		lp.basis = model.Basis(d.Basis)
		if !lp.basis.Valid() {
			return nil, bad("link definition %s: basis %q is not a basis", d.ID, d.Basis)
		}
	}
	return lp, p.linkKeyType(lp)
}

func (p *Plan) linkKeyType(lp *linkPlan) error {
	if lp.def.KeyType == "" {
		return nil
	}
	for _, vt := range p.reg.ValueTypes() {
		if vt.Name == lp.def.KeyType {
			lp.keyType = vt.ID
			return nil
		}
	}
	return bad("link definition %s: %q is no value type of the model", lp.def.ID, lp.def.KeyType)
}

func checkColumns(have, want []string, id string) error {
	for _, c := range want {
		if !slices.Contains(have, c) {
			return bad("link definition %s: column %q is not in the dataset", id, c)
		}
	}
	return nil
}
