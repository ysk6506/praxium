package graph

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// [REQ-762] [REQ-564]
func TestUtcTimeAValueOfTheKindTimeIsInTheOneFormOfTheStoreOrItIsARecordThatDoesNotFit(t *testing.T) {
	fx := mustFixture(t)
	v := newValidator(fx.Reg)
	for text, ok := range map[string]bool{
		"2030-01-01T00:00:00Z":           true,
		"2030-01-01T00:00:00.5Z":         true,
		"2030-01-01T00:00:00.123456Z":    true,
		"2030-01-01T00:00:00+09:00":      false, // an offset
		"2030-01-01T00:00:00+00:00":      false, // UTC, but not ending in Z
		"2030-01-01T00:00:00.50Z":        false, // a zero at the end of the fraction
		"2030-01-01T00:00:00.1234567Z":   false, // finer than a database keeps
		"2030-01-01T00:00:00":            false, // no zone: it would be read in the zone of the session
		"2030-01-01":                     false,
		"2030-01-01T00:00:00z":           false,
		" 2030-01-01T00:00:00Z":          false,
		"yesterday":                      false,
		"2030-13-01T00:00:00Z":           false,
		"2030-01-01T00:00:00.000000001Z": false,
	} {
		u := fx.Server("SN-1", "M1")
		u.Attrs = map[int]Attr{13: fact(text)}
		_, _, err := v.object(u)
		if ok && err != nil {
			t.Errorf("%q was refused: %v", text, err)
		}
		if !ok {
			wantCode(t, text, err, CodeBatchInvalid)
		}
	}
}

// [REQ-762]
func TestUtcTimeTheTimeASourceSawIsOfThatFormToo(t *testing.T) {
	fx := mustFixture(t)
	v := newValidator(fx.Reg)
	typ, _ := fx.Reg.ObjectType(fx.typeID("PhysicalServer"))
	server := fx.ServerID("SN-1")
	for text, ok := range map[string]bool{"": true, "2030-01-01T00:00:00Z": true, "2030-01-01T09:00:00+09:00": false, "last night": false} {
		err := v.volatile(typ, fx.Power(server, "on", text))
		if ok && err != nil {
			t.Errorf("the time seen %q was refused: %v", text, err)
		}
		if !ok && kernel.CodeOf(err) != CodeBatchInvalid {
			t.Errorf("the time seen %q: %v", text, err)
		}
	}
}

// [REQ-762]
func TestUtcTimeAQuantityThatIsVolatileIsANumberBecauseItsColumnHoldsOneNumber(t *testing.T) {
	fx := mustFixture(t)
	v := newValidator(fx.Reg)
	// the check is the one of the value kind: a map is refused for a quantity only where the value goes into a column
	vt := model.Property{ValueType: model.BaseValueTypeID(model.BaseQuantity), Name: "heat"}
	if err := v.volatileKind(vt, map[string]any{"value": "1", "unit": "kw"}); err == nil {
		t.Error("a quantity with a unit in it was taken as a volatile value")
	}
	if err := v.volatileKind(vt, "64.5"); err != nil {
		t.Errorf("a decimal text: %v", err)
	}
	if err := v.volatileKind(vt, int64(3)); err != nil {
		t.Errorf("an integer: %v", err)
	}
}
