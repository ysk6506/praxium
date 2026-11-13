package kernel_test

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Reference values of splitmix64, computed independently of this package.
func TestSeededRngGolden(t *testing.T) {
	tests := []struct {
		seed uint64
		want []uint64
	}{
		{0, []uint64{0xe220a8397b1dcdaf, 0x6e789e6aa1b965f4, 0x06c45d188009454f}},
		{42, []uint64{0xbdd732262feb6e95, 0x28efe333b266f103, 0x47526757130f9f52}},
	}
	for _, tt := range tests {
		rng := kernel.NewSeededRng(tt.seed)
		if rng.Seed() != tt.seed {
			t.Fatalf("Seed = %d, want %d", rng.Seed(), tt.seed)
		}
		for i, want := range tt.want {
			if got := rng.Uint64(); got != want {
				t.Fatalf("seed %d value %d = %#x, want %#x", tt.seed, i, got, want)
			}
		}
	}
}

func TestSeededRngIsReproducible(t *testing.T) {
	a, b, other := kernel.NewSeededRng(7), kernel.NewSeededRng(7), kernel.NewSeededRng(8)
	same, differs := true, false
	for i := 0; i < 100; i++ {
		x, y, z := a.Uint64(), b.Uint64(), other.Uint64()
		same = same && x == y
		differs = differs || x != z
	}
	if !same || !differs {
		t.Fatalf("same seed equal = %v, other seed differs = %v", same, differs)
	}
}

func TestFloat64StaysInRange(t *testing.T) {
	rng := kernel.NewSeededRng(1)
	sum := 0.0
	const n = 10000
	for i := 0; i < n; i++ {
		f := rng.Float64()
		if f < 0 || f >= 1 {
			t.Fatalf("Float64 = %v, outside [0, 1)", f)
		}
		sum += f
	}
	if mean := sum / n; mean < 0.45 || mean > 0.55 {
		t.Fatalf("mean = %v, want about 0.5", mean)
	}
}

func TestFork(t *testing.T) {
	parent := kernel.NewSeededRng(5)
	first := parent.Fork("sources").Uint64()
	parent.Uint64() // drawing from the parent must not change a fork
	if again := parent.Fork("sources").Uint64(); again != first {
		t.Fatalf("fork changed after the parent drew: %#x vs %#x", again, first)
	}
	if other := parent.Fork("schedule").Uint64(); other == first {
		t.Fatal("different labels gave the same stream")
	}
	if other := kernel.NewSeededRng(6).Fork("sources").Uint64(); other == first {
		t.Fatal("different seeds gave the same fork")
	}
}

var _ kernel.Rng = (*kernel.SeededRng)(nil)
