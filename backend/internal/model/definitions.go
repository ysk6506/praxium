package model

import "github.com/ysk6506/praxium/backend/internal/kernel"

// Kinds of typed ids. An id of one kind cannot be passed where another is expected.
type (
	// ObjectTypeKind marks ObjectTypeID.
	ObjectTypeKind struct{}
	// PropertyKind marks PropertyID.
	PropertyKind struct{}
	// ValueTypeKind marks ValueTypeID.
	ValueTypeKind struct{}
	// IdentifierSchemeKind marks the ids of identifier schemes.
	IdentifierSchemeKind struct{}
	// TraitKind marks TraitID.
	TraitKind struct{}
	// LinkTypeKind marks LinkTypeID.
	LinkTypeKind struct{}
	// BindingKind marks BindingID.
	BindingKind struct{}
	// ActionTypeKind marks ActionTypeID.
	ActionTypeKind struct{}
)

// The ids of definitions. An id never changes, even when the name does
// (REQ-233); every reference between definitions is by id.
type (
	// ObjectTypeID identifies an object type.
	ObjectTypeID = kernel.ID[ObjectTypeKind]
	// PropertyID identifies a property.
	PropertyID = kernel.ID[PropertyKind]
	// ValueTypeID identifies a value type.
	ValueTypeID = kernel.ID[ValueTypeKind]
	// IdentifierSchemeID is the id of an identifier scheme.
	IdentifierSchemeID = kernel.ID[IdentifierSchemeKind]
	// TraitID identifies a trait.
	TraitID = kernel.ID[TraitKind]
	// LinkTypeID identifies a link type.
	LinkTypeID = kernel.ID[LinkTypeKind]
	// BindingID identifies a binding.
	BindingID = kernel.ID[BindingKind]
	// ActionTypeID identifies an action type.
	ActionTypeID = kernel.ID[ActionTypeKind]
)

// Definitions is a set of definitions as written in data files: the input of
// the checks and of NewRegistry. Numbers left at zero are assigned by NewRegistry.
type Definitions struct {
	// ModelVersion is the version of the standard model; it goes up with the product.
	ModelVersion int `json:"model_version"`
	// Reserved lists numbers that were given once and may never be given again.
	Reserved    Reserved           `json:"reserved"`
	ValueTypes  []ValueType        `json:"value_types"`
	Schemes     []IdentifierScheme `json:"identifier_schemes"`
	Traits      []Trait            `json:"traits"`
	ObjectTypes []ObjectType       `json:"object_types"`
	LinkTypes   []LinkType         `json:"link_types"`
	ActionTypes []ActionType       `json:"action_types"`
}

// Reserved are the numbers that stay out of use. A number of a retired
// definition is listed here, so a new definition never gets it.
type Reserved struct {
	TypeNos     []int `json:"type_no"`
	LinkTypeNos []int `json:"link_type_no"`
	AttrNos     []int `json:"attr_no"`
}

// ValueType is a named value type on top of a base kind. Its check and its
// normalisation are code (see Registry.Normalize); the data names the type.
type ValueType struct {
	ID          ValueTypeID `json:"id"`
	Name        string      `json:"name"`
	Base        BaseKind    `json:"base"`
	Description string      `json:"description,omitempty"`
}

// Need is a channel that a trait or a type needs to be served by (kg-model.md 6).
// An empty strength is allowed only for the management channel, whose loss
// leaves the service alive.
type Need struct {
	Channel  Channel  `json:"channel"`
	Strength Strength `json:"strength,omitempty"`
}

// Trait is a property of many types: it gives properties and declares needs.
type Trait struct {
	ID          TraitID    `json:"id"`
	Name        string     `json:"name"`
	Aliases     []string   `json:"aliases,omitempty"`
	Description string     `json:"description,omitempty"`
	Properties  []Property `json:"properties,omitempty"`
	Needs       []Need     `json:"needs,omitempty"`
}

// Property is one attribute of an object type, a trait or a link type.
type Property struct {
	ID           PropertyID  `json:"id"`
	AttrNo       int         `json:"attr_no,omitempty"`
	Name         string      `json:"name"`
	Aliases      []string    `json:"aliases,omitempty"`
	ValueType    ValueTypeID `json:"value_type"`
	Unit         string      `json:"unit,omitempty"`
	EnumValues   []string    `json:"enum_values,omitempty"`
	Required     bool        `json:"required,omitempty"`
	ChangeClass  ChangeClass `json:"change_class"`
	Sensitivity  Sensitivity `json:"sensitivity,omitempty"`
	HistoryKind  HistoryKind `json:"history_kind,omitempty"`
	ScopeKey     bool        `json:"scope_key,omitempty"`
	Status       Status      `json:"status,omitempty"`
	SupersededBy PropertyID  `json:"superseded_by,omitempty"`
	Description  string      `json:"description,omitempty"`
}

// KeyComponent is one part of an identity key: a property of the type, whose
// value type tidies the value, or a context, the link type that leads to the
// object this one is identified inside (a port name inside its device,
// kg-model.md 10.2). Exactly one of the two is set.
type KeyComponent struct {
	Property PropertyID `json:"property,omitempty"`
	Context  LinkTypeID `json:"context,omitempty"`
}

// IdentityKey is one way to recognise an object, in priority order in the
// list of a type. Its components together are unique: within the context
// object when there is one, worldwide for a strong key and only inside a
// border for a weak one. A key may name an identifier scheme, the kind of the
// identifier in the identifier table (kg-model.md 10.1); with no component
// the identifier of that scheme is the whole key.
type IdentityKey struct {
	Components []KeyComponent     `json:"components,omitempty"`
	Strength   IdentityStrength   `json:"strength"`
	Scheme     IdentifierSchemeID `json:"scheme,omitempty"`
}

