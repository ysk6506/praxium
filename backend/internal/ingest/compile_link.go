package ingest

import (
	"slices"

	"github.com/ysk6506/praxium/backend/internal/model"
)

func (p *Plan) compileLinks(feeds map[string]*feedPlan) {
	for i, d := range p.Mapping.Links {
		p.links = append(p.links, p.compileLink(d, i, feeds))
	}
}

// compileLink checks one link definition. The checks that need the link type, or a feed that was found, are made when
// that is found.
func (p *Plan) compileLink(d LinkDef, index int, feeds map[string]*feedPlan) *linkPlan {
	at := ptr("links", index)
	lp := &linkPlan{def: d, basis: model.BasisDerived}
	ltOK := false
	if lt, ok := p.reg.LinkTypeByName(d.LinkType); ok {
		lp.lt, ltOK = lt, true
	} else {
		names := linkTypeNames(p.reg)
		p.errs.add(ecMapLinkType, at+ptr("link_type"), Params{"link": d.ID, "link_type": d.LinkType, "suggestions": nonNil(Similar(d.LinkType, names))},
			"link definition %s: %q is no link type of the model%s", d.ID, d.LinkType, p.didYouMean(d.LinkType, names, "praxium types list --kind link --all"))
	}
	if d.Condition != ConditionSameKey {
		p.errs.add(ecMapLinkCond, at+ptr("condition"), Params{"link": d.ID, "condition": d.Condition, "supported": []string{ConditionSameKey}},
			"link definition %s: condition %q is not in this build (same_key)", d.ID, d.Condition)
	}
	lp.from, lp.to = feeds[d.From.Feed], feeds[d.To.Feed]
	p.checkLinkFeed(lp.from, d, "from", at+ptr("from", "feed"), d.From.Feed)
	p.checkLinkFeed(lp.to, d, "to", at+ptr("to", "feed"), d.To.Feed)
	p.checkBinding(lp, ltOK, at)
	if len(d.From.Columns) == 0 || len(d.From.Columns) != len(d.To.Columns) {
		p.errs.add(ecMapLinkCount, at+ptr("from", "columns"), Params{"link": d.ID, "from_count": len(d.From.Columns), "to_count": len(d.To.Columns)},
			"link definition %s: both ends need the same number of key columns, at least one", d.ID)
	}
	p.checkEndColumns(lp.from, d, "from", at, d.From.Columns)
	p.checkEndColumns(lp.to, d, "to", at, d.To.Columns)
	p.checkBasis(lp, at)
	p.linkKeyType(lp, at)
	if ltOK {
		p.compileLinkAttrs(lp, at)
	}
	return lp
}

// checkBinding asks whether the link type allows a link between the types of the two feeds, when all three are known.
func (p *Plan) checkBinding(lp *linkPlan, ltOK bool, at string) {
	if !ltOK || lp.from == nil || lp.to == nil || !lp.from.typeOK || !lp.to.typeOK {
		return
	}
	if _, ok := p.reg.BindingFor(lp.from.typ.ID, lp.to.typ.ID, lp.lt.ID); !ok {
		p.errs.add(ecMapLinkBinding, at+ptr("link_type"), Params{"link": lp.def.ID, "link_type": lp.lt.Name, "from_type": lp.from.typ.Name, "to_type": lp.to.typ.Name},
			"link definition %s: %s does not allow a link from %s to %s", lp.def.ID, lp.lt.Name, lp.from.typ.Name, lp.to.typ.Name)
	}
}

// checkLinkFeed tells an end that names a feed that does not exist.
func (p *Plan) checkLinkFeed(fp *feedPlan, d LinkDef, end, at, feed string) {
	if fp == nil {
		p.errs.add(ecMapLinkFeed, at, Params{"link": d.ID, "end": end, "feed": feed}, "link definition %s names a feed that does not exist", d.ID)
	}
}

// checkEndColumns checks the key columns of one end against the dataset of its feed, when that was found.
func (p *Plan) checkEndColumns(fp *feedPlan, d LinkDef, end, at string, want []string) {
	if fp == nil || !fp.colsOK {
		return
	}
	for k, c := range want {
		if !slices.Contains(fp.cols, c) {
			p.errs.add(ecMapLinkColumn, at+ptr(end, "columns", k), Params{"link": d.ID, "end": end, "column": c, "dataset": fp.feed.Dataset},
				"link definition %s: column %q is not in the dataset", d.ID, c)
		}
	}
}

// checkBasis takes the basis the definition names, when it names one.
func (p *Plan) checkBasis(lp *linkPlan, at string) {
	if lp.def.Basis == "" {
		return
	}
	lp.basis = model.Basis(lp.def.Basis)
	if !lp.basis.Valid() {
		p.errs.add(ecMapLinkBasis, at+ptr("basis"), Params{"link": lp.def.ID, "basis": lp.def.Basis, "valid": basisNames},
			"link definition %s: basis %q is not a basis", lp.def.ID, lp.def.Basis)
	}
}

func (p *Plan) linkKeyType(lp *linkPlan, at string) {
	if lp.def.KeyType == "" {
		return
	}
	for _, vt := range p.reg.ValueTypes() {
		if vt.Name == lp.def.KeyType {
			lp.keyType = vt.ID
			return
		}
	}
	p.errs.add(ecMapLinkKeyType, at+ptr("key_type"), Params{"link": lp.def.ID, "key_type": lp.def.KeyType},
		"link definition %s: %q is no value type of the model", lp.def.ID, lp.def.KeyType)
}
