package graph

import (
	"encoding/hex"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// Kind markers of the ids of this unit.
type (
	// ObjectKind marks ObjectID.
	ObjectKind struct{}
	// LinkKind marks LinkID.
	LinkKind struct{}
	// KGKind marks KGID.
	KGKind struct{}
	// VersionKind marks VersionID.
	VersionKind struct{}
)

type (
	// ObjectID is the immutable internal id of an object: obj_ and 32 hex characters.
	ObjectID = kernel.ID[ObjectKind]
	// LinkID is the id of a link: lnk_ and 32 hex characters.
	LinkID = kernel.ID[LinkKind]
	// KGID identifies a KG. It comes from the injected id generator and is never hashed.
	KGID = kernel.ID[KGKind]
	// VersionID is the name of a sealed version: kv_ and 32 hex characters.
	VersionID = kernel.ID[VersionKind]
)

// Prefixes of the text forms.
const (
	PrefixObject  = "obj_"
	PrefixLink    = "lnk_"
	PrefixVersion = "kv_"
)

// PartKind says what one component of an identity key is (kg-model.md 10.2).
type PartKind string

// The kinds of key component. A scheme part is the identifier of a scheme that is the whole key.
const (
	PartProperty PartKind = "property"
	PartContext  PartKind = "context"
	PartSet      PartKind = "set"
	PartScheme   PartKind = "scheme"
)

// KeyPart is one component of the identity key an object was first seen with.
// Ref is the immutable id of the property, link type or scheme (never a
// number); Value is the tidied value of a property or scheme part; Objects are
// the context object (exactly one) or the members of a set (any order).
type KeyPart struct {
	Kind    PartKind
	Ref     string
	Value   string
	Objects []ObjectID
}

func (p KeyPart) encode() []any {
	members := make([]string, 0, len(p.Objects))
	for _, o := range p.Objects {
		members = append(members, o.String())
	}
	sort.Strings(members)
	list := make([]any, 0, len(members))
	for _, m := range members {
		list = append(list, m)
	}
	return []any{string(p.Kind), p.Ref, p.Value, list}
}

// deriveID builds the text of an id: the prefix and the first 16 bytes of the
// canonical hash of the parts. No generator is involved, so a replay matches.
func deriveID(prefix string, parts []any) (string, error) {
	sum, err := kernel.ContentHash(parts)
	if err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(sum[:16]), nil
}

// NewObjectID derives the id of an object from its type (the immutable id, not
// the number) and the identity key components it was first seen with.
func NewObjectID(typeID model.ObjectTypeID, key []KeyPart) (ObjectID, error) {
	parts := make([]any, 0, len(key))
	for _, p := range key {
		parts = append(parts, p.encode())
	}
	text, err := deriveID(PrefixObject, []any{"praxium.object.v1", typeID.String(), parts})
	if err != nil {
		return ObjectID{}, err
	}
	return kernel.ParseID[ObjectKind](text)
}

// NewLinkID derives the id of a link from its type, its ends and the discriminator.
func NewLinkID(typeID model.LinkTypeID, src, dst ObjectID, discriminator string) (LinkID, error) {
	text, err := deriveID(PrefixLink, []any{"praxium.link.v1", typeID.String(), src.String(), dst.String(), discriminator})
	if err != nil {
		return LinkID{}, err
	}
	return kernel.ParseID[LinkKind](text)
}

// NewVersionID derives the name of a version from the closed input list of
// kg-storage.md 7.1: the previous version, the definition set, the input digest
// of the ordered changes, the engine's semantic version and the hash rule.
func NewVersionID(prev VersionID, definitions, input kernel.Hash, engine int) (VersionID, error) {
	text, err := deriveID(PrefixVersion, []any{
		"praxium.kgversion.v1", prev.String(), definitions.String(), input.String(), engine, HashRuleVersion,
	})
	if err != nil {
		return VersionID{}, err
	}
	return kernel.ParseID[VersionKind](text)
}
