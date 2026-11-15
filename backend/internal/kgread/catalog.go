package kgread

import (
	"encoding/json"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The views are the shapes of the contract's schemas (backend/internal/contract/openapi/parts).
// They are written apart from the model's own types so the contract can stay
// the same while the model changes inside.

type propertyView struct {
	ID           string   `json:"id"`
	AttrNo       int      `json:"attr_no"`
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases"`
	ValueType    string   `json:"value_type"`
	Unit         *string  `json:"unit"`
	EnumValues   []string `json:"enum_values"`
	Required     bool     `json:"required"`
	ChangeClass  string   `json:"change_class"`
	Sensitivity  string   `json:"sensitivity"`
	HistoryKind  string   `json:"history_kind"`
	ScopeKey     bool     `json:"scope_key"`
	Status       string   `json:"status"`
	SupersededBy *string  `json:"superseded_by"`
	Description  string   `json:"description"`
}

type needView struct {
	Channel  string  `json:"channel"`
	Strength *string `json:"strength"`
}

type keyComponentView struct {
	Kind     string `json:"kind"`
	Ref      string `json:"ref"`
	Incoming bool   `json:"incoming"`
	Size     int    `json:"size"`
}

type identityKeyView struct {
	Strength   string             `json:"strength"`
	Scheme     *string            `json:"scheme"`
	Components []keyComponentView `json:"components"`
}

type endpointView struct {
	Kind string  `json:"kind"`
	Ref  *string `json:"ref"`
}

type bindingView struct {
	ID               string              `json:"id"`
	From             endpointView        `json:"from"`
	To               endpointView        `json:"to"`
	Cardinality      string              `json:"cardinality"`
	ParamConstraints map[string][]string `json:"param_constraints"`
	ParamDefaults    map[string]string   `json:"param_defaults,omitempty"`
}

type BoundLinkView struct {
	LinkType string      `json:"link_type"`
	Binding  bindingView `json:"binding"`
}

type TypeRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// strs gives a list that is never null in JSON.
func Strs(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// opt gives a pointer for an empty-means-absent text, so JSON shows null.
func Opt(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func propertyOf(p model.Property) propertyView {
	status := string(p.Status)
	if status == "" {
		status = string(model.StatusActive)
	}
	return propertyView{
		ID: p.ID.String(), AttrNo: p.AttrNo, Name: p.Name, Aliases: Strs(p.Aliases),
		ValueType: p.ValueType.String(), Unit: Opt(p.Unit), EnumValues: Strs(p.EnumValues),
		Required: p.Required, ChangeClass: string(p.ChangeClass), Sensitivity: string(p.EffectiveSensitivity()),
		HistoryKind: string(p.EffectiveHistory()), ScopeKey: p.ScopeKey, Status: status,
		SupersededBy: Opt(p.SupersededBy.String()), Description: p.Description,
	}
}

func PropertiesOf(in []model.Property) []propertyView {
	out := make([]propertyView, 0, len(in))
	for _, p := range in {
		out = append(out, propertyOf(p))
	}
	return out
}

func NeedsOf(in []model.Need) []needView {
	out := make([]needView, 0, len(in))
	for _, n := range in {
		out = append(out, needView{Channel: string(n.Channel), Strength: Opt(string(n.Strength))})
	}
	return out
}

func keyOf(k model.IdentityKey) identityKeyView {
	v := identityKeyView{Strength: string(k.Strength), Scheme: Opt(k.Scheme.String()), Components: []keyComponentView{}}
	for _, c := range k.Components {
		switch {
		case !c.Property.IsZero():
			v.Components = append(v.Components, keyComponentView{Kind: "property", Ref: c.Property.String()})
		case c.Set:
			v.Components = append(v.Components, keyComponentView{Kind: "set", Ref: c.Context.String(), Incoming: c.Incoming, Size: c.Size})
		default:
			v.Components = append(v.Components, keyComponentView{Kind: "context", Ref: c.Context.String()})
		}
	}
	return v
}

func KeysOf(in []model.IdentityKey) []identityKeyView {
	out := make([]identityKeyView, 0, len(in))
	for _, k := range in {
		out = append(out, keyOf(k))
	}
	return out
}

func endpointOf(e model.Endpoint) endpointView {
	return endpointView{Kind: string(e.Kind), Ref: Opt(e.Ref)}
}

func endpointsOf(in []model.Endpoint) []endpointView {
	out := make([]endpointView, 0, len(in))
	for _, e := range in {
		out = append(out, endpointOf(e))
	}
	return out
}

func BindingOf(b model.Binding) bindingView {
	constraints := map[string][]string{}
	for k, v := range b.ParamConstraints {
		constraints[k] = Strs(v)
	}
	return bindingView{
		ID: b.ID.String(), From: endpointOf(b.From), To: endpointOf(b.To),
		Cardinality: string(b.Cardinality), ParamConstraints: constraints, ParamDefaults: b.ParamDefaults,
	}
}

func BindingsOf(in []model.Binding) []bindingView {
	out := make([]bindingView, 0, len(in))
	for _, b := range in {
		out = append(out, BindingOf(b))
	}
	return out
}

func displayNames(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	return in
}

// toMap turns a view into the map a list row keeps, through its JSON form.
func ToMap(v any) map[string]any {
	data, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// objectTypeSummary is the shape of an item of GET /v1/object-types.
func ObjectTypeSummary(t model.ObjectType) map[string]any {
	return map[string]any{
		"id": t.ID.String(), "type_no": t.TypeNo, "name": t.Name, "aliases": Strs(t.Aliases),
		"namespace": string(t.Namespace), "layer": string(t.Layer), "tier": string(t.EffectiveTier()),
		"parent": Opt(t.Parent.String()), "traits": traitIDs(t.Traits), "status": string(t.Status),
		"superseded_by": Opt(t.SupersededBy.String()), "description": t.Description,
		"standard_ref": t.StandardRef, "display_names": displayNames(t.DisplayNames),
	}
}

func traitIDs(in []model.TraitID) []string {
	out := make([]string, 0, len(in))
	for _, id := range in {
		out = append(out, id.String())
	}
	return out
}

// covered lists the ids of the object types an end of a binding or an action covers:
// a type and its subtypes, the types that carry a trait, or every type.
func Covered(reg *model.Registry, all []string, e model.Endpoint) []string {
	switch e.Kind {
	case model.EndpointAny:
		return all
	case model.EndpointType:
		out := []string{e.Ref}
		id, err := kernel.ParseID[model.ObjectTypeKind](e.Ref)
		if err != nil {
			return out
		}
		for _, sub := range reg.Subtypes(id) {
			out = append(out, sub.ID.String())
		}
		return out
	case model.EndpointTrait:
		id, err := kernel.ParseID[model.TraitKind](e.Ref)
		if err != nil {
			return nil
		}
		var out []string
		for _, t := range reg.TypesWithTrait(id) {
			out = append(out, t.ID.String())
		}
		return out
	}
	return nil
}

func LinkTypeSummary(l model.LinkType) map[string]any {
	params := map[string]string{}
	for k, v := range l.MeaningParams {
		params[k] = v
	}
	return map[string]any{
		"id": l.ID.String(), "link_type_no": l.LinkTypeNo, "name": l.Name, "aliases": Strs(l.Aliases),
		"namespace": string(l.Namespace), "meaning": string(l.Meaning), "meaning_params": params,
		"delay_class": string(l.EffectiveDelay()), "status": string(l.Status),
		"superseded_by": Opt(l.SupersededBy.String()), "description": l.Description, "binding_count": len(l.Bindings),
	}
}

func ActionTypeView(a model.ActionType) map[string]any {
	return map[string]any{
		"id": a.ID.String(), "name": a.Name, "aliases": Strs(a.Aliases), "namespace": string(a.Namespace),
		"applies_to": ToAny(endpointsOf(a.AppliesTo)), "mutates": a.Mutates, "reversibility": Opt(string(a.Reversibility)),
		"disruption": string(a.Disruption), "physical": a.Physical, "declared_effects": Strs(a.DeclaredEffects),
		"inverse": Opt(a.Inverse.String()), "description": a.Description, "status": string(a.Status),
		"superseded_by": Opt(a.SupersededBy.String()),
	}
}

// toAny passes a view through JSON so it nests inside a map like any other value.
func ToAny(v any) any {
	m := ToMap(map[string]any{"v": v})
	return m["v"]
}

func TraitView(t model.Trait) map[string]any {
	return map[string]any{
		"id": t.ID.String(), "name": t.Name, "aliases": Strs(t.Aliases), "description": t.Description,
		"properties": ToAny(PropertiesOf(t.Properties)), "needs": ToAny(NeedsOf(t.Needs)),
	}
}

func SchemeView(s model.IdentifierScheme) map[string]any {
	return map[string]any{
		"id": s.ID.String(), "name": s.Name, "aliases": Strs(s.Aliases), "namespace": string(s.Namespace),
		"normalizer": s.Normalizer.String(), "description": s.Description,
	}
}
