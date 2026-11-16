package mcpapi

import (
	"encoding/json"

	"github.com/ysk6506/praxium/backend/internal/kgread"
)

// What the server repeats of the caller's own words (a name it did not know, a field it did not accept, the client's
// name for the log) goes through kgread.CleanText first and is cut short (kg-mcp.md 4.3).
const (
	shortText  = 64
	clientText = 100
)

func cleanShort(s string) string {
	out, _ := kgread.CleanText(s, shortText)
	return out
}

// cleanValue cleans every string of a JSON-shaped value (the params of a problem), keys included.
func cleanValue(v any) any {
	switch x := v.(type) {
	case string:
		out, _ := kgread.CleanText(x, 200)
		return out
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = cleanValue(s)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = cleanValue(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			ck, _ := kgread.CleanText(k, 200)
			out[ck] = cleanValue(e)
		}
		return out
	case json.Number, bool, nil, int, int64, float64:
		return x
	}
	return "?"
}
