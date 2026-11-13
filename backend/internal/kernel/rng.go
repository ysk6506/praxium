package kernel

import (
	"sync"
)

// Rng is the port for randomness. Code outside the kernel receives an Rng and
// never reads global or system randomness itself (REQ-001).
type Rng interface {
	// Uint64 returns the next 64 random bits.
	Uint64() uint64
	// Float64 returns the next number in [0, 1).
	Float64() float64
}

// SeededRng is a deterministic Rng (splitmix64): the same seed gives the same
// sequence on every platform. It is safe for concurrent use.
type SeededRng struct {
	mu    sync.Mutex
	seed  uint64
	state uint64
}

// NewSeededRng starts the sequence of seed.
func NewSeededRng(seed uint64) *SeededRng {
	return &SeededRng{seed: seed, state: seed}
}

// Seed returns the seed this Rng was made with.
func (r *SeededRng) Seed() uint64 { return r.seed }

// Uint64 returns the next 64 random bits.
func (r *SeededRng) Uint64() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state += 0x9e3779b97f4a7c15
	z := r.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// Float64 returns the next number in [0, 1) with 53 random bits.
func (r *SeededRng) Float64() float64 {
	return float64(r.Uint64()>>11) / (1 << 53)
}

// Fork derives an independent stream from a label, so results do not depend
// on the order in which unrelated parts draw numbers. The same seed and label
// always give the same stream.
func (r *SeededRng) Fork(label string) *SeededRng {
	return NewSeededRng(deriveSeed(r.seed, label))
}
