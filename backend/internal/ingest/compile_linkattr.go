package ingest

import (
	"slices"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// linkAttrPlan is one attribute of a link type that a link definition fills: from a column of the From feed's
// row, or with one fixed value (ADR-0066).
type linkAttrPlan struct {
	valueSpec
	column string
	// fixed is the converted fixed value and fixedText its text, when the definition gives a value instead of a column.
	fixed     any
	fixedText string
	isFixed   bool
}

// linkProperties indexes the properties of a link type by name and alias. A link type has no parent and no trait,
// so the properties it lists are all it has.
func linkProperties(lt model.LinkType) (map[string]model.Property, []string) {
	names := make([]string, 0, len(lt.Properties))
	for _, pr := range lt.Properties {
		names = append(names, pr.Name)
	}
	sort.Strings(names)
	return indexProperties(lt.Properties), names
}

// compileLinkAttrs checks the attributes of a link definition. Every mistake is found here, once, and not row by
// row in a run: a name the link type does not have, a cell and a fixed value together or neither, one attribute
// twice, a column the From feed's dataset lacks, a value a flat file cell cannot give, a fixed value that does not fit.
func (p *Plan) compileLinkAttrs(lp *linkPlan, at string) {
	if len(lp.def.Attributes) == 0 {
		return
	}
	props, names := linkProperties(lp.lt)
	id := lp.def.ID
	owner := attrOwner{who: "link definition " + id, what: "link type " + lp.lt.Name, names: names,
		key: "link", id: id, typeKey: "link_type", typeName: lp.lt.Name, unknown: ecMapLAUnknown, deprecated: ecMapLADeprecated}
	seen := map[model.PropertyID]string{}
	for k, a := range lp.def.Attributes {
		aat := at + ptr("attributes", k)
		pr, ok := p.property(owner, props, a.Attribute, aat+ptr("attribute"))
		if ok {
			if first, dup := seen[pr.ID]; dup {
				p.errs.add(ecMapLADup, aat+ptr("attribute"), Params{"link": id, "attribute": pr.Name, "first": first},
					"link definition %s: attribute %s is given twice (as %q and as %q); a link has one value of an attribute", id, pr.Name, first, a.Attribute)
				continue
			}
			seen[pr.ID] = a.Attribute
		}
		formOK := p.checkLinkAttrForm(lp, a, aat)
		if !ok {
			continue
		}
		if ap, good := p.linkAttrPlan(lp, a, pr, aat, formOK); good {
			lp.attrs = append(lp.attrs, ap)
		}
	}
}

func (p *Plan) linkAttrPlan(lp *linkPlan, a LinkAttrMap, pr model.Property, at string, formOK bool) (linkAttrPlan, bool) {
	id := lp.def.ID
	vt, ok := p.reg.ValueType(pr.ValueType)
	if !ok || !supportedBase(vt.Base) {
		p.errs.add(ecMapLAKind, at+ptr("attribute"), Params{"link": id, "attribute": a.Attribute, "kind": string(vt.Base)},
			"link definition %s: attribute %s has a value of kind %s, which a flat file cell does not give", id, a.Attribute, vt.Base)
		return linkAttrPlan{}, false
	}
	ap := linkAttrPlan{valueSpec: valueSpec{prop: pr, base: vt.Base}, column: a.Column}
	if a.Value == nil || !formOK {
		return ap, formOK
	}
	v, err := convertCell(ap.valueSpec, *a.Value)
	if err != nil {
		p.errs.add(ecMapLAValue, at+ptr("value"), Params{"link": id, "attribute": a.Attribute, "value": *a.Value},
			"link definition %s: the fixed value %q of attribute %s does not fit: %s", id, *a.Value, a.Attribute, plainMessage(err))
		return linkAttrPlan{}, false
	}
	ap.fixed, ap.fixedText, ap.isFixed = v, *a.Value, true
	return ap, true
}

// checkLinkAttrForm checks what the definition says of one attribute before the model is asked: a column or a fixed
// value, one of the two, and the column is one of the From feed's dataset. It tells whether the form is right.
func (p *Plan) checkLinkAttrForm(lp *linkPlan, a LinkAttrMap, at string) bool {
	id := lp.def.ID
	params := Params{"link": id, "attribute": a.Attribute}
	switch {
	case a.Column != "" && a.Value != nil:
		p.errs.add(ecMapLABoth, at, params, "link definition %s: attribute %s has a column and a fixed value; write one of the two", id, a.Attribute)
	case a.Column == "" && a.Value == nil:
		p.errs.add(ecMapLANeither, at, params, "link definition %s: attribute %s has neither a \"column\" nor a \"value\"; write one of the two", id, a.Attribute)
	case a.Value != nil && strings.TrimSpace(*a.Value) == "":
		p.errs.add(ecMapLAEmpty, at+ptr("value"), params, "link definition %s: attribute %s has an empty fixed value, which gives no value; write the value, or remove the attribute", id, a.Attribute)
	case a.Column != "" && lp.from != nil && lp.from.colsOK && !slices.Contains(lp.from.cols, a.Column):
		cols := lp.from.cols
		p.errs.add(ecMapLAColumn, at+ptr("column"), Params{"link": id, "attribute": a.Attribute, "column": a.Column, "dataset": lp.from.feed.Dataset, "feed": lp.from.feed.ID, "columns": cols},
			"link definition %s: attribute %s reads column %q, which dataset %s (the one feed %s reads, the From end) does not have; its columns are: %s",
			id, a.Attribute, a.Column, lp.from.feed.Dataset, lp.from.feed.ID, columnList(cols))
	default:
		return true
	}
	return false
}
