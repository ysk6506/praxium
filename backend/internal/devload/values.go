package devload

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// convert turns the JSON of a truth value into the Go form the graph unit takes for the base kind of the
// property: a text, an int64, a bool, a list or a map. A number that is not an integer becomes its decimal
// text, since the graph has no canonical form for a float.
func convert(base model.BaseKind, raw json.RawMessage) (any, error) {
	switch base {
	case model.BaseList, model.BaseMap, model.BaseQuantity:
		return convertTree(base, raw)
	}
	// a scalar is read without a decoder, which would allocate a buffer for every value
	if len(raw) == 0 || raw[0] == '{' || raw[0] == '[' {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a truth value of a %s property is not a scalar", base)
	}
	return fit(base, textOf(raw))
}

func convertTree(base model.BaseKind, raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "a truth value is not JSON")
	}
	if base == model.BaseQuantity {
		if _, isMap := v.(map[string]any); !isMap {
			return asText(v), nil
		}
	}
	return tidy(v), nil
}

func fit(base model.BaseKind, text string) (any, error) {
	switch base {
	case model.BaseInteger:
		return asInteger(text)
	case model.BaseBoolean:
		return asBool(text)
	}
	return text, nil
}

func asInteger(text string) (any, error) {
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%q is not an integer", text)
	}
	return n, nil
}

func asBool(text string) (any, error) {
	switch text {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return nil, kernel.Errorf(kernel.CodeInvalidValue, "%q is not a boolean", text)
}

// asText is the text of a scalar: a string as it is, a number as written, a bool as true or false.
func asText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

// tidy rewrites the numbers inside a decoded list or map: an integer is an int64, any other number its text.
func tidy(v any) any {
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}
		return x.String()
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = tidy(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = tidy(e)
		}
		return out
	}
	return v
}

// textOf is the text of a raw truth value as an identity key reads it: a string unquoted, a literal as written.
func textOf(raw json.RawMessage) string {
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	}
	return string(raw)
}
