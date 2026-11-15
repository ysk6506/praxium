package httpapi

import (
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/kgread"
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
	data := kgread.ObjectTypeSummary(t)
	data["properties"] = kgread.ToAny(kgread.PropertiesOf(t.Properties))
	data["needs"] = kgread.ToAny(kgread.NeedsOf(t.Needs))
	data["identity_keys"] = kgread.ToAny(kgread.KeysOf(t.IdentityKeys))
	data["title_property"] = kgread.Opt(t.TitleProperty.String())
	data["contacts"] = kgread.Strs(t.Contacts)
	for _, name := range include {
		data[name] = c.objectTypeInclude(t, name)
	}
	c.one("object_type", t.ID.String(), requested, data)
}

func (c *call) objectTypeInclude(t model.ObjectType, name string) any {
	reg := c.s.reg
	switch name {
	case "effective_properties":
		return kgread.ToAny(kgread.PropertiesOf(reg.EffectiveProperties(t.ID)))
	case "effective_needs":
		return kgread.ToAny(kgread.NeedsOf(reg.EffectiveNeeds(t.ID)))
	case "ancestors":
		out := []kgread.TypeRef{}
		for _, id := range reg.Ancestors(t.ID) {
			if a, ok := reg.ObjectType(id); ok {
				out = append(out, kgread.TypeRef{ID: a.ID.String(), Name: a.Name})
			}
		}
		return kgread.ToAny(out)
	case "subtypes":
		out := []kgread.TypeRef{}
		for _, sub := range reg.Subtypes(t.ID) {
			out = append(out, kgread.TypeRef{ID: sub.ID.String(), Name: sub.Name})
		}
		return kgread.ToAny(out)
	}
	return kgread.ToAny(c.bindingsOfType(t))
}

// bindingsOfType lists the bindings that have the type, or a type it is a subtype of, at an end.
func (c *call) bindingsOfType(t model.ObjectType) []kgread.BoundLinkView {
	out := []kgread.BoundLinkView{}
	id := t.ID.String()
	for _, lt := range c.s.reg.LinkTypes() {
		for _, b := range lt.Bindings {
			if contains(kgread.Covered(c.s.reg, c.s.lists.typeIDs, b.From), id) || contains(kgread.Covered(c.s.reg, c.s.lists.typeIDs, b.To), id) {
				out = append(out, kgread.BoundLinkView{LinkType: lt.ID.String(), Binding: kgread.BindingOf(b)})
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
	data := kgread.LinkTypeSummary(lt)
	data["properties"] = kgread.ToAny(kgread.PropertiesOf(lt.Properties))
	data["bindings"] = kgread.ToAny(kgread.BindingsOf(lt.Bindings))
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
		view := kgread.ToMap(kgread.BindingOf(b))
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
	c.one("action_type", a.ID.String(), requested, kgread.ActionTypeView(a))
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
	data := kgread.TraitView(t)
	carriers := []kgread.TypeRef{}
	for _, ot := range c.s.reg.TypesWithTrait(t.ID) {
		carriers = append(carriers, kgread.TypeRef{ID: ot.ID.String(), Name: ot.Name})
	}
	data["carriers"] = kgread.ToAny(carriers)
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
	c.one("identifier_scheme", sc.ID.String(), requested, kgread.SchemeView(sc))
}
