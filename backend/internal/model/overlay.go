package model

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Overlays (ADR-0050, kg-model.md 3.7). An organisation adds its own
// properties to a standard object type or trait without touching the standard
// files: the properties are an overlay, one definition item per target. An
// overlay only adds properties. It cannot take one away, rename a standard
// property or add a type; a property it adds is retired by marking it
// deprecated, never deleted, because rows and old versions of the graph keep
// pointing at its number.

// The kind and the schema version of the body of an overlay item.
const (
	OverlayKind          = "type_overlay"
	OverlaySchemaVersion = 1
)

// The kinds of target an overlay may have.
const (
	TargetObjectType = "object_type"
	TargetTrait      = "trait"
)

// The limits of an overlay (ADR-0050).
const (
	// MaxOverlayBytes is the most bytes of the body of one overlay item.
	MaxOverlayBytes = 256 << 10
	// MaxOverlayProperties is the most properties of one overlay.
	MaxOverlayProperties = 100
	// MaxOrgProperties is the most properties that all overlays together add.
	MaxOrgProperties = 1000
	// MaxEnumValues is the most values of an enum property.
	MaxEnumValues = 100
	// MaxEnumValueLength is the most characters of one enum value.
	MaxEnumValueLength = 64
	// MaxDisplayNameLength is the most characters of one display name.
	MaxDisplayNameLength = 100
	// MaxDescriptionLength is the most characters of a description.
	MaxDescriptionLength = 1000
)

// OverlayTarget names what an overlay adds its properties to.
type OverlayTarget struct {
	// Kind is TargetObjectType or TargetTrait.
	Kind string `json:"kind"`
	// ID is the id of the standard object type or trait.
	ID string `json:"id"`
}

// Overlay is the body of a type_overlay item: the properties one organisation
// adds to one target. In memory every property has the namespace org; the
// body does not write it (Body, ParseOverlay).
type Overlay struct {
	SchemaVersion int           `json:"schema_version"`
	Kind          string        `json:"kind"`
	Target        OverlayTarget `json:"target"`
	Properties    []Property    `json:"properties"`
}

// ItemID is the id of the definition item: "ov." and the id of the target.
// The target id never changes, so the item id never does.
func (o Overlay) ItemID() string { return "ov." + o.Target.ID }

// NewOverlay makes an overlay of the properties, which get the namespace org.
func NewOverlay(kind, targetID string, props []Property) Overlay {
	o := Overlay{SchemaVersion: OverlaySchemaVersion, Kind: OverlayKind, Target: OverlayTarget{Kind: kind, ID: targetID}}
	o.Properties = make([]Property, len(props))
	for i, p := range props {
		p = p.clone()
		p.Namespace = NamespaceOrg
		o.Properties[i] = p
	}
	return o
}

// Clone returns a copy that shares nothing with o.
func (o Overlay) Clone() Overlay {
	o.Properties = cloneEach(o.Properties, Property.clone)
	return o
}

// ParseOverlay reads the body of an overlay item. It refuses a body over
// MaxOverlayBytes, a field it does not know and text after the JSON value. The
// result is not checked: CheckOverlay does that.
func ParseOverlay(data []byte) (Overlay, error) {
	var o Overlay
	if len(data) > MaxOverlayBytes {
		return o, kernel.Errorf(CodeOverlayLimit, "an overlay body has %d bytes: at most %d", len(data), MaxOverlayBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&o); err != nil {
		return Overlay{}, kernel.Wrap(CodeDataInvalid, err, "an overlay body is not valid")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return Overlay{}, kernel.Errorf(CodeDataInvalid, "an overlay body has text after the JSON value")
	}
	for i := range o.Properties {
		switch o.Properties[i].Namespace {
		case "", NamespaceOrg:
			o.Properties[i].Namespace = NamespaceOrg
		default:
			return Overlay{}, kernel.Errorf(CodeOverlayField, "property %s has namespace %q: an overlay adds org properties", o.Properties[i].ID, o.Properties[i].Namespace)
		}
	}
	return o, nil
}

// bodyProperty is a property as the body writes it. An id is a struct, which
// omitempty does not leave out, so the successor is a pointer: nil is left out.
type bodyProperty struct {
	Property
	SupersededBy *PropertyID `json:"superseded_by,omitempty"`
}

type bodyOverlay struct {
	SchemaVersion int            `json:"schema_version"`
	Kind          string         `json:"kind"`
	Target        OverlayTarget  `json:"target"`
	Properties    []bodyProperty `json:"properties"`
}

// Body is the JSON of the overlay as it is stored: the properties without
// their namespace, which is always org. The same overlay always gives the same bytes.
func (o Overlay) Body() ([]byte, error) {
	c := bodyOverlay{SchemaVersion: o.SchemaVersion, Kind: o.Kind, Target: o.Target, Properties: make([]bodyProperty, len(o.Properties))}
	for i, p := range o.Properties {
		p = p.clone()
		p.Namespace = ""
		c.Properties[i] = bodyProperty{Property: p}
		if !p.SupersededBy.IsZero() {
			successor := p.SupersededBy
			c.Properties[i].SupersededBy = &successor
		}
	}
	data, err := json.Marshal(c)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot write an overlay body")
	}
	return data, nil
}

