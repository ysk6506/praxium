package model

import (
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// allowedOrgBases are the base kinds an organisation property may have: what
// a flat file can give. A reference points at another object, which is the
// work of a link type; a list and a map do not fit a column.
var allowedOrgBases = []BaseKind{BaseString, BaseInteger, BaseNumber, BaseBoolean, BaseTime, BaseEnum, BaseQuantity}

// ComposeOverlays returns the definitions with the overlays added: the
// properties of each overlay are put at the end of the properties of its
// target, with the namespace org. base is not changed. The result is not
// checked as a whole, so pass it to Check or NewRegistry (ComposeRegistry does
// both), which apply the ordinary rules of names, value types, numbers and
// aliases, among them that two properties of one type, own, inherited or from
// a trait, never share a name. With no overlay the result equals base.
//
// A violation of an overlay (CheckOverlay, a target that does not exist, is in
// sys or is not a standard definition, a value type a flat file cannot give,
// the overall limit) gives the errors and an empty result. A target that is
// deprecated is not an error: OverlayWarnings tells of it.
func ComposeOverlays(base Definitions, items []Overlay) (Definitions, []*kernel.Error) {
	ordered := slicesSortedByItem(items)
	errs := checkOverlaySet(ordered)
	out := base.Clone()
	bases := baseKinds(base)
	for _, o := range ordered {
		errs = append(errs, CheckOverlay(o)...)
		errs = append(errs, checkOverlayValueTypes(o, bases)...)
		props := orgProperties(o)
		switch o.Target.Kind {
		case TargetObjectType:
			errs = append(errs, addToType(&out, o, props)...)
		case TargetTrait:
			errs = append(errs, addToTrait(&out, o, props)...)
		}
	}
	if len(errs) > 0 {
		return Definitions{}, errs
	}
	return out, nil
}

// ComposeRegistry composes the overlays with the standard definitions and
// builds the registry, which assigns the numbers that are missing in the standard
// part (an overlay property already has its number). It gives every violation
// of the overlays or of the composed set, or the registry.
func ComposeRegistry(base Definitions, items []Overlay) (*Registry, []*kernel.Error) {
	defs, errs := ComposeOverlays(base, items)
	if len(errs) == 0 {
		errs = Check(defs)
	}
	if len(errs) > 0 {
		return nil, errs
	}
	r, err := NewRegistry(defs)
	if err != nil {
		return nil, []*kernel.Error{kernel.Wrap(kernel.CodeOf(err), err, "cannot build the registry")}
	}
	return r, nil
}

func slicesSortedByItem(items []Overlay) []Overlay {
	out := make([]Overlay, len(items))
	copy(out, items)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ItemID() < out[j].ItemID() })
	return out
}

// checkOverlaySet checks what only the whole list shows: a target twice and
// more properties than the limit.
func checkOverlaySet(items []Overlay) []*kernel.Error {
	var errs []*kernel.Error
	seen := map[string]bool{}
	total := 0
	for _, o := range items {
		key := o.Target.Kind + ":" + o.Target.ID
		if seen[key] {
			errs = append(errs, kernel.Errorf(CodeOverlayDuplicate, "two overlays have the target %s", o.Target.ID))
		}
		seen[key] = true
		total += len(o.Properties)
	}
	if total > MaxOrgProperties {
		errs = append(errs, kernel.Errorf(CodeOverlayLimit, "the overlays add %d properties: at most %d", total, MaxOrgProperties))
	}
	return errs
}

func baseKinds(d Definitions) map[ValueTypeID]BaseKind {
	out := map[ValueTypeID]BaseKind{}
	for _, vt := range builtinValueTypes() {
		out[vt.ID] = vt.Base
	}
	for _, vt := range d.ValueTypes {
		out[vt.ID] = vt.Base
	}
	return out
}

// checkOverlayValueTypes refuses a value type of a known base that a flat
// file cannot give. An unknown value type is the ordinary check's to report.
func checkOverlayValueTypes(o Overlay, bases map[ValueTypeID]BaseKind) []*kernel.Error {
	var errs []*kernel.Error
	for _, p := range o.Properties {
		base, known := bases[p.ValueType]
		if known && !oneOf(base, allowedOrgBases) {
			errs = append(errs, kernel.Errorf(CodeOverlayValueType,
				"property %s of overlay %s has the value type %s (%s): an organisation property takes a text, number, truth, time, enum or quantity (a link type is the way to point at another object)",
				p.ID, o.ItemID(), p.ValueType, base))
		}
	}
	return errs
}

