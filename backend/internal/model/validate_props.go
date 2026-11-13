package model

import "github.com/ysk6506/praxium/backend/internal/kernel"

// lookupIn makes the "does this id exist" test of checkLife for one kind.
func lookupIn[K any, V any](index map[kernel.ID[K]]V) func(string) bool {
	return func(raw string) bool {
		id, err := kernel.ParseID[K](raw)
		if err != nil {
			return false
		}
		_, found := index[id]
		return found
	}
}

func (c *checker) baseOf(id ValueTypeID) (BaseKind, bool) {
	vt, ok := c.vtypes[id]
	return vt.Base, ok
}

func (c *checker) checkValueTypes() {
	var claims []nameClaim
	for _, vt := range builtinValueTypes() {
		claims = append(claims, nameClaim{what: "the built-in " + vt.Name, text: vt.Name, ns: NamespaceStd})
	}
	for _, vt := range c.defs.ValueTypes {
		what := describe("value type", vt.ID)
		c.requireName(what, FormSnake, vt.Name)
		if !vt.Base.Valid() {
			c.fail(CodeClosedList, "%s has base %q: want one of the ten base kinds", what, vt.Base)
		}
		if _, ok := valueRules[vt.ID.String()]; !ok {
			c.fail(CodeValueTypeUnknown, "%s has no implementation: value types are the product's and cannot be added by data", what)
		}
		claims = append(claims, nameClaim{what: what, text: vt.Name, ns: NamespaceStd})
	}
	c.checkClaims("value type name", claims)
}

func (c *checker) checkAliases(what string, form NameForm, aliases []string) {
	for _, alias := range aliases {
		if !ValidName(form, alias) {
			c.fail(CodeNameInvalid, "%s has the alias %q, which does not have the form %s", what, alias, formText(form))
		}
	}
}

// checkProperty checks one property wherever it sits: a type, a trait or a link type.
func (c *checker) checkProperty(p *Property, owner string) {
	what := "property " + p.ID.String() + " of " + owner
	c.requireName(what, FormSnake, p.Name)
	c.checkAliases(what, FormSnake, p.Aliases)
	base, known := c.baseOf(p.ValueType)
	if !known {
		c.fail(CodeValueTypeUnknown, "%s uses the value type %q, which does not exist", what, p.ValueType)
	} else {
		c.checkEnumAndUnit(what, p, base)
		c.checkScopeKey(what, p, base)
	}
	c.checkClasses(what, p)
	c.checkLife(what, p.Status, p.SupersededBy.String(), p.ID.String(), lookupIn(c.props))
}

func (c *checker) checkEnumAndUnit(what string, p *Property, base BaseKind) {
	if (base == BaseEnum) != (len(p.EnumValues) > 0) {
		c.fail(CodeEnumValues, "%s: enum values are needed for an enum and only for an enum", what)
	}
	seen := map[string]bool{}
	for _, value := range p.EnumValues {
		if !ValidName(FormSnake, value) {
			c.fail(CodeNameInvalid, "%s has the enum value %q, which is not snake_case", what, value)
		}
		if seen[value] {
			c.fail(CodeEnumValues, "%s has the enum value %q twice", what, value)
		}
		seen[value] = true
	}
	if (base == BaseQuantity) != (p.Unit != "") {
		c.fail(CodeUnit, "%s: a unit is needed for a quantity and only for a quantity", what)
	}
}

func (c *checker) checkScopeKey(what string, p *Property, base BaseKind) {
	if !p.ScopeKey {
		return
	}
	if base != BaseString && base != BaseRef {
		c.fail(CodeScopeKeyType, "%s is a scope key and must be a short text or a reference, not %s", what, base)
	}
	if p.EffectiveHistory() != HistoryVersioned {
		c.fail(CodeScopeKeyVolatile, "%s is a scope key and must be versioned", what)
	}
}

func (c *checker) checkClasses(what string, p *Property) {
	if !oneOf(p.ChangeClass, []ChangeClass{ChangeStatic, ChangeSlow, ChangeDynamic}) {
		c.fail(CodeClosedList, "%s has change_class %q: want static, slow or dynamic", what, p.ChangeClass)
	}
	if p.Sensitivity != "" && !oneOf(p.Sensitivity, []Sensitivity{SensitivityOpen, SensitivityRestricted, SensitivityConfidential}) {
		c.fail(CodeClosedList, "%s has sensitivity %q: want open, restricted or confidential", what, p.Sensitivity)
	}
	if p.HistoryKind != "" && !oneOf(p.HistoryKind, []HistoryKind{HistoryVersioned, HistoryVolatile}) {
		c.fail(CodeClosedList, "%s has history_kind %q: want versioned or volatile", what, p.HistoryKind)
	}
}

// checkPropertyNames reports a property name or alias that two properties of
// one owner share, whether they are its own, inherited or given by a trait.
func (c *checker) checkPropertyNames(owner string, props []*Property) {
	seen := map[string]PropertyID{}
	for _, p := range props {
		for _, text := range append([]string{p.Name}, p.Aliases...) {
			if first, dup := seen[text]; dup && first != p.ID {
				c.fail(CodePropertyDuplicate, "%s has two properties named %q (%s and %s)", owner, text, first, p.ID)
			}
			seen[text] = p.ID
		}
	}
}

// checkNeeds checks the channels a trait or a type needs. A need has a
// strength; only the management channel may leave it out, because losing it
// leaves the service alive.
func (c *checker) checkNeeds(what string, needs []Need) {
	seen := map[Channel]bool{}
	for _, need := range needs {
		switch {
		case !need.Channel.Valid():
			c.fail(CodeClosedList, "%s needs the channel %q: want one of the six channels", what, need.Channel)
		case seen[need.Channel]:
			c.fail(CodeNeed, "%s declares a need for %s twice", what, need.Channel)
		case need.Strength == "" && need.Channel != ChannelManagement:
			c.fail(CodeNeed, "%s: the need for %s has no strength (hard or soft)", what, need.Channel)
		case need.Strength != "" && need.Strength != StrengthHard && need.Strength != StrengthSoft:
			c.fail(CodeClosedList, "%s: the need for %s has strength %q: want hard or soft", what, need.Channel, need.Strength)
		}
		seen[need.Channel] = true
	}
}

func (c *checker) checkTraits() {
	var claims []nameClaim
	for i := range c.defs.Traits {
		t := &c.defs.Traits[i]
		what := describe("trait", t.ID)
		c.requireName(what, FormPascal, t.Name)
		c.checkAliases(what, FormPascal, t.Aliases)
		c.checkNeeds(what, t.Needs)
		var own []*Property
		for j := range t.Properties {
			c.checkProperty(&t.Properties[j], what)
			own = append(own, &t.Properties[j])
		}
		c.checkPropertyNames(what, own)
		claims = append(claims, claimsOf(what, NamespaceStd, t.Name, t.Aliases)...)
	}
	c.checkClaims("trait name", claims)
}
