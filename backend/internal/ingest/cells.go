package ingest

import (
	"strconv"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// convertCell turns the text of a cell into the Go value the graph takes for the property: a text, an int64 or a
// bool. A number that is not an integer stays its decimal text, since the graph has no canonical form for a float.
// The value is checked the way the graph checks it, so a bad cell is found here, in the row it belongs to, and
// does not stop the whole batch.
func convertCell(s valueSpec, text string) (any, error) {
	text = utcTime(s, dateAsTime(s, text))
	if s.prop.Name == "name" && len(text) > graph.MaxNameBytes {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a name of %d bytes is too long: at most %d", len(text), graph.MaxNameBytes)
	}
	v, err := convertBase(s, text)
	if err != nil {
		return nil, err
	}
	if v != nil {
		return v, nil
	}
	if _, err := model.NormalizeValue(s.prop.ValueType, text); err != nil {
		return nil, err
	}
	return text, nil
}

// dateAsTime gives a date with no time of the day (the form a date cell of a workbook has, 2026-03-14) the time it
// stands for, midnight UTC, when the property is a time; every other text is as it was.
func dateAsTime(s valueSpec, text string) string {
	if s.base != model.BaseTime {
		return text
	}
	trimmed := strings.TrimSpace(text)
	if _, err := time.Parse("2006-01-02", trimmed); err != nil {
		return text
	}
	return trimmed + "T00:00:00Z"
}

// utcTime gives a time that has an offset (2026-03-14T18:30:00+09:00) the one form the graph takes, UTC ending in Z (ADR-0101),
// so that the same instant is the same value whatever way a source writes it. The time is kept to the microsecond, which is
// all the store holds. A text that is no time, or no time of a property that is a time, is as it was and is refused later.
func utcTime(s valueSpec, text string) string {
	if s.base != model.BaseTime {
		return text
	}
	t, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return text
	}
	return t.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
}

// convertBase checks a cell by its base kind. It returns the value for the kinds that have a Go value of their own and
// nil for the kinds whose value is the text itself.
func convertBase(s valueSpec, text string) (any, error) {
	trimmed := strings.TrimSpace(text)
	switch s.base {
	case model.BaseInteger:
		n, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "%q is not an integer", text)
		}
		return n, nil
	case model.BaseBoolean:
		if trimmed != "true" && trimmed != "false" {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "%q is not true or false", text)
		}
		return trimmed == "true", nil
	case model.BaseNumber, model.BaseQuantity:
		if err := graph.NumberFits(s.base, trimmed); err != nil {
			return nil, err // the one gate of the graph: a value that the store cannot keep as it is is a problem of this cell
		}
		return trimmed, nil
	case model.BaseTime, model.BaseEnum:
		return nil, checkText(s, text)
	}
	return nil, nil
}

// checkText checks the kinds whose value is the text itself.
func checkText(s valueSpec, text string) error {
	if s.base == model.BaseTime {
		return graph.TimeFits(text)
	} else if p := s.prop; len(p.EnumValues) > 0 && !contains(p.EnumValues, text) {
		return kernel.Errorf(kernel.CodeInvalidValue, "%q is none of the values %v of %s", text, p.EnumValues, p.Name)
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}
