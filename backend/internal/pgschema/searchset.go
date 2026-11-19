package pgschema

import (
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// SearchEntry says that the attribute AttrNo is searched in the table of the type TypeNo, so it gets an index.
type SearchEntry struct {
	TypeNo int
	AttrNo int
}

// SearchSet is the set of attributes that are marked "used in search". It is an input of the plan, not part of
// the model, and it stays out of the hash of the model: marking an attribute is an index and nothing else.
type SearchSet []SearchEntry

// NewSearchSet orders the entries and drops the repeated ones, so equal sets are equal whatever order they
// were written in.
func NewSearchSet(entries ...SearchEntry) SearchSet {
	out := append(SearchSet(nil), entries...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].TypeNo != out[j].TypeNo {
			return out[i].TypeNo < out[j].TypeNo
		}
		return out[i].AttrNo < out[j].AttrNo
	})
	kept := out[:0]
	for i, e := range out {
		if i == 0 || e != out[i-1] {
			kept = append(kept, e)
		}
	}
	return kept
}

// Hash is the content hash of the set.
func (s SearchSet) Hash() (kernel.Hash, error) {
	list := make([]any, 0, len(s))
	for _, e := range NewSearchSet(s...) {
		list = append(list, []any{e.TypeNo, e.AttrNo})
	}
	return kernel.ContentHash(map[string]any{"search": list})
}

// DefaultSearchSet is the first search set: the properties that make up an identity key, for every type that
// has the key (a type that inherits the key from its ancestor gets the same indexes). It is worked out from
// the model. A list that someone keeps by hand is for the day it is needed.
func DefaultSearchSet(reg *model.Registry) (SearchSet, error) {
	sh, err := collect(reg)
	if err != nil {
		return nil, err
	}
	var entries []SearchEntry
	for _, ts := range sh.Types {
		t, ok := reg.ObjectTypeByNo(ts.TypeNo)
		if !ok {
			continue
		}
		for _, key := range reg.IdentityKeys(t.ID) {
			for _, part := range key.Components {
				if part.Property.IsZero() {
					continue
				}
				ref, ok := reg.Property(part.Property)
				if !ok {
					continue
				}
				if col, found := findColumn(ts.Docs, ref.Property.AttrNo); found && Indexable(col.Kind) {
					entries = append(entries, SearchEntry{TypeNo: ts.TypeNo, AttrNo: col.AttrNo})
				}
			}
		}
	}
	return NewSearchSet(entries...), nil
}

func findColumn(cols []Column, attrNo int) (Column, bool) {
	for _, c := range cols {
		if c.AttrNo == attrNo {
			return c, true
		}
	}
	return Column{}, false
}
