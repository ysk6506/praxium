package devload

import (
	"slices"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/worldgen"
)

// sourceRef is the source reference of every value this loader wrote; the truth id of the record is the
// reference of the object or link itself.
const sourceRef = "devload/truth"

// attrBuilder turns the properties of a truth record into attributes and volatile writes.
type attrBuilder struct {
	reg *model.Registry
	// seen is the time text given to every value as the time the source saw it.
	seen string
}

func (b attrBuilder) base(p model.Property) (model.BaseKind, error) {
	vt, ok := b.reg.ValueType(p.ValueType)
	if !ok {
		return "", kernel.Errorf(kernel.CodeNotFound, "value type %s of property %s is not in the model", p.ValueType, p.Name)
	}
	return vt.Base, nil
}

// grades gives the grade of a property: the record's, assumed or unknown where the record says so.
func grades(rec *worldgen.Record, name string) model.Grade {
	switch {
	case slices.Contains(rec.Unknown, name):
		return model.GradeUnknown
	case slices.Contains(rec.Assumed, name):
		return model.GradeAssumed
	}
	return model.Grade(rec.Grade)
}

// attr builds one attribute; ok is false for a property that has no value and is not marked unknown.
func (b attrBuilder) attr(rec *worldgen.Record, p model.Property) (graph.Attr, bool, error) {
	grade := grades(rec, p.Name)
	raw, has := rec.Props[p.Name]
	if !has && grade != model.GradeUnknown {
		return graph.Attr{}, false, nil
	}
	a := graph.Attr{Grade: grade, Basis: model.Basis(rec.Basis), SourceTime: b.seen, SourceRef: sourceRef}
	if grade == model.GradeUnknown || !has {
		a.Grade = model.GradeUnknown
		return a, true, nil
	}
	base, err := b.base(p)
	if err != nil {
		return a, false, err
	}
	a.Value, err = convert(base, raw)
	return a, err == nil, err
}

// object fills the versioned attributes of an upsert and returns the volatile writes of the object.
func (b attrBuilder) object(o *object) (map[int]graph.Attr, []graph.VolatileWrite, error) {
	attrs := map[int]graph.Attr{}
	var vol []graph.VolatileWrite
	for _, name := range sortedNames(o.rec, o.typ.props) {
		p := o.typ.props[name]
		a, ok, err := b.attr(&o.rec, p)
		if err != nil {
			return nil, nil, kernel.Wrap(kernel.CodeInvalidValue, err, "object "+o.rec.ID+" property "+name)
		}
		if !ok {
			continue
		}
		if p.EffectiveHistory() == model.HistoryVolatile {
			vol = append(vol, graph.VolatileWrite{Object: o.id, AttrNo: p.AttrNo, Value: a.Value, Grade: a.Grade, Basis: a.Basis,
				SeenAt: a.SourceTime, Ref: a.SourceRef})
			continue
		}
		attrs[p.AttrNo] = a
	}
	return attrs, vol, nil
}

// linkAttrs fills the attributes of a link.
func (b attrBuilder) link(l *link) (map[int]graph.Attr, error) {
	props := map[string]model.Property{}
	for _, p := range l.typ.Properties {
		props[p.Name] = p
	}
	attrs := map[int]graph.Attr{}
	for _, name := range sortedNames(l.rec, props) {
		a, ok, err := b.attr(&l.rec, props[name])
		if err != nil {
			return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "link "+l.rec.ID+" property "+name)
		}
		if ok {
			attrs[props[name].AttrNo] = a
		}
	}
	return attrs, nil
}

// sortedNames lists the properties the record gives a value to or marks unknown, that the type has, in a fixed
// order. A name the type does not have is left out: the generator's own check (worldgen validate) is the one that finds it.
func sortedNames(rec worldgen.Record, props map[string]model.Property) []string {
	names := make([]string, 0, len(rec.Props)+len(rec.Unknown))
	for n := range rec.Props {
		if _, ok := props[n]; ok {
			names = append(names, n)
		}
	}
	for _, n := range rec.Unknown {
		if _, ok := props[n]; ok && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	return names
}

// nameOf is the name of an object: its name property, else the last part of its truth id.
func nameOf(o *object) string {
	if raw, ok := o.rec.Props["name"]; ok {
		if s := strings.TrimSpace(textOf(raw)); s != "" {
			return s
		}
	}
	return o.rec.ID
}
