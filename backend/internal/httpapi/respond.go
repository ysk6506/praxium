package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/access"
)

const jsonMediaType = "application/json"

// call is one request being served: the pieces every handler needs.
type call struct {
	s       *Server
	w       http.ResponseWriter
	r       *http.Request
	trace   string
	authed  bool
	subject access.Subject
	// asked is the object the question named in its body, for the answer of a missing object.
	asked string
	// idem is the claim on the Idempotency-Key of a creating or acting call.
	idem *idemSlot
}

// writeJSON writes a JSON body with its status and media type.
func (c *call) writeJSON(status int, mediaType string, body any) {
	data, err := json.Marshal(body)
	if err != nil {
		c.s.log.Error("cannot encode a response", "error", err, "trace_id", c.trace)
		status, mediaType = http.StatusInternalServerError, problemMediaType
		data = []byte(`{"type":"/v1/problems/server.internal","title":"Server error","status":500,"code":"server.internal","trace_id":"` + c.trace + `"}`)
	}
	c.w.Header().Set("Content-Type", mediaType)
	c.w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	c.w.WriteHeader(status)
	_, _ = c.w.Write(data)
	c.remember(status, data)
}

// envelope is the shape of every success answer: data and meta (rest-api.md 3-2).
type envelope struct {
	Data any            `json:"data"`
	Meta map[string]any `json:"meta"`
}

// ok writes a 200 answer; trace_id is added to meta.
func (c *call) ok(data any, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	meta["trace_id"] = c.trace
	c.writeJSON(http.StatusOK, jsonMediaType, envelope{Data: data, Meta: meta})
}

// allowedAction is one entry of meta.allowed_actions (rest-api.md section 4).
type allowedAction struct {
	Action     string  `json:"action"`
	Allowed    bool    `json:"allowed"`
	ReasonCode *string `json:"reason_code"`
}

// allowedActions lists what the subject may do with a resource. The type
// registry offers one action in this step: edit (the edit_definitions ability
// of the role table); the proposal flow that performs it comes later.
func (c *call) allowedActions(kind, id string) []allowedAction {
	d := c.s.authz.Decide(c.subject, access.AbilityEditDefinitions, access.Target{Kind: kind, ID: id})
	entry := allowedAction{Action: "edit", Allowed: d.Allowed}
	if !d.Allowed {
		reason := d.ReasonCode
		entry.ReasonCode = &reason
	}
	return []allowedAction{entry}
}

// quote makes a strong entity tag from a hash text.
func quote(hash string) string { return `"` + hash + `"` }

// digestETag is the entity tag of bytes: the hex SHA-256 in quotes.
func digestETag(data []byte) string {
	sum := sha256.Sum256(data)
	return quote(hex.EncodeToString(sum[:]))
}

// notModified sets ETag and the caching headers and, when the request's
// If-None-Match already names this tag, answers 304 and returns true. A GET
// whose answer depends on the caller (the token) varies on Authorization.
func (c *call) notModified(etag string) bool {
	h := c.w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "private, no-cache")
	h.Add("Vary", "Authorization")
	if !matchesETag(c.r.Header.Get("If-None-Match"), etag) {
		return false
	}
	c.w.WriteHeader(http.StatusNotModified)
	return true
}

// matchesETag implements the weak comparison of If-None-Match: a list of tags,
// each optionally weak, or *.
func matchesETag(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || strings.TrimPrefix(part, "W/") == etag {
			return true
		}
	}
	return false
}
