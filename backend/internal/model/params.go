package model

import "slices"

// ResolvedParams are the parameters of one link that the impact query reads, after the order of the values has been
// applied (ResolveParams).
type ResolvedParams struct {
	Strength       string
	WholeNeedsPart string
	Resource       string
	// FailoverTo is the object types a failover may land on; it is the binding's list, not a single value.
	FailoverTo []string
}

// ResolveParams works out the enum parameters of a link, one value each, in this order (ADR-0059, model-links.md 2):
//
//  1. the value the link type fixes;
//  2. the value the link states itself (written), when the binding allows it; a value outside the allowed ones is
//     not taken;
//  3. the default of the binding (param_defaults);
//  4. the only value the binding allows, if it allows just one;
//  5. the default of the meaning: strength is hard and whole_needs_part is none.
//
// No link states values yet, so callers pass nil for written; the place is kept so that a later change can pass
// them without changing this order. A parameter nothing says anything about (resource of a link type that does not
// fix one) is empty.
func ResolveParams(lt LinkType, b Binding, written map[string]string) ResolvedParams {
	return ResolvedParams{
		Strength:       resolveEnum(&lt, &b, written, paramStrength),
		WholeNeedsPart: resolveEnum(&lt, &b, written, paramWhole),
		Resource:       resolveEnum(&lt, &b, written, paramResource),
		FailoverTo:     slices.Clone(b.ParamConstraints[paramFailover.Name]),
	}
}

func resolveEnum(lt *LinkType, b *Binding, written map[string]string, param ParamSpec) string {
	if v := lt.MeaningParams[param.Name]; v != "" {
		return v
	}
	allowed := allowedValues(lt, b, param)
	if v := written[param.Name]; v != "" && slices.Contains(allowed, v) {
		return v
	}
	if v := b.ParamDefaults[param.Name]; v != "" {
		return v
	}
	if len(allowed) == 1 {
		return allowed[0]
	}
	return param.Default
}

// ParamsFor gives the parameters of a link of the link type from an object of type from to one of type to, by the
// binding BindingFor picks. It is false when no binding fits.
func (r *Registry) ParamsFor(from, to ObjectTypeID, lt LinkTypeID, written map[string]string) (ResolvedParams, bool) {
	b, l, ok := r.bindingOf(from, to, lt)
	if !ok {
		return ResolvedParams{}, false
	}
	return ResolveParams(*l, *b, written), true
}
