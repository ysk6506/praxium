package kernel

import (
	"fmt"
	"strings"
	"sync"
)

// maxIDLength is the longest ID, in bytes.
const maxIDLength = 128

// ID is an identifier of a thing of kind K. K is a marker type that is never
// instantiated (declare `type Object struct{}` in the unit that owns the
// thing), so an ID of one kind cannot be passed where another is expected.
// IDs appear in URLs: 1 to 128 characters of letters, digits and _ . : -,
// starting with a letter or a digit.
type ID[K any] struct {
	value string
}

// ParseID validates raw and returns it as an ID of kind K.
func ParseID[K any](raw string) (ID[K], error) {
	if !validID(raw) {
		return ID[K]{}, Errorf(CodeInvalidValue,
			"invalid id %q: use 1-%d characters of letters, digits and _ . : - (starting with a letter or a digit)",
			raw, maxIDLength)
	}
	return ID[K]{value: raw}, nil
}

func validID(raw string) bool {
	if raw == "" || len(raw) > maxIDLength {
		return false
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if alnum || (i > 0 && strings.IndexByte("_.:-", c) >= 0) {
			continue
		}
		return false
	}
	return true
}

// String returns the ID as text.
func (id ID[K]) String() string { return id.value }

// IsZero reports whether the ID is the zero value (no ID).
func (id ID[K]) IsZero() bool { return id.value == "" }

// MarshalText makes IDs encode as plain strings in JSON.
func (id ID[K]) MarshalText() ([]byte, error) { return []byte(id.value), nil }

// UnmarshalText validates while decoding, so a bad ID never enters the program.
func (id *ID[K]) UnmarshalText(text []byte) error {
	parsed, err := ParseID[K](string(text))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

// IDGenerator is the port that issues new IDs.
type IDGenerator interface {
	// Next returns a new ID text starting with prefix. A prefix is a valid ID
	// itself, so the result is always a valid ID.
	Next(prefix string) string
}

// NewID asks g for an ID of kind K. It fails only when prefix is not valid.
func NewID[K any](g IDGenerator, prefix string) (ID[K], error) {
	if !validID(prefix) || len(prefix) > maxIDLength-idSuffixLength-1 {
		return ID[K]{}, Errorf(CodeInvalidValue, "invalid id prefix %q", prefix)
	}
	return ParseID[K](g.Next(prefix))
}

// idSuffixLength is the longest suffix the generators add after the prefix
// and the dash.
const idSuffixLength = 20

// SequentialIDs issues prefix-000001, prefix-000002, ... with one counter per
// prefix. The same calls in the same order give the same IDs. It is safe for
// concurrent use.
type SequentialIDs struct {
	mu     sync.Mutex
	counts map[string]uint64
}

// NewSequentialIDs starts every prefix at 1.
func NewSequentialIDs() *SequentialIDs {
	return &SequentialIDs{counts: map[string]uint64{}}
}

// Next returns the next ID of prefix.
func (s *SequentialIDs) Next(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[prefix]++
	return fmt.Sprintf("%s-%06d", prefix, s.counts[prefix])
}

// RandomIDs issues prefix-<16 hex characters> drawn from an Rng. With a
// SeededRng the sequence is reproducible; with the system Rng it is not.
type RandomIDs struct {
	rng Rng
}

// NewRandomIDs issues IDs from rng.
func NewRandomIDs(rng Rng) *RandomIDs {
	return &RandomIDs{rng: rng}
}

// Next returns a new ID of prefix.
func (r *RandomIDs) Next(prefix string) string {
	return fmt.Sprintf("%s-%016x", prefix, r.rng.Uint64())
}
