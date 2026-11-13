package kernel_test

import (
	"math"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The golden values below were computed by an independent implementation of
// the encoding written in hash.go (a short Python script), not by this
// package. A failing golden test means the encoding changed: fix the code, or
// introduce a new encoding version; never edit the expected value.
// [REQ-001]
func TestEncodeGolden(t *testing.T) {
	tests := []struct {
		name  string
		value any
		hex   string
	}{
		{"null", nil, "00"},
		{"false", false, "01"},
		{"true", true, "02"},
		{"zero", 0, "030000000000000000"},
		{"minus one", -1, "03ffffffffffffffff"},
		{"max int64", int64(math.MaxInt64), "037fffffffffffffff"},
		{"min int64", int64(math.MinInt64), "038000000000000000"},
		{"unsigned within range", uint8(255), "0300000000000000ff"},
		{"empty string", "", "040000000000000000"},
		{"ascii", "hello", "04000000000000000568656c6c6f"},
		{"korean", "프락시움", "04000000000000000ced9484eb9dbdec8b9cec9b80"},
		{"empty list", []any{}, "050000000000000000"},
		{"nil slice is empty", []string(nil), "050000000000000000"},
		{"mixed list", []any{1, "a", nil, true}, "050000000000000004030000000000000001040000000000000001610002"},
		{"empty map", map[string]any{}, "060000000000000000"},
		{"nil map is empty", map[string]int(nil), "060000000000000000"},
		{"nil pointer is null", (*int)(nil), "00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := kernel.Encode(tt.value)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if hexOf(got) != tt.hex {
				t.Fatalf("Encode = %s, want %s", hexOf(got), tt.hex)
			}
		})
	}
}

// [REQ-001]
func TestContentHashGolden(t *testing.T) {
	nested := map[string]any{
		"name": "rack-01",
		"tags": []any{"x", "y"},
		"size": map[string]any{"u": 42, "ok": true},
		"note": nil,
	}
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"null", nil, "6e340b9cffb37a989ca544e6bb780a2c78901d3fb33738768511a30617afa01d"},
		{"true", true, "dbc1b4c900ffe48d575b5da5c638040125f65db0fe3e24494b76ea986457d986"},
		{"minus one", -1, "339eed010a5e07b4314cb00f1234d636dc30df8fce73917165c299c6b4811804"},
		{"hello", "hello", "1d88eab6361a5e887c2ec2fdb69c1babd2bfe872d7fb5d5d1d1242d85d070632"},
		{"korean", "프락시움", "fd104680e079611a951546c94074cc5184363cdf6fb22f5ce1c6d5b79d3702b0"},
		{"empty map", map[string]any{}, "69c29c051218dbea38795b1cdfd816855c312a9cbc0b1daaa79bb9a0b895361a"},
		{"map", map[string]any{"b": 2, "a": 1}, "47a64a2fab90e7ed099a9c51a5f1fb3840f01b9dd276b6b18bd643cb911b48f8"},
		{"nested", nested, "98ab72fd79e156a37fa120b49edae185b165369cf5eed85ee5815c784307576f"},
		{"keys sort by bytes", map[string]any{"가": 3, "z": 2, "é": 1}, "9c04cda2a5e98ee8ad405a7c5f9a4181c85c6b8c42476557457f8f15c465c095"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := kernel.ContentHash(tt.value)
			if err != nil {
				t.Fatalf("ContentHash: %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("ContentHash = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestContentHashIgnoresRepresentation(t *testing.T) {
	type named string
	type count int32
	tests := []struct {
		name string
		a, b any
	}{
		{"integer widths", int8(7), uint64(7)},
		{"named types", named("x"), "x"},
		{"named integers", count(3), 3},
		{"typed and untyped slices", []string{"a", "b"}, []any{"a", "b"}},
		{"arrays and slices", [2]int{1, 2}, []int{1, 2}},
		{"typed and untyped maps", map[string]int{"a": 1, "b": 2}, map[string]any{"b": 2, "a": 1}},
		{"pointer and value", ptr("v"), "v"},
		{"bytes are integers", []byte{1, 2}, []any{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, errA := kernel.ContentHash(tt.a)
			b, errB := kernel.ContentHash(tt.b)
			if errA != nil || errB != nil {
				t.Fatalf("ContentHash errors: %v, %v", errA, errB)
			}
			if a != b {
				t.Fatalf("hashes differ: %s vs %s", a, b)
			}
		})
	}
}

func TestContentHashSeparatesValues(t *testing.T) {
	values := []any{nil, false, true, 0, 1, "", "0", "1", []any{}, []any{nil}, map[string]any{}, map[string]any{"": nil}, []any{"a", "b"}, []any{"ab"}}
	seen := map[kernel.Hash]int{}
	for i, v := range values {
		h, err := kernel.ContentHash(v)
		if err != nil {
			t.Fatalf("ContentHash(%v): %v", v, err)
		}
		if j, dup := seen[h]; dup {
			t.Fatalf("values %d (%v) and %d (%v) hash equal", j, values[j], i, v)
		}
		seen[h] = i
	}
}

func TestEncodeRejects(t *testing.T) {
	type thing struct{ A int }
	tests := []struct {
		name  string
		value any
		code  kernel.Code
	}{
		{"float", 1.5, kernel.CodeUnsupported},
		{"struct", thing{A: 1}, kernel.CodeUnsupported},
		{"function", func() {}, kernel.CodeUnsupported},
		{"channel", make(chan int), kernel.CodeUnsupported},
		{"integer keys", map[int]string{1: "a"}, kernel.CodeUnsupported},
		{"unsigned above int64", uint64(math.MaxInt64) + 1, kernel.CodeInvalidValue},
		{"invalid utf-8 value", "a\xffb", kernel.CodeInvalidValue},
		{"invalid utf-8 key", map[string]int{"\xff": 1}, kernel.CodeInvalidValue},
		{"bad item in a list", []any{1, 2.5}, kernel.CodeUnsupported},
		{"bad value in a map", map[string]any{"a": 2.5}, kernel.CodeUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := kernel.Encode(tt.value)
			if got := kernel.CodeOf(err); got != tt.code {
				t.Fatalf("code = %q, want %q (err %v)", got, tt.code, err)
			}
			if _, err := kernel.ContentHash(tt.value); err == nil {
				t.Fatal("ContentHash accepted a value that has no encoding")
			}
		})
	}
}

func TestHashRoundTrip(t *testing.T) {
	want := kernel.HashBytes([]byte("abc"))
	if want.String() != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("HashBytes(abc) = %s", want)
	}
	got, err := kernel.ParseHash(want.String())
	if err != nil || got != want {
		t.Fatalf("ParseHash = %v, %v", got, err)
	}
	for _, bad := range []string{"", "zz", strings.Repeat("a", 63), strings.Repeat("a", 66)} {
		if _, err := kernel.ParseHash(bad); kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Fatalf("ParseHash(%q) = %v, want invalid_value", bad, err)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 2*len(b))
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&15])
	}
	return string(out)
}
