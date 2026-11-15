package kgread

import "fmt"

// Problem is a refusal as the catalogue of error codes names it: the code, its params, and the entries of the fields
// that are wrong. How it is shown (a status, a body, a tool error) is the adapter's.
type Problem struct {
	Code   string
	Params map[string]any
	Fields []FieldError
	// RetryAfter is the seconds a refused caller should wait; 0 when the problem has no such advice.
	RetryAfter int
}

// FieldError is one wrong field of a body: where it is, why, and the params of the reason.
type FieldError struct {
	Pointer string
	Code    string
	Params  map[string]any
}

// field is the entry of one wrong field of a body.
func field(pointer, code string, params map[string]any) FieldError {
	return FieldError{Pointer: pointer, Code: code, Params: params}
}

// fieldProblem is invalid_input.field with the entries of the wrong fields.
func fieldProblem(entries ...FieldError) *Problem {
	return &Problem{Code: "invalid_input.field", Fields: entries}
}

// BodyTooLargeError is what the reader of a body gives when the body is over the limit of the adapter. The adapter
// turns the error of its own reader into this one, because the limit is the adapter's and so is the way it is measured.
type BodyTooLargeError struct{ Limit int64 }

func (e *BodyTooLargeError) Error() string { return fmt.Sprintf("body larger than %d bytes", e.Limit) }
