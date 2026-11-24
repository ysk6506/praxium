package pgstore

import (
	"context"
	"database/sql"
	"math/big"
	"strconv"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// A value that the check of the graph lets through is a value that the column of its kind takes and gives back as it is: random inputs (a
// fixed seed) and the edges of each kind are checked by the gate, and what passes is written and read back (ADR-0101, the one gate).
// [REQ-760] [REQ-762]
func TestVgateWhatTheGateLetsThroughComesBackFromTheColumnAsItWent(t *testing.T) {
	w := v2rWorldOf(t)
	ctx := context.Background()
	fx, err := graph.NewFixture()
	noErr(t, "fixture", err)
	rng := vgateRng{kernel.NewSeededRng(20261124)}
	byKind := map[model.BaseKind]vcolWhere{}
	for _, c := range vcolColumns(w) {
		byKind[c.kind] = c
	}
	for i, kind := range []model.BaseKind{model.BaseNumber, model.BaseQuantity, model.BaseTime} {
		c, ok := byKind[kind]
		if !ok {
			t.Fatalf("the standard model has no volatile attribute of the kind %s", kind)
		}
		no := 300 + i
		id := fx.ServerID("VGATE-" + strconv.Itoa(i))
		vcolRegister(t, w, no, id, c.typeNo)
		passed, refused := 0, 0
		for _, text := range vgateInputs(kind, rng) {
			var err error
			switch kind {
			case model.BaseTime:
				err = graph.TimeFits(text)
			default:
				err = graph.NumberFits(kind, text)
			}
			if err != nil {
				refused++
				continue
			}
			passed++
			item := volatileItem{objNo: int64(no), typeNo: c.typeNo, attrNo: c.attrNo, value: text, doc: provDoc{Grade: "fact", Basis: "observed", Seen: "2030-02-03T04:05:06Z"}}
			noErr(t, "write "+text, w.a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
				ch := &kgChunk{writer: newWriter(ctx, tx, w.kg.ID), a: w.a, seq: 1}
				ch.writeItems([]volatileItem{item})
				return ch.err
			}))
			var got graph.VolatileRow
			noErr(t, "read "+text, w.a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
				var err error
				got, _, err = (&kgSnapshot{a: w.a}).volatileOf(ctx, tx, regEntry{ref: id.String(), objNo: int64(no), typeNo: c.typeNo}, id, c.attrNo)
				return err
			}))
			if !vgateSame(kind, text, got.Value) {
				t.Errorf("%s %q came back as %#v", kind, text, got.Value)
			}
		}
		if passed < 20 || refused < 20 {
			t.Errorf("%s: %d passed and %d refused: the inputs do not reach both sides of the gate", kind, passed, refused)
		}
	}
}

func vgateSame(kind model.BaseKind, sent string, got any) bool {
	if kind == model.BaseTime {
		return got == sent
	}
	want, _ := new(big.Rat).SetString(sent)
	var back *big.Rat
	switch x := got.(type) {
	case int64:
		back = new(big.Rat).SetInt64(x)
	case string:
		back, _ = new(big.Rat).SetString(x)
		// a number comes back without a power of ten below 1e21
		if strings.ContainsAny(x, "eE") && kind == model.BaseNumber && back != nil && new(big.Rat).Abs(back).Cmp(new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil))) < 0 {
			return false
		}
	}
	return want != nil && back != nil && want.Cmp(back) == 0
}

func vgateInputs(kind model.BaseKind, rng vgateRng) []string {
	digits := func(n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteByte(byte('0' + rng.Intn(10)))
		}
		return b.String()
	}
	var out []string
	switch kind {
	case model.BaseNumber, model.BaseQuantity:
		out = append(out, "0", "-0", "1.10", "12.5", "5e-324", "1e-400", "1e400", "1.7976931348623157e308", "9007199254740992", "9007199254740993", "0.30000000000000001",
			"0x1p4", "1e5", ".5", "5.", "NaN", "-1e21", "123456789012345678901234567890", "1e21", "1e-7", "0.000001", "100000000000000000000")
		for i := 0; i < 400; i++ {
			s := [...]string{"", "-", "+"}[rng.Intn(3)] + digits(1+rng.Intn(20))
			if rng.Intn(2) == 0 {
				s += "." + digits(1+rng.Intn(20))
			}
			if rng.Intn(3) == 0 {
				s += "e" + strconv.Itoa(rng.Intn(700)-350)
			}
			out = append(out, s)
		}
	case model.BaseTime:
		out = append(out, "0001-01-01T00:00:00Z", "9999-12-31T23:59:59.999999Z", "0000-01-01T00:00:00Z", "10000-01-01T00:00:00Z", "1970-01-01T00:00:00Z", "2030-01-01T00:00:00+09:00")
		for i := 0; i < 400; i++ {
			s := strconv.Itoa(rng.Intn(10001)) + "-" + digits(2) + "-" + digits(2) + "T" + digits(2) + ":" + digits(2) + ":" + digits(2)
			if rng.Intn(2) == 0 {
				s += "." + digits(1+rng.Intn(7))
			}
			out = append(out, s+"Z")
			y := 1 + rng.Intn(9998)
			m, d, h, mi, sec := 1+rng.Intn(12), 1+rng.Intn(28), rng.Intn(24), rng.Intn(60), rng.Intn(60)
			frac := ""
			if rng.Intn(2) == 0 {
				frac = "." + strconv.Itoa(1+rng.Intn(999999))
			}
			out = append(out, vgatePad(y, 4)+"-"+vgatePad(m, 2)+"-"+vgatePad(d, 2)+"T"+vgatePad(h, 2)+":"+vgatePad(mi, 2)+":"+vgatePad(sec, 2)+frac+"Z")
		}
	}
	return out
}

func vgatePad(n, width int) string {
	s := strconv.Itoa(n)
	return strings.Repeat("0", width-len(s)) + s
}

// A batch with a value that the gate refuses is a problem of the batch and leaves the store as it was; a name of the longest size the gate
// allows is written and found by the name index; a value that the column could not take, if it ever got past the gate, comes out as a problem
// of the batch and not as an internal failure.
// [REQ-762] [REQ-761]
func TestVgateARefusedValueIsAProblemOfTheBatchAndALongestNameIsKept(t *testing.T) {
	r := v2wRigOf(t, graph.Options{})
	long := r.fx.Server("SN-LONG", "M1")
	long.Name = strings.Repeat("n", graph.MaxNameBytes)
	r.v2wApply(t, graph.Batch{Objects: []graph.ObjectUpsert{long}})
	tooLong := r.fx.Server("SN-TOO", "M1")
	tooLong.Name = strings.Repeat("n", graph.MaxNameBytes+1)
	_, err := r.app.Apply(r.ctx, r.kg.ID, r.lease, graph.Batch{Objects: []graph.ObjectUpsert{tooLong}})
	if kernel.CodeOf(err) != graph.CodeBatchInvalid {
		t.Errorf("a name past the limit: %v", err)
	}
	if n := unfitValue(nil, 1); n != nil {
		t.Errorf("no error became %v", n)
	}
	if n := r.v2wCount(t, `SELECT count(*) FROM `+r.a.typeTable(r.serverNo)+` WHERE name = $1`, long.Name); n != 1 {
		t.Errorf("the object with the longest name is not found by its name: %d", n)
	}
}

// vgateRng gives the integers that the inputs are made of, from the seeded generator of the kernel.
type vgateRng struct{ r *kernel.SeededRng }

// Intn is a number in [0, n).
func (g vgateRng) Intn(n int) int { return int(g.r.Uint64() % uint64(n)) }
