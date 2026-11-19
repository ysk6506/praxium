package pgstore

import (
	"sort"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// Entities as the change records name them in their numbers (pgschema.EntityKindObject and EntityKindLink) and as
// the port names them in a ChangeEntry.
const (
	entityNameObject = "object"
	entityNameLink   = "link"
)

// gradeNo is the number a grade has in the tables: its place in the list, best first. A grade outside the list is
// a value that was never checked, so it is refused.
func gradeNo(g model.Grade) (int16, error) {
	for i, c := range model.Grades() {
		if c == g {
			return int16(i), nil
		}
	}
	return 0, kernel.Errorf(kernel.CodeInvalidValue, "the grade %q is not one of the grades", g)
}

// gradeOf is the grade of a number in the tables.
func gradeOf(n int16) (model.Grade, error) {
	list := model.Grades()
	if n < 0 || int(n) >= len(list) {
		return "", kernel.Errorf(kernel.CodeInternal, "a stored grade has the number %d", n)
	}
	return list[n], nil
}

// keyDoc is the stored form of the key an object was first seen with (first_key of the registry).
type keyDoc struct {
	Kind    string   `json:"k"`
	Ref     string   `json:"r"`
	Value   string   `json:"v,omitempty"`
	Objects []string `json:"o,omitempty"`
}

// keyJSON is first_key of a new object; no key is null.
func keyJSON(key []graph.KeyPart) (any, error) {
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
	raw, err := marshal(docs)
	return string(raw), err
}

// keySignature is what the identification index (object_key) keeps of a key: the context object that the key
// hangs from (its first component, when it is a context; the object is found by ref and its number is ctx_no,
// 0 when there is none), the hash of the shape of the key and the values of the components that are not a
// context (16 bytes, the same rule as the ids), and the text the hash was made from. A key made of a set
// has no place in the index. A key that has no parts has none either.
func keySignature(key []graph.KeyPart) (ctx string, hash []byte, sig string, ok bool, err error) {
	if len(key) == 0 {
		return "", nil, "", false, nil
	}
	shape := make([]any, 0, len(key))
	values := make([]any, 0, len(key))
	for _, p := range key {
		switch p.Kind {
		case graph.PartSet:
			return "", nil, "", false, nil
		case graph.PartContext:
			if len(p.Objects) != 1 {
				return "", nil, "", false, nil
			}
			ctx = p.Objects[0].String()
			shape = append(shape, []any{string(p.Kind), p.Ref})
		default:
			shape = append(shape, []any{string(p.Kind), p.Ref})
			values = append(values, p.Value)
		}
	}
	sum, err := kernel.ContentHash([]any{"praxium.objectkey.v1", shape, values})
	if err != nil {
		return "", nil, "", false, err
	}
	text, err := marshal([]any{shape, values})
	return ctx, sum[:16], string(text), true, err
}
