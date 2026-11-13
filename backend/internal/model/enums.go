package model

// The closed lists of the model. Only a decision record changes them
// (ADR-0007): no API and no definition adds a value. Every list has a function
// that returns the values in their documented order.

// Namespace says who owns a definition (kg-model.md 3.7).
type Namespace string

// The namespaces. Sys is reserved for the product's own parts.
const (
	NamespaceStd Namespace = "std"
	NamespaceOrg Namespace = "org"
	NamespaceExt Namespace = "ext"
	NamespaceSys Namespace = "sys"
)

// Layer is the layer of an object type: the nine layers of the standard model
// and the tenth, system, for sys types (standard-model.md).
type Layer string

// The ten layers.
const (
	LayerFacility       Layer = "facility"
	LayerHardware       Layer = "hardware"
	LayerNetwork        Layer = "network"
	LayerStorage        Layer = "storage"
	LayerVirtualization Layer = "virtualization"
	LayerContainer      Layer = "container"
	LayerPlatform       Layer = "platform"
	LayerService        Layer = "service"
	LayerCrossCutting   Layer = "cross_cutting"
	LayerSystem         Layer = "system"
)

// Status is the life of a type or a property: it is never deleted (kg-model.md 11.1).
type Status string

// The statuses.
const (
	StatusExperimental Status = "experimental"
	StatusActive       Status = "active"
	StatusDeprecated   Status = "deprecated"
)

// ChangeClass is how fast a property changes (kg-model.md 3.2).
type ChangeClass string

// The change classes.
const (
	ChangeStatic  ChangeClass = "static"
	ChangeSlow    ChangeClass = "slow"
	ChangeDynamic ChangeClass = "dynamic"
)

// HistoryKind is what a change of a property leaves behind.
type HistoryKind string

// The history kinds. Versioned changes enter the history and the content
// hash; volatile ones keep only the current value and the time it was seen.
const (
	HistoryVersioned HistoryKind = "versioned"
	HistoryVolatile  HistoryKind = "volatile"
)

// Sensitivity is the access class of a property value (ADR-0012).
type Sensitivity string

// The sensitivities.
const (
	SensitivityOpen         Sensitivity = "open"
	SensitivityRestricted   Sensitivity = "restricted"
	SensitivityConfidential Sensitivity = "confidential"
)

// Channel is one of the six kinds of state an object has (kg-model.md 5.1).
type Channel string

// The six channels.
const (
	ChannelService    Channel = "service"
	ChannelPower      Channel = "power"
	ChannelCooling    Channel = "cooling"
	ChannelNetwork    Channel = "network"
	ChannelStorage    Channel = "storage"
	ChannelManagement Channel = "management"
)

// ChannelState is the value of a channel: always one of three.
type ChannelState string

// The three values of a channel.
const (
	StateUp      ChannelState = "up"
	StateDown    ChannelState = "down"
	StateUnknown ChannelState = "unknown"
)

// Meaning is what a link means for the spread of state (kg-model.md 5.2).
type Meaning string

// The eleven meanings.
const (
	MeaningRunsOn      Meaning = "runs_on"
	MeaningRequires    Meaning = "requires"
	MeaningSuppliedBy  Meaning = "supplied_by"
	MeaningRealizedBy  Meaning = "realized_by"
	MeaningPartOf      Meaning = "part_of"
	MeaningContainedIn Meaning = "contained_in"
	MeaningConnectedTo Meaning = "connected_to"
	MeaningMemberOf    Meaning = "member_of"
	MeaningManagedBy   Meaning = "managed_by"
	MeaningOwnedBy     Meaning = "owned_by"
	MeaningRecordOnly  Meaning = "record_only"
)

// Strength is how much one side needs the other: a hard need kills, a soft one reduces.
type Strength string

// The strengths.
const (
	StrengthHard Strength = "hard"
	StrengthSoft Strength = "soft"
)

// Cardinality is the count of a binding, written from end to end.
type Cardinality string

// The cardinalities.
const (
	CardOneToOne   Cardinality = "1:1"
	CardOneToMany  Cardinality = "1:N"
	CardManyToOne  Cardinality = "N:1"
	CardManyToMany Cardinality = "N:M"
)

// DelayClass tells whether a spread along a link shows up late.
type DelayClass string

// The delay classes. An empty delay class means immediate.
const (
	DelayImmediate DelayClass = "immediate"
	DelayDelayed   DelayClass = "delayed"
)

// Grade is how far a value or a link can be trusted (ADR-0007 10).
type Grade string

// The evidence grades, best first.
const (
	GradeFact    Grade = "fact"
	GradeAssumed Grade = "assumed"
	GradeUnknown Grade = "unknown"
)

// Basis is where a value came from: a different axis than the grade.
type Basis string

// The four bases.
const (
	BasisConfigured Basis = "configured"
	BasisObserved   Basis = "observed"
	BasisDeclared   Basis = "declared"
	BasisDerived    Basis = "derived"
)

