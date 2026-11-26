package graph

import (
	"encoding/json"
	"sort"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The meaning of a row of an object or a link that the row by row path (Applier) and the staged path (the new ingestion flow) share
// (ADR-0115): the content hash, the lowest grade and the stored forms of the document, the source references, the key an object was first
// seen with and a staged volatile entry. The staged path computes them in Go without the store, so they are public here and the store
// (pgstore) calls them: there is one implementation of what they mean.

// ContentHasher computes the content hashes of the rows of a KG against a registry: names do not enter, the immutable ids do. The
// key provider turns a confidential value into its keyed hash.
type ContentHasher struct{ h hasher }

// NewContentHasher makes the hasher of the KG kg.
func NewContentHasher(reg *model.Registry, keys KeyProvider, kg KGID) *ContentHasher {
	objects, links := typeIDs(reg)
	return &ContentHasher{h: hasher{reg: reg, keys: keys, kg: kg, objTypes: objects, linkTypes: links}}
}

// Object is the content hash of an object of the type number typeNo with the name and the attributes.
func (c *ContentHasher) Object(typeNo int, name string, attrs map[int]Attr) (kernel.Hash, error) {
	return c.h.objectContent(ObjectRow{TypeNo: typeNo, Name: name, Attrs: attrs})
}

// Link is the content hash of a link row (its type number, ends, discriminator, grade, basis and attributes).
func (c *ContentHasher) Link(row LinkRow) (kernel.Hash, error) { return c.h.linkContent(row) }

// MinGrade is the lowest grade of the attributes of a row.
func MinGrade(attrs map[int]Attr) model.Grade { return minGrade(attrs) }

// attrJSON is the stored form of one attribute: the value and what says how far it can be trusted.
type attrJSON struct {
	Value      any    `json:"v"`
	Grade      string `json:"g"`
	Basis      string `json:"b"`
	SourceTime string `json:"t,omitempty"`
	SourceRef  string `json:"r,omitempty"`
}

// EncodeAttrs turns the versioned attributes of a row into the JSON a store keeps (the document). Keys are the attribute numbers.
func EncodeAttrs(attrs map[int]Attr) ([]byte, error) {
	out := make(map[string]attrJSON, len(attrs))
	for no, a := range attrs {
		out[strconv.Itoa(no)] = attrJSON{Value: a.Value, Grade: string(a.Grade), Basis: string(a.Basis), SourceTime: a.SourceTime, SourceRef: a.SourceRef}
	}
	return marshalStored(out)
}

// EncodeRefs is the stored form of the source references of a row; nil is an empty list.
func EncodeRefs(refs []string) ([]byte, error) {
	if refs == nil {
		refs = []string{}
	}
	return marshalStored(refs)
}

// keyDoc is the stored form of the key an object was first seen with.
type keyDoc struct {
	Kind    string   `json:"k"`
	Ref     string   `json:"r"`
	Value   string   `json:"v,omitempty"`
	Objects []string `json:"o,omitempty"`
}

// EncodeKey is the stored form of the first key of an object (first_key of the registry); no key is nil.
func EncodeKey(key []KeyPart) (any, error) {
	if len(key) == 0 {
		return nil, nil
	}
	docs := make([]keyDoc, 0, len(key))
	for _, p := range key {
		d := keyDoc{Kind: string(p.Kind), Ref: p.Ref, Value: p.Value}
		for _, o := range p.Objects {
			d.Objects = append(d.Objects, o.String())
		}
		sort.Strings(d.Objects)
		docs = append(docs, d)
	}
	raw, err := marshalStored(docs)
	return string(raw), err
}

// DecodeKey reads the stored first key back (the objects of a part by their ids).
func DecodeKey(raw []byte) ([]KeyPart, error) {
	var docs []keyDoc
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "a stored first key is not the JSON this store wrote")
	}
	out := make([]KeyPart, 0, len(docs))
	for _, d := range docs {
		p := KeyPart{Kind: PartKind(d.Kind), Ref: d.Ref, Value: d.Value}
		for _, o := range d.Objects {
			id, err := kernel.ParseID[ObjectKind](o)
			if err != nil {
				return nil, kernel.Wrap(kernel.CodeInternal, err, "a stored first key names a damaged object id")
			}
			p.Objects = append(p.Objects, id)
		}
		out = append(out, p)
	}
	return out, nil
}

// VolatileEntry is a staged volatile value (the entry column of stage_volatile): the value as the source gave it, the grade, the
// basis, the time the source saw it and the reference.
type volatileEntry struct {
	Value any    `json:"v"`
	Grade string `json:"g"`
	Basis string `json:"b"`
	Seen  string `json:"t,omitempty"`
	Ref   string `json:"r,omitempty"`
}

// EncodeVolatileEntry is the stored form of a staged volatile value.
func EncodeVolatileEntry(w VolatileWrite) ([]byte, error) {
	return marshalStored(volatileEntry{Value: w.Value, Grade: string(w.Grade), Basis: string(w.Basis), Seen: w.SeenAt, Ref: w.Ref})
}

// marshalStored is json.Marshal with the error in the code of this unit.
func marshalStored(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "a value cannot be stored as JSON")
	}
	return raw, nil
}
