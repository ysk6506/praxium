package graph

import (
	"context"
	"math/big"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// sameValue says whether the value of a write is the value that is there. A number or a quantity is compared as the number it
// is, for a store keeps it in a column of numbers and gives back one number for "1.10" and "1.1" or for 5 and "5.0"; any other
// value is compared by its mark.
func (c *chunkBuilder) sameValue(ctx context.Context, w VolatileWrite, cur any) (bool, error) {
	if kind, ok := c.numberKind(ctx, w); ok {
		a, okA := numberKey(kind, w.Value)
		b, okB := numberKey(kind, cur)
		if okA && okB {
			return a == b, nil
		}
	}
	m1, err := ValueMark(w.Value)
	if err != nil {
		return false, err
	}
	m2, err := ValueMark(cur)
	return m1 == m2, err
}

// numberKind is the kind of the attribute of a write when it is a number or a quantity.
func (c *chunkBuilder) numberKind(ctx context.Context, w VolatileWrite) (model.BaseKind, bool) {
	row, ok, err := c.snap.Object(ctx, w.Object)
	if err != nil || !ok {
		return "", false
	}
	typ, ok := c.a.reg.ObjectTypeByNo(row.TypeNo)
	if !ok {
		return "", false
	}
	for _, p := range c.a.reg.EffectiveProperties(typ.ID) {
		if p.AttrNo != w.AttrNo {
			continue
		}
		if vt, ok := c.a.reg.ValueType(p.ValueType); ok && (vt.Base == model.BaseNumber || vt.Base == model.BaseQuantity) {
			return vt.Base, true
		}
	}
	return "", false
}

// numberKey is a form of a number that is the same for the same number: an exact fraction for a quantity (a column of the type
// numeric) and the shortest text of the nearest float for a number (a column of the type double precision).
func numberKey(kind model.BaseKind, v any) (string, bool) {
	var text string
	switch x := v.(type) {
	case int64:
		text = strconv.FormatInt(x, 10)
	case string:
		text = x
	default:
		return "", false
	}
	if kind == model.BaseQuantity {
		r, ok := new(big.Rat).SetString(text)
		if !ok {
			return "", false
		}
		return r.RatString(), true
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return "", false
	}
	return strconv.FormatFloat(f, 'g', -1, 64), true
}
