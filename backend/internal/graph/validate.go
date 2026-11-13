package graph

import (
	"math"
	"strconv"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

func invalid(format string, args ...any) error {
	return kernel.Errorf(CodeBatchInvalid, format, args...)
}

// validator checks batch items against the registry. It caches the property
// sets of the types it has seen.
type validator struct {
	reg   *model.Registry
	props map[string]map[int]model.Property
}

func newValidator(reg *model.Registry) *validator {
	return &validator{reg: reg, props: map[string]map[int]model.Property{}}
}

func (v *validator) objectProps(id model.ObjectTypeID) map[int]model.Property {
	if m, ok := v.props[id.String()]; ok {
		return m
	}
	m := map[int]model.Property{}
	for _, p := range v.reg.EffectiveProperties(id) {
		m[p.AttrNo] = p
	}
	v.props[id.String()] = m
	return m
}

func (v *validator) linkProps(lt model.LinkType) map[int]model.Property {
	if m, ok := v.props[lt.ID.String()]; ok {
		return m
	}
	m := map[int]model.Property{}
	for _, p := range lt.Properties {
		m[p.AttrNo] = p
	}
	v.props[lt.ID.String()] = m
	return m
}

// object checks an upsert and derives its id.
func (v *validator) object(u ObjectUpsert) (model.ObjectType, ObjectID, error) {
	typ, ok := v.reg.ObjectType(u.Type)
	if !ok {
		return typ, ObjectID{}, invalid("unknown object type %q", u.Type)
	}
	if u.Name == "" {
		return typ, ObjectID{}, invalid("an object of type %s has no name", typ.Name)
	}
	if err := v.key(typ, u.Key); err != nil {
		return typ, ObjectID{}, err
	}
	if err := v.attrs(v.objectProps(typ.ID), u.Attrs, true); err != nil {
		return typ, ObjectID{}, err
	}
	id, err := NewObjectID(typ.ID, u.Key)
	return typ, id, err
}

// key checks that the components have the shape of one identity key of the
// type: same kinds and same refs in the same order, and the parts' content.
func (v *validator) key(typ model.ObjectType, key []KeyPart) error {
	if len(key) == 0 {
		return invalid("an object of type %s has no identity key", typ.Name)
	}
	for _, k := range v.reg.IdentityKeys(typ.ID) {
		if keyMatches(k, key) {
			return checkParts(key)
		}
	}
	return invalid("the key of an object of type %s matches none of its identity keys", typ.Name)
}

func partMatches(c model.KeyComponent, p KeyPart) bool {
	switch {
	case !c.Property.IsZero():
		return p.Kind == PartProperty && p.Ref == c.Property.String()
	case c.Set:
		return p.Kind == PartSet && p.Ref == c.Context.String()
	}
	return p.Kind == PartContext && p.Ref == c.Context.String()
}

func keyMatches(k model.IdentityKey, key []KeyPart) bool {
	if len(k.Components) == 0 {
		return len(key) == 1 && key[0].Kind == PartScheme && key[0].Ref == k.Scheme.String()
	}
	if len(k.Components) != len(key) {
		return false
	}
	for i, c := range k.Components {
		if !partMatches(c, key[i]) {
			return false
		}
	}
	return true
}

func checkParts(key []KeyPart) error {
	for _, p := range key {
		switch p.Kind {
		case PartProperty, PartScheme:
			if p.Value == "" || len(p.Objects) != 0 {
				return invalid("key part %s needs a value and no objects", p.Ref)
			}
		case PartContext:
			if len(p.Objects) != 1 || p.Value != "" {
				return invalid("context part %s needs exactly one object", p.Ref)
			}
		case PartSet:
			if len(p.Objects) == 0 || p.Value != "" {
				return invalid("set part %s needs at least one object", p.Ref)
			}
		default:
			return invalid("unknown key part kind %q", p.Kind)
		}
	}
	return nil
}

// link checks an upsert (not its ends: the planner sees the state) and derives its id.
func (v *validator) link(u LinkUpsert) (model.LinkType, LinkID, error) {
	lt, ok := v.reg.LinkType(u.Type)
	if !ok {
		return lt, LinkID{}, invalid("unknown link type %q", u.Type)
	}
	if !u.Grade.Valid() || !u.Basis.Valid() {
		return lt, LinkID{}, invalid("a link of type %s needs an evidence grade and a basis", lt.Name)
	}
	if err := v.attrs(v.linkProps(lt), u.Attrs, false); err != nil {
		return lt, LinkID{}, err
	}
	id, err := NewLinkID(lt.ID, u.Src, u.Dst, u.Discriminator)
	return lt, id, err
}

func (v *validator) attrs(props map[int]model.Property, attrs map[int]Attr, object bool) error {
	for no, a := range attrs {
		p, ok := props[no]
		if !ok {
			return invalid("attribute number %d does not belong to the type", no)
		}
		if p.EffectiveHistory() == model.HistoryVolatile {
			return kernel.Errorf(CodeVolatileInRow, "attribute %s is volatile: write it as a volatile value", p.Name)
		}
		if !a.Grade.Valid() || !a.Basis.Valid() {
			return invalid("attribute %s needs an evidence grade and a basis", p.Name)
		}
		if err := v.value(p, a.Value, a.Grade); err != nil {
			return err
		}
	}
	return nil
}

// volatile checks a volatile write against the type of its object.
func (v *validator) volatile(typ model.ObjectType, w VolatileWrite) error {
	p, ok := v.objectProps(typ.ID)[w.AttrNo]
	if !ok {
		return invalid("attribute number %d does not belong to type %s", w.AttrNo, typ.Name)
	}
	if p.EffectiveHistory() != model.HistoryVolatile {
		return invalid("attribute %s is versioned: it belongs in the row", p.Name)
	}
	if !w.Grade.Valid() || !w.Basis.Valid() {
		return invalid("volatile attribute %s needs an evidence grade and a basis", p.Name)
	}
	return v.value(p, w.Value, w.Grade)
}

// value checks a value against the value type of its property.
func (v *validator) value(p model.Property, val any, grade model.Grade) error {
	if val == nil {
		if grade != model.GradeUnknown {
			return invalid("attribute %s has no value but a grade other than unknown", p.Name)
		}
		return nil
	}
	if err := encodable(val); err != nil {
		return invalid("attribute %s: %v", p.Name, err)
	}
	vt, ok := v.reg.ValueType(p.ValueType)
	if !ok {
		return invalid("attribute %s has an unknown value type", p.Name)
	}
	if err := checkBase(vt.Base, p, val); err != nil {
		return err
	}
	if s, isText := val.(string); isText {
		if _, err := model.NormalizeValue(p.ValueType, s); err != nil {
			return invalid("attribute %s: %v", p.Name, err)
		}
	}
	return nil
}

func checkBase(base model.BaseKind, p model.Property, val any) error {
	if !fitsBase(base, p, val) {
		return invalid("attribute %s: the value does not fit value type %s", p.Name, base)
	}
	return nil
}

func fitsText(base model.BaseKind, p model.Property, val any) bool {
	s, ok := val.(string)
	switch base {
	case model.BaseTime:
		return ok && validTime(s)
	case model.BaseEnum:
		return ok && inList(p.EnumValues, s)
	}
	return ok
}

func fitsBase(base model.BaseKind, p model.Property, val any) bool {
	switch base {
	case model.BaseString, model.BaseRef, model.BaseTime, model.BaseEnum:
		return fitsText(base, p, val)
	case model.BaseInteger:
		_, ok := val.(int64)
		return ok
	case model.BaseBoolean:
		_, ok := val.(bool)
		return ok
	case model.BaseNumber:
		return isNumber(val)
	case model.BaseQuantity:
		_, ok := val.(map[string]any)
		return ok || isNumber(val)
	case model.BaseList:
		_, ok := val.([]any)
		return ok
	case model.BaseMap:
		_, ok := val.(map[string]any)
		return ok
	}
	return false
}

func inList(list []string, s string) bool {
	if len(list) == 0 {
		return true
	}
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func validTime(s string) bool {
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

// isNumber accepts an int64 or a decimal text: floats have no canonical form.
func isNumber(val any) bool {
	switch x := val.(type) {
	case int64:
		return true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	return false
}

// encodable accepts the value forms the canonical encoding knows.
func encodable(val any) error {
	switch x := val.(type) {
	case nil, string, int64, bool:
		return nil
	case []any:
		for _, e := range x {
			if err := encodable(e); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		for _, e := range x {
			if err := encodable(e); err != nil {
				return err
			}
		}
		return nil
	}
	return invalid("a value of Go type %T has no canonical form", val)
}
