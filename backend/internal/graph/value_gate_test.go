package graph

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// [REQ-762] [REQ-760]
func TestGateWhatTheStoreCannotKeepAsItIsIsRefusedForANumberAQuantityAndATime(t *testing.T) {
	for _, c := range []struct {
		kind model.BaseKind
		val  any
		ok   bool
	}{
		{model.BaseNumber, "1.10", true}, {model.BaseNumber, "12.5", true}, {model.BaseNumber, int64(1) << 53, true}, {model.BaseNumber, "5e-324", true},
		{model.BaseNumber, "1.7976931348623157e308", true}, {model.BaseNumber, "-0", true}, {model.BaseNumber, "1e5", true}, {model.BaseNumber, ".5", true},
		{model.BaseNumber, "1e-400", false}, {model.BaseNumber, "1e400", false}, {model.BaseNumber, "9007199254740993", false}, {model.BaseNumber, int64(1)<<53 + 1, false},
		{model.BaseNumber, "0.30000000000000001", false}, {model.BaseNumber, "0x1p4", false}, {model.BaseNumber, "NaN", false}, {model.BaseNumber, "Inf", false},
		{model.BaseNumber, "1e99999", false}, {model.BaseNumber, "", false}, {model.BaseNumber, " 5", false}, {model.BaseNumber, strings.Repeat("1", 401), false},
		{model.BaseQuantity, "64.5", true}, {model.BaseQuantity, "1.10", true}, {model.BaseQuantity, "0.30000000000000001", true}, {model.BaseQuantity, "9007199254740993", true},
		{model.BaseQuantity, int64(1)<<62 + 1, true}, {model.BaseQuantity, "-.5", true},
		{model.BaseQuantity, "0x1p4", false}, {model.BaseQuantity, "0x1P-2", false}, {model.BaseQuantity, "1e2", false}, {model.BaseQuantity, "NaN", false},
		{model.BaseQuantity, "1,5", false}, {model.BaseQuantity, "", false}, {model.BaseQuantity, "5.5.5", false},
	} {
		if err := NumberFits(c.kind, c.val); (err == nil) != c.ok {
			t.Errorf("%s %v: %v, wanted ok=%v", c.kind, c.val, err, c.ok)
		}
	}
	for text, ok := range map[string]bool{
		"0001-01-01T00:00:00Z": true, "9999-12-31T23:59:59.999999Z": true, "2030-01-01T00:00:00Z": true,
		"0000-01-01T00:00:00Z": false, "10000-01-01T00:00:00Z": false, "0000-12-31T23:59:59Z": false, "2030-01-01T00:00:00+09:00": false,
	} {
		if err := TimeFits(text); (err == nil) != ok {
			t.Errorf("time %q: %v, wanted ok=%v", text, err, ok)
		}
	}
}

// [REQ-761]
func TestGateTheNameOfAnObjectIsAtMostTheBytesTheNameIndexesAllow(t *testing.T) {
	fx := mustFixture(t)
	v := newValidator(fx.Reg)
	for _, c := range []struct {
		name string
		ok   bool
	}{{strings.Repeat("a", MaxNameBytes), true}, {strings.Repeat("a", MaxNameBytes+1), false}, {strings.Repeat("가", MaxNameBytes/3), true}, {strings.Repeat("가", MaxNameBytes/3+1), false}} {
		u := fx.Server("SN-1", "M1")
		u.Name = c.name
		_, _, err := v.object(u)
		if (err == nil) != c.ok {
			t.Errorf("a name of %d bytes: %v", len(c.name), err)
		}
		if !c.ok {
			wantCode(t, "a long name", err, CodeBatchInvalid)
		}
	}
}
