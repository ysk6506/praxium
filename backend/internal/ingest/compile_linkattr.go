package ingest

import (
	"slices"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
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
func (p *Plan) compileLinkAttrs(lp *linkPlan) error {
	if len(lp.def.Attributes) == 0 {
		return nil
	}
	props, names := linkProperties(lp.lt)
	owner := attrOwner{"link definition " + lp.def.ID, "link type " + lp.lt.Name, names}
	cols := p.columns[lp.from.feed.Dataset]
	seen := map[model.PropertyID]string{}
	for _, a := range lp.def.Attributes {
		pr, err := p.property(owner, props, a.Attribute)
		if err != nil {
			return err
		}
		if first, dup := seen[pr.ID]; dup {
			return bad("link definition %s: attribute %s is given twice (as %q and as %q); a link has one value of an attribute", lp.def.ID, pr.Name, first, a.Attribute)
		}
		seen[pr.ID] = a.Attribute
		ap, err := p.linkAttrPlan(lp, a, pr, cols)
		if err != nil {
			return err
		}
		lp.attrs = append(lp.attrs, ap)
	}
	return nil
}

func (p *Plan) linkAttrPlan(lp *linkPlan, a LinkAttrMap, pr model.Property, cols []string) (linkAttrPlan, error) {
	id := lp.def.ID
	switch {
	case a.Column != "" && a.Value != nil:
		return linkAttrPlan{}, bad("link definition %s: attribute %s has a column and a fixed value; write one of the two", id, a.Attribute)
	case a.Column == "" && a.Value == nil:
		return linkAttrPlan{}, bad("link definition %s: attribute %s has neither a \"column\" nor a \"value\"; write one of the two", id, a.Attribute)
	case a.Value != nil && strings.TrimSpace(*a.Value) == "":
		return linkAttrPlan{}, bad("link definition %s: attribute %s has an empty fixed value, which gives no value; write the value, or remove the attribute", id, a.Attribute)
	case a.Column != "" && !slices.Contains(cols, a.Column):
		return linkAttrPlan{}, bad("link definition %s: attribute %s reads column %q, which dataset %s (the one feed %s reads, the From end) does not have; its columns are: %s",
			id, a.Attribute, a.Column, lp.from.feed.Dataset, lp.from.feed.ID, columnList(cols))
	}
	vt, ok := p.reg.ValueType(pr.ValueType)
	if !ok || !supportedBase(vt.Base) {
		return linkAttrPlan{}, kernel.Errorf(kernel.CodeUnsupported, "link definition %s: attribute %s has a value of kind %s, which a flat file cell does not give", id, a.Attribute, vt.Base)
	}
	ap := linkAttrPlan{valueSpec: valueSpec{prop: pr, base: vt.Base}, column: a.Column}
	if a.Value == nil {
		return ap, nil
	}
	v, err := convertCell(ap.valueSpec, *a.Value)
	if err != nil {
		return linkAttrPlan{}, bad("link definition %s: the fixed value %q of attribute %s does not fit: %s", id, *a.Value, a.Attribute, plainMessage(err))
	}
	ap.fixed, ap.fixedText, ap.isFixed = v, *a.Value, true
	return ap, nil
}
