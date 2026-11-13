package devload

import (
	"io"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/worldgen"
)

// object is one object of the truth with what loading it needs.
type object struct {
	rec     worldgen.Record
	typ     *typeInfo
	id      graph.ObjectID
	key     []graph.KeyPart
	state   uint8
	ok      bool
	out, in []*link
}

const (
	stateNew uint8 = iota
	stateResolving
	stateDone
)

// link is one link of the truth.
type link struct {
	rec      worldgen.Record
	typ      model.LinkType
	from, to *object
}

// typeInfo is what the loader keeps per object type: the type, its properties by name and its identity keys.
type typeInfo struct {
	typ   model.ObjectType
	props map[string]model.Property
	keys  []model.IdentityKey
}

// world is the whole truth in memory: the loader is for worlds that fit.
type world struct {
	reg     *model.Registry
	types   map[string]*typeInfo
	links   map[string]model.LinkType
	objects []*object
	byID    map[string]*object
	edges   []*link
	// order is the objects that can be loaded, parents before the objects they identify.
	order []*object
}

func readWorld(reg *model.Registry, r io.Reader) (*world, error) {
	w := &world{reg: reg, types: map[string]*typeInfo{}, links: map[string]model.LinkType{}, byID: map[string]*object{}}
	err := worldgen.ReadTruth(r, func(rec *worldgen.Record) error {
		switch rec.Kind {
		case "object":
			return w.addObject(*rec)
		case "link":
			w.edges = append(w.edges, &link{rec: *rec})
			return nil
		}
		return kernel.Errorf(kernel.CodeInvalidValue, "unknown truth record kind %q", rec.Kind)
	})
	if err != nil {
		return nil, err
	}
	return w, w.join()
}

func (w *world) addObject(rec worldgen.Record) error {
	ti, err := w.typeInfo(rec.Type)
	if err != nil {
		return err
	}
	if _, dup := w.byID[rec.ID]; dup {
		return kernel.Errorf(kernel.CodeInvalidValue, "truth object %s appears twice", rec.ID)
	}
	o := &object{rec: rec, typ: ti}
	w.objects = append(w.objects, o)
	w.byID[rec.ID] = o
	return nil
}

func (w *world) typeInfo(id string) (*typeInfo, error) {
	if ti, ok := w.types[id]; ok {
		return ti, nil
	}
	tid, err := kernel.ParseID[model.ObjectTypeKind](id)
	if err != nil {
		return nil, err
	}
	typ, ok := w.reg.ObjectType(tid)
	if !ok {
		return nil, kernel.Errorf(kernel.CodeNotFound, "truth type %s is not in the model", id)
	}
	ti := &typeInfo{typ: typ, props: map[string]model.Property{}, keys: w.reg.IdentityKeys(tid)}
	for _, p := range w.reg.EffectiveProperties(tid) {
		ti.props[p.Name] = p
	}
	w.types[id] = ti
	return ti, nil
}

// join binds the ends of every link and the link types.
func (w *world) join() error {
	for _, l := range w.edges {
		typ, err := w.linkType(l.rec.Type)
		if err != nil {
			return err
		}
		l.typ = typ
		l.from, l.to = w.byID[l.rec.From], w.byID[l.rec.To]
		if l.from == nil || l.to == nil {
			return kernel.Errorf(kernel.CodeInvalidValue, "truth link %s has an end that does not exist", l.rec.ID)
		}
		l.from.out = append(l.from.out, l)
		l.to.in = append(l.to.in, l)
	}
	return nil
}

// linkType looks a link type up once: the registry copies what it gives out.
func (w *world) linkType(id string) (model.LinkType, error) {
	if typ, ok := w.links[id]; ok {
		return typ, nil
	}
	lid, err := kernel.ParseID[model.LinkTypeKind](id)
	if err != nil {
		return model.LinkType{}, err
	}
	typ, ok := w.reg.LinkType(lid)
	if !ok {
		return model.LinkType{}, kernel.Errorf(kernel.CodeNotFound, "truth link type %s is not in the model", id)
	}
	w.links[id] = typ
	return typ, nil
}
