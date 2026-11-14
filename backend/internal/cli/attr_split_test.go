package cli

import (
	"reflect"
	"testing"
)

// [REQ-371]
func TestAttrIsCutAtTheLastDotSoAnOrganizationNameKeepsItsOwn(t *testing.T) {
	for _, tc := range []struct {
		text, name, op string
		values         []string
	}{
		{"power_state.equals=on", "power_state", "equals", []string{"on"}},
		{"org.management_department.equals=Infra Team 1", "org.management_department", "equals", []string{"Infra Team 1"}},
		{"org.management_department.in=A,B", "org.management_department", "in", []string{"A", "B"}},
		{"org.introduced_year.range=2020..2023", "org.introduced_year", "range", []string{"2020", "2023"}},
		{"org.contract_no.prefix=C-1.2", "org.contract_no", "prefix", []string{"C-1.2"}},
		{"org.ratio.equals=a=b", "org.ratio", "equals", []string{"a=b"}},
	} {
		got, err := attributeBody(tc.text)
		if err != nil {
			t.Errorf("%s: %v", tc.text, err)
			continue
		}
		want := map[string]any{"name": tc.name, "op": tc.op, "values": tc.values}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, want %v", tc.text, got, want)
		}
	}
	for _, bad := range []string{"model", "model=M1", ".equals=x", "model.=x", "org.x.range=5"} {
		if _, err := attributeBody(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

// [REQ-371]
func TestFilterOfAListIsCutAtTheLastDotToo(t *testing.T) {
	q, err := listQuery([]string{"layer.equals=service", "org.team.equals=a"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("filter[layer][equals]") != "service" || q.Get("filter[org.team][equals]") != "a" {
		t.Fatalf("filters: %v", q)
	}
}
