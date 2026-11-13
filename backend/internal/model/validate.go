package model

import (
	"fmt"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Check applies every rule the design documents state to a set of
// definitions and returns the violations in a fixed order (the order of the
// definitions). An empty result means the set is sound. Numbers left at zero
// are fine here: NewRegistry assigns them.
func Check(defs Definitions) []*kernel.Error {
	c := newChecker(&defs)
	c.checkHeader()
	c.checkValueTypes()
	c.checkTraits()
	c.checkSchemes()
	c.checkObjectTypes()
	c.checkLinkTypes()
	c.checkActionTypes()
	c.checkNumbers()
	return c.errs
}

// Validate returns the first violation of Check, naming how many more there are.
func Validate(defs Definitions) error {
	errs := Check(defs)
	if len(errs) == 0 {
		return nil
	}
	first := errs[0]
	if len(errs) == 1 {
		return first
	}
	return kernel.Errorf(first.Code, "%s (and %d more violations)", first.Message, len(errs)-1)
}

// checker holds the definitions, indexed by id, and the violations found.
type checker struct {
	defs    *Definitions
	errs    []*kernel.Error
	vtypes  map[ValueTypeID]ValueType
	traits  map[TraitID]*Trait
	types   map[ObjectTypeID]*ObjectType
	links   map[LinkTypeID]*LinkType
	actions map[ActionTypeID]*ActionType
	schemes map[IdentifierSchemeID]*IdentifierScheme
	props   map[PropertyID]*Property
}

func (c *checker) fail(code kernel.Code, format string, args ...any) {
	c.errs = append(c.errs, kernel.Errorf(code, format, args...))
}

func newChecker(defs *Definitions) *checker {
	c := &checker{defs: defs, vtypes: map[ValueTypeID]ValueType{}, props: map[PropertyID]*Property{}}
	for _, vt := range builtinValueTypes() {
		c.vtypes[vt.ID] = vt
	}
	for i := range defs.ValueTypes {
		c.addValueType(defs.ValueTypes[i])
	}
	c.traits = indexByID(c, "trait", defs.Traits, func(t *Trait) TraitID { return t.ID })
	c.types = indexByID(c, "object type", defs.ObjectTypes, func(t *ObjectType) ObjectTypeID { return t.ID })
	c.links = indexByID(c, "link type", defs.LinkTypes, func(l *LinkType) LinkTypeID { return l.ID })
	c.actions = indexByID(c, "action type", defs.ActionTypes, func(a *ActionType) ActionTypeID { return a.ID })
	c.schemes = indexByID(c, "identifier scheme", defs.Schemes, func(s *IdentifierScheme) IdentifierSchemeID { return s.ID })
	c.indexProperties()
	return c
}

func (c *checker) addValueType(vt ValueType) {
	if vt.ID.IsZero() {
		c.fail(CodeRequiredField, "a value type has no id")
		return
	}
	if _, dup := c.vtypes[vt.ID]; dup {
		c.fail(CodeDuplicateID, "value type id %s is used twice", vt.ID)
		return
	}
	c.vtypes[vt.ID] = vt
}

// indexByID indexes definitions by id and reports an empty or repeated id.
func indexByID[T any, K interface {
	comparable
	IsZero() bool
	String() string
}](c *checker, kind string, items []T, id func(*T) K) map[K]*T {
	out := make(map[K]*T, len(items))
	for i := range items {
		key := id(&items[i])
		switch {
		case key.IsZero():
			c.fail(CodeRequiredField, "a %s has no id", kind)
		case out[key] != nil:
			c.fail(CodeDuplicateID, "%s id %s is used twice", kind, key)
		default:
			out[key] = &items[i]
		}
	}
	return out
}

// forEachProperty visits every property with a text that says where it is.
func (c *checker) forEachProperty(visit func(p *Property, owner string)) {
	for i := range c.defs.Traits {
		t := &c.defs.Traits[i]
		for j := range t.Properties {
			visit(&t.Properties[j], "trait "+t.ID.String())
		}
	}
	for i := range c.defs.ObjectTypes {
		t := &c.defs.ObjectTypes[i]
		for j := range t.Properties {
			visit(&t.Properties[j], "object type "+t.ID.String())
		}
	}
	for i := range c.defs.LinkTypes {
		l := &c.defs.LinkTypes[i]
		for j := range l.Properties {
			visit(&l.Properties[j], "link type "+l.ID.String())
		}
	}
}

func (c *checker) indexProperties() {
	c.forEachProperty(func(p *Property, owner string) {
		switch {
		case p.ID.IsZero():
			c.fail(CodeRequiredField, "a property of %s has no id", owner)
		case c.props[p.ID] != nil:
			c.fail(CodeDuplicateID, "property id %s is used twice (again in %s)", p.ID, owner)
		default:
			c.props[p.ID] = p
		}
	})
}

func (c *checker) checkHeader() {
	if c.defs.ModelVersion < 1 {
		c.fail(CodeRequiredField, "model_version must be 1 or more, not %d", c.defs.ModelVersion)
	}
}

// statusOf reads an empty status as active.
func statusOf(s Status) Status {
	if s == "" {
		return StatusActive
	}
	return s
}

func validStatus(s Status) bool {
	return oneOf(s, []Status{StatusExperimental, StatusActive, StatusDeprecated})
}

// checkLife checks status and superseded_by together: a deprecated definition
// points at its successor and nothing else does. exists tells whether the id is a definition of the kind.
func (c *checker) checkLife(what string, status Status, successor string, self string, exists func(string) bool) {
	if !validStatus(statusOf(status)) {
		c.fail(CodeClosedList, "%s has status %q: want experimental, active or deprecated", what, status)
		return
	}
	switch {
	case statusOf(status) != StatusDeprecated && successor != "":
		c.fail(CodeSuperseded, "%s is not deprecated but names a successor %s", what, successor)
	case statusOf(status) == StatusDeprecated && successor == "":
		c.fail(CodeSuperseded, "%s is deprecated and must name its successor", what)
	case successor != "" && (successor == self || !exists(successor)):
		c.fail(CodeSuperseded, "%s names %s as successor, which is itself or does not exist", what, successor)
	}
}

// requireName checks the form of a name; an empty name is a missing field.
func (c *checker) requireName(what string, form NameForm, name string) {
	switch {
	case name == "":
		c.fail(CodeRequiredField, "%s has no name", what)
	case !ValidName(form, name):
		c.fail(CodeNameInvalid, "%s has the name %q, which does not have the form %s", what, name, formText(form))
	}
}

func formText(form NameForm) string {
	switch form {
	case FormPascal:
		return "PascalCase"
	case FormUpperSnake:
		return "UPPER_SNAKE_CASE"
	}
	return "snake_case"
}

// nameClaim is a name or an alias that one definition holds.
type nameClaim struct {
	what string
	text string
	ns   Namespace
}

// checkClaims reports names and aliases that two definitions of one kind
// share. A clash with a name of std or sys, from another namespace, is a
// reserved name; any other clash is a duplicate.
func (c *checker) checkClaims(kind string, claims []nameClaim) {
	seen := map[string]nameClaim{}
	for _, claim := range claims {
		first, dup := seen[claim.text]
		switch {
		case !dup:
			seen[claim.text] = claim
		case first.ns != claim.ns && (isReservedSpace(first.ns) || isReservedSpace(claim.ns)):
			c.fail(CodeNameReserved, "%s %q of %s is owned by %s", kind, claim.text, claim.what, first.what)
		default:
			c.fail(CodeNameDuplicate, "%s %q is held by %s and by %s", kind, claim.text, first.what, claim.what)
		}
	}
}

func isReservedSpace(ns Namespace) bool { return ns == NamespaceStd || ns == NamespaceSys }

// claimsOf makes the claims of one definition: its name and its aliases.
func claimsOf(what string, ns Namespace, name string, aliases []string) []nameClaim {
	out := []nameClaim{{what: what, text: name, ns: ns}}
	for _, alias := range aliases {
		out = append(out, nameClaim{what: what, text: alias, ns: ns})
	}
	return out
}

func describe(kind string, id fmt.Stringer) string { return kind + " " + id.String() }
