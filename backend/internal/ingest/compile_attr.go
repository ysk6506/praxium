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
// attributes it has.
type attrOwner struct {
	who, what string
	names     []string
}

// attributeFor finds the property an attribute of a feed names, or says why none can be used: the type has no such
// property (and an organization property of that name, if there is one), or the property is deprecated (and what
// replaces it, if something does).
func (p *Plan) attributeFor(fp *feedPlan, props map[string]model.Property, name string) (model.Property, error) {
	return p.property(attrOwner{"feed " + fp.feed.ID, "type " + fp.typ.Name, p.reg.PropertyNames(fp.typ.ID)}, props, name)
}

// property is the one lookup of an attribute name that an object attribute and a link attribute share.
func (p *Plan) property(o attrOwner, props map[string]model.Property, name string) (model.Property, error) {
	pr, ok := props[name]
	if !ok {
		hint := ""
		if _, org := props["org."+name]; org {
			hint = "; the organization property of that name is org." + name + ", write that"
		}
		return pr, bad("%s: %s has no attribute %q%s; its attributes are: %s", o.who, o.what, name, hint, strings.Join(o.names, ", "))
	}
	if pr.Status == model.StatusDeprecated {
		return pr, bad("%s: attribute %s is deprecated%s", o.who, name, p.successorHint(pr))
	}
	return pr, nil
}

// successorHint says what replaces a deprecated property, when the model says.
func (p *Plan) successorHint(pr model.Property) string {
	if next, ok := p.reg.Property(pr.SupersededBy); ok && !pr.SupersededBy.IsZero() {
		return "; use " + next.Property.Name + " instead"
	}
	return " and nothing replaces it; remove it from the mapping, or ask whoever keeps the model to put it back to active"
}

// checkKeyProperty refuses an organization property in an identity key: a key says what an object is, and changing
// that for objects that are stored already would lose their history, so only the standard model's keys are used.
func (p *Plan) checkKeyProperty(fp *feedPlan, pr model.Property) error {
	if pr.Namespace == model.NamespaceOrg || pr.Namespace == model.NamespaceExt {
		return bad("feed %s: identity component %s is an organization property; an identity key is made of the standard properties of the type (which keys the type has: see its identity keys in praxium types show), because changing what identifies the objects that are stored would lose their history", fp.feed.ID, pr.Name)
	}
	return nil
}
