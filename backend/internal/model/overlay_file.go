package model

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The file a person writes (ADR-0050). It names a target by its name instead
// of its id and a value type by its short name, and it may leave out the id
// and the number of a property, which are given when the property is first
// stored. It says the whole state: a property that was stored and is not in
// the file is a breaking change (ClassifyOverlay), so what the file says is
// what the model has.
//
//	{ "format": "praxium-org-model", "version": 1,
//	  "overlays": [ { "type": "PhysicalServer", "properties": [
//	      {"name": "org.management_department", "value_type": "string", "display_names": {"ko": "관리 부서"}} ] } ] }
//
// An overlay names either "type" (an object type) or "trait".
const (
	// OrgModelFormat is the format name of the file.
	OrgModelFormat = "praxium-org-model"
	// OrgModelVersion is the version of the format.
	OrgModelVersion = 1
	// MaxOrgModelFileBytes is the most bytes of a file.
	MaxOrgModelFileBytes = 1 << 20
)

type orgModelFile struct {
	Format   string           `json:"format"`
	Version  int              `json:"version"`
	Overlays []orgModelTarget `json:"overlays"`
}

type orgModelTarget struct {
	Type       string            `json:"type,omitempty"`
	Trait      string            `json:"trait,omitempty"`
	Properties []orgModelPropert `json:"properties"`
}

// orgModelPropert is a property as the file writes it. It has the fields that
// organisation properties do not take, so that they are refused with the
// reason and not as an unknown field.
type orgModelPropert struct {
	ID           string            `json:"id,omitempty"`
	AttrNo       int               `json:"attr_no,omitempty"`
	Name         string            `json:"name"`
	Aliases      []string          `json:"aliases,omitempty"`
	ValueType    string            `json:"value_type"`
	Unit         string            `json:"unit,omitempty"`
	EnumValues   []string          `json:"enum_values,omitempty"`
	ChangeClass  ChangeClass       `json:"change_class,omitempty"`
	Sensitivity  Sensitivity       `json:"sensitivity,omitempty"`
	Status       Status            `json:"status,omitempty"`
	DisplayNames map[string]string `json:"display_names,omitempty"`
	Description  string            `json:"description,omitempty"`
	Required     bool              `json:"required,omitempty"`
	ScopeKey     bool              `json:"scope_key,omitempty"`
	HistoryKind  HistoryKind       `json:"history_kind,omitempty"`
	SupersededBy string            `json:"superseded_by,omitempty"`
}

// ParseOrgModelFile reads the file a person writes into overlays, in the
// order of their targets' item ids. base gives the names of the targets and
// of the value types. A field it does not know, a target or a value type it
// cannot find, a target twice and a file over MaxOrgModelFileBytes are errors.
// A property without an id or a number is left without; FillOverlays gives
// them. The overlays are not checked further: CheckOverlay and ComposeOverlays do that.
func ParseOrgModelFile(data []byte, base Definitions) ([]Overlay, error) {
	if len(data) > MaxOrgModelFileBytes {
		return nil, kernel.Errorf(CodeOverlayLimit, "the file has %d bytes: at most %d", len(data), MaxOrgModelFileBytes)
	}
	var file orgModelFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, kernel.Wrap(CodeDataInvalid, err, "the file is not valid")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, kernel.Errorf(CodeDataInvalid, "the file has text after the JSON value")
	}
	if file.Format != OrgModelFormat || file.Version != OrgModelVersion {
		return nil, kernel.Errorf(CodeDataInvalid, "the file has format %q version %d: want %s version %d", file.Format, file.Version, OrgModelFormat, OrgModelVersion)
	}
	names := valueTypeNames(base)
	var out []Overlay
	seen := map[string]bool{}
	for _, entry := range file.Overlays {
		o, err := entry.overlay(base, names)
		if err != nil {
			return nil, err
		}
		if seen[o.ItemID()] {
			return nil, kernel.Errorf(CodeOverlayDuplicate, "the file has two overlays for %s", o.Target.ID)
		}
		seen[o.ItemID()] = true
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ItemID() < out[j].ItemID() })
	return out, nil
}

// valueTypeNames maps the short name of every value type to its id.
func valueTypeNames(base Definitions) map[string]ValueTypeID {
	out := map[string]ValueTypeID{}
	for _, vt := range append(builtinValueTypes(), base.ValueTypes...) {
		out[vt.Name] = vt.ID
	}
	return out
}

