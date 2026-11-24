package pgstore

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The same number sent again in another form is the same value in the row path too, as in the staged path: the column keeps a number and
// gives back one form, and a write that differs only in the form writes nothing and takes no change number (REQ-566, ADR-0101).
// [REQ-760]
func TestNumformTheSameNumberInAnotherFormIsTheSameValueInTheRowPathToo(t *testing.T) {
	r := v2wRigOf(t, graph.Options{})
	var typ model.ObjectType
	for _, ot := range r.fx.Reg.ObjectTypes() {
		if ot.Name == "Ups" {
			typ = ot
		}
	}
	if typ.Name == "" {
		t.Fatal("the model has no type Ups")
	}
	key := []graph.KeyPart{{Kind: graph.PartProperty, Ref: "pr.serialized.manufacturer", Value: "ACME"},
		{Kind: graph.PartProperty, Ref: "pr.serialized.serial_number", Value: "UPS-1"}}
	id, err := graph.NewObjectID(typ.ID, key)
	noErr(t, "id", err)
	const quantity, number = 96, 97 // runtime_at_load_s (quantity), load_pct (number)
	write := func(q, n any) graph.Batch {
		w := func(no int, v any) graph.VolatileWrite {
			return graph.VolatileWrite{Object: id, AttrNo: no, Value: v, Grade: model.GradeFact, Basis: model.BasisObserved}
		}
		return graph.Batch{Objects: []graph.ObjectUpsert{{Type: typ.ID, Key: key, Name: "Ups 1"}}, Volatile: []graph.VolatileWrite{w(quantity, q), w(number, n)}}
	}
	if res := r.v2wApply(t, write("1.10", "5.0")); !res.Changed() {
		t.Fatal("the first write changed nothing")
	}
	for _, again := range [][2]any{{"1.10", "5.0"}, {"1.1", int64(5)}, {"1.100", "5"}, {int64(1), "5.00"}} {
		want := again[0] == int64(1) // 1 is another quantity than 1.10
		if res := r.v2wApply(t, write(again[0], again[1])); res.Changed() != want {
			t.Errorf("%v: changed %v, wanted %v", again, res.Changed(), want)
		}
	}
	if res := r.v2wApply(t, write("1.5", int64(5))); !res.Changed() {
		t.Error("another quantity was taken as the same")
	}
}