// IdentifierScheme is a kind of external identifier (a serial number, a
// management system's id) declared once, so that no key names one as free
// text. Its normaliser is the value type that tidies the value (kg-model.md
// 10.2); the border an identifier is valid in is stored on each identifier.
type IdentifierScheme struct {
	ID          IdentifierSchemeID `json:"id"`
	Name        string             `json:"name"`
	Aliases     []string           `json:"aliases,omitempty"`
	Namespace   Namespace          `json:"namespace"`
	Normalizer  ValueTypeID        `json:"normalizer"`
	Description string             `json:"description,omitempty"`
}

// ObjectType is a kind of object (kg-model.md 3.1).
type ObjectType struct {
	ID            ObjectTypeID      `json:"id"`
	TypeNo        int               `json:"type_no,omitempty"`
	Name          string            `json:"name"`
	Aliases       []string          `json:"aliases,omitempty"`
	Namespace     Namespace         `json:"namespace"`
	Layer         Layer             `json:"layer"`
	Tier          Tier              `json:"tier,omitempty"`
	Parent        ObjectTypeID      `json:"parent,omitempty"`
	Traits        []TraitID         `json:"traits,omitempty"`
	Needs         []Need            `json:"needs,omitempty"`
	Properties    []Property        `json:"properties,omitempty"`
	IdentityKeys  []IdentityKey     `json:"identity_keys,omitempty"`
	TitleProperty PropertyID        `json:"title_property,omitempty"`
	DisplayNames  map[string]string `json:"display_names,omitempty"`
	StandardRef   string            `json:"standard_ref,omitempty"`
	Description   string            `json:"description,omitempty"`
	Status        Status            `json:"status"`
	SupersededBy  ObjectTypeID      `json:"superseded_by,omitempty"`
	Contacts      []string          `json:"contacts,omitempty"`
}

// Endpoint is one end of a binding or one target of an action: a type (with
// its subtypes), a trait, or any object. Ref is the id of the type or trait.
type Endpoint struct {
	Kind EndpointKind `json:"kind"`
	Ref  string       `json:"ref,omitempty"`
}

// Binding is one pair of ends a link type may join (kg-model.md 3.5). The
// parameter constraints narrow what the link type allows: a parameter name and
// the values (or, for failover_to, the object type ids) this binding accepts.
type Binding struct {
	ID               BindingID           `json:"id"`
	From             Endpoint            `json:"from"`
	To               Endpoint            `json:"to"`
	Cardinality      Cardinality         `json:"cardinality"`
	ParamConstraints map[string][]string `json:"param_constraints,omitempty"`
}

// LinkType is a kind of link: one name, one meaning, many bindings.
type LinkType struct {
	ID            LinkTypeID        `json:"id"`
	LinkTypeNo    int               `json:"link_type_no,omitempty"`
	Name          string            `json:"name"`
	Aliases       []string          `json:"aliases,omitempty"`
	Namespace     Namespace         `json:"namespace"`
	Meaning       Meaning           `json:"meaning"`
	MeaningParams map[string]string `json:"meaning_params,omitempty"`
	DelayClass    DelayClass        `json:"delay_class,omitempty"`
	Properties    []Property        `json:"properties,omitempty"`
	Bindings      []Binding         `json:"bindings,omitempty"`
	Description   string            `json:"description,omitempty"`
	Status        Status            `json:"status"`
	SupersededBy  LinkTypeID        `json:"superseded_by,omitempty"`
}

// ActionType is a kind of action with the four risk axes (kg-model.md 3.6, 12).
// Only the list and the meaning exist for now; running an action is a later step.
type ActionType struct {
	ID              ActionTypeID  `json:"id"`
	Name            string        `json:"name"`
	Aliases         []string      `json:"aliases,omitempty"`
	Namespace       Namespace     `json:"namespace"`
	AppliesTo       []Endpoint    `json:"applies_to"`
	Mutates         bool          `json:"mutates"`
	Reversibility   Reversibility `json:"reversibility,omitempty"`
	Disruption      Disruption    `json:"disruption"`
	Physical        bool          `json:"physical"`
	DeclaredEffects []string      `json:"declared_effects,omitempty"`
	Inverse         ActionTypeID  `json:"inverse,omitempty"`
	Description     string        `json:"description,omitempty"`
	Status          Status        `json:"status"`
	SupersededBy    ActionTypeID  `json:"superseded_by,omitempty"`
}

// EffectiveHistory is the history kind a property really has: its own when
// set, else the default of its change class (static and slow are versioned,
// dynamic is volatile).
func (p Property) EffectiveHistory() HistoryKind {
	if p.HistoryKind != "" {
		return p.HistoryKind
	}
	if p.ChangeClass == ChangeDynamic {
		return HistoryVolatile
	}
	return HistoryVersioned
}

// EffectiveSensitivity is the sensitivity of a property; an empty one is open.
func (p Property) EffectiveSensitivity() Sensitivity {
	if p.Sensitivity == "" {
		return SensitivityOpen
	}
	return p.Sensitivity
}

// EffectiveTier is the tier of an object type; an empty one is core.
func (t ObjectType) EffectiveTier() Tier {
	if t.Tier == "" {
		return TierCore
	}
	return t.Tier
}

// EffectiveDelay is the delay class of a link type; an empty one is immediate.
func (l LinkType) EffectiveDelay() DelayClass {
	if l.DelayClass == "" {
		return DelayImmediate
	}
	return l.DelayClass
}
