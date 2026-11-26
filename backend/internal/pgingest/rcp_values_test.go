package pgingest

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// What the computation of the objects makes of the values of the records: the volatile values, the identifiers, the problems of the
// cells and of the values that disagree (ADR-0115).

// rcpVolatileMapping is the mapping of the tests of the expansion with a feed that only attaches the power state, which is volatile, to
// the servers (it makes no object), so a cell it cannot read is a problem of the computation and not of the receiving.
func rcpVolatileMapping(t *testing.T) string {
	t.Helper()
	m := pgiMappingJSON
	feed := ` {"id":"f_state","dataset":"ds_servers","object_type":"PhysicalServer","creates_objects":false,"basis":"observed",
  "attributes":[{"attribute":"power_state","column":"power_state"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]},
`
	m = strings.Replace(m, ` {"id":"f_vm"`, feed+` {"id":"f_vm"`, 1)
	if !strings.Contains(m, "f_state") || strings.Count(m, "power_state") != 2 {
		t.Fatal("the mapping of the test was not made")
	}
	return m
}

// [REQ-942]
func TestRcpAVolatileValueIsStagedAsAnEntryAndLeftOutOfTheDocument(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, rcpVolatileMapping(t)))
	l, _ := idnPipeline(s, t, pgiTexts{"ds_places": pgiPlaces, "ds_servers": pgiServers, "ds_vms": pgiVMs}, rdfT0, 0)
	rcpExpand(s, t, l, true)
	counts := rcpDo(s, t, l, 0, 0)
	got := rcpStaged(s, t)
	if counts.Volatile != 2 {
		t.Fatalf("%+v", counts)
	}
	seen := 0
	for ref, o := range got {
		for _, v := range o.Volatile {
			seen++
			attr, entry, _ := strings.Cut(v, "=")
			var doc map[string]map[string]any
			if err := json.Unmarshal([]byte(o.Doc), &doc); err != nil {
				t.Fatal(err)
			}
			if _, in := doc[attr]; in {
				t.Errorf("%s: the volatile attribute %s is in the document too", ref, attr)
			}
			var e map[string]any
			if err := json.Unmarshal([]byte(entry), &e); err != nil {
				t.Fatal(err)
			}
			if e["t"] != rcpSeen || e["b"] != "observed" || (e["v"] != "on" && e["v"] != "off") || !strings.HasPrefix(fmt.Sprint(e["r"]), "src_pgi/ds_servers/") {
				t.Errorf("%s: the entry is %s", ref, entry)
			}
		}
	}
	if seen != 2 {
		t.Errorf("%d volatile values were found in the staged objects", seen)
	}
}

// [REQ-943]
func TestRcpACellAFeedThatMakesNoObjectCannotReadIsAProblemOfTheRecordAndTheValueIsLeftOut(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, rcpVolatileMapping(t)))
	servers := "asset_id,serial,manufacturer,rack_tag,power_state\nSV1,sn1,Zorvane,RK1,maybe\nSV2,sn2,Zorvane,RK2,off\n"
	l, _ := idnPipeline(s, t, pgiTexts{"ds_places": pgiPlaces, "ds_servers": servers, "ds_vms": pgiVMs}, rdfT0, 0)
	rcpExpand(s, t, l, true)
	rcpDo(s, t, l, 0, 0)
	var found []string
	for _, p := range rcpProblems(s, t) {
		if strings.Contains(p, "invalid_value") {
			found = append(found, p)
		}
	}
	if len(found) != 1 || !strings.HasPrefix(found[0], "ds_servers/SV1/") || !strings.HasSuffix(found[0], "/invalid_value/power_state") {
		t.Errorf("the problems of the cells are %v", found)
	}
	volatile := 0
	for _, o := range rcpStaged(s, t) {
		volatile += len(o.Volatile)
	}
	if volatile != 1 {
		t.Errorf("%d volatile values: the one that was wrong is left out and the other is kept", volatile)
	}
}

// [REQ-933]
func TestRcpTwoRecordsOfOneObjectThatSayDifferentThingsHaveOneValueAndTheLosingOneIsAProblem(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnMoreSource, idnMoreMapping))
	a := "asset_id,serial,manufacturer,uuid,mac\nA,sn1,Zorvane,u1,aa:bb:cc:dd:ee:99\nA2,sn1,Zorvane,u1,aa:bb:cc:dd:ee:01\n"
	b := "asset_id,serial,manufacturer,uuid\n"
	l, _ := idnPipeline(s, t, pgiTexts{"ds_a": a, "ds_b": b}, rdfT0, 0)
	rcpExpand(s, t, l, true)
	rcpDo(s, t, l, 0, 0)
	got := rcpStaged(s, t)
	if len(got) != 1 {
		t.Fatalf("%d objects: %s", len(got), rcpText(got))
	}
	for _, o := range got {
		// the records were opened by the same run and are of the same source, feed and dataset: the natural key, smaller first
		if o.Name != "A" || strings.Join(o.Idents, ",") != "is.bmc_mac=aabbccddee99" {
			t.Errorf("the name and the identifier are not the ones of the record with the smaller key: %s", o.rcpText())
		}
		if !strings.Contains(o.SourceRefs, "src_idm/ds_a/A") || !strings.Contains(o.SourceRefs, "src_idm/ds_a/A2") {
			t.Errorf("the references are %s", o.SourceRefs)
		}
	}
	problems := rcpProblems(s, t)
	if len(problems) != 1 || problems[0] != fmt.Sprintf("ds_a/A2/0/%s/asset_id", pgschema.ProblemSourceConflict) {
		t.Errorf("the problems are %v", problems)
	}
}

// [REQ-932]
func TestRcpAnObjectThatNoRecordGivesANameKeepsTheNameTheRegistryHasAndOneThatARecordNamesTakesTheValue(t *testing.T) {
	const m = 2
	unnamed := strings.Replace(idnServerMapping, `"attributes":[{"attribute":"name","column":"asset_id"}],`, "", 1)
	if unnamed == idnServerMapping {
		t.Fatal("the mapping of the test was not made")
	}
	s, l := rcpSharedWith(t, m, unnamed)
	rcpDo(s, t, l, 0, 0)
	got := rcpStaged(s, t)
	for _, o := range got {
		if !strings.HasPrefix(o.Name, "n") || len(o.Name) != 2 {
			t.Errorf("the name is %q, not the one of the registry: %s", o.Name, o.rcpText())
		}
	}
	if len(got) != m+1 {
		t.Errorf("%d objects", len(got))
	}
	named, l2 := rcpShared(t, m)
	rcpDo(named, t, l2, 0, 0)
	for _, o := range rcpStaged(named, t) {
		if o.Name != "n1" && o.Name != "n2" && o.Name != "big" {
			t.Errorf("the name is %q, not the value of the cell the records give", o.Name)
		}
	}
}
