package graph

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// HashRuleVersion is the version of the closed input list of the content
// hash (kg-storage.md 7.3). A change of the list raises it.
const HashRuleVersion = 1

// Sum is a 256-bit number, big-endian. The graph hash is the sum of all row
// hashes modulo 2^256, so it does not depend on order (kg-storage.md 7.4).
type Sum [32]byte

// Add returns s + o modulo 2^256.
func (s Sum) Add(o Sum) Sum {
	var out Sum
	carry := 0
	for i := len(s) - 1; i >= 0; i-- {
		v := int(s[i]) + int(o[i]) + carry
		out[i] = byte(v)
		carry = v >> 8
	}
	return out
}

// Sub returns s - o modulo 2^256.
func (s Sum) Sub(o Sum) Sum {
	var out Sum
	borrow := 0
	for i := len(s) - 1; i >= 0; i-- {
		v := int(s[i]) - int(o[i]) - borrow
		borrow = 0
		if v < 0 {
			v += 256
			borrow = 1
		}
		out[i] = byte(v)
	}
	return out
}

// String returns the sum as 64 hex characters.
func (s Sum) String() string { return hex.EncodeToString(s[:]) }

// IsZero reports whether the sum is 0.
func (s Sum) IsZero() bool { return s == Sum{} }

// PartKey names a partial sum: the entity kind and the immutable type id.
type PartKey struct {
	Entity string
	TypeID string
}

// Part is a partial sum and its row count.
type Part struct {
	Sum  Sum
	Rows int64
}

// HashState is the running graph hash with its partial sums by type.
type HashState struct {
	Sum   Sum
	Rows  int64
	Parts map[PartKey]Part
}

// Clone returns a copy that shares nothing with h.
func (h HashState) Clone() HashState {
	parts := make(map[PartKey]Part, len(h.Parts))
	for k, p := range h.Parts {
		parts[k] = p
	}
	h.Parts = parts
	return h
}

// Add counts a row in.
func (h *HashState) Add(key PartKey, row Sum) { h.move(key, row, false) }

// Remove counts a row out.
func (h *HashState) Remove(key PartKey, row Sum) { h.move(key, row, true) }

func (h *HashState) move(key PartKey, row Sum, remove bool) {
	if h.Parts == nil {
		h.Parts = map[PartKey]Part{}
	}
	p := h.Parts[key]
	delta := int64(1)
	if remove {
		h.Sum, p.Sum, delta = h.Sum.Sub(row), p.Sum.Sub(row), -1
	} else {
		h.Sum, p.Sum = h.Sum.Add(row), p.Sum.Add(row)
	}
	h.Rows += delta
	p.Rows += delta
	if p.Rows == 0 {
		delete(h.Parts, key)
		return
	}
	h.Parts[key] = p
}

// Equal reports whether two states have the same sum, count and parts.
func (h HashState) Equal(o HashState) bool {
	if h.Sum != o.Sum || h.Rows != o.Rows || len(h.Parts) != len(o.Parts) {
		return false
	}
	for k, p := range h.Parts {
		if o.Parts[k] != p {
			return false
		}
	}
	return true
}

// KeyProvider is the port that turns a confidential value into the keyed hash
// that enters a content hash (kg-storage.md 7.3). The adapter owns the KG's
// hash key; nobody can read it, and without it the value cannot be recovered.
type KeyProvider interface {
	// KeyedHash returns HMAC-SHA-256 of data under the hash key of the KG.
	KeyedHash(kg KGID, data []byte) (kernel.Hash, error)
}

type testKeys struct{ secret []byte }

// NewTestKeyProvider is a deterministic KeyProvider for tests and development:
// one fixed secret for every KG. It is not for production.
func NewTestKeyProvider(secret string) KeyProvider { return testKeys{secret: []byte(secret)} }

func (t testKeys) KeyedHash(_ KGID, data []byte) (kernel.Hash, error) {
	mac := hmac.New(sha256.New, t.secret)
	_, _ = mac.Write(data)
	var out kernel.Hash
	copy(out[:], mac.Sum(nil))
	return out, nil
}

