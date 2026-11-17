package ingest

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
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
	// errs gathers the errors of the check that is going on; broken are the refinements whose own input was refused.
	errs   *collector
	broken map[string]bool
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
	feed  Feed
	index int
	typ   model.ObjectType
	// typeOK and colsOK say whether the object type and the dataset of the feed were found. A feed whose type or
	// dataset is not found is still looked at, so that the checks that do not need them are made, and the checks that do
	// are left out (the mistake would only be told again).
	typeOK, colsOK bool
	cols           []string
	attrs          []attrPlan
	keys           []keyPlan
	idents         []identPlan
	basis          model.Basis
	grade          model.Grade
	nameCol        string
}

type linkPlan struct {
	def      LinkDef
	lt       model.LinkType
	from, to *feedPlan
	keyType  model.ValueTypeID
	basis    model.Basis
	attrs    []linkAttrPlan
}

// Compile checks the mapping against the source and the registry and returns the plan. It is the one place the
// definitions are judged: a name the standard model does not know, a key it does not have or a link it does not
// allow is an error here and never a surprise in a run. It tells every mistake that does not come of another, as a
// *DefinitionErrors (ADR-0070).
func Compile(reg *model.Registry, src Source, m Mapping) (*Plan, error) {
	p := &Plan{Source: src, Mapping: m, reg: reg, columns: map[string][]string{}, roots: map[string]string{}, errs: newCollector(DocumentMapping), broken: map[string]bool{}}
	for _, d := range src.Datasets {
		p.roots[d.ID] = d.ID
		for _, c := range d.Columns {
			p.columns[d.ID] = append(p.columns[d.ID], c.Name)
		}
	}
	p.errs.checkVersion(m.Version, "mapping definition")
	p.errs.checkID(ecMapIDInvalid, ptr("id"), "mapping", m.ID, nil)
	p.compileRefinements()
	if len(m.Feeds) == 0 {
		p.errs.add(ecMapNoFeeds, ptr("feeds"), nil, "the mapping has no feed")
	}
	byID := map[string]*feedPlan{}
	firstAt := map[string]int{}
	for i, f := range m.Feeds {
		fp := p.compileFeed(f, i)
		if at, dup := firstAt[f.ID]; dup && f.ID != "" {
			p.errs.add(ecMapFeedDup, ptr("feeds", i, "id"), Params{"id": f.ID, "first": at}, "feed %s appears twice", f.ID)
		} else if f.ID != "" {
			byID[f.ID], firstAt[f.ID] = fp, i
		}
		p.feeds = append(p.feeds, fp)
	}
	if len(m.Feeds) > 0 {
		p.compileLinks(byID)
	}
	err := p.errs.err()
	p.errs = nil
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Plan) compileRefinements() {
	for ri, r := range p.Mapping.Refinements {
		at := ptr("refinements", ri)
		cols, ok := p.columns[r.Input]
		if !ok {
			p.errs.add(ecMapRefInput, at+ptr("input"), Params{"refinement": r.ID, "input": r.Input, "available": p.datasetIDs()},
				"refinement %s reads %q, which is no dataset or earlier refinement", r.ID, r.Input)
		}
		idOK := true
		p.errs.checkID(ecMapRefIDInvalid, at+ptr("id"), "refinement", r.ID, nil)
		switch {
		case r.ID == "":
			idOK = false
			p.errs.add(ecMapRefIDMissing, at+ptr("id"), nil, "refinement id %q is empty or already used", r.ID)
		case p.columns[r.ID] != nil:
			idOK = false
			p.errs.add(ecMapRefIDUsed, at+ptr("id"), Params{"id": r.ID}, "refinement id %q is empty or already used", r.ID)
		}
		if !ok {
			p.broken[r.ID] = idOK
			continue
		}
		for i := range r.Ops {
			checkOp(p.errs, r, ri, i, cols)
		}
		if idOK {
			p.columns[r.ID], p.roots[r.ID] = cols, p.roots[r.Input]
			p.refs = append(p.refs, r)
		}
	}
}

// compileFeed checks one feed. It always gives a plan, which is only to be run when the whole check found nothing.
func (p *Plan) compileFeed(f Feed, index int) *feedPlan {
	at := ptr("feeds", index)
	fp := &feedPlan{feed: f, index: index, grade: model.GradeFact}
	if f.ID == "" {
		p.errs.add(ecMapFeedIDMissing, at+ptr("id"), nil, "feed %d needs an id", index)
	}
	p.errs.checkID(ecMapFeedIDInvalid, at+ptr("id"), "feed", f.ID, nil)
	fp.cols, fp.colsOK = p.columns[f.Dataset]
	if !fp.colsOK && !p.broken[f.Dataset] {
		p.errs.add(ecMapFeedDataset, at+ptr("dataset"), Params{"feed": f.ID, "dataset": f.Dataset, "available": p.datasetIDs()},
			"feed %s reads %q, which is no dataset or refinement; those are: %s", f.ID, f.Dataset, strings.Join(p.datasetIDs(), ", "))
	}
	if typ, ok := p.reg.ObjectTypeByName(f.ObjectType); ok {
		fp.typ, fp.typeOK = typ, true
	} else {
		names := objectTypeNames(p.reg)
		p.errs.add(ecMapFeedType, at+ptr("object_type"), Params{"feed": f.ID, "object_type": f.ObjectType, "suggestions": nonNil(Similar(f.ObjectType, names))},
			"feed %s: %q is no object type of the model%s", f.ID, f.ObjectType, p.didYouMean(f.ObjectType, names, "praxium types list --all"))
	}
	fp.basis = model.Basis(f.Basis)
	if !fp.basis.Valid() {
		p.errs.add(ecMapFeedBasis, at+ptr("basis"), Params{"feed": f.ID, "basis": f.Basis, "valid": basisNames},
			"feed %s: basis %q is none of configured, observed, declared, derived", f.ID, f.Basis)
	}
	if f.Approximate {
		fp.grade = model.GradeAssumed
	}
	if f.RetiresObjects && !f.CreatesObjects {
		p.errs.add(ecMapFeedRetire, at+ptr("retires_objects"), Params{"feed": f.ID}, "feed %s retires objects it does not create", f.ID)
	}
	p.compileAttrs(fp, at)
	p.compileKeys(fp, at)
	p.compileIdentifiers(fp, at)
	return fp
}

