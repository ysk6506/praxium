package kernel_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// These tests touch the real clock and the system source of randomness, so
// they assert shapes, never values.
func TestSystemClockIsUTCAndMoves(t *testing.T) {
	clock := kernel.SystemClock{}
	first := clock.Now()
	if first.Location() != time.UTC || first.Year() < 2026 {
		t.Fatalf("Now = %v", first)
	}
	if second := clock.Now(); second.Before(first) {
		t.Fatalf("the clock went back: %v then %v", first, second)
	}
}

func TestSystemRngSeedsDiffer(t *testing.T) {
	a, b := kernel.NewSystemRng(), kernel.NewSystemRng()
	if a.Seed() == b.Seed() {
		t.Fatal("two system seeds are equal")
	}
	if kernel.SystemSeed() == kernel.SystemSeed() {
		t.Fatal("two system seeds are equal")
	}
}

func TestNewToken(t *testing.T) {
	form := regexp.MustCompile(`^[0-9a-f]+$`)
	tests := []struct{ bytes, chars int }{{0, 0}, {1, 2}, {18, 36}}
	for _, tt := range tests {
		token := kernel.NewToken(tt.bytes)
		if len(token) != tt.chars || (tt.chars > 0 && !form.MatchString(token)) {
			t.Fatalf("NewToken(%d) = %q", tt.bytes, token)
		}
	}
	if kernel.NewToken(18) == kernel.NewToken(18) {
		t.Fatal("two tokens are equal")
	}
}