// rowSum is row_hash = SHA-256(kind, id, content hash) as a 256-bit number.
func rowSum(entity, id string, content kernel.Hash) (Sum, error) {
	h, err := kernel.ContentHash([]any{"praxium.row.v1", entity, id, content.String()})
	return Sum(h), err
}

// hasher computes content hashes against a registry: names do not enter, the
// immutable ids do, so renaming a type or a property leaves every hash as it was.
type hasher struct {
	reg  *model.Registry
	keys KeyProvider
	kg   KGID
}

func (h hasher) objectContent(row ObjectRow) (kernel.Hash, error) {
	typ, ok := h.reg.ObjectTypeByNo(row.TypeNo)
	if !ok {
		return kernel.Hash{}, kernel.Errorf(CodeBatchInvalid, "unknown type number %d", row.TypeNo)
	}
	attrs, err := h.attrList(row.Attrs)
	if err != nil {
		return kernel.Hash{}, err
	}
	return kernel.ContentHash([]any{"praxium.content.v1", HashRuleVersion, "object", typ.ID.String(), row.Name, attrs})
}

func (h hasher) linkContent(row LinkRow) (kernel.Hash, error) {
	typ, ok := h.reg.LinkTypeByNo(row.TypeNo)
	if !ok {
		return kernel.Hash{}, kernel.Errorf(CodeBatchInvalid, "unknown link type number %d", row.TypeNo)
	}
	attrs, err := h.attrList(row.Attrs)
	if err != nil {
		return kernel.Hash{}, err
	}
	return kernel.ContentHash([]any{"praxium.content.v1", HashRuleVersion, "link", typ.ID.String(),
		row.Src.String(), row.Dst.String(), row.Discriminator, string(row.Grade), string(row.Basis), attrs})
}

// attrList is the list of (property id, value, grade, basis) in property id order.
func (h hasher) attrList(attrs map[int]Attr) ([]any, error) {
	type entry struct {
		id   string
		item []any
	}
	entries := make([]entry, 0, len(attrs))
	for no, a := range attrs {
		ref, ok := h.reg.PropertyByNo(no)
		if !ok {
			return nil, kernel.Errorf(CodeBatchInvalid, "unknown attribute number %d", no)
		}
		value, err := h.valueForHash(ref.Property, a.Value)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry{ref.Property.ID.String(), []any{ref.Property.ID.String(), value, string(a.Grade), string(a.Basis)}})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].id < entries[j].id })
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.item)
	}
	return out, nil
}

// valueForHash is the canonical form of a value: lists are sorted by their
// encoding (the model declares no ordered list), and a confidential value
// becomes its keyed hash.
func (h hasher) valueForHash(p model.Property, v any) (any, error) {
	canon, err := canonicalValue(v)
	if err != nil {
		return nil, err
	}
	if p.EffectiveSensitivity() != model.SensitivityConfidential || v == nil {
		return canon, nil
	}
	raw, err := kernel.Encode(canon)
	if err != nil {
		return nil, err
	}
	mac, err := h.keys.KeyedHash(h.kg, raw)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "keyed hash of a confidential value")
	}
	return "keyed:" + mac.String(), nil
}

func canonicalValue(v any) (any, error) {
	switch x := v.(type) {
	case []any:
		items := make([][]byte, 0, len(x))
		byBytes := make(map[string]any, len(x))
		for _, e := range x {
			c, err := canonicalValue(e)
			if err != nil {
				return nil, err
			}
			raw, err := kernel.Encode(c)
			if err != nil {
				return nil, err
			}
			items = append(items, raw)
			byBytes[string(raw)] = c
		}
		sort.Slice(items, func(i, j int) bool { return bytes.Compare(items[i], items[j]) < 0 })
		out := make([]any, len(items))
		for i, raw := range items {
			out[i] = byBytes[string(raw)]
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			c, err := canonicalValue(e)
			if err != nil {
				return nil, err
			}
			out[k] = c
		}
		return out, nil
	case nil, string, int64, bool:
		return v, nil
	}
	return nil, kernel.Errorf(CodeBatchInvalid, "a value of Go type %T has no canonical form", v)
}

// ValueMark is the short hash of a volatile value that the light change line carries.
func ValueMark(v any) (string, error) {
	sum, err := kernel.ContentHash(v)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum[:8]), nil
}
