package kernel

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"reflect"
	"sort"
	"unicode/utf8"
)

// Hash is a SHA-256 digest.
type Hash [sha256.Size]byte

// String returns the digest as 64 lowercase hex characters.
func (h Hash) String() string { return hex.EncodeToString(h[:]) }

// ParseHash reads the 64-hex-character form back.
func ParseHash(text string) (Hash, error) {
	var h Hash
	raw, err := hex.DecodeString(text)
	if err != nil || len(raw) != len(h) {
		return h, Errorf(CodeInvalidValue, "invalid hash %q: want %d hex characters", text, 2*len(h))
	}
	copy(h[:], raw)
	return h, nil
}

// HashBytes is the SHA-256 of raw bytes.
func HashBytes(data []byte) Hash { return sha256.Sum256(data) }

// ContentHash is the SHA-256 of the canonical encoding of v (see Encode).
// Equal values hash equal on every platform and in every run. It is the hash
// to store or to share with another program (ADR-0006, TD-0004).
func ContentHash(v any) (Hash, error) {
	encoded, err := Encode(v)
	if err != nil {
		return Hash{}, err
	}
	return HashBytes(encoded), nil
}

// The canonical encoding (version 1) writes one tag byte and then the value.
// All numbers are big-endian.
//
//	null     0x00
//	false    0x01
//	true     0x02
//	integer  0x03, then the 8-byte two's complement value
//	string   0x04, then the byte length as 8 bytes, then the UTF-8 bytes
//	list     0x05, then the item count as 8 bytes, then each item
//	map      0x06, then the entry count as 8 bytes, then each entry as a key
//	         (encoded like a string) and a value, entries in ascending byte
//	         order of the key
//
// Slices and arrays are lists and maps with string keys are maps; a nil slice
// or map is empty, because Go and JSON decoders produce nil and empty ones
// without telling them apart. A nil pointer or nil interface is null and any
// other pointer is what it points to. A []byte is a list of integers. Floats, structs, channels and functions have
// no canonical form and are errors, as are strings that are not valid UTF-8
// and unsigned integers above the int64 range. A change to this encoding is a
// new version, never an edit: stored hashes must stay comparable.
const (
	tagNull    byte = 0x00
	tagFalse   byte = 0x01
	tagTrue    byte = 0x02
	tagInteger byte = 0x03
	tagString  byte = 0x04
	tagList    byte = 0x05
	tagMap     byte = 0x06
)

// Encode returns the canonical encoding of v.
func Encode(v any) ([]byte, error) {
	return encodeValue(nil, reflect.ValueOf(v))
}

func encodeValue(dst []byte, v reflect.Value) ([]byte, error) {
	if !v.IsValid() {
		return append(dst, tagNull), nil
	}
	switch v.Kind() {
	case reflect.Bool:
		return appendBool(dst, v.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return appendInteger(dst, v.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return encodeUnsigned(dst, v.Uint())
	case reflect.String:
		return appendString(dst, v.String())
	case reflect.Slice, reflect.Array:
		return encodeList(dst, v)
	case reflect.Map:
		return encodeMap(dst, v)
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return append(dst, tagNull), nil
		}
		return encodeValue(dst, v.Elem())
	}
	return nil, Errorf(CodeUnsupported, "no canonical encoding for %s", v.Type())
}

func appendBool(dst []byte, b bool) []byte {
	if b {
		return append(dst, tagTrue)
	}
	return append(dst, tagFalse)
}

func appendUint64(dst []byte, n uint64) []byte {
	return binary.BigEndian.AppendUint64(dst, n)
}

func appendInteger(dst []byte, n int64) []byte {
	return appendUint64(append(dst, tagInteger), uint64(n))
}

func encodeUnsigned(dst []byte, n uint64) ([]byte, error) {
	if n > 1<<63-1 {
		return nil, Errorf(CodeInvalidValue, "integer %d is above the int64 range", n)
	}
	return appendInteger(dst, int64(n)), nil
}

func appendString(dst []byte, s string) ([]byte, error) {
	if !utf8.ValidString(s) {
		return nil, Errorf(CodeInvalidValue, "string is not valid UTF-8")
	}
	dst = appendUint64(append(dst, tagString), uint64(len(s)))
	return append(dst, s...), nil
}

func encodeList(dst []byte, v reflect.Value) ([]byte, error) {
	dst = appendUint64(append(dst, tagList), uint64(v.Len()))
	var err error
	for i := 0; i < v.Len(); i++ {
		if dst, err = encodeValue(dst, v.Index(i)); err != nil {
			return nil, err
		}
	}
	return dst, nil
}

func encodeMap(dst []byte, v reflect.Value) ([]byte, error) {
	if v.Type().Key().Kind() != reflect.String {
		return nil, Errorf(CodeUnsupported, "map keys must be strings, not %s", v.Type().Key())
	}
	keys := v.MapKeys()
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	dst = appendUint64(append(dst, tagMap), uint64(len(keys)))
	var err error
	for _, key := range keys {
		if dst, err = appendString(dst, key.String()); err != nil {
			return nil, err
		}
		if dst, err = encodeValue(dst, v.MapIndex(key)); err != nil {
			return nil, err
		}
	}
	return dst, nil
}

// deriveSeed makes the seed of a forked stream from a parent seed and a label.
func deriveSeed(seed uint64, label string) uint64 {
	sum := sha256.Sum256(append(appendUint64(nil, seed), label...))
	return binary.BigEndian.Uint64(sum[:8])
}
