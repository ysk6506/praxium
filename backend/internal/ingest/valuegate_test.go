package ingest

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// A cell is checked by the same gate as the graph: what the store cannot keep as it is is a problem of the cell (ADR-0101).
// [REQ-762] [REQ-760]
func TestGatecellAValueTheStoreCannotKeepIsAProblemOfItsCell(t *testing.T) {
	spec := func(k model.BaseKind, name string) valueSpec {
		return valueSpec{prop: model.Property{Name: name, ValueType: model.BaseValueTypeID(k)}, base: k}
	}
	for _, c := range []struct {
		kind model.BaseKind
		text string
		ok   bool
	}{
		{model.BaseNumber, "1.10", true}, {model.BaseNumber, "9007199254740993", false}, {model.BaseNumber, "0.30000000000000001", false},
		{model.BaseNumber, "1e-400", false}, {model.BaseNumber, "0x1p4", false},
		{model.BaseQuantity, "1.10", true}, {model.BaseQuantity, "0.30000000000000001", true}, {model.BaseQuantity, "0x1P-2", false},
		{model.BaseTime, "0001-01-01T00:00:00Z", true}, {model.BaseTime, "0000-01-01T00:00:00Z", false}, {model.BaseTime, "2030-01-01T09:00:00+09:00", true},
	} {
		_, err := convertCell(spec(c.kind, "x"), c.text)
		if (err == nil) != c.ok {
			t.Errorf("%s %q: %v, wanted ok=%v", c.kind, c.text, err, c.ok)
		}
	}
	if _, err := convertCell(spec(model.BaseString, "name"), strings.Repeat("n", graph.MaxNameBytes)); err != nil {
		t.Errorf("the longest name: %v", err)
	}
	if _, err := convertCell(spec(model.BaseString, "name"), strings.Repeat("n", graph.MaxNameBytes+1)); err == nil {
		t.Error("a name past the limit was taken")
	}
}
