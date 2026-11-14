package memstore

import (
	"encoding/binary"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The value of an attribute is nil, a string, an int64, a bool, a list or a map with string keys. gob cannot carry a
// nil inside a list and does not keep an empty list apart from none, so the journal writes values in a small form
// of its own: one tag byte, then the value (numbers as varints, strings and counts with a varint length).
const (
	tagNil byte = iota
	tagFalse
	tagTrue
	tagString
	tagInt
	tagList
	tagMap
)

// putValue appends the form of v to b. A Go type that is not one of the six is an error: the applier refuses such
// a value long before, and a value the journal cannot write must not be applied.
func putValue(b []byte, v any) ([]byte, error) {
	switch x := v.(type) {
	case nil:
		return append(b, tagNil), nil
	case bool:
		if x {
			return append(b, tagTrue), nil
		}
		return append(b, tagFalse), nil
	case string:
		return putText(append(b, tagString), x), nil
	case int64:
		return binary.AppendVarint(append(b, tagInt), x), nil
	case []any:
		return putList(b, x)
	case map[string]any:
		return putMap(b, x)
	}
	return nil, kernel.Errorf(kernel.CodeInvalidValue, "the journal cannot write a value of Go type %T", v)
}

func putText(b []byte, s string) []byte {
	return append(binary.AppendUvarint(b, uint64(len(s))), s...)
}

func putList(b []byte, x []any) ([]byte, error) {
	b = binary.AppendUvarint(append(b, tagList), uint64(len(x)))
	var err error
	for _, e := range x {
		if b, err = putValue(b, e); err != nil {
			return nil, err
		}
	}
	return b, nil
}

func putMap(b []byte, x map[string]any) ([]byte, error) {
	b = binary.AppendUvarint(append(b, tagMap), uint64(len(x)))
	keys := make([]string, 0, len(x))
	for k := range x {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var err error
	for _, k := range keys {
		if b, err = putValue(putText(b, k), x[k]); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// valueReader reads the form back.
type valueReader struct {
	b   []byte
	err error
}

func (r *valueReader) fail() {
	if r.err == nil {
		r.err = kernel.Errorf(kernel.CodeInvalidValue, "a value in the journal is cut short or malformed")
	}
}

func (r *valueReader) uvarint() int {
	v, n := binary.Uvarint(r.b)
	if n <= 0 || v > uint64(len(r.b)) {
		r.fail()
		return 0
	}
	r.b = r.b[n:]
	return int(v)
}

func (r *valueReader) text() string {
	n := r.uvarint()
	if r.err != nil || n > len(r.b) {
		r.fail()
		return ""
	}
	s := string(r.b[:n])
	r.b = r.b[n:]
	return s
}

func (r *valueReader) value() any {
	if r.err != nil || len(r.b) == 0 {
		r.fail()
		return nil
	}
	tag := r.b[0]
	r.b = r.b[1:]
	switch tag {
	case tagNil:
		return nil
	case tagFalse:
		return false
	case tagTrue:
		return true
	case tagString:
		return r.text()
	case tagInt:
		return r.integer()
	case tagList:
		return r.list()
	case tagMap:
		return r.object()
	}
	r.fail()
	return nil
}

func (r *valueReader) integer() any {
	v, n := binary.Varint(r.b)
	if n <= 0 {
		r.fail()
		return nil
	}
	r.b = r.b[n:]
	return v
}

func (r *valueReader) list() any {
	n := r.uvarint()
	out := make([]any, 0, n)
	for i := 0; i < n && r.err == nil; i++ {
		out = append(out, r.value())
	}
	return out
}

func (r *valueReader) object() any {
	n := r.uvarint()
	out := make(map[string]any, n)
	for i := 0; i < n && r.err == nil; i++ {
		k := r.text()
		out[k] = r.value()
	}
	return out
}

// readValue reads one value from b, which must hold nothing else.
func readValue(b []byte) (any, error) {
	r := valueReader{b: b}
	v := r.value()
	if r.err == nil && len(r.b) != 0 {
		r.fail()
	}
	return v, r.err
}
