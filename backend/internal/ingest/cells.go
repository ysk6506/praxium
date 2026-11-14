package ingest

import (
	"strconv"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// convertCell turns the text of a cell into the Go value the graph takes for the property: a text, an int64 or a
// bool. A number that is not an integer stays its decimal text, since the graph has no canonical form for a float.
// The value is checked the way the graph checks it, so a bad cell is found here, in the row it belongs to, and
// does not stop the whole batch.
func convertCell(s valueSpec, text string) (any, error) {
	p := s.prop
	switch s.base {
	case model.BaseInteger:
		n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "%q is not an integer", text)
		}
		return n, nil
	case model.BaseBoolean:
		switch strings.TrimSpace(text) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%q is not true or false", text)
	case model.BaseNumber, model.BaseQuantity:
		if _, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err != nil {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "%q is not a number", text)
		}
		return strings.TrimSpace(text), nil
	case model.BaseTime:
		if _, err := time.Parse(time.RFC3339, text); err != nil {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "%q is not a time of the form 2006-01-02T15:04:05Z", text)
		}
	case model.BaseEnum:
		if len(p.EnumValues) > 0 && !contains(p.EnumValues, text) {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "%q is none of the values %v of %s", text, p.EnumValues, p.Name)
		}
	}
	if _, err := model.NormalizeValue(p.ValueType, text); err != nil {
		return nil, err
	}
	return text, nil
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}
