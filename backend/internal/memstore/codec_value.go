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
		b = append(b, tagString)
		b = binary.AppendUvarint(b, uint64(len(x)))
		return append(b, x...), nil
	case int64:
		return binary.AppendVarint(append(b, tagInt), x), nil
	case []any:
		b = binary.AppendUvarint(append(b, tagList), uint64(len(x)))
		var err error
		for _, e := range x {
			if b, err = putValue(b, e); err != nil {
				return nil, err
			}
		}
		return b, nil
	case map[string]any:
		b = binary.AppendUvarint(append(b, tagMap), uint64(len(x)))
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var err error
		for _, k := range keys {
			b = binary.AppendUvarint(b, uint64(len(k)))
			b = append(b, k...)
			if b, err = putValue(b, x[k]); err != nil {
				return nil, err
			}
		}
		return b, nil
	}
	return nil, kernel.Errorf(kernel.CodeInvalidValue, "the journal cannot write a value of Go type %T", v)
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
		v, n := binary.Varint(r.b)
		if n <= 0 {
			r.fail()
			return nil
		}
		r.b = r.b[n:]
		return v
	case tagList:
		n := r.uvarint()
		out := make([]any, 0, n)
		for i := 0; i < n && r.err == nil; i++ {
			out = append(out, r.value())
		}
		return out
	case tagMap:
		n := r.uvarint()
		out := make(map[string]any, n)
		for i := 0; i < n && r.err == nil; i++ {
			k := r.text()
			out[k] = r.value()
		}
		return out
	}
	r.fail()
	return nil
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
