package httpapi

import (
	"encoding/json"

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
}

type boundLinkView struct {
	LinkType string      `json:"link_type"`
	Binding  bindingView `json:"binding"`
}

type typeRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// strs gives a list that is never null in JSON.
func strs(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// opt gives a pointer for an empty-means-absent text, so JSON shows null.
func opt(s string) *string {
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
		ID: p.ID.String(), AttrNo: p.AttrNo, Name: p.Name, Aliases: strs(p.Aliases),
		ValueType: p.ValueType.String(), Unit: opt(p.Unit), EnumValues: strs(p.EnumValues),
		Required: p.Required, ChangeClass: string(p.ChangeClass), Sensitivity: string(p.EffectiveSensitivity()),
		HistoryKind: string(p.EffectiveHistory()), ScopeKey: p.ScopeKey, Status: status,
		SupersededBy: opt(p.SupersededBy.String()), Description: p.Description,
	}
}

func propertiesOf(in []model.Property) []propertyView {
	out := make([]propertyView, 0, len(in))
	for _, p := range in {
		out = append(out, propertyOf(p))
	}
	return out
}

func needsOf(in []model.Need) []needView {
	out := make([]needView, 0, len(in))
	for _, n := range in {
		out = append(out, needView{Channel: string(n.Channel), Strength: opt(string(n.Strength))})
	}
	return out
}

func keyOf(k model.IdentityKey) identityKeyView {
	v := identityKeyView{Strength: string(k.Strength), Scheme: opt(k.Scheme.String()), Components: []keyComponentView{}}
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

func keysOf(in []model.IdentityKey) []identityKeyView {
	out := make([]identityKeyView, 0, len(in))
	for _, k := range in {
		out = append(out, keyOf(k))
	}
	return out
}

func endpointOf(e model.Endpoint) endpointView {
	return endpointView{Kind: string(e.Kind), Ref: opt(e.Ref)}
}

func endpointsOf(in []model.Endpoint) []endpointView {
	out := make([]endpointView, 0, len(in))
	for _, e := range in {
		out = append(out, endpointOf(e))
	}
	return out
}

func bindingOf(b model.Binding) bindingView {
	constraints := map[string][]string{}
	for k, v := range b.ParamConstraints {
		constraints[k] = strs(v)
	}
	return bindingView{
		ID: b.ID.String(), From: endpointOf(b.From), To: endpointOf(b.To),
		Cardinality: string(b.Cardinality), ParamConstraints: constraints,
	}
}

func bindingsOf(in []model.Binding) []bindingView {
	out := make([]bindingView, 0, len(in))
	for _, b := range in {
		out = append(out, bindingOf(b))
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
func toMap(v any) map[string]any {
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
