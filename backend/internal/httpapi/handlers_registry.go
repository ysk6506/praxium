package httpapi

import (
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

var objectTypeIncludes = []string{"effective_properties", "ancestors", "subtypes", "bindings", "effective_needs"}

func (c *call) objectTypeDetail() {
	requested := c.r.PathValue("type_id")
	reg := c.s.reg
	t, ok := reg.ObjectTypeByName(requested)
	if id, err := kernel.ParseID[model.ObjectTypeKind](requested); err == nil {
		if byID, found := reg.ObjectType(id); found {
			t, ok = byID, true
		}
	}
	if !ok {
		c.notFound("object_type", requested)
		return
	}
	include, valid := c.getQuery(objectTypeIncludes)
	if !valid {
		return
	}
	data := objectTypeSummary(t)
	data["properties"] = toAny(propertiesOf(t.Properties))
	data["needs"] = toAny(needsOf(t.Needs))
	data["identity_keys"] = toAny(keysOf(t.IdentityKeys))
	data["title_property"] = opt(t.TitleProperty.String())
	data["contacts"] = strs(t.Contacts)
	for _, name := range include {
		data[name] = c.objectTypeInclude(t, name)
	}
	c.one("object_type", t.ID.String(), requested, data)
}

func (c *call) objectTypeInclude(t model.ObjectType, name string) any {
	reg := c.s.reg
	switch name {
	case "effective_properties":
		return toAny(propertiesOf(reg.EffectiveProperties(t.ID)))
	case "effective_needs":
		return toAny(needsOf(reg.EffectiveNeeds(t.ID)))
	case "ancestors":
		out := []typeRef{}
		for _, id := range reg.Ancestors(t.ID) {
			if a, ok := reg.ObjectType(id); ok {
				out = append(out, typeRef{ID: a.ID.String(), Name: a.Name})
			}
		}
		return toAny(out)
	case "subtypes":
		out := []typeRef{}
		for _, sub := range reg.Subtypes(t.ID) {
			out = append(out, typeRef{ID: sub.ID.String(), Name: sub.Name})
		}
		return toAny(out)
	}
	return toAny(c.bindingsOfType(t))
}

// bindingsOfType lists the bindings that have the type, or a type it is a subtype of, at an end.
func (c *call) bindingsOfType(t model.ObjectType) []boundLinkView {
	out := []boundLinkView{}
	id := t.ID.String()
	for _, lt := range c.s.reg.LinkTypes() {
		for _, b := range lt.Bindings {
			if contains(covered(c.s.reg, c.s.lists.typeIDs, b.From), id) || contains(covered(c.s.reg, c.s.lists.typeIDs, b.To), id) {
				out = append(out, boundLinkView{LinkType: lt.ID.String(), Binding: bindingOf(b)})
			}
		}
	}
	return out
}

func (c *call) linkTypeDetail() {
	requested := c.r.PathValue("link_type_id")
	lt, ok := c.findLinkType(requested)
	if !ok {
		c.notFound("link_type", requested)
		return
	}
	if _, valid := c.getQuery(nil); !valid {
		return
	}
	data := linkTypeSummary(lt)
	data["properties"] = toAny(propertiesOf(lt.Properties))
	data["bindings"] = toAny(bindingsOf(lt.Bindings))
	c.one("link_type", lt.ID.String(), requested, data)
}

func (c *call) findLinkType(requested string) (model.LinkType, bool) {
	if id, err := kernel.ParseID[model.LinkTypeKind](requested); err == nil {
		if lt, ok := c.s.reg.LinkType(id); ok {
			return lt, true
		}
	}
	return c.s.reg.LinkTypeByName(requested)
}

// linkTypeBindings lists the bindings of one link type as a list resource.
func (c *call) linkTypeBindings() {
	requested := c.r.PathValue("link_type_id")
	lt, ok := c.findLinkType(requested)
	if !ok {
		c.notFound("link_type", requested)
		return
	}
	def := &listDef{}
	for _, b := range lt.Bindings {
		view := toMap(bindingOf(b))
		def.rows = append(def.rows, row{id: b.ID.String(), vals: map[string]any{}, out: view})
	}
	finish(def)
	listWith(c, def)
}

func (c *call) actionTypeDetail() {
	requested := c.r.PathValue("action_type_id")
	a, ok := c.s.reg.ActionTypeByName(requested)
	if id, err := kernel.ParseID[model.ActionTypeKind](requested); err == nil {
		if byID, found := c.s.reg.ActionType(id); found {
			a, ok = byID, true
		}
	}
	if !ok {
		c.notFound("action_type", requested)
		return
	}
	if _, valid := c.getQuery(nil); !valid {
		return
	}
	c.one("action_type", a.ID.String(), requested, actionTypeView(a))
}

func (c *call) traitDetail() {
	requested := c.r.PathValue("trait_id")
	t, ok := c.s.reg.TraitByName(requested)
	if id, err := kernel.ParseID[model.TraitKind](requested); err == nil {
		if byID, found := c.s.reg.Trait(id); found {
			t, ok = byID, true
		}
	}
	if !ok {
		c.notFound("trait", requested)
		return
	}
	if _, valid := c.getQuery(nil); !valid {
		return
	}
	data := traitView(t)
	carriers := []typeRef{}
	for _, ot := range c.s.reg.TypesWithTrait(t.ID) {
		carriers = append(carriers, typeRef{ID: ot.ID.String(), Name: ot.Name})
	}
	data["carriers"] = toAny(carriers)
	c.one("trait", t.ID.String(), requested, data)
}

func (c *call) schemeDetail() {
	requested := c.r.PathValue("scheme_id")
	sc, ok := c.s.reg.IdentifierSchemeByName(requested)
	if id, err := kernel.ParseID[model.IdentifierSchemeKind](requested); err == nil {
		if byID, found := c.s.reg.IdentifierScheme(id); found {
			sc, ok = byID, true
		}
	}
	if !ok {
		c.notFound("identifier_scheme", requested)
		return
	}
	if _, valid := c.getQuery(nil); !valid {
		return
	}
	c.one("identifier_scheme", sc.ID.String(), requested, schemeView(sc))
}
