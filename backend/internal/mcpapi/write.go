package mcpapi

import (
	"encoding/json"
	"net/http"

	"github.com/ysk6506/praxium/backend/internal/contract"
	"github.com/ysk6506/praxium/backend/internal/kgread"
)

const (
	problemMediaType = "application/problem+json"
	jsonMediaType    = "application/json"
)

// problemTitles are the short English titles by group, as the REST server has them.
var problemTitles = map[string]string{
	"auth": "Unauthorized", "permission": "Forbidden", "not_found": "Not found",
	"invalid_input": "Invalid input", "conflict": "Conflict", "rate_limited": "Too many requests",
	"blocked": "Blocked", "stale": "Stale", "server": "Server error",
}

// problemBody is the body of a refusal that is an HTTP matter (rfc 9457), the same shape the REST server answers.
type problemBody struct {
	Type    string         `json:"type"`
	Title   string         `json:"title"`
	Status  int            `json:"status"`
	Code    string         `json:"code"`
	Params  map[string]any `json:"params,omitempty"`
	TraceID string         `json:"trace_id"`
}

// answer is where an HTTP answer is written: the writer and the trace id of the call.
type answer struct {
	w     http.ResponseWriter
	trace string
}

// header sets the headers every answer of /mcp carries: JSON only, never cached, never sniffed.
func (a answer) header(media string) {
	h := a.w.Header()
	h.Set("Content-Type", media)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
}

func (a answer) json(status int, media string, body any) {
	data, err := json.Marshal(body)
	if err != nil {
		a.header(media)
		a.w.WriteHeader(http.StatusInternalServerError)
		_, _ = a.w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error."}}`))
		return
	}
	a.header(media)
	a.w.WriteHeader(status)
	_, _ = a.w.Write(append(data, '\n'))
}

// problem answers with a problem of the catalogue; a code outside it is a bug of the server and answers as one.
func (a answer) problem(code string, params map[string]any) {
	entry, ok := contract.LookupProblem(code)
	if !ok {
		entry, _ = contract.LookupProblem("server.internal")
		params = nil
	}
	if entry.Status == http.StatusUnauthorized {
		a.w.Header().Set("WWW-Authenticate", "Bearer")
	}
	a.json(entry.Status, problemMediaType, problemBody{
		Type: "/v1/problems/" + entry.Code, Title: problemTitles[entry.Group], Status: entry.Status,
		Code: entry.Code, Params: params, TraceID: a.trace,
	})
}

// kgProblem answers with the problem the gate or the use case gave.
func (a answer) kgProblem(p *kgread.Problem) {
	if p.RetryAfter > 0 {
		a.w.Header().Set("Retry-After", itoa(p.RetryAfter))
	}
	a.problem(p.Code, p.Params)
}

// rpcError answers an HTTP error with a JSON-RPC error body. id is the id of the request, when it had one.
func (a answer) rpcError(status int, id json.RawMessage, e rpcError) {
	a.json(status, jsonMediaType, errMessage{JSONRPC: "2.0", ID: id, Error: &e})
}

// result answers 200 with the result of a request.
func (a answer) result(id json.RawMessage, result any) {
	a.json(http.StatusOK, jsonMediaType, okMessage{JSONRPC: "2.0", ID: id, Result: result})
}

// accepted answers 202 to a notification, with no body.
func (a answer) accepted() {
	a.header(jsonMediaType)
	a.w.Header().Del("Content-Type")
	a.w.WriteHeader(http.StatusAccepted)
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
