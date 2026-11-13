package kernel

import (
	"errors"
	"fmt"
	"sort"
)

// Code is the stable, machine-readable name of an error. Codes appear in API
// responses and in logs, so a code is never renamed or reused: a retired code
// stays in the catalogue marked as retired.
type Code string

// The seed of the code catalogue. Units add codes for their own failures in
// the same snake_case form and list them in the catalogue the contract unit
// publishes.
const (
	// CodeInternal is a failure the caller cannot fix: a bug or a broken dependency.
	CodeInternal Code = "internal"
	// CodeInvalidValue is an argument or stored value that breaks a rule.
	CodeInvalidValue Code = "invalid_value"
	// CodeNotFound is a reference to something that does not exist.
	CodeNotFound Code = "not_found"
	// CodeConflict is a valid request that conflicts with the current state.
	CodeConflict Code = "conflict"
	// CodeUnsupported is a request this build, profile or source cannot serve.
	CodeUnsupported Code = "unsupported"
	// CodeReadOnlyViolation is an attempt to write to a real system. Praxium
	// only ever reads real systems (REQ-011).
	CodeReadOnlyViolation Code = "read_only_violation"
)

var catalogue = map[Code]string{
	CodeInternal:          "A bug or a broken dependency; retrying does not help.",
	CodeInvalidValue:      "An argument or a stored value breaks a rule.",
	CodeNotFound:          "A referenced thing does not exist.",
	CodeConflict:          "The request conflicts with the current state.",
	CodeUnsupported:       "This build, profile or source cannot serve the request.",
	CodeReadOnlyViolation: "Something tried to write to a real system.",
}

// Codes lists every code in the catalogue, sorted.
func Codes() []Code {
	out := make([]Code, 0, len(catalogue))
	for code := range catalogue {
		out = append(out, code)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Meaning is the one-line meaning of a code, or false for an unknown code.
func (c Code) Meaning() (string, bool) {
	meaning, ok := catalogue[c]
	return meaning, ok
}

// Error is the error type every unit returns. It carries a Code, a message
// for people reading logs, and optionally the error it wraps.
type Error struct {
	Code    Code
	Message string
	Cause   error
}

// Errorf builds an Error with a formatted message.
func Errorf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap builds an Error that keeps cause for errors.Is and errors.As.
func Wrap(code Code, cause error, message string) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}

// Error implements the error interface: "code: message", then the cause.
func (e *Error) Error() string {
	if e.Cause == nil {
		return string(e.Code) + ": " + e.Message
	}
	return string(e.Code) + ": " + e.Message + ": " + e.Cause.Error()
}

// Unwrap returns the wrapped error, if any.
func (e *Error) Unwrap() error { return e.Cause }

// CodeOf returns the code of the first Error in err's chain. An error that is
// not an Error, or a nil error, has CodeInternal or the empty code.
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	var found *Error
	if errors.As(err, &found) {
		return found.Code
	}
	return CodeInternal
}
