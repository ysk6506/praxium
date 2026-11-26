package graph

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The forms the staged rows are stored in and the hashes of their content, which the computation of the objects publishes (ADR-0115).

// [REQ-930]
func TestRcpTheStoredFormsOfTheRowsAreTheOnesTheyAreReadBackFrom(t *testing.T) {
	for i, g := range model.Grades() {
		if no, err := GradeNo(g); err != nil || int(no) != i {
			t.Errorf("%s: %d %v", g, no, err)
		}
	}
	if _, err := GradeNo(model.Grade("rcp-not-a-grade")); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a grade that is not one: %v", err)
	}
	attrs := map[int]Attr{2: {Value: "x", Grade: model.Grades()[0], Basis: model.Basis("observed"), SourceRef: "s/d/k"}}
	if doc, err := EncodeAttrs(attrs); err != nil || !strings.Contains(string(doc), `"2":{"v":"x"`) {
		t.Errorf("%s %v", doc, err)
	}
	if _, err := EncodeAttrs(map[int]Attr{1: {Value: make(chan int)}}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a value that cannot be stored: %v", err)
	}
	if refs, err := EncodeRefs(nil); err != nil || string(refs) != "[]" {
		t.Errorf("%s %v", refs, err)
	}
	if got := MinGrade(attrs); got != model.Grades()[0] {
		t.Errorf("%s", got)
	}
	entry, err := EncodeVolatileEntry(VolatileWrite{Value: "on", Grade: model.Grades()[0], Basis: model.Basis("observed"), SeenAt: "2026-01-01T00:00:00Z", Ref: "r"})
	if err != nil || !strings.Contains(string(entry), `"v":"on"`) || !strings.Contains(string(entry), `"t":"2026-01-01T00:00:00Z"`) {
		t.Errorf("%s %v", entry, err)
	}
	if _, err := EncodeVolatileEntry(VolatileWrite{Value: make(chan int)}); err == nil {
		t.Error("a value that cannot be stored")
	}
}

// [REQ-930]
func TestRcpAFirstKeyIsStoredAndReadBackAndADamagedOneIsRefused(t *testing.T) {
	if got, err := EncodeKey(nil); got != nil || err != nil {
		t.Errorf("no key: %v %v", got, err)
	}
	key := []KeyPart{{Kind: PartProperty, Ref: "p", Value: "v"}, {Kind: PartContext, Ref: "c"}}
	text, err := EncodeKey(key)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeKey([]byte(text.(string)))
	if err != nil || len(back) != 2 || back[0].Value != "v" || back[0].Ref != "p" || back[1].Kind != PartContext {
		t.Errorf("%+v %v", back, err)
	}
	if got, err := DecodeKey(nil); got != nil || err != nil {
		t.Errorf("an empty key: %v %v", got, err)
	}
	for _, bad := range []string{"{", `[{"k":"context","r":"c","o":["a b"]}]`} {
		if _, err := DecodeKey([]byte(bad)); kernel.CodeOf(err) != kernel.CodeInternal {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

// [REQ-930]
func TestRcpTheContentHashOfAnObjectDependsOnItsNameAndItsAttributes(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	h := NewContentHasher(reg, NewTestKeyProvider("rcp"), KGID{})
	a, errA := h.Object(1, "a", nil)
	b, errB := h.Object(1, "b", nil)
	again, errC := h.Object(1, "a", nil)
	if errA != nil || errB != nil || errC != nil || a != again || a == b {
		t.Errorf("%v %v %v: %s %s %s", errA, errB, errC, a, b, again)
	}
	if _, err := h.Link(LinkRow{}); err == nil {
		t.Error("a link without a type has a hash")
	}
}
