package graph

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

func TestSumArithmeticIsExactAndOrderFree(t *testing.T) {
	rng := kernel.NewSeededRng(11)
	random := func() Sum {
		var s Sum
		for i := 0; i < len(s); i += 8 {
			v := rng.Uint64()
			for j := 0; j < 8; j++ {
				s[i+j] = byte(v >> (8 * j))
			}
		}
		return s
	}
	for i := 0; i < 200; i++ {
		a, b, c := random(), random(), random()
		if a.Add(b).Sub(b) != a {
			t.Fatalf("add and sub are not inverse for %s %s", a, b)
		}
		if a.Add(b).Add(c) != c.Add(a).Add(b) {
			t.Fatalf("the sum depends on the order")
		}
	}
	var max Sum
	for i := range max {
		max[i] = 0xff
	}
	one := Sum{31: 1}
	if !max.Add(one).IsZero() || max.Add(one).Sub(one) != max {
		t.Errorf("the sum does not wrap at 2^256")
	}
	if one.String() != "0000000000000000000000000000000000000000000000000000000000000001" {
		t.Errorf("sum text %s", one)
	}
}

func TestHashStateKeepsPartsAndCleansUp(t *testing.T) {
	var h HashState
	key := PartKey{Entity: "object", TypeID: "ot.x"}
	a, b := Sum{31: 5}, Sum{31: 9}
	h.Add(key, a)
	h.Add(key, b)
	if h.Rows != 2 || h.Parts[key].Rows != 2 || h.Sum != a.Add(b) {
		t.Fatalf("state %+v", h)
	}
	copyOf := h.Clone()
	h.Remove(key, a)
	h.Remove(key, b)
	if h.Rows != 0 || len(h.Parts) != 0 || !h.Sum.IsZero() {
		t.Errorf("an emptied state keeps parts: %+v", h)
	}
	if copyOf.Rows != 2 || copyOf.Equal(h) || !copyOf.Equal(copyOf.Clone()) {
		t.Errorf("clone shares state or Equal is wrong")
	}
	other := copyOf.Clone()
	other.Parts[key] = Part{Sum: Sum{1}, Rows: 2}
	if copyOf.Equal(other) {
		t.Errorf("Equal ignores a different part")
	}
}

func TestCanonicalValuesIgnoreListOrderAndKeepConfidentialValuesOut(t *testing.T) {
	h := hasher{reg: mustFixture(t).Reg, keys: NewTestKeyProvider("one")}
	open := model.Property{Sensitivity: model.SensitivityOpen}
	secret := model.Property{Sensitivity: model.SensitivityConfidential}
	a := []any{"x", int64(2), map[string]any{"k": []any{"b", "a"}}}
	b := []any{map[string]any{"k": []any{"a", "b"}}, "x", int64(2)}
	va, err := h.valueForHash(open, a)
	if err != nil {
		t.Fatal(err)
	}
	vb, _ := h.valueForHash(open, b)
	ea, _ := kernel.Encode(va)
	eb, _ := kernel.Encode(vb)
	if string(ea) != string(eb) {
		t.Errorf("list order changes the hashed value")
	}
	got, err := h.valueForHash(secret, "s3cret")
	if err != nil || got == "s3cret" {
		t.Fatalf("a confidential value went into the hash as it is: %v %v", got, err)
	}
	again, _ := h.valueForHash(secret, "s3cret")
	other, _ := h.valueForHash(secret, "s3cret!")
	if got != again || got == other {
		t.Errorf("the keyed hash is not a function of the value")
	}
	h2 := hasher{reg: h.reg, keys: NewTestKeyProvider("two")}
	if differentKey, _ := h2.valueForHash(secret, "s3cret"); differentKey == got {
		t.Errorf("the key does not enter the keyed hash")
	}
	if nothing, err := h.valueForHash(secret, nil); err != nil || nothing != nil {
		t.Errorf("no value must stay no value: %v %v", nothing, err)
	}
	if _, err := h.valueForHash(open, 1.5); err == nil {
		t.Errorf("a float has no canonical form and must be refused")
	}
}

func TestContentHashSeesWhatMattersOnly(t *testing.T) {
	fx := mustFixture(t)
	h := hasher{reg: fx.Reg, keys: NewTestKeyProvider("k")}
	typ, _ := fx.Reg.ObjectType(fx.typeID("Site"))
	base := ObjectRow{TypeNo: typ.TypeNo, Name: "n", Attrs: map[int]Attr{AttrSiteAddress: fact("a")}}
	want, err := h.objectContent(base)
	if err != nil {
		t.Fatal(err)
	}
	same := base.Clone()
	same.SourceRefs = []string{"somewhere"}
	attr := same.Attrs[AttrSiteAddress]
	attr.SourceTime, attr.SourceRef = "2030-01-01T00:00:00Z", "raw/1"
	same.Attrs[AttrSiteAddress] = attr
	same.SeqFrom, same.MinGrade = 9, model.GradeUnknown
	if got, _ := h.objectContent(same); got != want {
		t.Errorf("source references, times, the interval or min_grade changed the content hash")
	}
	for name, edit := range map[string]func(*ObjectRow){
		"name":  func(r *ObjectRow) { r.Name = "m" },
		"value": func(r *ObjectRow) { r.Attrs[AttrSiteAddress] = fact("b") },
		"grade": func(r *ObjectRow) {
			r.Attrs[AttrSiteAddress] = Attr{Value: "a", Grade: model.GradeAssumed, Basis: model.BasisConfigured}
		},
		"basis": func(r *ObjectRow) {
			r.Attrs[AttrSiteAddress] = Attr{Value: "a", Grade: model.GradeFact, Basis: model.BasisDeclared}
		},
	} {
		row := base.Clone()
		edit(&row)
		if got, _ := h.objectContent(row); got == want {
			t.Errorf("%s does not enter the content hash", name)
		}
	}
	link := LinkRow{TypeNo: 1, Src: fx.SiteID("a"), Dst: fx.SiteID("b"), Grade: model.GradeFact, Basis: model.BasisConfigured}
	lw, err := h.linkContent(link)
	if err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*LinkRow){
		"grade": func(r *LinkRow) { r.Grade = model.GradeAssumed },
		"basis": func(r *LinkRow) { r.Basis = model.BasisDerived },
		"dst":   func(r *LinkRow) { r.Dst = fx.SiteID("c") },
	} {
		row := link
		edit(&row)
		if got, _ := h.linkContent(row); got == lw {
			t.Errorf("link %s does not enter the content hash", name)
		}
	}
	if _, err := h.objectContent(ObjectRow{TypeNo: 99999}); err == nil {
		t.Errorf("an unknown type number must be refused")
	}
	if _, err := h.linkContent(LinkRow{TypeNo: 99999}); err == nil {
		t.Errorf("an unknown link type number must be refused")
	}
	if _, err := h.objectContent(ObjectRow{TypeNo: typ.TypeNo, Attrs: map[int]Attr{99999: fact("x")}}); err == nil {
		t.Errorf("an unknown attribute number must be refused")
	}
}
