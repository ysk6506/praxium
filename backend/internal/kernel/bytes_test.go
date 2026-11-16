package kernel_test

import (
	"bytes"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func TestSeededBytesRepeatTheSameSequenceForTheSameSeed(t *testing.T) {
	first, second := make([]byte, 21), make([]byte, 21)
	kernel.NewSeededBytes(7).Fill(first)
	kernel.NewSeededBytes(7).Fill(second)
	if !bytes.Equal(first, second) {
		t.Fatal("the same seed gave different bytes")
	}
	other := make([]byte, 21)
	kernel.NewSeededBytes(8).Fill(other)
	if bytes.Equal(first, other) {
		t.Fatal("two seeds gave the same bytes")
	}
}

func TestSeededBytesContinueWhereTheyStoppedAndFillEveryByte(t *testing.T) {
	src := kernel.NewSeededBytes(3)
	a, b := make([]byte, 32), make([]byte, 32)
	src.Fill(a)
	src.Fill(b)
	if bytes.Equal(a, b) {
		t.Fatal("a second fill repeated the first")
	}
	whole := make([]byte, 64)
	kernel.NewSeededBytes(3).Fill(whole)
	if !bytes.Equal(whole, append(append([]byte{}, a...), b...)) {
		t.Fatal("two fills are not the same as one fill of the same length")
	}
}

// These touch the system source of randomness, so they assert shapes, never values.
func TestSystemBytesFillAndDiffer(t *testing.T) {
	a, b := make([]byte, 32), make([]byte, 32)
	kernel.SystemBytes{}.Fill(a)
	kernel.SystemBytes{}.Fill(b)
	if bytes.Equal(a, b) || bytes.Equal(a, make([]byte, 32)) {
		t.Fatal("system bytes repeat or are all zero")
	}
}
