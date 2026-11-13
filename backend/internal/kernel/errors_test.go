package kernel_test

import (
	"errors"
	"fmt"
	"regexp"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The catalogue is a contract: a code is never renamed or removed. Adding a
// code means adding it here too, on purpose.
func TestCodeCatalogueIsStable(t *testing.T) {
	want := []kernel.Code{"conflict", "internal", "invalid_value", "not_found", "read_only_violation", "unsupported"}
	got := kernel.Codes()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("Codes() = %v, want %v", got, want)
	}
	form := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	for _, code := range got {
		if !form.MatchString(string(code)) {
			t.Errorf("code %q is not snake_case", code)
		}
		if meaning, ok := code.Meaning(); !ok || meaning == "" {
			t.Errorf("code %q has no meaning", code)
		}
	}
}

func TestUnknownCodeHasNoMeaning(t *testing.T) {
	if _, ok := kernel.Code("never_defined").Meaning(); ok {
		t.Fatal("an unknown code has a meaning")
	}
}

func TestErrorText(t *testing.T) {
	cause := errors.New("disk full")
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"formatted", kernel.Errorf(kernel.CodeNotFound, "object %q", "r1"), `not_found: object "r1"`},
		{"wrapped", kernel.Wrap(kernel.CodeInternal, cause, "write failed"), "internal: write failed: disk full"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCodeOf(t *testing.T) {
	cause := errors.New("plain")
	tests := []struct {
		name string
		err  error
		want kernel.Code
	}{
		{"nil", nil, ""},
		{"direct", kernel.Errorf(kernel.CodeConflict, "x"), kernel.CodeConflict},
		{"wrapped by fmt", fmt.Errorf("outer: %w", kernel.Errorf(kernel.CodeUnsupported, "x")), kernel.CodeUnsupported},
		{"outermost wins", kernel.Wrap(kernel.CodeInvalidValue, kernel.Errorf(kernel.CodeNotFound, "x"), "y"), kernel.CodeInvalidValue},
		{"foreign error is internal", cause, kernel.CodeInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := kernel.CodeOf(tt.err); got != tt.want {
				t.Fatalf("CodeOf = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWrapKeepsCause(t *testing.T) {
	cause := errors.New("root")
	err := kernel.Wrap(kernel.CodeInternal, cause, "outer")
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is cannot reach the cause")
	}
	if kernel.Errorf(kernel.CodeConflict, "x").Unwrap() != nil {
		t.Fatal("an Error without a cause unwraps to something")
	}
}
