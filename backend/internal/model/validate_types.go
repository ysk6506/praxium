package model

// maxScopeKeys is the most scope keys a type has, own and inherited (kg-model.md 3.1).
const maxScopeKeys = 4

// lineage returns t and its ancestors, nearest first. It stops at a missing
// parent or at a type it has already seen.
func (c *checker) lineage(t *ObjectType) []*ObjectType {
	chain := []*ObjectType{t}
	seen := map[ObjectTypeID]bool{t.ID: true}
	for cur := t; !cur.Parent.IsZero(); {
		next := c.types[cur.Parent]
		if next == nil || seen[next.ID] {
			break
		}
		seen[next.ID] = true
		chain = append(chain, next)
		cur = next
	}
	return chain
}

// inCycle reports whether the parent chain of t leads back to t.
func (c *checker) inCycle(t *ObjectType) bool {
	seen := map[ObjectTypeID]bool{}
	for cur := t; !cur.Parent.IsZero(); {
		next := c.types[cur.Parent]
		if next == nil || seen[next.ID] {
			return false
		}
		if next.ID == t.ID {
			return true
		}
		seen[next.ID] = true
		cur = next
	}
	return false
}

// effectiveProps returns the properties a type has: those of its ancestors
// first, and at each level the trait properties before the own ones. A
// property reached twice, through one trait on two levels, counts once.
func (c *checker) effectiveProps(t *ObjectType) []*Property {
	chain := c.lineage(t)
	var out []*Property
	seen := map[PropertyID]bool{}
	add := func(p *Property) {
		if !seen[p.ID] {
			seen[p.ID] = true
			out = append(out, p)
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		for _, traitID := range chain[i].Traits {
			if trait := c.traits[traitID]; trait != nil {
				for j := range trait.Properties {
					add(&trait.Properties[j])
				}
			}
		}
		for j := range chain[i].Properties {
			add(&chain[i].Properties[j])
		}
	}
	return out
}

// rank orders the namespaces a type may build on: std below org below ext.
func rank(ns Namespace) int {
	switch ns {
	case NamespaceOrg:
		return 1
	case NamespaceExt:
		return 2
	}
	return 0
}

// parentAllowed tells whether a type of namespace child may have a parent of
// namespace parent: standard types never build on org or ext types, so an
// upgrade of the standard cannot depend on what an organisation added; sys
// types are the product's own and only sys types build on them.
func parentAllowed(child, parent Namespace) bool {
	switch {
	case child == NamespaceSys:
		return true
	case parent == NamespaceSys:
		return false
	}
	return rank(parent) <= rank(child)
}

func (c *checker) checkObjectTypes() {
	var claims []nameClaim
	for i := range c.defs.ObjectTypes {
		t := &c.defs.ObjectTypes[i]
		what := describe("object type", t.ID)
		c.requireName(what, FormPascal, t.Name)
		c.checkAliases(what, FormPascal, t.Aliases)
		c.checkPlacement(what, t)
		c.checkTypeParts(what, t)
		c.checkLife(what, t.Status, t.SupersededBy.String(), t.ID.String(), lookupIn(c.types))
		claims = append(claims, claimsOf(what, t.Namespace, t.Name, t.Aliases)...)
	}
	c.checkClaims("object type name", claims)
}

// checkPlacement checks where a type sits: namespace, layer, tier and parent.
func (c *checker) checkPlacement(what string, t *ObjectType) {
	if !t.Namespace.Valid() {
		c.fail(CodeClosedList, "%s has namespace %q: want std, org, ext or sys", what, t.Namespace)
	}
	if !t.Layer.Valid() {
		c.fail(CodeClosedList, "%s has layer %q: want one of the ten layers", what, t.Layer)
	}
	if t.Tier != "" && t.Tier != TierCore && t.Tier != TierExtension {
		c.fail(CodeClosedList, "%s has tier %q: want core or extension", what, t.Tier)
	}
	if (t.Namespace == NamespaceSys) != (t.Layer == LayerSystem) && t.Namespace.Valid() && t.Layer.Valid() {
		c.fail(CodeLayerNamespace, "%s: the layer system and the namespace sys go together (namespace %s, layer %s)", what, t.Namespace, t.Layer)
	}
	c.checkParent(what, t)
}

func (c *checker) checkParent(what string, t *ObjectType) {
	if t.Parent.IsZero() {
		return
	}
	parent := c.types[t.Parent]
	switch {
	case parent == nil:
		c.fail(CodeParentUnknown, "%s has the parent %s, which does not exist", what, t.Parent)
		return
	case c.inCycle(t):
		c.fail(CodeParentCycle, "%s is its own ancestor through %s", what, t.Parent)
		return
	case t.Namespace.Valid() && parent.Namespace.Valid() && !parentAllowed(t.Namespace, parent.Namespace):
		c.fail(CodeParentNamespace, "%s (namespace %s) may not build on %s (namespace %s)", what, t.Namespace, parent.ID, parent.Namespace)
	}
	userDefined := t.Namespace == NamespaceOrg || t.Namespace == NamespaceExt
	if userDefined && t.Layer != parent.Layer {
		c.fail(CodeLayerNotInherited, "%s is in layer %s but its parent %s is in layer %s: an %s type follows its parent", what, t.Layer, parent.ID, parent.Layer, t.Namespace)
	}
}

// checkTypeParts checks traits, needs, properties and the keys built on them.
func (c *checker) checkTypeParts(what string, t *ObjectType) {
	seen := map[TraitID]bool{}
	for _, id := range t.Traits {
		if c.traits[id] == nil {
			c.fail(CodeTraitUnknown, "%s has the trait %s, which does not exist", what, id)
		}
		if seen[id] {
			c.fail(CodeDuplicateID, "%s lists the trait %s twice", what, id)
		}
		seen[id] = true
	}
	c.checkNeeds(what, t.Needs)
	for j := range t.Properties {
		c.checkProperty(&t.Properties[j], what)
	}
	eff := c.effectiveProps(t)
	c.checkPropertyNames(what, eff)
	c.checkScopeKeyCount(what, eff)
	c.checkIdentityKeys(what, t, eff)
	c.checkTitle(what, t, eff)
}

func (c *checker) checkScopeKeyCount(what string, eff []*Property) {
	count := 0
	for _, p := range eff {
		if p.ScopeKey {
			count++
		}
	}
	if count > maxScopeKeys {
		c.fail(CodeScopeKeyLimit, "%s has %d scope keys: at most %d", what, count, maxScopeKeys)
	}
}

func (c *checker) checkTitle(what string, t *ObjectType, eff []*Property) {
	if t.TitleProperty.IsZero() {
		return
	}
	for _, p := range eff {
		if p.ID == t.TitleProperty {
			return
		}
	}
	c.fail(CodeTitleProperty, "%s has the title property %s, which is not one of its properties", what, t.TitleProperty)
}
