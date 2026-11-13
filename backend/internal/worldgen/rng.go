package worldgen

import (
	"math/bits"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// draw is a small counter-based random stream (splitmix64). It is a value
// type with no lock, started from a key that is mixed from the injected Rng
// and a label, so every object draws its own numbers and the result does not
// depend on the order in which unrelated objects are made.
type draw struct{ s uint64 }

func newDraw(key uint64) draw { return draw{s: key} }

func (d *draw) u64() uint64 {
	d.s += 0x9e3779b97f4a7c15
	z := d.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// intn returns a number in [0, n); n must be positive.
func (d *draw) intn(n int) int {
	hi, _ := bits.Mul64(d.u64(), uint64(n))
	return int(hi)
}

// between returns a number in [lo, hi].
func (d *draw) between(lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + d.intn(hi-lo+1)
}

// float returns a number in [0, 1).
func (d *draw) float() float64 { return float64(d.u64()>>11) / (1 << 53) }

// chance is true with probability p.
func (d *draw) chance(p float64) bool { return p > 0 && d.float() < p }

// hash64 hashes text (FNV-1a with a final mix).
func hash64(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return mix(h, 0x2545f4914f6cdd1d)
}

// mix combines two numbers into one well-spread number.
func mix(a, b uint64) uint64 {
	d := newDraw(a ^ (b * 0x9e3779b97f4a7c15))
	return d.u64()
}

// worldKeys are the independent streams of one world, all taken from the
// injected Rng once at the start.
type worldKeys struct {
	obj, layout, service, change, view uint64
}

func drawKeys(rng kernel.Rng) worldKeys {
	return worldKeys{
		obj: rng.Uint64(), layout: rng.Uint64(), service: rng.Uint64(),
		change: rng.Uint64(), view: rng.Uint64(),
	}
}

// forID is the stream of one object.
func (k worldKeys) forID(id string) draw { return newDraw(mix(k.obj, hash64(id))) }

// splitEven divides total into parts as evenly as possible and returns the
// size of part i.
func splitEven(total, parts, i int) int {
	if parts <= 0 {
		return 0
	}
	n := total / parts
	if i < total%parts {
		n++
	}
	return n
}

// splitStart is the sum of the sizes of the parts before part i.
func splitStart(total, parts, i int) int {
	if parts <= 0 {
		return 0
	}
	q, r := total/parts, total%parts
	return i*q + min(i, r)
}

func ceilDiv(a, b int) int {
	if b <= 0 {
		return 0
	}
	return (a + b - 1) / b
}
