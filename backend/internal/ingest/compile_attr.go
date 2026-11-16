package ingest

import (
	"strings"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// propertiesByName indexes the properties of a type by name, own, inherited and from traits. The old name of a renamed
// property is an alias and finds it too, so a mapping written before the rename keeps working; a name that is a
// property's own name is never taken over by an alias.
func propertiesByName(reg *model.Registry, typ model.ObjectType) map[string]model.Property {
	return indexProperties(reg.EffectiveProperties(typ.ID))
}

// indexProperties is the index by name and alias of a list of properties; the object types and the link types use it
// alike (ADR-0066).
func indexProperties(effective []model.Property) map[string]model.Property {
	props := map[string]model.Property{}
	for _, pr := range effective {
		props[pr.Name] = pr
	}
	for _, pr := range effective {
		for _, alias := range pr.Aliases {
			if _, taken := props[alias]; !taken {
				props[alias] = pr
			}
		}
	}
	return props
}

// attrOwner says whose attribute is looked for, for the message that refuses a name: who is the part of the mapping
// ("feed f_server"), what is the type that lacks it ("type PhysicalServer", "link type MOUNTED_IN") and names are the
// attributes it has. The rest is for the error: the key and id of the owner in the params, the key and name of the type,
// and the codes of the two ways a name is refused.
type attrOwner struct {
	who, what string
	names     []string

	key, id, typeKey, typeName string
	unknown, deprecated        *errCode
}

// attributeFor finds the property an attribute of a feed names, or says why none can be used: the type has no such
// property (and an organization property of that name, if there is one), or the property is deprecated (and what
// replaces it, if something does).
func (p *Plan) attributeFor(fp *feedPlan, props map[string]model.Property, name, at string) (model.Property, bool) {
	o := attrOwner{who: "feed " + fp.feed.ID, what: "type " + fp.typ.Name, names: p.reg.PropertyNames(fp.typ.ID),
		key: "feed", id: fp.feed.ID, typeKey: "type", typeName: fp.typ.Name, unknown: ecMapAttrUnknown, deprecated: ecMapAttrDeprecated}
	return p.property(o, props, name, at)
}

// property is the one lookup of an attribute name that an object attribute and a link attribute share.
func (p *Plan) property(o attrOwner, props map[string]model.Property, name, at string) (model.Property, bool) {
	pr, ok := props[name]
	if !ok {
		hint := ""
		params := Params{o.key: o.id, o.typeKey: o.typeName, "attribute": name, "attributes": nonNil(o.names)}
		if _, org := props["org."+name]; org {
			hint = "; the organization property of that name is org." + name + ", write that"
			params["org_property"] = "org." + name
		}
		p.errs.add(o.unknown, at, params, "%s: %s has no attribute %q%s; its attributes are: %s", o.who, o.what, name, hint, strings.Join(o.names, ", "))
		return pr, false
	}
	if pr.Status == model.StatusDeprecated {
		next, _ := p.successor(pr)
		p.errs.add(o.deprecated, at, Params{o.key: o.id, "attribute": name, "replacement": next}, "%s: attribute %s is deprecated%s", o.who, name, p.successorHint(pr))
		return pr, false
	}
	return pr, true
}

// successor is the name of the property that replaces a deprecated one, when the model says.
func (p *Plan) successor(pr model.Property) (string, bool) {
	if next, ok := p.reg.Property(pr.SupersededBy); ok && !pr.SupersededBy.IsZero() {
		return next.Property.Name, true
	}
	return "", false
}

// successorHint says what replaces a deprecated property, when the model says.
func (p *Plan) successorHint(pr model.Property) string {
	if next, ok := p.successor(pr); ok {
		return "; use " + next + " instead"
	}
	return " and nothing replaces it; remove it from the mapping, or ask whoever keeps the model to put it back to active"
}
