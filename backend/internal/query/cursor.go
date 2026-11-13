package query

import (
	"encoding/base64"
	"encoding/json"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// cursor is the opaque continuation of a paged answer. It carries the
// generation and the change number it was issued at, so a page in the middle
// of a flow of changes continues at the same place (kg-queries.md 3), the
// fingerprint of the query it belongs to, and the position after which the
// next page starts: an object id (find) or a step and a link id (neighbors).
type cursor struct {
	Gen     int    `json:"g"`
	Seq     int64  `json:"s"`
	Version string `json:"v,omitempty"`
	Query   string `json:"q"`
	Step    int    `json:"t,omitempty"`
	After   string `json:"k"`
}

func (c cursor) encode() string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(text, query string) (cursor, error) {
	var c cursor
	raw, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil || json.Unmarshal(raw, &c) != nil || c.After == "" {
		return c, kernel.Errorf(CodeCursorInvalid, "the cursor is not one this query issued")
	}
	if c.Query != query {
		return c, kernel.Errorf(CodeCursorInvalid, "the cursor belongs to another query")
	}
	return c, nil
}

// fingerprint identifies a query apart from its page: two requests with the
// same fingerprint ask the same thing.
func fingerprint(name string, parts ...any) string {
	h, err := kernel.ContentHash(append([]any{"praxium.query.v1", name}, parts...))
	if err != nil {
		return name
	}
	return h.String()[:16]
}
