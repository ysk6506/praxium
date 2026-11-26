package pgingest

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// The links of the KG the difference closes and the ones the check of the cardinality counts as staying are the two sides of one condition
// (isoKGLinkStays, ADR-0127): were they not, a link the check kept would be closed and its place lost, or a link the check let go would stay
// beside the one that took its place, and the KG would hold an end to two links. A run after another, on captures in which two records of
// one server say two racks, the links the difference closes are held to that condition written again here, apart from the step's statements,
// and the KG after the apply holds every server to one rack.

// dfiShouldClose is the links of the KG the run did not stage that do not stay (written apart from the step: the isolation did not take them
// out, no end of them retires, and a record the run does not read stands under them or none does), by their ids.
func dfiShouldClose(s *rdfSetup, t *testing.T) []string {
	t.Helper()
	return expList(s.e, t, fmt.Sprintf(`SELECT k.ref FROM "s_%[1]d".link k WHERE NOT EXISTS (SELECT 1 FROM "s_%[1]d".stage_link l WHERE l.link_no = k.link_no) AND NOT (
		NOT EXISTS (SELECT 1 FROM "s_%[1]d".calc_isolated i WHERE i.kind = 2 AND i.entity_no = k.link_no)
		AND NOT EXISTS (SELECT 1 FROM "s_%[1]d".calc_object c WHERE c.obj_no IN (k.src, k.dst) AND c.why = 4 AND NOT EXISTS (SELECT 1 FROM "s_%[1]d".stage_object x WHERE x.obj_no = c.obj_no))
		AND (EXISTS (SELECT 1 FROM "s_%[1]d".record_ref f WHERE f.role = 2 AND f.target_no = k.link_no AND f.state = 0
				AND NOT EXISTS (SELECT 1 FROM "s_%[1]d".calc_record c WHERE c.src_no = f.src_no AND c.dataset_no = f.dataset_no AND c.key = f.key))
			OR NOT EXISTS (SELECT 1 FROM "s_%[1]d".record_ref f WHERE f.role = 2 AND f.target_no = k.link_no AND f.state = 0))) ORDER BY k.ref COLLATE "C"`, s.ref.KGNo))
}

// [REQ-962]
func TestDfiTheLinksTheDifferenceClosesAreTheOnesTheCheckOfTheCardinalityLetsGoAndTheKGKeepsItsPlaces(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, rclSourceA, dfiMappingRetire))
	k := dfiApplierOf(s, t)
	racks := []string{"RA,RK1", "RB,RK2", "RC,RK3"}
	steps := [][]string{
		// SV1 and SV1b are one server that two racks are said for; so are SV3, SV3b and SV3c
		{"SV1,Zorvane,sn1,RK1,1,on", "SV1b,Zorvane,sn1,RK2,2,on", "SV3,Zorvane,sn3,RK3,4,on", "SV3b,Zorvane,sn3,RK1,5,on", "SV3c,Zorvane,sn3,RK2,6,on"},
		// the record whose link the KG has says another rack; another record of the server says nothing new
		{"SV1,Zorvane,sn1,RK3,1,on", "SV1b,Zorvane,sn1,RK2,2,on", "SV3,Zorvane,sn3,RK3,4,on", "SV3b,Zorvane,sn3,RK1,5,on", "SV3c,Zorvane,sn3,RK2,7,on"},
		// the record that had the place says the rack another record says; a record that lost says another
		{"SV1,Zorvane,sn1,RK2,1,on", "SV1b,Zorvane,sn1,RK2,2,on", "SV3,Zorvane,sn3,RK2,4,on", "SV3b,Zorvane,sn3,RK3,5,on", "SV3c,Zorvane,sn3,RK2,7,on"},
		// a record is not seen any more (its link stays until the record is closed, three runs later)
		{"SV1,Zorvane,sn1,RK2,1,on", "SV1b,Zorvane,sn1,RK2,2,on", "SV3,Zorvane,sn3,RK2,4,on", "SV3b,Zorvane,sn3,RK3,5,on"},
	}
	closed := 0
	for n, servers := range steps {
		at := rdfT0.Add(time.Duration(n) * 13 * time.Hour)
		l, counts := dfiRunTo(s, t, rclTextsA(racks, servers, nil), at, false, 0, 0)
		dfiShape(s, t, counts, 0)
		want := dfiShouldClose(s, t)
		got := expList(s.e, t, fmt.Sprintf(`SELECT ref FROM "s_%d".ingest_diff WHERE kind = 2 AND change_kind = 2 ORDER BY ref COLLATE "C"`, s.ref.KGNo))
		if !slices.Equal(got, want) {
			t.Errorf("step %d: the difference closes %v and the links that do not stay are %v", n, got, want)
		}
		closed += len(got)
		dfiApply(s, t, k, l)
		dfiSettle(s, t, l)
		dfiHead(s, t, k)
		// every server is mounted in one rack at most
		if two := expList(s.e, t, fmt.Sprintf(`SELECT src::text FROM "s_%d".link GROUP BY src HAVING count(*) > 1`, s.ref.KGNo)); len(two) > 0 {
			t.Errorf("step %d: the servers %v are mounted twice:\n%s", n, two, dfiKGText(s, t))
		}
	}
	if closed < 2 {
		t.Errorf("the runs closed %d links", closed)
	}
	if got := dfiKGText(s, t); !strings.Contains(got, "l ") {
		t.Errorf("the KG has no link:\n%s", got)
	}
}