var basisNames = []string{"configured", "observed", "declared", "derived"}

// nonNil gives an empty list for none, so that a list in the params is a list in JSON.
func nonNil(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

func (p *Plan) compileAttrs(fp *feedPlan, at string) {
	var props map[string]model.Property
	if fp.typeOK {
		props = propertiesByName(p.reg, fp.typ)
	}
	for k, a := range fp.feed.Attributes {
		aat := at + ptr("attributes", k)
		var pr model.Property
		prOK := false
		if fp.typeOK {
			pr, prOK = p.attributeFor(fp, props, a.Attribute, aat+ptr("attribute"))
		}
		colOK := true
		if fp.colsOK && !slices.Contains(fp.cols, a.Column) {
			colOK = false
			p.errs.add(ecMapAttrColumn, aat+ptr("column"), Params{"feed": fp.feed.ID, "attribute": a.Attribute, "column": a.Column, "dataset": fp.feed.Dataset, "columns": fp.cols},
				"feed %s: attribute %s reads column %q, which dataset %s does not have; its columns are: %s", fp.feed.ID, a.Attribute, a.Column, fp.feed.Dataset, columnList(fp.cols))
		}
		if !prOK {
			continue
		}
		vt, ok := p.reg.ValueType(pr.ValueType)
		if !ok || !supportedBase(vt.Base) {
			p.errs.add(ecMapAttrKind, aat+ptr("attribute"), Params{"feed": fp.feed.ID, "attribute": a.Attribute, "kind": string(vt.Base)},
				"feed %s: attribute %s has a value of kind %s, which a flat file cell does not give", fp.feed.ID, a.Attribute, vt.Base)
			continue
		}
		if colOK {
			fp.attrs = append(fp.attrs, attrPlan{valueSpec: valueSpec{prop: pr, base: vt.Base, volatile: pr.EffectiveHistory() == model.HistoryVolatile}, column: a.Column, rank: a.Rank})
		}
		if a.Attribute == "name" {
			fp.nameCol = a.Column
		}
	}
}

func supportedBase(b model.BaseKind) bool {
	switch b {
	case model.BaseString, model.BaseEnum, model.BaseInteger, model.BaseNumber, model.BaseBoolean, model.BaseTime, model.BaseQuantity:
		return true
	}
	return false
}

// keysHint lists the identity keys the model gives the type of a feed, for the message that refuses a key.
func (p *Plan) keysHint(fp *feedPlan) string {
	keys := p.reg.DescribeIdentityKeys(fp.typ.ID)
	if len(keys) == 0 {
		return fmt.Sprintf("Type %s has no identity key.", fp.typ.Name)
	}
	return fmt.Sprintf("The identity keys of %s are: %s. In a mapping a key is written as \"components\" (properties and the columns they read) or as \"scheme\" with a \"column\"; a key that has a link in it cannot be written in this build.", fp.typ.Name, strings.Join(keys, "; "))
}

// schemesHint lists the identifier schemes: those that are a key of the type first, then all of them.
func (p *Plan) schemesHint(fp *feedPlan) string {
	own := p.reg.SchemeKeyNames(fp.typ.ID)
	out := "The schemes of the model are: " + strings.Join(p.reg.IdentifierSchemeNames(), ", ") + "."
	if len(own) > 0 {
		out = fmt.Sprintf("The schemes that are an identity key of %s: %s. ", fp.typ.Name, strings.Join(own, ", ")) + out
	}
	return out
}

func (p *Plan) datasetIDs() []string {
	ids := make([]string, 0, len(p.columns))
	for id := range p.columns {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func objectTypeNames(reg *model.Registry) []string {
	var out []string
	for _, t := range reg.ObjectTypes() {
		out = append(out, t.Name)
	}
	return out
}

func linkTypeNames(reg *model.Registry) []string {
	var out []string
	for _, t := range reg.LinkTypes() {
		out = append(out, t.Name)
	}
	return out
}

// didYouMean is " (did you mean X, Y?)" for the names that are close to a word. When none is, it tells which command
// lists the names there are.
func (p *Plan) didYouMean(word string, names []string, listCommand string) string {
	if near := Similar(word, names); len(near) > 0 {
		return " (did you mean " + strings.Join(near, ", ") + "?)"
	}
	return " (no type of the model has a name like it; see the names there are with: " + listCommand + ")"
}
