package pgstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// pgTime is a time as the database keeps it: UTC with microseconds, so what is written is what is read back.
func pgTime(t time.Time) time.Time { return t.UTC().Truncate(time.Microsecond) }

// hashBytes is the stored form of a hash; an all-zero hash means "none" and is stored as null by the callers
// that allow it.
func hashBytes(h kernel.Hash) []byte { return h[:] }

// hashOf reads a stored hash back. A null (nil) is the zero hash; any other length is a damaged row.
func hashOf(b []byte) (kernel.Hash, error) {
	var h kernel.Hash
	if len(b) == 0 {
		return h, nil
	}
	if len(b) != len(h) {
		return h, kernel.Errorf(kernel.CodeInternal, "a stored hash has %d bytes, wanted %d", len(b), len(h))
	}
	copy(h[:], b)
	return h, nil
}

// attrJSON is the stored form of one attribute: the value and what says how far it can be trusted.
type attrJSON struct {
	Value      any    `json:"v"`
	Grade      string `json:"g"`
	Basis      string `json:"b"`
	SourceTime string `json:"t,omitempty"`
	SourceRef  string `json:"r,omitempty"`
}

// encodeAttrs turns the versioned attributes of a row into JSON. Keys are the attribute numbers.
func encodeAttrs(attrs map[int]graph.Attr) ([]byte, error) {
	out := make(map[string]attrJSON, len(attrs))
	for no, a := range attrs {
		out[strconv.Itoa(no)] = attrJSON{Value: a.Value, Grade: string(a.Grade), Basis: string(a.Basis),
			SourceTime: a.SourceTime, SourceRef: a.SourceRef}
	}
	return marshal(out)
}

// decodeAttrs is the reverse of encodeAttrs. An empty object is a nil map.
func decodeAttrs(raw []byte) (map[int]graph.Attr, error) {
	var in map[string]attrJSON
	if err := unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[int]graph.Attr, len(in))
	for key, a := range in {
		no, err := strconv.Atoi(key)
		if err != nil {
			return nil, kernel.Errorf(kernel.CodeInternal, "a stored attribute has the key %q", key)
		}
		v, err := plainValue(a.Value)
		if err != nil {
			return nil, err
		}
		out[no] = graph.Attr{Value: v, Grade: model.Grade(a.Grade), Basis: model.Basis(a.Basis),
			SourceTime: a.SourceTime, SourceRef: a.SourceRef}
	}
	return out, nil
}

// encodeValue is the stored form of one value (a volatile value): nil, a string, an int64, a bool, a list or a map.
func encodeValue(v any) ([]byte, error) { return marshal(v) }

// decodeValue reads a stored value back with the Go types the port uses.
func decodeValue(raw []byte) (any, error) {
	var v any
	if err := unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return plainValue(v)
}

// marshal is json.Marshal with the error in the code of this unit.
func marshal(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "a value cannot be stored as JSON")
	}
	return raw, nil
}

// unmarshal reads JSON keeping every number exact (json.Number), so that an int64 comes back as an int64.
func unmarshal(raw []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(into); err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "a stored value is not the JSON this store wrote")
	}
	return nil
}

// plainValue turns what the decoder gives into the types of the port: a number is an int64 (the port has no
// other number: decimals are strings), a list is []any and an object is map[string]any.
func plainValue(v any) (any, error) {
	switch x := v.(type) {
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			return nil, kernel.Errorf(kernel.CodeInternal, "a stored number %s is not an int64", x)
		}
		return n, nil
	case []any:
		for i, e := range x {
			p, err := plainValue(e)
			if err != nil {
				return nil, err
			}
			x[i] = p
		}
		return x, nil
	case map[string]any:
		for k, e := range x {
			p, err := plainValue(e)
			if err != nil {
				return nil, err
			}
			x[k] = p
		}
		return x, nil
	}
	return v, nil
}

// refsJSON stores the source references of a row; nil is an empty list.
func refsJSON(refs []string) ([]byte, error) {
	if refs == nil {
		refs = []string{}
	}
	return marshal(refs)
}

