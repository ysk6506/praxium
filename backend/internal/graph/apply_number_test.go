package graph

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// [REQ-760]
func TestNumkeyANumberIsTheSameNumberInEveryFormItIsWritten(t *testing.T) {
	for _, c := range []struct {
		kind model.BaseKind
		a, b any
		same bool
	}{
		{model.BaseQuantity, "1.10", "1.1", true},
		{model.BaseQuantity, "5", int64(5), true},
		{model.BaseQuantity, "1e2", "100", true},
		{model.BaseQuantity, "1.5", "1.6", false},
		{model.BaseNumber, "5.0", int64(5), true},
		{model.BaseNumber, "0.10", "0.1", true},
		{model.BaseNumber, "0.1", "0.2", false},
	} {
		x, okX := numberKey(c.kind, c.a)
		y, okY := numberKey(c.kind, c.b)
		if !okX || !okY || (x == y) != c.same {
			t.Errorf("%s %v %v: %q %q", c.kind, c.a, c.b, x, y)
		}
	}
	for _, kind := range []model.BaseKind{model.BaseQuantity, model.BaseNumber} {
		for _, bad := range []any{"abc", 1.5, map[string]any{}, nil} {
			if _, ok := numberKey(kind, bad); ok {
				t.Errorf("%s: %v has a number key", kind, bad)
			}
		}
	}
}

// [REQ-760]
func TestNumkeyTheSameNumberInAnotherFormMakesNoChangeAndAnotherNumberDoes(t *testing.T) {
	u := newUnit(t, nil, Options{})
	var typ model.ObjectType
	for _, ot := range u.fx.Reg.ObjectTypes() {
		if ot.Name == "Ups" {
			typ = ot
		}
	}
	key := []KeyPart{{Kind: PartProperty, Ref: "pr.serialized.manufacturer", Value: "ACME"},
		{Kind: PartProperty, Ref: "pr.serialized.serial_number", Value: "UPS-1"}}
	id, err := NewObjectID(typ.ID, key)
	if err != nil {
		t.Fatal(err)
	}
	write := func(q, n any) Batch {
		w := func(no int, v any) VolatileWrite {
			return VolatileWrite{Object: id, AttrNo: no, Value: v, Grade: model.GradeFact, Basis: model.BasisObserved}
		}
		return Batch{Objects: []ObjectUpsert{{Type: typ.ID, Key: key, Name: "Ups 1"}}, Volatile: []VolatileWrite{w(96, q), w(97, n)}}
	}
	if !u.apply(write("1.10", "5.0")).Changed() {
		t.Fatal("the first write changed nothing")
	}
	if u.apply(write("1.1", int64(5))).Changed() {
		t.Error("the same numbers in other forms made a change")
	}
	if !u.apply(write("1.5", int64(5))).Changed() {
		t.Error("another quantity was taken as the same")
	}
}
