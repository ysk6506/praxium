package model

import (
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Registry is the type registry: a set of checked definitions with their
// numbers, indexed for lookup. It never changes after NewRegistry returns, so
// it can be shared by any number of readers. Every method returns copies.
type Registry struct {
	defs       Definitions
	valueTypes map[ValueTypeID]ValueType
	traits     map[TraitID]int
	types      map[ObjectTypeID]int
	typeByNo   map[int]ObjectTypeID
	typeByName map[string]ObjectTypeID
	links      map[LinkTypeID]int
	linkByNo   map[int]LinkTypeID
	linkByName map[string]LinkTypeID
	actions    map[ActionTypeID]int
	actByName  map[string]ActionTypeID
	schemes    map[IdentifierSchemeID]int
	schemeName map[string]IdentifierSchemeID
	traitName  map[string]TraitID
	props      map[PropertyID]PropertyRef
	propByNo   map[int]PropertyID
	info       map[ObjectTypeID]*typeInfo
	children   map[ObjectTypeID][]ObjectTypeID
	hash       kernel.Hash
}

// PropertyRef is a property with the definition that owns it.
type PropertyRef struct {
	Property Property
	// Owner is the id of the owning object type, trait or link type.
	Owner string
	// OwnerKind is "object_type", "trait" or "link_type".
	OwnerKind string
}

// typeInfo is what is worked out once for each object type.
type typeInfo struct {
	ancestors []ObjectTypeID // nearest first
	traits    map[TraitID]bool
	props     []Property
	scopeKeys []PropertyID
	keys      []IdentityKey
	needs     []Need
}

// NewRegistry checks the definitions, assigns the numbers that are missing and
// builds the registry. The result does not share memory with defs. A set that
// breaks a rule gives the first violation (see Check for all of them).
func NewRegistry(defs Definitions) (*Registry, error) {
	if err := Validate(defs); err != nil {
		return nil, err
	}
	d := defs.Clone()
	assignNumbers(&d)
	r := &Registry{defs: d}
	r.index()
	c := newChecker(&r.defs)
	r.buildInfo(c)
	hash, err := r.computeHash()
	if err != nil {
		return nil, err
	}
	r.hash = hash
	return r, nil
}

func (r *Registry) index() {
	d := &r.defs
	r.valueTypes = map[ValueTypeID]ValueType{}
	for _, vt := range append(builtinValueTypes(), d.ValueTypes...) {
		r.valueTypes[vt.ID] = vt
	}
	r.traits, r.traitName = map[TraitID]int{}, map[string]TraitID{}
	for i, t := range d.Traits {
		r.traits[t.ID] = i
		r.traitName[t.Name] = t.ID
		for _, alias := range t.Aliases {
			r.traitName[alias] = t.ID
		}
	}
	r.props, r.propByNo = map[PropertyID]PropertyRef{}, map[int]PropertyID{}
	r.indexTypes()
	r.indexLinks()
	r.indexActions()
	r.schemes, r.schemeName = map[IdentifierSchemeID]int{}, map[string]IdentifierSchemeID{}
	for i, s := range d.Schemes {
		r.schemes[s.ID] = i
		r.schemeName[s.Name] = s.ID
		for _, alias := range s.Aliases {
			r.schemeName[alias] = s.ID
		}
	}
}

func (r *Registry) addProps(props []Property, owner, kind string) {
	for _, p := range props {
		r.props[p.ID] = PropertyRef{Property: p.clone(), Owner: owner, OwnerKind: kind}
		r.propByNo[p.AttrNo] = p.ID
	}
}

func (r *Registry) indexTypes() {
	r.types, r.typeByNo, r.typeByName = map[ObjectTypeID]int{}, map[int]ObjectTypeID{}, map[string]ObjectTypeID{}
	r.children = map[ObjectTypeID][]ObjectTypeID{}
	for i, t := range r.defs.ObjectTypes {
		r.types[t.ID] = i
		r.typeByNo[t.TypeNo] = t.ID
		r.typeByName[t.Name] = t.ID
		for _, alias := range t.Aliases {
			r.typeByName[alias] = t.ID
		}
		if !t.Parent.IsZero() {
			r.children[t.Parent] = append(r.children[t.Parent], t.ID)
		}
		r.addProps(t.Properties, t.ID.String(), "object_type")
	}
	for i := range r.defs.Traits {
		r.addProps(r.defs.Traits[i].Properties, r.defs.Traits[i].ID.String(), "trait")
	}
}

func (r *Registry) indexLinks() {
	r.links, r.linkByNo, r.linkByName = map[LinkTypeID]int{}, map[int]LinkTypeID{}, map[string]LinkTypeID{}
	for i, l := range r.defs.LinkTypes {
		r.links[l.ID] = i
		r.linkByNo[l.LinkTypeNo] = l.ID
		r.linkByName[l.Name] = l.ID
		for _, alias := range l.Aliases {
			r.linkByName[alias] = l.ID
		}
		r.addProps(l.Properties, l.ID.String(), "link_type")
	}
}

func (r *Registry) indexActions() {
	r.actions, r.actByName = map[ActionTypeID]int{}, map[string]ActionTypeID{}
	for i, a := range r.defs.ActionTypes {
		r.actions[a.ID] = i
		r.actByName[a.Name] = a.ID
		for _, alias := range a.Aliases {
			r.actByName[alias] = a.ID
		}
	}
}

// ModelVersion is the version of the model the registry was built from.
func (r *Registry) ModelVersion() int { return r.defs.ModelVersion }

// Hash is the content hash of the whole model (see computeHash): it depends on
// ids, not on names, so renaming a definition does not change it.
func (r *Registry) Hash() kernel.Hash { return r.hash }

// Definitions returns a copy of the definitions with every number assigned,
// as a base to build more definitions on.
func (r *Registry) Definitions() Definitions { return r.defs.Clone() }

// ValueType returns a value type, built in or named.
func (r *Registry) ValueType(id ValueTypeID) (ValueType, bool) {
	vt, ok := r.valueTypes[id]
	return vt, ok
}

// ValueTypes returns every value type, the built-in ones first, then the named ones by name.
func (r *Registry) ValueTypes() []ValueType {
	out := builtinValueTypes()
	named := append([]ValueType(nil), r.defs.ValueTypes...)
	sort.Slice(named, func(i, j int) bool { return named[i].Name < named[j].Name })
	return append(out, named...)
}

// Trait returns a trait by id.
func (r *Registry) Trait(id TraitID) (Trait, bool) {
	i, ok := r.traits[id]
	if !ok {
		return Trait{}, false
	}
	return r.defs.Traits[i].clone(), true
}

// TraitByName returns a trait by its name or one of its old names.
func (r *Registry) TraitByName(name string) (Trait, bool) {
	id, ok := r.traitName[name]
	if !ok {
		return Trait{}, false
	}
	return r.Trait(id)
}

// Traits returns every trait by name.
func (r *Registry) Traits() []Trait {
	out := cloneEach(r.defs.Traits, Trait.clone)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ObjectType returns an object type by id.
func (r *Registry) ObjectType(id ObjectTypeID) (ObjectType, bool) {
	i, ok := r.types[id]
	if !ok {
		return ObjectType{}, false
	}
	return r.defs.ObjectTypes[i].clone(), true
}

// ObjectTypeByNo returns an object type by its stable number.
func (r *Registry) ObjectTypeByNo(no int) (ObjectType, bool) {
	id, ok := r.typeByNo[no]
	if !ok {
		return ObjectType{}, false
	}
	return r.ObjectType(id)
}

// ObjectTypeByName returns an object type by its name or one of its old names.
func (r *Registry) ObjectTypeByName(name string) (ObjectType, bool) {
	id, ok := r.typeByName[name]
	if !ok {
		return ObjectType{}, false
	}
	return r.ObjectType(id)
}

// ObjectTypes returns every object type in the order of their numbers.
func (r *Registry) ObjectTypes() []ObjectType {
	out := cloneEach(r.defs.ObjectTypes, ObjectType.clone)
	sort.Slice(out, func(i, j int) bool { return out[i].TypeNo < out[j].TypeNo })
	return out
}

// LinkType returns a link type with its bindings by id.
func (r *Registry) LinkType(id LinkTypeID) (LinkType, bool) {
	i, ok := r.links[id]
	if !ok {
		return LinkType{}, false
	}
	return r.defs.LinkTypes[i].clone(), true
}

// LinkTypeByNo returns a link type by its stable number.
func (r *Registry) LinkTypeByNo(no int) (LinkType, bool) {
	id, ok := r.linkByNo[no]
	if !ok {
		return LinkType{}, false
	}
	return r.LinkType(id)
}

// LinkTypeByName returns a link type by its name or one of its old names.
func (r *Registry) LinkTypeByName(name string) (LinkType, bool) {
	id, ok := r.linkByName[name]
	if !ok {
		return LinkType{}, false
	}
	return r.LinkType(id)
}

// LinkTypes returns every link type in the order of their numbers.
func (r *Registry) LinkTypes() []LinkType {
	out := cloneEach(r.defs.LinkTypes, LinkType.clone)
	sort.Slice(out, func(i, j int) bool { return out[i].LinkTypeNo < out[j].LinkTypeNo })
	return out
}

// ActionType returns an action type by id.
func (r *Registry) ActionType(id ActionTypeID) (ActionType, bool) {
	i, ok := r.actions[id]
	if !ok {
		return ActionType{}, false
	}
	return r.defs.ActionTypes[i].clone(), true
}

// ActionTypeByName returns an action type by its name or one of its old names.
func (r *Registry) ActionTypeByName(name string) (ActionType, bool) {
	id, ok := r.actByName[name]
	if !ok {
		return ActionType{}, false
	}
	return r.ActionType(id)
}

// ActionTypes returns every action type by name.
func (r *Registry) ActionTypes() []ActionType {
	out := cloneEach(r.defs.ActionTypes, ActionType.clone)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Property returns a property by id with its owner.
func (r *Registry) Property(id PropertyID) (PropertyRef, bool) {
	ref, ok := r.props[id]
	if !ok {
		return PropertyRef{}, false
	}
	ref.Property = ref.Property.clone()
	return ref, true
}

// PropertyByNo returns a property by its stable attribute number.
func (r *Registry) PropertyByNo(no int) (PropertyRef, bool) {
	id, ok := r.propByNo[no]
	if !ok {
		return PropertyRef{}, false
	}
	return r.Property(id)
}

// IdentifierScheme returns an identifier scheme by id.
func (r *Registry) IdentifierScheme(id IdentifierSchemeID) (IdentifierScheme, bool) {
	i, ok := r.schemes[id]
	if !ok {
		return IdentifierScheme{}, false
	}
	return r.defs.Schemes[i].clone(), true
}

// IdentifierSchemeByName returns an identifier scheme by name or alias.
func (r *Registry) IdentifierSchemeByName(name string) (IdentifierScheme, bool) {
	id, ok := r.schemeName[name]
	if !ok {
		return IdentifierScheme{}, false
	}
	return r.IdentifierScheme(id)
}

// IdentifierSchemes returns every identifier scheme in the order of the data.
func (r *Registry) IdentifierSchemes() []IdentifierScheme {
	return cloneEach(r.defs.Schemes, IdentifierScheme.clone)
}
