package model

// ParamKind is the kind of a meaning parameter.
type ParamKind string

// The parameter kinds. Enum and type_ref parameters can be fixed by a link
// type and narrowed by a binding; number and text ones are set on each link.
const (
	ParamEnum    ParamKind = "enum"
	ParamTypeRef ParamKind = "type_ref"
	ParamNumber  ParamKind = "number"
	ParamText    ParamKind = "text"
)

// ParamSpec is one parameter of a meaning.
type ParamSpec struct {
	Name   string
	Kind   ParamKind
	Values []string // the allowed values of an enum parameter
}

// MeaningSpec says how a meaning spreads state: which channel of the start
// depends on which channel of the end (kg-model.md 5.2). An empty channel means
// the meaning does not spread along a channel. For supplied_by the channel of
// the start is the value of the resource parameter (FromParam).
type MeaningSpec struct {
	Meaning   Meaning
	From      Channel
	To        Channel
	FromParam string
	Params    []ParamSpec
}

var (
	paramStrength = ParamSpec{Name: "strength", Kind: ParamEnum, Values: []string{"hard", "soft"}}
	paramResource = ParamSpec{Name: "resource", Kind: ParamEnum, Values: []string{"power", "cooling", "network", "storage"}}
	paramWhole    = ParamSpec{Name: "whole_needs_part", Kind: ParamEnum, Values: []string{"hard", "soft", "none"}}
	paramFailover = ParamSpec{Name: "failover_to", Kind: ParamTypeRef}
	paramDemand   = ParamSpec{Name: "demand", Kind: ParamNumber}
	paramWeight   = ParamSpec{Name: "weight", Kind: ParamNumber}
	paramRole     = ParamSpec{Name: "role", Kind: ParamText}
)

var meaningSpecs = []MeaningSpec{
	{Meaning: MeaningRunsOn, From: ChannelService, To: ChannelService, Params: []ParamSpec{paramFailover}},
	{Meaning: MeaningRequires, From: ChannelService, To: ChannelService, Params: []ParamSpec{paramStrength, paramDemand}},
	{Meaning: MeaningSuppliedBy, To: ChannelService, FromParam: "resource", Params: []ParamSpec{paramResource, paramDemand}},
	{Meaning: MeaningRealizedBy, From: ChannelService, To: ChannelService},
	{Meaning: MeaningPartOf, From: ChannelService, To: ChannelService, Params: []ParamSpec{paramWhole}},
	{Meaning: MeaningContainedIn},
	{Meaning: MeaningConnectedTo, From: ChannelNetwork, To: ChannelNetwork},
	{Meaning: MeaningMemberOf, From: ChannelService, To: ChannelService, Params: []ParamSpec{paramWeight, paramRole}},
	{Meaning: MeaningManagedBy, From: ChannelManagement, To: ChannelService},
	{Meaning: MeaningOwnedBy, Params: []ParamSpec{paramRole}},
	{Meaning: MeaningRecordOnly},
}

// MeaningSpecs returns the specification of the eleven meanings in their documented order.
func MeaningSpecs() []MeaningSpec {
	out := make([]MeaningSpec, len(meaningSpecs))
	for i, spec := range meaningSpecs {
		out[i] = spec
		out[i].Params = append([]ParamSpec(nil), spec.Params...)
		for j := range out[i].Params {
			out[i].Params[j].Values = append([]string(nil), spec.Params[j].Values...)
		}
	}
	return out
}

// SpecOf returns the specification of a meaning, or false for a value outside the closed list.
func SpecOf(meaning Meaning) (MeaningSpec, bool) {
	for _, spec := range MeaningSpecs() {
		if spec.Meaning == meaning {
			return spec, true
		}
	}
	return MeaningSpec{}, false
}

// Param returns the parameter of the meaning with the name, or false.
func (s MeaningSpec) Param(name string) (ParamSpec, bool) {
	for _, param := range s.Params {
		if param.Name == name {
			return param, true
		}
	}
	return ParamSpec{}, false
}

// Spreads reports whether the meaning carries state along a channel.
func (s MeaningSpec) Spreads() bool { return s.To != "" }

// narrowable reports whether a link type can fix the parameter and a binding narrow it.
func (p ParamSpec) narrowable() bool { return p.Kind == ParamEnum || p.Kind == ParamTypeRef }