// refsOf reads them back; an empty list is nil.
func refsOf(raw []byte) ([]string, error) {
	var refs []string
	if err := unmarshal(raw, &refs); err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, nil
	}
	return refs, nil
}

// partJSON is one partial sum of a hash state.
type partJSON struct {
	Entity string `json:"e"`
	TypeID string `json:"t"`
	Sum    string `json:"s"`
	Rows   int64  `json:"r"`
}

// partsJSON is the stored form of the partial sums of a hash state (the head keeps the total in columns).
func partsJSON(parts map[graph.PartKey]graph.Part) ([]byte, error) {
	list := make([]partJSON, 0, len(parts))
	for k, p := range parts {
		list = append(list, partJSON{Entity: k.Entity, TypeID: k.TypeID, Sum: p.Sum.String(), Rows: p.Rows})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Entity != list[j].Entity {
			return list[i].Entity < list[j].Entity
		}
		return list[i].TypeID < list[j].TypeID
	})
	return marshal(list)
}

// partsOf reads the partial sums back.
func partsOf(raw []byte) (map[graph.PartKey]graph.Part, error) {
	var list []partJSON
	if err := unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make(map[graph.PartKey]graph.Part, len(list))
	for _, p := range list {
		sum, err := sumOf(p.Sum)
		if err != nil {
			return nil, err
		}
		out[graph.PartKey{Entity: p.Entity, TypeID: p.TypeID}] = graph.Part{Sum: sum, Rows: p.Rows}
	}
	return out, nil
}

// sumOf reads a 64-character hex sum.
func sumOf(text string) (graph.Sum, error) {
	var s graph.Sum
	h, err := kernel.ParseHash(text)
	if err != nil {
		return s, kernel.Wrap(kernel.CodeInternal, err, "a stored partial sum is damaged")
	}
	copy(s[:], h[:])
	return s, nil
}

// sumBytes is the stored form of a sum.
func sumBytes(s graph.Sum) []byte { return s[:] }

// sumFrom reads a stored sum.
func sumFrom(b []byte) (graph.Sum, error) {
	var s graph.Sum
	if len(b) != len(s) {
		return s, kernel.Errorf(kernel.CodeInternal, "a stored sum has %d bytes, wanted %d", len(b), len(s))
	}
	copy(s[:], b)
	return s, nil
}

// linkMapJSON is the stored form of the old-to-new link pairs of a merge.
func linkMapJSON(m []graph.LinkMapping) ([]byte, error) {
	pairs := make([][2]string, 0, len(m))
	for _, p := range m {
		pairs = append(pairs, [2]string{p.Old.String(), p.New.String()})
	}
	return marshal(pairs)
}

// linkMapOf reads it back.
func linkMapOf(raw []byte) ([]graph.LinkMapping, error) {
	var pairs [][2]string
	if err := unmarshal(raw, &pairs); err != nil {
		return nil, err
	}
	var out []graph.LinkMapping
	for _, p := range pairs {
		oldID, err := kernel.ParseID[graph.LinkKind](p[0])
		if err != nil {
			return nil, err
		}
		newID, err := kernel.ParseID[graph.LinkKind](p[1])
		if err != nil {
			return nil, err
		}
		out = append(out, graph.LinkMapping{Old: oldID, New: newID})
	}
	return out, nil
}

// parseObject and parseLink read an id from a column; they are errors only when the database holds a text no
// id has, which is a damaged row.
func parseObject(text string) (graph.ObjectID, error) { return parseID[graph.ObjectKind](text) }
func parseLink(text string) (graph.LinkID, error)     { return parseID[graph.LinkKind](text) }

func parseID[K any](text string) (kernel.ID[K], error) {
	id, err := kernel.ParseID[K](text)
	if err != nil {
		return id, kernel.Wrap(kernel.CodeInternal, err, fmt.Sprintf("a stored id %q is damaged", text))
	}
	return id, nil
}
