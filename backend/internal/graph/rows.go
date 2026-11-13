package graph

import (
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// Attr is one attribute value with how far it can be trusted and where it
// came from. Rows key attributes by attr_no; hashes use the property id.
type Attr struct {
	// Value is nil, a string, an int64, a bool, a []any or a map[string]any.
	Value any
	Grade model.Grade
	Basis model.Basis
	// SourceTime is when the source saw the value (a UTC time text). It is not hashed.
	SourceTime string
	// SourceRef points at the raw record the value came from. It is not hashed.
	SourceRef string
}

// ObjectRow is one valid interval of an object: object_live and object_hist.
// Only versioned attributes are in a row. SeqTo is 0 while the row is open.
type ObjectRow struct {
	ID          ObjectID
	TypeNo      int
	Name        string
	MinGrade    model.Grade
	Attrs       map[int]Attr
	SourceRefs  []string
	ContentHash kernel.Hash
	SeqFrom     int64
	SeqTo       int64
}

// LinkRow is one valid interval of a link: link_live and link_hist. A link has
// a grade and a basis of its own; its attributes are all versioned.
type LinkRow struct {
	ID            LinkID
	TypeNo        int
	Src           ObjectID
	Dst           ObjectID
	Discriminator string
	Grade         model.Grade
	Basis         model.Basis
	Attrs         map[int]Attr
	SourceRefs    []string
	ContentHash   kernel.Hash
	SeqFrom       int64
	SeqTo         int64
}

// VolatileRow is the one current value of a volatile attribute (attr_volatile).
// It has no history; SeenAt is the time the source saw it.
type VolatileRow struct {
	Object ObjectID
	AttrNo int
	Value  any
	Grade  model.Grade
	Basis  model.Basis
	SeenAt string
	Ref    string
}

// Open reports whether the row is still valid.
func (r ObjectRow) Open() bool { return r.SeqTo == 0 }

// Open reports whether the row is still valid.
func (r LinkRow) Open() bool { return r.SeqTo == 0 }

// ValidAt reports whether the row is valid in the state after change number s.
func (r ObjectRow) ValidAt(s int64) bool { return r.SeqFrom <= s && (r.SeqTo == 0 || s < r.SeqTo) }

// ValidAt reports whether the row is valid in the state after change number s.
func (r LinkRow) ValidAt(s int64) bool { return r.SeqFrom <= s && (r.SeqTo == 0 || s < r.SeqTo) }

// Clone returns a copy that shares nothing with r.
func (r ObjectRow) Clone() ObjectRow {
	r.Attrs = cloneAttrs(r.Attrs)
	r.SourceRefs = append([]string(nil), r.SourceRefs...)
	return r
}

// Clone returns a copy that shares nothing with r.
func (r LinkRow) Clone() LinkRow {
	r.Attrs = cloneAttrs(r.Attrs)
	r.SourceRefs = append([]string(nil), r.SourceRefs...)
	return r
}

// Clone returns a copy that shares nothing with r.
func (r VolatileRow) Clone() VolatileRow {
	r.Value = cloneValue(r.Value)
	return r
}

func cloneAttrs(in map[int]Attr) map[int]Attr {
	if in == nil {
		return nil
	}
	out := make(map[int]Attr, len(in))
	for no, a := range in {
		a.Value = cloneValue(a.Value)
		out[no] = a
	}
	return out
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = cloneValue(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = cloneValue(e)
		}
		return out
	}
	return v
}