// orgProperties returns the properties of the overlay as they are added: copies with the namespace org.
func orgProperties(o Overlay) []Property {
	props := cloneEach(o.Properties, Property.clone)
	for i := range props {
		props[i].Namespace = NamespaceOrg
	}
	return props
}

func addToType(out *Definitions, o Overlay, props []Property) []*kernel.Error {
	for i := range out.ObjectTypes {
		t := &out.ObjectTypes[i]
		if t.ID.String() != o.Target.ID {
			continue
		}
		switch t.Namespace {
		case NamespaceStd:
			t.Properties = append(t.Properties, props...)
			return nil
		case NamespaceSys:
			return []*kernel.Error{kernel.Errorf(CodeOverlayTarget, "overlay %s targets %s, a sys type: sys types are the product's own", o.ItemID(), o.Target.ID)}
		}
		return []*kernel.Error{kernel.Errorf(CodeOverlayTarget, "overlay %s targets %s, which is in the namespace %s: an overlay targets a standard type", o.ItemID(), o.Target.ID, t.Namespace)}
	}
	return []*kernel.Error{kernel.Errorf(CodeOverlayTarget, "overlay %s targets the object type %s, which does not exist", o.ItemID(), o.Target.ID)}
}

func addToTrait(out *Definitions, o Overlay, props []Property) []*kernel.Error {
	for i := range out.Traits {
		if out.Traits[i].ID.String() == o.Target.ID {
			out.Traits[i].Properties = append(out.Traits[i].Properties, props...)
			return nil
		}
	}
	return []*kernel.Error{kernel.Errorf(CodeOverlayTarget, "overlay %s targets the trait %s, which does not exist", o.ItemID(), o.Target.ID)}
}

// OverlayWarning is something about an overlay that deserves a look but does not stop it.
type OverlayWarning struct {
	// ItemID is the overlay item the warning is about.
	ItemID  string
	Message string
}

// OverlayWarnings lists what does not stop an overlay but is worth a look: a
// target the standard has deprecated, and a property whose short name (the
// name without org.) is, or looks like (the same without underscores), the
// name of a property the target already has from the standard. The standard
// may have added the property after the organisation did; the organisation
// decides whether to move to it, nothing moves by itself.
func OverlayWarnings(base Definitions, items []Overlay) []OverlayWarning {
	var out []OverlayWarning
	for _, o := range slicesSortedByItem(items) {
		status, standard := targetFacts(base, o.Target)
		if status == StatusDeprecated {
			out = append(out, OverlayWarning{o.ItemID(), "the target " + o.Target.ID + " is deprecated in the standard"})
		}
		for _, p := range o.Properties {
			short := strings.TrimPrefix(p.Name, "org.")
			for _, name := range standard {
				if squash(name) == squash(short) {
					out = append(out, OverlayWarning{o.ItemID(), "the property " + p.Name + " looks like the standard property " + name + " of " + o.Target.ID + ": consider moving to it"})
				}
			}
		}
	}
	return out
}

func squash(name string) string { return strings.ReplaceAll(name, "_", "") }

// targetFacts gives the status of the target and the names of the standard
// properties it has: its own, its ancestors', those of its traits, or, for a
// trait, its own. A target that does not exist has neither.
func targetFacts(d Definitions, target OverlayTarget) (Status, []string) {
	c := newChecker(&d)
	switch target.Kind {
	case TargetTrait:
		for i := range d.Traits {
			if d.Traits[i].ID.String() == target.ID {
				return StatusActive, propertyNames(propertyPointers(d.Traits[i].Properties))
			}
		}
	case TargetObjectType:
		for i := range d.ObjectTypes {
			if d.ObjectTypes[i].ID.String() == target.ID {
				return statusOf(d.ObjectTypes[i].Status), propertyNames(c.effectiveProps(&d.ObjectTypes[i]))
			}
		}
	}
	return StatusActive, nil
}

func propertyPointers(props []Property) []*Property {
	out := make([]*Property, len(props))
	for i := range props {
		out[i] = &props[i]
	}
	return out
}

func propertyNames(props []*Property) []string {
	var out []string
	for _, p := range props {
		if p.Namespace == "" {
			out = append(out, p.Name)
		}
	}
	return out
}
