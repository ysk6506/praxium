package model_test

import "testing"

// [REQ-261]
func TestBindingForGivesTheFirstBindingOfOneLinkType(t *testing.T) {
	r := newBaseline(t)
	b, ok := r.BindingFor(otID("ot.server"), otID("ot.rack"), ltID("lt.mounted_in"))
	if !ok || b.ID.String() != "bd.mounted_in.mountable.rack" {
		t.Errorf("server to rack over mounted_in = %v %v, want the first binding by id", b.ID, ok)
	}
	if _, ok := r.BindingFor(otID("ot.rack"), otID("ot.server"), ltID("lt.mounted_in")); ok {
		t.Errorf("rack to server has a binding")
	}
	if _, ok := r.BindingFor(otID("ot.server"), otID("ot.rack"), ltID("lt.nothing")); ok {
		t.Errorf("an unknown link type has a binding")
	}
}