// Reversibility is the second risk axis of an action.
type Reversibility string

// The reversibilities. An action that does not mutate has none.
const (
	Reversible   Reversibility = "reversible"
	Irreversible Reversibility = "irreversible"
)

// Disruption is the third risk axis of an action.
type Disruption string

// The disruptions.
const (
	DisruptionNone             Disruption = "none"
	DisruptionServiceAffecting Disruption = "service_affecting"
	DisruptionDataLoss         Disruption = "data_loss"
)

// IdentityStrength tells whether an identity key is unique worldwide or only within a border.
type IdentityStrength string

// The identity strengths.
const (
	IdentityStrong IdentityStrength = "strong"
	IdentityWeak   IdentityStrength = "weak"
)

// BaseKind is the data kind of a value; named value types sit on top of one.
type BaseKind string

// The ten base kinds.
const (
	BaseString   BaseKind = "string"
	BaseInteger  BaseKind = "integer"
	BaseNumber   BaseKind = "number"
	BaseBoolean  BaseKind = "boolean"
	BaseTime     BaseKind = "time"
	BaseEnum     BaseKind = "enum"
	BaseQuantity BaseKind = "quantity"
	BaseRef      BaseKind = "ref"
	BaseList     BaseKind = "list"
	BaseMap      BaseKind = "map"
)

// Tier says whether a standard type is core (needed to compute impact) or an
// extension (common in sources or in operations): standard-model.md.
type Tier string

// The tiers. An empty tier means core.
const (
	TierCore      Tier = "core"
	TierExtension Tier = "extension"
)

// EndpointKind is what a binding end or an action target names.
type EndpointKind string

// The endpoint kinds: one type (with its subtypes), one trait, any object.
const (
	EndpointType  EndpointKind = "type"
	EndpointTrait EndpointKind = "trait"
	EndpointAny   EndpointKind = "any"
)

func oneOf[T comparable](value T, list []T) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// Namespaces returns the four namespaces.
func Namespaces() []Namespace {
	return []Namespace{NamespaceStd, NamespaceOrg, NamespaceExt, NamespaceSys}
}

// Layers returns the ten layers in their documented order.
func Layers() []Layer {
	return []Layer{
		LayerFacility, LayerHardware, LayerNetwork, LayerStorage, LayerVirtualization,
		LayerContainer, LayerPlatform, LayerService, LayerCrossCutting, LayerSystem,
	}
}

// Channels returns the six channels.
func Channels() []Channel {
	return []Channel{ChannelService, ChannelPower, ChannelCooling, ChannelNetwork, ChannelStorage, ChannelManagement}
}

// Meanings returns the eleven link meanings.
func Meanings() []Meaning {
	return []Meaning{
		MeaningRunsOn, MeaningRequires, MeaningSuppliedBy, MeaningRealizedBy, MeaningPartOf, MeaningContainedIn,
		MeaningConnectedTo, MeaningMemberOf, MeaningManagedBy, MeaningOwnedBy, MeaningRecordOnly,
	}
}

// Grades returns the evidence grades, best first.
func Grades() []Grade { return []Grade{GradeFact, GradeAssumed, GradeUnknown} }

// Bases returns the four bases.
func Bases() []Basis { return []Basis{BasisConfigured, BasisObserved, BasisDeclared, BasisDerived} }

// BaseKinds returns the ten base kinds.
func BaseKinds() []BaseKind {
	return []BaseKind{
		BaseString, BaseInteger, BaseNumber, BaseBoolean, BaseTime,
		BaseEnum, BaseQuantity, BaseRef, BaseList, BaseMap,
	}
}

// Valid reports whether n is one of the four namespaces.
func (n Namespace) Valid() bool { return oneOf(n, Namespaces()) }

// Valid reports whether l is one of the ten layers.
func (l Layer) Valid() bool { return oneOf(l, Layers()) }

// Valid reports whether c is one of the six channels.
func (c Channel) Valid() bool { return oneOf(c, Channels()) }

// Valid reports whether m is one of the eleven meanings.
func (m Meaning) Valid() bool { return oneOf(m, Meanings()) }

// Valid reports whether g is one of the three grades.
func (g Grade) Valid() bool { return oneOf(g, Grades()) }

// Valid reports whether b is one of the four bases.
func (b Basis) Valid() bool { return oneOf(b, Bases()) }

// Valid reports whether b is one of the ten base kinds.
func (b BaseKind) Valid() bool { return oneOf(b, BaseKinds()) }

// Lowest returns the lowest of the grades: the grade of an answer is the lowest
// grade of the inputs it used. No grades, or only grades outside the list,
// give unknown.
func Lowest(grades ...Grade) Grade {
	order := Grades()
	lowest := -1
	for _, grade := range grades {
		for rank, candidate := range order {
			if candidate == grade && rank > lowest {
				lowest = rank
			}
		}
	}
	if lowest < 0 {
		return GradeUnknown
	}
	return order[lowest]
}