func (e orgModelTarget) overlay(base Definitions, names map[string]ValueTypeID) (Overlay, error) {
	kind, id, err := e.resolveTarget(base)
	if err != nil {
		return Overlay{}, err
	}
	props := make([]Property, len(e.Properties))
	for i, fp := range e.Properties {
		p, err := fp.property(names)
		if err != nil {
			return Overlay{}, err
		}
		props[i] = p
	}
	return NewOverlay(kind, id, props), nil
}

// resolveTarget finds the target by its name or one of its aliases.
func (e orgModelTarget) resolveTarget(base Definitions) (kind, id string, err error) {
	switch {
	case e.Type != "" && e.Trait != "":
		return "", "", kernel.Errorf(CodeOverlayTarget, "an overlay names a type or a trait, not both (%s and %s)", e.Type, e.Trait)
	case e.Type != "":
		for _, t := range base.ObjectTypes {
			if t.Name == e.Type || containsString(t.Aliases, e.Type) {
				return TargetObjectType, t.ID.String(), nil
			}
		}
	case e.Trait != "":
		for _, t := range base.Traits {
			if t.Name == e.Trait || containsString(t.Aliases, e.Trait) {
				return TargetTrait, t.ID.String(), nil
			}
		}
	default:
		return "", "", kernel.Errorf(CodeOverlayTarget, "an overlay names no type and no trait")
	}
	return "", "", kernel.Errorf(CodeOverlayTarget, "the overlay target %q does not exist", e.Type+e.Trait)
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// property turns a property of the file into a Property. The value type is a
// short name (string) or an id (vt.string); an empty change class is slow.
func (fp orgModelPropert) property(names map[string]ValueTypeID) (Property, error) {
	p := Property{
		AttrNo: fp.AttrNo, Name: fp.Name, Aliases: fp.Aliases, Unit: fp.Unit, EnumValues: fp.EnumValues,
		ChangeClass: fp.ChangeClass, Sensitivity: fp.Sensitivity, Status: fp.Status, DisplayNames: fp.DisplayNames,
		Description: fp.Description, Required: fp.Required, ScopeKey: fp.ScopeKey, HistoryKind: fp.HistoryKind,
	}
	if p.ChangeClass == "" {
		p.ChangeClass = ChangeSlow
	}
	var err error
	if fp.ID != "" {
		if p.ID, err = kernel.ParseID[PropertyKind](fp.ID); err != nil {
			return p, kernel.Wrap(CodeOverlayName, err, "the property "+fp.Name+" has an id that is not valid")
		}
	}
	if fp.SupersededBy != "" {
		if p.SupersededBy, err = kernel.ParseID[PropertyKind](fp.SupersededBy); err != nil {
			return p, kernel.Wrap(CodeOverlayField, err, "the property "+fp.Name+" has a successor id that is not valid")
		}
	}
	id, ok := names[strings.TrimPrefix(fp.ValueType, "vt.")]
	if !ok {
		return p, kernel.Errorf(CodeValueTypeUnknown, "the property %s has the value type %q, which does not exist", fp.Name, fp.ValueType)
	}
	p.ValueType = id
	return p, nil
}

// FormatOrgModelFile writes overlays as a file a person can read and edit,
// with the id and the number of every property, so that writing it back keeps them.
func FormatOrgModelFile(base Definitions, items []Overlay) ([]byte, error) {
	file := orgModelFile{Format: OrgModelFormat, Version: OrgModelVersion, Overlays: []orgModelTarget{}}
	for _, o := range slicesSortedByItem(items) {
		entry := orgModelTarget{Properties: []orgModelPropert{}}
		name, ok := targetName(base, o.Target)
		if !ok {
			return nil, kernel.Errorf(CodeOverlayTarget, "overlay %s targets %s, which does not exist", o.ItemID(), o.Target.ID)
		}
		if o.Target.Kind == TargetTrait {
			entry.Trait = name
		} else {
			entry.Type = name
		}
		for _, p := range o.Properties {
			entry.Properties = append(entry.Properties, fileProperty(p))
		}
		file.Overlays = append(file.Overlays, entry)
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot write the file")
	}
	return append(data, '\n'), nil
}

func fileProperty(p Property) orgModelPropert {
	return orgModelPropert{
		ID: p.ID.String(), AttrNo: p.AttrNo, Name: p.Name, Aliases: p.Aliases,
		ValueType: strings.TrimPrefix(p.ValueType.String(), "vt."), Unit: p.Unit, EnumValues: p.EnumValues,
		ChangeClass: p.ChangeClass, Sensitivity: p.Sensitivity, Status: p.Status,
		DisplayNames: p.DisplayNames, Description: p.Description,
		Required: p.Required, ScopeKey: p.ScopeKey, HistoryKind: p.HistoryKind, SupersededBy: p.SupersededBy.String(),
	}
}

// targetName is the name of the target in the definitions.
func targetName(base Definitions, target OverlayTarget) (string, bool) {
	switch target.Kind {
	case TargetObjectType:
		for _, t := range base.ObjectTypes {
			if t.ID.String() == target.ID {
				return t.Name, true
			}
		}
	case TargetTrait:
		for _, t := range base.Traits {
			if t.ID.String() == target.ID {
				return t.Name, true
			}
		}
	}
	return "", false
}

var wordStart = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// snakeOf turns a PascalCase name into snake_case: PhysicalServer into physical_server.
func snakeOf(name string) string {
	return strings.ToLower(wordStart.ReplaceAllString(name, "${1}_${2}"))
}

// FillOverlays gives a property without an id or a number the ones it should
// have, and returns the overlays; it does not change its arguments. It is a
// pure function: the numbers come from issue, which the store backs with its
// ledger (the numbers are never given twice, even when a write is dropped).
//
//   - A property without an id that has the name or an alias of a stored property
//     of the same target is that property: it keeps its id and its number.
//   - A property with an id that is stored keeps the stored number; a number
//     written in the file for a stored property is kept as written, so that
//     ClassifyOverlay sees the change.
//   - Any other property gets the id pr.org.<target in snake_case>.<short name>,
//     with _2, _3 and so on when the id is taken by a stored property, a
//     standard one or another in the file, and a number from issue.
//
// issue is called in the order of the item ids, then of the properties.
func FillOverlays(base Definitions, stored, wanted []Overlay, issue func() int) []Overlay {
	taken := takenIDs(base, stored, wanted)
	storedBy := map[string]Overlay{}
	for _, o := range stored {
		storedBy[o.ItemID()] = o
	}
	out := make([]Overlay, 0, len(wanted))
	for _, o := range slicesSortedByItem(wanted) {
		o = o.Clone()
		old := storedBy[o.ItemID()]
		name, _ := targetName(base, o.Target)
		for i := range o.Properties {
			fillProperty(&o.Properties[i], old, snakeOf(name), taken, issue)
		}
		out = append(out, o)
	}
	return out
}

// fillProperty fills one property from what is stored, or newly.
func fillProperty(p *Property, old Overlay, targetSnake string, taken map[string]bool, issue func() int) {
	if p.ID.IsZero() {
		if match, ok := findStored(old, p.Name); ok {
			p.ID = match.ID
			if p.AttrNo == 0 {
				p.AttrNo = match.AttrNo
			}
		}
	}
	if p.ID.IsZero() {
		p.ID = newPropertyID(targetSnake, p.Name, taken)
	}
	if p.AttrNo == 0 {
		if match, ok := storedByID(old, p.ID); ok {
			p.AttrNo = match.AttrNo
		} else {
			p.AttrNo = issue()
		}
	}
}

func findStored(old Overlay, name string) (Property, bool) {
	for _, s := range old.Properties {
		if s.Name == name || containsString(s.Aliases, name) {
			return s, true
		}
	}
	return Property{}, false
}

func storedByID(old Overlay, id PropertyID) (Property, bool) {
	for _, s := range old.Properties {
		if s.ID == id {
			return s, true
		}
	}
	return Property{}, false
}

// takenIDs are the property ids that a new one must not be: those of the standard, the stored overlays and the file.
func takenIDs(base Definitions, stored, wanted []Overlay) map[string]bool {
	taken := map[string]bool{}
	mark := func(props []Property) {
		for _, p := range props {
			taken[p.ID.String()] = true
		}
	}
	for _, t := range base.Traits {
		mark(t.Properties)
	}
	for _, t := range base.ObjectTypes {
		mark(t.Properties)
	}
	for _, l := range base.LinkTypes {
		mark(l.Properties)
	}
	for _, o := range append(append([]Overlay(nil), stored...), wanted...) {
		mark(o.Properties)
	}
	return taken
}

// newPropertyID makes pr.org.<target>.<short name> and marks it taken, adding _2, _3 until it is free.
func newPropertyID(targetSnake, name string, taken map[string]bool) PropertyID {
	short := strings.TrimPrefix(name, "org.")
	base := "pr.org." + targetSnake + "." + short
	id := base
	for n := 2; taken[id]; n++ {
		id = base + "_" + strconv.Itoa(n)
	}
	taken[id] = true
	parsed, err := kernel.ParseID[PropertyKind](id)
	if err != nil {
		return PropertyID{}
	}
	return parsed
}
