package model

import (
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func (c *checker) checkLinkTypes() {
	var claims []nameClaim
	bindingIDs := map[BindingID]bool{}
	for i := range c.defs.LinkTypes {
		l := &c.defs.LinkTypes[i]
		what := describe("link type", l.ID)
		c.requireName(what, FormUpperSnake, l.Name)
		c.checkAliases(what, FormUpperSnake, l.Aliases)
		if !l.Namespace.Valid() {
			c.fail(CodeClosedList, "%s has namespace %q: want std, org, ext or sys", what, l.Namespace)
		}
		if l.DelayClass != "" && l.DelayClass != DelayImmediate && l.DelayClass != DelayDelayed {
			c.fail(CodeClosedList, "%s has delay_class %q: want immediate or delayed", what, l.DelayClass)
		}
		c.checkLinkMeaning(what, l)
		c.checkLinkProperties(what, l)
		c.checkBindings(what, l, bindingIDs)
		c.checkLife(what, l.Status, l.SupersededBy.String(), l.ID.String(), lookupIn(c.links))
		claims = append(claims, claimsOf(what, l.Namespace, l.Name, l.Aliases)...)
	}
	c.checkClaims("link type name", claims)
}

// checkLinkMeaning checks the meaning and the parameters a link type fixes.
// Only enum parameters are fixed at the link type; supplied_by always fixes its resource.
func (c *checker) checkLinkMeaning(what string, l *LinkType) {
	spec, ok := SpecOf(l.Meaning)
	if !ok {
		c.fail(CodeClosedList, "%s has the meaning %q: want one of the eleven meanings", what, l.Meaning)
		return
	}
	names := make([]string, 0, len(l.MeaningParams))
	for name := range l.MeaningParams {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		param, found := spec.Param(name)
		switch {
		case !found || param.Kind != ParamEnum:
			c.fail(CodeLinkMeaning, "%s: the meaning %s has no enum parameter %q to fix", what, l.Meaning, name)
		case !oneOf(l.MeaningParams[name], param.Values):
			c.fail(CodeLinkMeaning, "%s fixes %s to %q: want one of %v", what, name, l.MeaningParams[name], param.Values)
		}
	}
	if spec.FromParam != "" && l.MeaningParams[spec.FromParam] == "" {
		c.fail(CodeLinkMeaning, "%s: the meaning %s must fix %s", what, l.Meaning, spec.FromParam)
	}
}

// checkLinkProperties checks the properties of a link. A link is never volatile:
// even a link that moves often is an ordinary change (ADR-0007 2).
func (c *checker) checkLinkProperties(what string, l *LinkType) {
	var own []*Property
	for j := range l.Properties {
		p := &l.Properties[j]
		c.checkProperty(p, what)
		if p.EffectiveHistory() == HistoryVolatile {
			c.fail(CodeLinkVolatile, "%s has the property %s, which is volatile: links are always versioned", what, p.ID)
		}
		own = append(own, p)
	}
	c.checkPropertyNames(what, own)
}

type bindingKey struct{ from, to Endpoint }

func (c *checker) checkBindings(what string, l *LinkType, bindingIDs map[BindingID]bool) {
	seen := map[bindingKey]bool{}
	for j := range l.Bindings {
		b := &l.Bindings[j]
		bwhat := describe("binding", b.ID) + " of " + what
		switch {
		case b.ID.IsZero():
			c.fail(CodeRequiredField, "a binding of %s has no id", what)
		case bindingIDs[b.ID]:
			c.fail(CodeDuplicateID, "binding id %s is used twice", b.ID)
		}
		bindingIDs[b.ID] = true
		if !oneOf(b.Cardinality, []Cardinality{CardOneToOne, CardOneToMany, CardManyToOne, CardManyToMany}) {
			c.fail(CodeClosedList, "%s has cardinality %q: want 1:1, 1:N, N:1 or N:M", bwhat, b.Cardinality)
		}
		c.checkEndpoint(bwhat, b.From, CodeBindingEndpoint)
		c.checkEndpoint(bwhat, b.To, CodeBindingEndpoint)
		if seen[bindingKey{b.From, b.To}] {
			c.fail(CodeBindingDuplicate, "%s joins the same ends as an earlier binding", bwhat)
		}
		seen[bindingKey{b.From, b.To}] = true
		c.checkConstraints(bwhat, l, b)
	}
}

// checkEndpoint checks that the type or trait an end names exists. The code is
// the one the caller wants: a binding end and an action target fail differently.
func (c *checker) checkEndpoint(what string, e Endpoint, code kernel.Code) {
	switch e.Kind {
	case EndpointAny:
		if e.Ref != "" {
			c.fail(code, "%s: an end for any object names nothing, not %q", what, e.Ref)
		}
	case EndpointType:
		if !lookupIn(c.types)(e.Ref) {
			c.fail(code, "%s: the end names the object type %q, which does not exist", what, e.Ref)
		}
	case EndpointTrait:
		if !lookupIn(c.traits)(e.Ref) {
			c.fail(code, "%s: the end names the trait %q, which does not exist", what, e.Ref)
		}
	default:
		c.fail(CodeClosedList, "%s: an end is a type, a trait or any, not %q", what, e.Kind)
	}
}

// checkConstraints checks the parameter constraints of a binding: they name
// enum or type-reference parameters of the meaning and narrow what the link
// type allows, never widen it.
func (c *checker) checkConstraints(what string, l *LinkType, b *Binding) {
	spec, ok := SpecOf(l.Meaning)
	if !ok {
		return
	}
	names := make([]string, 0, len(b.ParamConstraints))
	for name := range b.ParamConstraints {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		values := b.ParamConstraints[name]
		param, found := spec.Param(name)
		if !found || !param.narrowable() || len(values) == 0 {
			c.fail(CodeBindingConstraint, "%s constrains %q, which is not a parameter of %s that a binding can narrow (or lists no value)", what, name, l.Meaning)
			continue
		}
		c.checkConstraintValues(what, l, param, values)
	}
}

func (c *checker) checkConstraintValues(what string, l *LinkType, param ParamSpec, values []string) {
	fixed := l.MeaningParams[param.Name]
	for _, value := range values {
		switch {
		case param.Kind == ParamTypeRef && !lookupIn(c.types)(value):
			c.fail(CodeBindingConstraint, "%s: %s names the object type %q, which does not exist", what, param.Name, value)
		case param.Kind == ParamEnum && !oneOf(value, param.Values):
			c.fail(CodeBindingConstraint, "%s: %s allows %q, which is not one of %v", what, param.Name, value, param.Values)
		case fixed != "" && value != fixed:
			c.fail(CodeBindingConstraint, "%s: %s allows %q but the link type fixes it to %q", what, param.Name, value, fixed)
		}
	}
}

func (c *checker) checkActionTypes() {
	var claims []nameClaim
	for i := range c.defs.ActionTypes {
		a := &c.defs.ActionTypes[i]
		what := describe("action type", a.ID)
		c.requireName(what, FormSnake, a.Name)
		c.checkAliases(what, FormSnake, a.Aliases)
		if !a.Namespace.Valid() {
			c.fail(CodeClosedList, "%s has namespace %q: want std, org, ext or sys", what, a.Namespace)
		}
		if len(a.AppliesTo) == 0 {
			c.fail(CodeRequiredField, "%s names no type or trait it applies to", what)
		}
		for _, target := range a.AppliesTo {
			c.checkEndpoint(what, target, CodeActionRef)
		}
		c.checkRisk(what, a)
		c.checkLife(what, a.Status, a.SupersededBy.String(), a.ID.String(), lookupIn(c.actions))
		claims = append(claims, claimsOf(what, a.Namespace, a.Name, a.Aliases)...)
	}
	c.checkClaims("action type name", claims)
}

// checkRisk checks the four risk axes against each other: an action that
// mutates says whether it can be undone, one that does not has no
// reversibility, and an action with an inverse is reversible.
func (c *checker) checkRisk(what string, a *ActionType) {
	if !oneOf(a.Disruption, []Disruption{DisruptionNone, DisruptionServiceAffecting, DisruptionDataLoss}) {
		c.fail(CodeClosedList, "%s has disruption %q: want none, service_affecting or data_loss", what, a.Disruption)
	}
	switch {
	case a.Mutates && a.Reversibility != Reversible && a.Reversibility != Irreversible:
		c.fail(CodeActionRisk, "%s mutates and must say reversible or irreversible, not %q", what, a.Reversibility)
	case !a.Mutates && a.Reversibility != "":
		c.fail(CodeActionRisk, "%s does not mutate, so it has no reversibility", what)
	}
	if a.Inverse.IsZero() {
		return
	}
	if c.actions[a.Inverse] == nil || a.Inverse == a.ID {
		c.fail(CodeActionRef, "%s names the inverse %s, which is itself or does not exist", what, a.Inverse)
	}
	if a.Reversibility != Reversible {
		c.fail(CodeActionRisk, "%s has an inverse and must be reversible", what)
	}
}
