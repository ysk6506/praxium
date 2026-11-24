package ingest

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// [REQ-762]
func TestUtcCellATimeWithAnOffsetBecomesTheTimeInUtcAndTheSameInstantIsTheSameValue(t *testing.T) {
	spec := valueSpec{prop: model.Property{Name: "when", ValueType: model.BaseValueTypeID(model.BaseTime)}, base: model.BaseTime}
	for in, want := range map[string]string{
		"2026-03-14T18:30:00+09:00":    "2026-03-14T09:30:00Z",
		"2026-03-14T09:30:00Z":         "2026-03-14T09:30:00Z",
		"2026-03-14T09:30:00+00:00":    "2026-03-14T09:30:00Z",
		"2026-03-14T04:30:00-05:00":    "2026-03-14T09:30:00Z",
		"2026-03-14T09:30:00.500Z":     "2026-03-14T09:30:00.5Z",
		"2026-03-14T09:30:00.1234567Z": "2026-03-14T09:30:00.123456Z",
		"2026-03-14T18:30:00.50+09:00": "2026-03-14T09:30:00.5Z",
		"2026-03-14":                   "2026-03-14T00:00:00Z",
	} {
		got, err := convertCell(spec, in)
		if err != nil || got != want {
			t.Errorf("%q: %v %v, want %q", in, got, err, want)
		}
	}
	if _, err := convertCell(spec, "last night"); err == nil {
		t.Error("a text that is no time was taken")
	}
}
