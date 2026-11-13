package devload

import (
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// resolveAll gives every loadable object its id and fills w.order with parents before the objects they
// identify. An object is loadable when no source is blocked from delivering it (it is ingestible) and
// one of its identity keys can be formed.
func (w *world) resolveAll() error {
	for _, o := range w.objects {
		if err := w.resolve(o); err != nil {
			return err
		}
	}
	return nil
}

func (w *world) resolve(o *object) error {
	if o.state != stateNew {
		return nil
	}
	o.state = stateResolving
	defer func() { o.state = stateDone }()
	if o.rec.NotIngestible != "" {
		return nil
	}
	for _, k := range o.typ.keys {
		key, formed, err := w.formKey(o, k)
		if err != nil {
			return err
		}
		if !formed {
			continue
		}
		id, err := graph.NewObjectID(o.typ.typ.ID, key)
		if err != nil {
			return err
		}
		o.key, o.id, o.ok = key, id, true
		w.order = append(w.order, o)
		return nil
	}
	return nil
}

// formKey builds the key parts of one identity key, or says that the object cannot form it.
func (w *world) formKey(o *object, k model.IdentityKey) ([]graph.KeyPart, bool, error) {
	if len(k.Components) == 0 {
		return w.schemePart(o, k)
	}
	parts := make([]graph.KeyPart, 0, len(k.Components))
	for _, c := range k.Components {
		part, formed, err := w.component(o, c)
		if err != nil || !formed {
			return nil, false, err
		}
		parts = append(parts, part)
	}
	return parts, true, nil
}

func (w *world) schemePart(o *object, k model.IdentityKey) ([]graph.KeyPart, bool, error) {
	text, ok := o.rec.Identifiers[k.Scheme.String()]
	if !ok {
		return nil, false, nil
	}
	sch, found := w.reg.IdentifierScheme(k.Scheme)
	if !found {
		return nil, false, kernel.Errorf(kernel.CodeNotFound, "identifier scheme %s is not in the model", k.Scheme)
	}
	// The generator's identifier values are placeholders that need not be valid for the scheme's normaliser
	// (a MAC address, a serial number): such a value forms no key.
	value, err := model.NormalizeValue(sch.Normalizer, text)
	if err != nil || value == "" {
		return nil, false, nil
	}
	return []graph.KeyPart{{Kind: graph.PartScheme, Ref: k.Scheme.String(), Value: value}}, true, nil
}

func (w *world) component(o *object, c model.KeyComponent) (graph.KeyPart, bool, error) {
	switch {
	case !c.Property.IsZero():
		return w.propertyPart(o, c)
	case c.Set:
		return w.setPart(o, c)
	}
	return w.contextPart(o, c)
}

func (w *world) propertyPart(o *object, c model.KeyComponent) (graph.KeyPart, bool, error) {
	ref, ok := w.reg.Property(c.Property)
	if !ok {
		return graph.KeyPart{}, false, kernel.Errorf(kernel.CodeNotFound, "key property %s is not in the model", c.Property)
	}
	raw, has := o.rec.Props[ref.Property.Name]
	if !has {
		return graph.KeyPart{}, false, nil
	}
	value, err := model.NormalizeValue(ref.Property.ValueType, textOf(raw))
	if err != nil || value == "" {
		return graph.KeyPart{}, false, err
	}
	return graph.KeyPart{Kind: graph.PartProperty, Ref: c.Property.String(), Value: value}, true, nil
}

// contextPart finds the object the link of the context type leads to; that object is resolved first.
func (w *world) contextPart(o *object, c model.KeyComponent) (graph.KeyPart, bool, error) {
	for _, l := range o.out {
		if l.typ.ID != c.Context {
			continue
		}
		if err := w.resolve(l.to); err != nil || !l.to.ok {
			return graph.KeyPart{}, false, err
		}
		return graph.KeyPart{Kind: graph.PartContext, Ref: c.Context.String(), Objects: []graph.ObjectID{l.to.id}}, true, nil
	}
	return graph.KeyPart{}, false, nil
}

// setPart collects the objects at the far end of the links of the type; all of them are resolved first.
func (w *world) setPart(o *object, c model.KeyComponent) (graph.KeyPart, bool, error) {
	var members []graph.ObjectID
	edges, far := o.out, func(l *link) *object { return l.to }
	if c.Incoming {
		edges, far = o.in, func(l *link) *object { return l.from }
	}
	for _, l := range edges {
		if l.typ.ID != c.Context {
			continue
		}
		m := far(l)
		if err := w.resolve(m); err != nil || !m.ok {
			return graph.KeyPart{}, false, err
		}
		members = append(members, m.id)
	}
	if len(members) == 0 || (c.Size > 0 && len(members) != c.Size) {
		return graph.KeyPart{}, false, nil
	}
	return graph.KeyPart{Kind: graph.PartSet, Ref: c.Context.String(), Objects: members}, true, nil
}
