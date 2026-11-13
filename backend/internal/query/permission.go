package query

import (
	"github.com/ysk6506/praxium/backend/internal/model"
)

// Target is what a read decision may look at: the type of an object and the
// values of its scope-key attributes (access-control.md 5-4, 6). ScopeKeys is
// filled only when the filter says it needs conditions.
type Target struct {
	Type      model.ObjectType
	ScopeKeys map[string]string
}

// Decision is the read decision for one object: whether any grant matches it
// and, if so, the highest sensitivity the matching grants allow (the highest
// cap among them, never per type; access-control.md 5-4).
type Decision struct {
	Visible bool
	Cap     model.Sensitivity
}

// ReadFilter is the read-permission port (access-control.md 7-5). The API
// builds one per request from the grants of the subject; the queries only
// ask it. A query never sees the grants themselves.
type ReadFilter interface {
	// Decide judges one object.
	Decide(Target) Decision
	// NeedsScopeKeys says whether Decide reads Target.ScopeKeys; if not, the queries skip computing them.
	NeedsScopeKeys() bool
	// Floor is the highest sensitivity every visible object is sure to allow.
	// A filter on an attribute above the floor is refused.
	Floor() model.Sensitivity
	// Exact says whether hidden counts are given exactly (the whole scope) or only as present or absent.
	Exact() bool
}

// SensitivityRank orders the three sensitivities: open < restricted < confidential.
func SensitivityRank(s model.Sensitivity) int {
	switch s {
	case model.SensitivityConfidential:
		return 2
	case model.SensitivityRestricted:
		return 1
	}
	return 0
}

type allowAll struct{}

// AllowAll is the filter of a caller whose scope is everything, up to confidential.
func AllowAll() ReadFilter { return allowAll{} }

func (allowAll) Decide(Target) Decision {
	return Decision{Visible: true, Cap: model.SensitivityConfidential}
}
func (allowAll) NeedsScopeKeys() bool     { return false }
func (allowAll) Floor() model.Sensitivity { return model.SensitivityConfidential }
func (allowAll) Exact() bool              { return true }

// Selector picks object types in a grant: Kind is "type", "layer" or
// "namespace" and Ref the name (access-control.md 5-2). A type selector also
// matches the subtypes of the type.
type Selector struct {
	Kind string
	Ref  string
}

// Grant is the read triple of one grant: which types, which scope-key values,
// and the sensitivity cap. Empty Types means every type.
type Grant struct {
	Types      []Selector
	Conditions map[string][]string
	Max        model.Sensitivity
}

// Grants is a ReadFilter over a list of grants. It is the restrictive test
// double of this unit and also the evaluation the API can use once it maps
// roles to grants; Full says the caller's scope is not limited, which gives
// exact hidden counts.
type Grants struct {
	List []Grant
	Full bool
	reg  *model.Registry
}

// NewGrants builds a filter over grants. The registry is used to expand a
// type selector to its subtypes.
func NewGrants(reg *model.Registry, full bool, grants ...Grant) *Grants {
	return &Grants{List: grants, Full: full, reg: reg}
}

// Decide implements ReadFilter.
func (g *Grants) Decide(t Target) Decision {
	d := Decision{}
	for _, gr := range g.List {
		if !g.matches(gr, t) {
			continue
		}
		if !d.Visible || SensitivityRank(gr.Max) > SensitivityRank(d.Cap) {
			d.Cap = gr.Max
		}
		d.Visible = true
	}
	if d.Visible && d.Cap == "" {
		d.Cap = model.SensitivityOpen
	}
	return d
}

func (g *Grants) matches(gr Grant, t Target) bool {
	if len(gr.Types) > 0 && !g.selected(gr.Types, t.Type) {
		return false
	}
	for name, allowed := range gr.Conditions {
		ok := false
		for _, v := range allowed {
			if t.ScopeKeys[name] == v {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func (g *Grants) selected(sels []Selector, t model.ObjectType) bool {
	for _, s := range sels {
		switch s.Kind {
		case "type":
			if root, ok := g.reg.ObjectTypeByName(s.Ref); ok && g.reg.IsSubtypeOf(t.ID, root.ID) {
				return true
			}
		case "layer":
			if string(t.Layer) == s.Ref {
				return true
			}
		case "namespace":
			if string(t.Namespace) == s.Ref {
				return true
			}
		}
	}
	return false
}

// NeedsScopeKeys implements ReadFilter.
func (g *Grants) NeedsScopeKeys() bool {
	for _, gr := range g.List {
		if len(gr.Conditions) > 0 {
			return true
		}
	}
	return false
}

// Floor implements ReadFilter: the lowest cap among the grants.
func (g *Grants) Floor() model.Sensitivity {
	floor := model.SensitivityConfidential
	for _, gr := range g.List {
		c := gr.Max
		if c == "" {
			c = model.SensitivityOpen
		}
		if SensitivityRank(c) < SensitivityRank(floor) {
			floor = c
		}
	}
	if len(g.List) == 0 {
		return model.SensitivityOpen
	}
	return floor
}

// Exact implements ReadFilter.
func (g *Grants) Exact() bool { return g.Full }
