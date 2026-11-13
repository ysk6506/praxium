package model

import (
	"reflect"
	"strings"
)

// checkSchemes checks the identifier schemes: a name, a namespace and a
// normaliser that is a value type.
func (c *checker) checkSchemes() {
	var claims []nameClaim
	for i := range c.defs.Schemes {
		s := &c.defs.Schemes[i]
		what := describe("identifier scheme", s.ID)
		c.requireName(what, FormSnake, s.Name)
		c.checkAliases(what, FormSnake, s.Aliases)
		if !s.Namespace.Valid() {
			c.fail(CodeClosedList, "%s has namespace %q: want std, org, ext or sys", what, s.Namespace)
		}
		if _, ok := c.vtypes[s.Normalizer]; !ok {
			c.fail(CodeSchemeUnknown, "%s has the normaliser %q, which is not a value type", what, s.Normalizer)
		}
		claims = append(claims, claimsOf(what, s.Namespace, s.Name, s.Aliases)...)
	}
	c.checkClaims("identifier scheme name", claims)
}

// checkIdentityKeys checks the identity keys in priority order. A key has a
// component or a scheme. A property component names a property the type has,
// once, neither confidential (a confidential value is hashed with a key and
// cannot identify anything) nor volatile (a value that changes all the time
// cannot recognise anything). A context component names a link type that
// leads to the object this one is identified inside. No two keys are the
// same. An org or ext type that builds on a parent keeps its identity keys.
func (c *checker) checkIdentityKeys(what string, t *ObjectType, eff []*Property) {
	byID := map[PropertyID]*Property{}
	for _, p := range eff {
		byID[p.ID] = p
	}
	seen := map[string]bool{}
	for _, key := range t.IdentityKeys {
		c.checkKey(what, t, key, byID)
		sig := keySignature(key)
		if seen[sig] {
			c.fail(CodeIdentityKey, "%s has two identity keys that are the same", what)
		}
		seen[sig] = true
	}
	c.checkInheritedKeys(what, t)
}

func keySignature(key IdentityKey) string {
	parts := make([]string, len(key.Components))
	for i, comp := range key.Components {
		parts[i] = comp.Property.String() + "@" + comp.Context.String()
	}
	return strings.Join(parts, "+") + "#" + key.Scheme.String()
}

func (c *checker) checkKey(what string, t *ObjectType, key IdentityKey, byID map[PropertyID]*Property) {
	if key.Strength != IdentityStrong && key.Strength != IdentityWeak {
		c.fail(CodeClosedList, "%s has an identity key with strength %q: want strong or weak", what, key.Strength)
	}
	if !key.Scheme.IsZero() && c.schemes[key.Scheme] == nil {
		c.fail(CodeSchemeUnknown, "%s has an identity key with the scheme %s, which does not exist", what, key.Scheme)
	}
	properties, seen := 0, map[KeyComponent]bool{}
	for _, comp := range key.Components {
		if seen[comp] {
			c.fail(CodeIdentityKey, "%s has an identity key that uses a component twice", what)
		}
		seen[comp] = true
		switch {
		case comp.Property.IsZero() == comp.Context.IsZero():
			c.fail(CodeIdentityKey, "%s has an identity key component that is not exactly one of a property and a context", what)
		case !comp.Property.IsZero():
			properties++
			c.checkKeyProperty(what, comp.Property, byID)
		default:
			c.checkKeyContext(what, t, comp.Context)
		}
	}
	if properties == 0 && key.Scheme.IsZero() {
		c.fail(CodeIdentityKey, "%s has an identity key with no property and no scheme", what)
	}
}

func (c *checker) checkKeyProperty(what string, id PropertyID, byID map[PropertyID]*Property) {
	p := byID[id]
	switch {
	case p == nil:
		c.fail(CodeIdentityKey, "%s has an identity key on %s, which is not one of its properties", what, id)
	case p.EffectiveSensitivity() == SensitivityConfidential:
		c.fail(CodeIdentityKey, "%s has an identity key on %s, which is confidential", what, id)
	case p.EffectiveHistory() == HistoryVolatile:
		c.fail(CodeIdentityKeyVolatile, "%s has an identity key on %s, which is volatile", what, id)
	}
}

// checkKeyContext needs a link type with a binding that starts at the type
// (or at a type it builds on, or a trait it has) and lets one object have at
// most one object at the other end: that object is the context.
func (c *checker) checkKeyContext(what string, t *ObjectType, id LinkTypeID) {
	l := c.links[id]
	if l == nil {
		c.fail(CodeIdentityContext, "%s has an identity key in the context of %s, which is not a link type", what, id)
		return
	}
	for _, b := range l.Bindings {
		if (b.Cardinality == CardManyToOne || b.Cardinality == CardOneToOne) && c.covers(b.From, t) {
			return
		}
	}
	c.fail(CodeIdentityContext, "%s has an identity key in the context of %s, which has no N:1 or 1:1 binding from it", what, id)
}

// covers reports whether an end of a binding covers objects of the type.
func (c *checker) covers(e Endpoint, t *ObjectType) bool {
	if e.Kind == EndpointAny {
		return true
	}
	for _, cur := range c.lineage(t) {
		if e.Kind == EndpointType && e.Ref == cur.ID.String() {
			return true
		}
		if e.Kind == EndpointTrait {
			for _, tr := range cur.Traits {
				if e.Ref == tr.String() {
					return true
				}
			}
		}
	}
	return false
}

func (c *checker) checkInheritedKeys(what string, t *ObjectType) {
	userDefined := t.Namespace == NamespaceOrg || t.Namespace == NamespaceExt
	parent := c.types[t.Parent]
	if !userDefined || parent == nil || len(t.IdentityKeys) == 0 {
		return
	}
	if !reflect.DeepEqual(t.IdentityKeys, c.effectiveKeys(parent)) {
		c.fail(CodeIdentityKeyChanged, "%s changes the identity keys of its parent %s", what, parent.ID)
	}
}

// effectiveKeys returns the identity keys a type has: its own, else the nearest ancestor's.
func (c *checker) effectiveKeys(t *ObjectType) []IdentityKey {
	for _, cur := range c.lineage(t) {
		if len(cur.IdentityKeys) > 0 {
			return cur.IdentityKeys
		}
	}
	return nil
}
