package mcpapi

import (
	"encoding/base64"
	"net/http"
	"unicode/utf8"

	"github.com/ysk6506/praxium/backend/internal/access"
)

// route settles which version's rules answer a message and hands it to them. A message that cannot be placed is
// turned away with the rejection of kg-mcp.md 2.1 (the rows H, U and N8 to N10).
func (h *Handler) route(a answer, r *http.Request, req *rpcRequest, subject access.Subject) {
	hdr := r.Header.Values("Mcp-Protocol-Version")
	value := ""
	if len(hdr) > 0 {
		value = hdr[0]
	}
	v := decideVersion(req, value, len(hdr) > 0)
	if len(hdr) > 1 {
		v = reject(http.StatusBadRequest, codeHeaderMismatch, "The MCP-Protocol-Version header is given more than once.", nil)
	}
	if v.reject != nil {
		h.countRejection(v.reject)
		a.rpcError(v.reject.status, idOfRequest(req), v.reject.err)
		return
	}
	ex := &exchange{h: h, a: a, r: r, req: req, subject: subject, version: v.version}
	if v.initialize {
		h.counts.add(countInitialize)
		ex.initialize()
		return
	}
	h.counts.add(v.version)
	if v.version == version2026 {
		if rej := ex.modernChecks(); rej != nil {
			h.counts.add(countHeaderMismatch)
			a.rpcError(rej.status, idOfRequest(req), rej.err)
			return
		}
	}
	if !req.hasID {
		a.accepted()
		return
	}
	ex.dispatch()
}

// countRejection counts a request turned away for its version.
func (h *Handler) countRejection(rej *rejection) {
	switch rej.err.Code {
	case codeUnsupportedVers:
		h.counts.add(countUnsupportedHeader)
	case codeHeaderMismatch:
		if rej.err.Message == noHeaderMessage {
			h.counts.add(countNoVersionHeader)
		} else {
			h.counts.add(countHeaderMismatch)
		}
	}
}

// exchange is one message on its way to an answer.
type exchange struct {
	h       *Handler
	a       answer
	r       *http.Request
	req     *rpcRequest
	subject access.Subject
	version string
}

func idOfRequest(req *rpcRequest) []byte {
	if req.hasID {
		return req.id
	}
	return nil
}

// modernChecks is what a request of 2026-07-28 must also hold (rows N9, N10): the _meta that carries the capabilities of
// the client, and the headers Mcp-Method and (for tools/call) Mcp-Name, which must say what the body says. A
// notification needs none of the headers: the revision does not define them for it.
func (ex *exchange) modernChecks() *rejection {
	meta, _ := objectAt(ex.req.params, "_meta")
	if _, ok := meta[metaCapsKey].(map[string]any); !ok {
		return &rejection{http.StatusBadRequest, rpcError{Code: codeInvalidParams, Message: `The request must carry "` + metaCapsKey + `" in params._meta.`}}
	}
	if !ex.req.hasID {
		return nil
	}
	if got, ok := singleHeader(ex.r, "Mcp-Method"); !ok || got != ex.req.method {
		return headerMismatch("The Mcp-Method header is missing or does not match the method of the body.")
	}
	if ex.req.method != "tools/call" {
		return nil
	}
	got, ok := singleHeader(ex.r, "Mcp-Name")
	if !ok {
		return headerMismatch("The Mcp-Name header is missing.")
	}
	name, ok := decodeHeaderValue(got)
	if !ok {
		return headerMismatch("The Mcp-Name header is not valid: its base64 form is broken.")
	}
	if body, _ := ex.req.params["name"].(string); body != name {
		return headerMismatch("The Mcp-Name header does not match params.name of the body.")
	}
	return nil
}

func headerMismatch(msg string) *rejection {
	return &rejection{http.StatusBadRequest, rpcError{Code: codeHeaderMismatch, Message: msg}}
}

// singleHeader gives the one value of a header; a header given twice, or not at all, is not a value.
func singleHeader(r *http.Request, name string) (string, bool) {
	v := r.Header.Values(name)
	if len(v) != 1 {
		return "", false
	}
	return v[0], true
}

// decodeHeaderValue reads a header value that may use the base64 form of the specification: "=?base64?" and "?=" around
// the base64 of the UTF-8 text, written in lower case exactly. Anything else is the value itself; a value that wears the
// form and is not valid base64 of UTF-8 is not a value (false).
func decodeHeaderValue(v string) (string, bool) {
	const prefix, suffix = "=?base64?", "?="
	if len(v) < len(prefix)+len(suffix) || v[:len(prefix)] != prefix || v[len(v)-len(suffix):] != suffix {
		return v, true
	}
	raw, err := base64.StdEncoding.DecodeString(v[len(prefix) : len(v)-len(suffix)])
	if err != nil || !utf8.Valid(raw) {
		return "", false
	}
	return string(raw), true
}
