package model

import (
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The error codes of this unit. A code is never renamed or reused. The kernel
// keeps the seed catalogue; each unit lists its own codes (kernel.Code) and the
// contract unit will publish them together. Codes() is this unit's list and a
// test locks it.
const (
	// CodeDataInvalid is a definition file that is not valid JSON of the expected shape.
	CodeDataInvalid kernel.Code = "model_data_invalid"
	// CodeNotStandard is a definition in the standard files that is not in the std namespace.
	CodeNotStandard kernel.Code = "model_not_standard"
	// CodeRequiredField is a definition without a field it must have.
	CodeRequiredField kernel.Code = "model_required_field"
	// CodeDuplicateID is an id used by two definitions.
	CodeDuplicateID kernel.Code = "model_duplicate_id"
	// CodeNameInvalid is a name that breaks the naming rule of its kind.
	CodeNameInvalid kernel.Code = "model_name_invalid"
	// CodeNameDuplicate is a name or alias that two definitions of one kind share.
	CodeNameDuplicate kernel.Code = "model_name_duplicate"
	// CodeNameReserved is a name outside std that a standard definition already owns.
	CodeNameReserved kernel.Code = "model_name_reserved"
	// CodeClosedList is a value outside a closed list of the model.
	CodeClosedList kernel.Code = "model_not_in_closed_list"
	// CodeLayerNamespace is a sys type outside the system layer or the reverse.
	CodeLayerNamespace kernel.Code = "model_layer_namespace"
	// CodeLayerNotInherited is an org or ext type in another layer than its parent.
	CodeLayerNotInherited kernel.Code = "model_layer_not_inherited"
	// CodeParentUnknown is a parent that does not exist.
	CodeParentUnknown kernel.Code = "model_parent_unknown"
	// CodeParentCycle is a parent chain that returns to itself.
	CodeParentCycle kernel.Code = "model_parent_cycle"
	// CodeParentNamespace is a parent in a namespace the child may not build on.
	CodeParentNamespace kernel.Code = "model_parent_namespace"
	// CodeTraitUnknown is a trait that does not exist.
	CodeTraitUnknown kernel.Code = "model_trait_unknown"
	// CodePropertyDuplicate is a property name used twice in one type, own, inherited or from a trait.
	CodePropertyDuplicate kernel.Code = "model_property_duplicate"
	// CodeValueTypeUnknown is a value type that does not exist.
	CodeValueTypeUnknown kernel.Code = "model_value_type_unknown"
	// CodeEnumValues is an enum without values or values on a property that is not an enum.
	CodeEnumValues kernel.Code = "model_enum_values"
	// CodeUnit is a quantity without a unit or a unit on a property that is not a quantity.
	CodeUnit kernel.Code = "model_unit"
	// CodeScopeKeyType is a scope key whose value is not a short text or a reference.
	CodeScopeKeyType kernel.Code = "model_scope_key_type"
	// CodeScopeKeyVolatile is a scope key that is not versioned.
	CodeScopeKeyVolatile kernel.Code = "model_scope_key_volatile"
	// CodeScopeKeyLimit is a type with more than four scope keys.
	CodeScopeKeyLimit kernel.Code = "model_scope_key_limit"
	// CodeIdentityKey is an identity key that breaks a rule: unknown or repeated property, or a confidential one.
	CodeIdentityKey kernel.Code = "model_identity_key"
	// CodeIdentityKeyVolatile is an identity key with a property that changes all the time.
	CodeIdentityKeyVolatile kernel.Code = "model_identity_key_volatile"
	// CodeIdentityContext is a context component whose link type has no binding from the type that is N:1 or 1:1.
	CodeIdentityContext kernel.Code = "model_identity_context"
	// CodeSchemeUnknown is an identifier scheme that does not exist.
	CodeSchemeUnknown kernel.Code = "model_scheme_unknown"
	// CodeIdentityKeyChanged is an org or ext type that changes the identity keys of its parent.
	CodeIdentityKeyChanged kernel.Code = "model_identity_key_changed"
	// CodeTitleProperty is a title property that is not a property of the type.
	CodeTitleProperty kernel.Code = "model_title_property"
	// CodeSuperseded is a status and superseded_by that do not agree.
	CodeSuperseded kernel.Code = "model_superseded"
	// CodeNeed is a declared need that breaks a rule.
	CodeNeed kernel.Code = "model_need"
	// CodeNumber is a type, link type or attribute number that is missing, not positive, used twice or reserved.
	CodeNumber kernel.Code = "model_number"
	// CodeLinkMeaning is a meaning parameter that the meaning does not have, or a wrong value of one.
	CodeLinkMeaning kernel.Code = "model_link_meaning"
	// CodeLinkVolatile is a link property that is volatile.
	CodeLinkVolatile kernel.Code = "model_link_volatile"
	// CodeBindingEndpoint is a binding end that does not exist.
	CodeBindingEndpoint kernel.Code = "model_binding_endpoint"
	// CodeBindingDuplicate is a binding that a link type has twice.
	CodeBindingDuplicate kernel.Code = "model_binding_duplicate"
	// CodeBindingConstraint is a parameter constraint that the meaning does not allow or that widens the link type.
	CodeBindingConstraint kernel.Code = "model_binding_constraint"
	// CodeActionRisk is a risk axis that does not fit the others.
	CodeActionRisk kernel.Code = "model_action_risk"
	// CodeActionRef is an action that points at a type, trait or action that does not exist.
	CodeActionRef kernel.Code = "model_action_ref"
	// CodeValueInvalid is a value that a value type rejects.
	CodeValueInvalid kernel.Code = "model_value_invalid"
)

var catalogue = map[kernel.Code]string{
	CodeDataInvalid:         "A definition file is not valid JSON of the expected shape.",
	CodeNotStandard:         "The standard files hold only std definitions.",
	CodeRequiredField:       "A definition lacks a field it must have.",
	CodeDuplicateID:         "Two definitions share an id.",
	CodeNameInvalid:         "A name breaks the naming rule of its kind.",
	CodeNameDuplicate:       "Two definitions of one kind share a name or an alias.",
	CodeNameReserved:        "A name outside std is already owned by a standard definition.",
	CodeClosedList:          "A value is outside a closed list of the model.",
	CodeLayerNamespace:      "Only sys types belong to the system layer, and every sys type does.",
	CodeLayerNotInherited:   "An org or ext type must be in the layer of its parent.",
	CodeParentUnknown:       "A parent type does not exist.",
	CodeParentCycle:         "The parent chain of a type returns to itself.",
	CodeParentNamespace:     "A type may not build on a parent from a namespace above it.",
	CodeTraitUnknown:        "A trait does not exist.",
	CodePropertyDuplicate:   "A type has two properties with one name.",
	CodeValueTypeUnknown:    "A value type does not exist.",
	CodeEnumValues:          "Enum values are required for an enum and allowed only there.",
	CodeUnit:                "A unit is required for a quantity and allowed only there.",
	CodeScopeKeyType:        "A scope key must be a short text or a reference.",
	CodeScopeKeyVolatile:    "A scope key must be versioned.",
	CodeScopeKeyLimit:       "A type has at most four scope keys.",
	CodeIdentityKey:         "An identity key breaks a rule.",
	CodeIdentityKeyVolatile: "An identity key may not use a property that is volatile.",
	CodeIdentityContext:     "A context component needs a link type with a many-to-one binding from the type.",
	CodeSchemeUnknown:       "An identity key or scheme names an identifier scheme or normaliser that does not exist.",
	CodeIdentityKeyChanged:  "An org or ext type may not change the identity keys of its parent.",
	CodeTitleProperty:       "The title property is not a property of the type.",
	CodeSuperseded:          "Status and superseded_by do not agree.",
	CodeNeed:                "A declared need breaks a rule.",
	CodeNumber:              "A number is missing, not positive, used twice or reserved.",
	CodeLinkMeaning:         "A meaning parameter is not allowed or has a wrong value.",
	CodeLinkVolatile:        "A link property may not be volatile.",
	CodeBindingEndpoint:     "A binding end does not exist.",
	CodeBindingDuplicate:    "A link type has the same binding twice.",
	CodeBindingConstraint:   "A parameter constraint is not allowed or widens the link type.",
	CodeActionRisk:          "The risk axes of an action do not fit each other.",
	CodeActionRef:           "An action points at something that does not exist.",
	CodeValueInvalid:        "A value type rejects the value.",
}

// Codes lists the codes of this unit, sorted.
func Codes() []kernel.Code {
	out := make([]kernel.Code, 0, len(catalogue))
	for code := range catalogue {
		out = append(out, code)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// CodeMeaning is the one-line meaning of a code of this unit, or false.
func CodeMeaning(code kernel.Code) (string, bool) {
	meaning, ok := catalogue[code]
	return meaning, ok
}