// CheckOverlay applies the rules of an overlay that need nothing but the
// overlay: the item, the target form, and each property (id, name, number,
// the fields and values that organisation properties do not take yet, and the
// limits). That the target exists and what a value type is are for ComposeOverlays.
func CheckOverlay(o Overlay) []*kernel.Error {
	var errs []*kernel.Error
	fail := func(code kernel.Code, format string, args ...any) {
		errs = append(errs, kernel.Errorf(code, format, args...))
	}
	where := "overlay " + o.ItemID()
	if o.SchemaVersion != OverlaySchemaVersion {
		fail(CodeDataInvalid, "%s has schema_version %d: want %d", where, o.SchemaVersion, OverlaySchemaVersion)
	}
	if o.Kind != OverlayKind {
		fail(CodeDataInvalid, "%s has kind %q: want %s", where, o.Kind, OverlayKind)
	}
	if o.Target.Kind != TargetObjectType && o.Target.Kind != TargetTrait {
		fail(CodeOverlayTarget, "%s has target kind %q: want object_type or trait", where, o.Target.Kind)
	}
	if o.Target.ID == "" {
		fail(CodeOverlayTarget, "an overlay has no target id")
	}
	if len(o.Properties) > MaxOverlayProperties {
		fail(CodeOverlayLimit, "%s has %d properties: at most %d", where, len(o.Properties), MaxOverlayProperties)
	}
	seen := map[PropertyID]bool{}
	for _, p := range o.Properties {
		if seen[p.ID] && !p.ID.IsZero() {
			fail(CodeOverlayDuplicate, "%s has the property id %s twice", where, p.ID)
		}
		seen[p.ID] = true
		errs = append(errs, checkOverlayProperty(where, p)...)
	}
	return errs
}

func checkOverlayProperty(where string, p Property) []*kernel.Error {
	what := "property " + p.ID.String() + " of " + where
	var errs []*kernel.Error
	fail := func(code kernel.Code, format string, args ...any) {
		errs = append(errs, kernel.Errorf(code, "%s "+format, append([]any{what}, args...)...))
	}
	if p.ID.IsZero() {
		fail(CodeRequiredField, "has no id")
	} else if !strings.HasPrefix(p.ID.String(), "pr.org.") {
		fail(CodeOverlayName, "has an id that does not begin with pr.org.")
	}
	if !ValidPropertyName(NamespaceOrg, p.Name) {
		fail(CodeOverlayName, "has the name %q: want org. and snake_case, at most %d characters", p.Name, MaxPropertyNameLength)
	}
	for _, alias := range p.Aliases {
		if !ValidPropertyName(NamespaceOrg, alias) {
			fail(CodeOverlayName, "has the alias %q: want org. and snake_case, at most %d characters", alias, MaxPropertyNameLength)
		}
	}
	switch {
	case p.AttrNo == 0:
		fail(CodeOverlayNumber, "has no number: a number of %d or more is given when the property is first stored", OrgNumberBase)
	case p.AttrNo < OrgNumberBase:
		fail(CodeOverlayNumber, "has the number %d: want %d or more (the numbers below are the standard's)", p.AttrNo, OrgNumberBase)
	}
	errs = append(errs, checkOverlayFields(what, p)...)
	return append(errs, checkOverlayLimits(what, p)...)
}

// checkOverlayFields refuses what organisation properties do not take yet.
func checkOverlayFields(what string, p Property) []*kernel.Error {
	var errs []*kernel.Error
	refuse := func(format string, args ...any) {
		errs = append(errs, kernel.Errorf(CodeOverlayField, what+" "+format, args...))
	}
	if p.Required {
		refuse("is required: required properties wait for the fill rate to mean something")
	}
	if p.ScopeKey {
		refuse("is a scope key: the stored rows have no scope column yet")
	}
	if p.HistoryKind != "" {
		refuse("sets history_kind: it follows from the change class")
	}
	if !p.SupersededBy.IsZero() {
		refuse("names a successor: an organisation property is retired by status deprecated alone")
	}
	switch p.ChangeClass {
	case ChangeStatic, ChangeSlow:
	case ChangeDynamic:
		refuse("has change_class dynamic: a property that changes all the time comes with flow ingestion")
	default:
		refuse("has change_class %q: want static or slow", p.ChangeClass)
	}
	if p.Sensitivity != "" && !oneOf(p.Sensitivity, []Sensitivity{SensitivityOpen, SensitivityRestricted, SensitivityConfidential}) {
		errs = append(errs, kernel.Errorf(CodeClosedList, "%s has sensitivity %q: want open, restricted or confidential", what, p.Sensitivity))
	}
	if !validStatus(statusOf(p.Status)) {
		errs = append(errs, kernel.Errorf(CodeClosedList, "%s has status %q: want experimental, active or deprecated", what, p.Status))
	}
	return errs
}

// checkOverlayLimits checks the sizes of the values of a property.
func checkOverlayLimits(what string, p Property) []*kernel.Error {
	var errs []*kernel.Error
	over := func(format string, args ...any) {
		errs = append(errs, kernel.Errorf(CodeOverlayLimit, what+" "+format, args...))
	}
	if len(p.EnumValues) > MaxEnumValues {
		over("has %d enum values: at most %d", len(p.EnumValues), MaxEnumValues)
	}
	for _, value := range p.EnumValues {
		if utf8.RuneCountInString(value) > MaxEnumValueLength {
			over("has the enum value %q of more than %d characters", value, MaxEnumValueLength)
		}
	}
	if utf8.RuneCountInString(p.Description) > MaxDescriptionLength {
		over("has a description of more than %d characters", MaxDescriptionLength)
	}
	for _, lang := range slices.Sorted(maps.Keys(p.DisplayNames)) {
		name := p.DisplayNames[lang]
		if lang == "" || name == "" {
			errs = append(errs, kernel.Errorf(CodeOverlayField, "%s has a display name without a language or without a text", what))
		}
		if utf8.RuneCountInString(name) > MaxDisplayNameLength {
			over("has the display name %q of more than %d characters", lang, MaxDisplayNameLength)
		}
	}
	return errs
}
