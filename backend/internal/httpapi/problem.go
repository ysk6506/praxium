package httpapi

import (
	"net/http"

	"github.com/ysk6506/praxium/backend/internal/contract"
)

// problemMediaType is the media type of every error (RFC 9457).
const problemMediaType = "application/problem+json"

// problemTitles are the short English titles by group; people read the title in
// logs, screens build their words from code and params (rest-api.md 3-9).
var problemTitles = map[string]string{
	"auth": "Unauthorized", "permission": "Forbidden", "not_found": "Not found",
	"invalid_input": "Invalid input", "conflict": "Conflict", "rate_limited": "Too many requests",
	"blocked": "Blocked", "stale": "Stale", "server": "Server error",
}

// fieldError is one entry of errors in a problem: which field is wrong and why.
type fieldError struct {
	Pointer string         `json:"pointer"`
	Code    string         `json:"code"`
	Params  map[string]any `json:"params"`
}

// problem is the body of an error (rest-api.md 3-9).
type problem struct {
	Type    string         `json:"type"`
	Title   string         `json:"title"`
	Status  int            `json:"status"`
	Code    string         `json:"code"`
	Params  map[string]any `json:"params,omitempty"`
	TraceID string         `json:"trace_id"`
	Errors  []fieldError   `json:"errors,omitempty"`
}

// ProblemType is the address a problem's type points at: the explanation of the code.
func ProblemType(code string) string { return "/v1/problems/" + code }

// fail answers with the problem of a catalogue code. A code outside the
// catalogue is a bug of the server and is answered as server.internal.
func (c *call) fail(code string, params map[string]any, errs ...fieldError) {
	entry, ok := contract.LookupProblem(code)
	if !ok {
		c.s.log.Error("a handler used a code outside the catalogue", "code", code, "trace_id", c.trace)
		entry, _ = contract.LookupProblem("server.internal")
		params, errs = nil, nil
	}
	if entry.Status == http.StatusUnauthorized {
		c.w.Header().Set("WWW-Authenticate", "Bearer")
	}
	body := problem{
		Type: ProblemType(entry.Code), Title: problemTitles[entry.Group], Status: entry.Status,
		Code: entry.Code, Params: params, TraceID: c.trace, Errors: errs,
	}
	c.writeJSON(entry.Status, problemMediaType, body)
}

// notFound is the answer for a missing or invisible resource (both look alike).
func (c *call) notFound(resource string) {
	c.fail("not_found.resource", map[string]any{"resource": resource})
}
