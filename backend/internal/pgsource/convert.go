package pgsource

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// The way a value of the source becomes a value of a record (sources-and-adapters.md 2.3, ADR-0093): every scalar is its
// text, a number keeps all its digits (a numeric and an integer past 2^53 are read as the text the server gives), a
// time is written the way the definitions write it, and a json value, or an array that the server turned into json, is
// the JSON value itself, so that a path of a column can go into it. NULL is no value. A binary column is refused
// when the statement is described, so no binary value arrives here.
func convertValue(typ string, v any) (any, *ingest.SourceError) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64), nil
	case bool:
		return x, nil
	case string:
		return cleanText(x), nil
	case time.Time:
		return timeText(typ, x), nil
	case []byte:
		return convertBytes(typ, x)
	}
	return nil, sourceErr(ingest.SrcResponseFormat, "a value of the source has a type the adapter does not read", nil)
}

func convertBytes(typ string, b []byte) (any, *ingest.SourceError) {
	switch typ {
	case "JSON", "JSONB":
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		var out any
		if err := dec.Decode(&out); err != nil {
			return nil, sourceErr(ingest.SrcResponseFormat, "a json value of the source is not json", nil)
		}
		return out, nil
	case "BYTEA":
		return nil, sourceErr(ingest.SrcResponseFormat, "a binary value of the source cannot be read as text", nil)
	}
	return cleanText(string(b)), nil
}

// cleanText is the text with every byte that is no UTF-8 replaced (a database whose encoding is not UTF-8 may hold
// them), so that what goes on is always text.
func cleanText(s string) string { return strings.ToValidUTF8(s, "�") }

func timeText(typ string, t time.Time) string {
	switch typ {
	case "DATE":
		return t.Format("2006-01-02")
	case "TIME":
		return t.Format("15:04:05.999999999")
	case "TIMETZ":
		return t.Format("15:04:05.999999999Z07:00")
	case "TIMESTAMP":
		return t.Format("2006-01-02T15:04:05.999999999")
	}
	return t.UTC().Format(time.RFC3339Nano)
}
