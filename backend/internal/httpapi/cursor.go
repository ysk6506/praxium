package httpapi

import (
	"encoding/base64"
	"encoding/json"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// cursorVersion is bumped when the cursor's content changes shape.
const cursorVersion = 1

// cursorBody is what a cursor carries (rest-api.md 3-3): where the last page
// ended (the sort key values and the id, not a position), a fingerprint of the
// query so a cursor is only taken with the same filter and sort, and the
// generation the list was read at. For the registry the generation is the
// model hash; for a KG it will be the KG's generation and change_seq.
type cursorBody struct {
	V     int      `json:"v"`
	Gen   string   `json:"g"`
	Query string   `json:"q"`
	Keys  []string `json:"k"`
	ID    string   `json:"i"`
}

// cursorError says why a cursor was refused.
type cursorError int

const (
	cursorOK cursorError = iota
	// cursorMalformed is a cursor that is not one this server issued, or for another query.
	cursorMalformed
	// cursorStaleGeneration is a cursor from another generation: read the state again.
	cursorStaleGeneration
)

// encodeCursor turns a cursor into an opaque text.
func encodeCursor(b cursorBody) string {
	b.V = cursorVersion
	data, err := json.Marshal(b)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

// decodeCursor reads a cursor back and checks it belongs to this query and generation.
func decodeCursor(text, generation, query string) (cursorBody, cursorError) {
	raw, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return cursorBody{}, cursorMalformed
	}
	var b cursorBody
	if err := json.Unmarshal(raw, &b); err != nil || b.V != cursorVersion || b.ID == "" {
		return cursorBody{}, cursorMalformed
	}
	if b.Query != query {
		return cursorBody{}, cursorMalformed
	}
	if b.Gen != generation {
		return cursorBody{}, cursorStaleGeneration
	}
	return b, cursorOK
}

// fingerprint is a short stable text of a query's filter, sort and fields.
func fingerprint(parts ...string) string {
	h := kernel.HashBytes([]byte(joinParts(parts)))
	return h.String()[:16]
}

func joinParts(parts []string) string {
	out := ""
	for _, p := range parts {
		out += p + "\x00"
	}
	return out
}
