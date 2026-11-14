package query

import (
	"errors"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The codes of this unit. Each names one way a request can be refused; the
// HTTP adapter maps each to an entry of the error catalogue of the API
// (rest-api.md 3-9), named in the comment.
const (
	// CodeHiddenFilter is a filter on an attribute the caller may not read
	// (permission.hidden_attribute). It is refused, not filtered quietly,
	// because a quiet filter would leak the value by presence and absence.
	CodeHiddenFilter kernel.Code = "query_hidden_filter"
	// CodeVolatileFilter is a filter on a volatile attribute, which this build
	// does not serve (invalid_input.filter_field_unknown).
	CodeVolatileFilter kernel.Code = "query_volatile_filter"
	// CodeScopeUnsupported is a scope_id, which needs stored scopes that do not exist yet (invalid_input.param_invalid).
	CodeScopeUnsupported kernel.Code = "query_scope_unsupported"
	// CodeFilterField is a filter on a name no attribute of the chosen types has (invalid_input.filter_field_unknown).
	CodeFilterField kernel.Code = "query_filter_field_unknown"
	// CodeFilterOp is an operation outside equals, in, prefix and range (invalid_input.filter_op_unknown).
	CodeFilterOp kernel.Code = "query_filter_op_unknown"
	// CodeCursorInvalid is a cursor this query did not issue (invalid_input.cursor_invalid).
	CodeCursorInvalid kernel.Code = "query_cursor_invalid"
	// CodeLimitInvalid is a limit outside 1 to the maximum (invalid_input.limit_invalid).
	CodeLimitInvalid kernel.Code = "query_limit_invalid"
	// CodeIncludeUnknown is an include name the query does not know (invalid_input.include_unknown).
	CodeIncludeUnknown kernel.Code = "query_include_unknown"
	// CodeObjectNotFound is an object that does not exist, is retired at the
	// read point, or is hidden from the caller; the three look the same (not_found.resource).
	CodeObjectNotFound kernel.Code = "query_object_not_found"
	// CodeInput is any other input that breaks a rule (invalid_input.field).
	CodeInput kernel.Code = kernel.CodeInvalidValue
)

var codeMeanings = map[kernel.Code]string{
	CodeHiddenFilter:     "a filter names an attribute the caller may not read",
	CodeVolatileFilter:   "filters on volatile attributes are not served yet",
	CodeScopeUnsupported: "scope_id needs stored scopes, which do not exist yet",
	CodeFilterField:      "no attribute of the chosen types has that name",
	CodeFilterOp:         "the filter operation is not equals, in, prefix or range",
	CodeCursorInvalid:    "the cursor was not issued for this query",
	CodeLimitInvalid:     "the limit is not a whole number from 1 to the maximum",
	CodeIncludeUnknown:   "the include list names something the query cannot include",
	CodeObjectNotFound:   "no such object at this read point, or it is not visible to the caller",
}

// Codes lists the codes of this unit, sorted.
func Codes() []kernel.Code {
	out := make([]kernel.Code, 0, len(codeMeanings))
	for c := range codeMeanings {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// CodeMeaning is the one-line meaning of a code of this unit, or false.
func CodeMeaning(code kernel.Code) (string, bool) {
	m, ok := codeMeanings[code]
	return m, ok
}

// fieldInput is an input error that also says which field of the request it is about (invalid_input.field).
type fieldInput struct {
	field string
	err   *kernel.Error
}

func (e *fieldInput) Error() string { return e.err.Error() }
func (e *fieldInput) Unwrap() error { return e.err }

// inputf is a CodeInput error about one field of the question; the field is named as the API names it.
func inputf(field, format string, args ...any) error {
	return &fieldInput{field: field, err: kernel.Errorf(CodeInput, format, args...)}
}

// FieldOf is the request field an error is about, or "" when it names none.
func FieldOf(err error) string {
	var f *fieldInput
	if errors.As(err, &f) {
		return f.field
	}
	return ""
}
