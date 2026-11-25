package pgingest

import (
	"fmt"
	"strings"
	"testing"
)

const idnServerSource = `{"id":"src_idn","name":"idn","adapter":"file","datasets":[
 {"id":"ds_s","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"serial"},{"name":"manufacturer"},{"name":"uuid"}]}]}`

const idnServerMapping = `{"feeds":[
 {"id":"f_s","dataset":"ds_s","object_type":"PhysicalServer","creates_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"asset_id"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1},
              {"components":[{"property":"system_uuid","column":"uuid"}],"priority":2}]}]}`

func idnServers(rows ...string) pgiTexts {
	return pgiTexts{"ds_s": "asset_id,serial,manufacturer,uuid\n" + idnJoin(rows...)}
}

// [REQ-881] [REQ-882]
func TestIdnTheKeysOfARecordFindNoObjectOneObjectOrSeveralAndTheGroupsAreMadeWhateverTheOrder(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	first := []string{"A,sn1,Zorvane,u1", "B,sn2,Zorvane,u2"}
	l1, _ := s.idnPipeline(t, idnServers(first...), rdfT0, 0)
	a1 := s.idnBound(t)
	if len(a1) != 2 || idnRefOf(a1, "ds_s", "A") == idnRefOf(a1, "ds_s", "B") {
		t.Fatalf("%+v", a1)
	}
	refA, refB := idnRefOf(a1, "ds_s", "A"), idnRefOf(a1, "ds_s", "B")
	s.idnApply(t)
	s.rdfSettle(t, l1)
	second := append(append([]string{}, first...),
		"C,sn1,Zorvane,u3",  // the serial finds A; the uuid is new and is bound to A
		"D,snD,Zorvane,u2",  // the uuid finds B; the serial is new and is bound to B
		"E,sn1,Zorvane,u2",  // the serial finds A and the uuid finds B: the first key wins, and it is told
		"F,snN,Zorvane,uN",  // three records that share keys one after another are one new object
		"G,snN,Zorvane,uM",  //
		"H,snM,Zorvane,uM",  //
		"L1,snL,Zorvane,u1", // a new serial that leads to A and to B is bound to neither
		"L2,snL,Zorvane,u2")
	for run, order := range [][]string{second, reversed(second), rotated(second, 3)} {
		l, counts := s.idnPipelineAfter(t, idnServers(order...), run)
		bs := s.idnBound(t)
		want := map[string]string{"C": refA, "D": refB, "E": refA, "L1": refA, "L2": refB}
		for key, ref := range want {
			if got := idnRefOf(bs, "ds_s", key); got != ref {
				t.Errorf("run %d: %s is %s, want %s", run, key, got, ref)
			}
		}
		fgh := idnRefOf(bs, "ds_s", "F")
		if fgh == "" || fgh == refA || fgh == refB || idnRefOf(bs, "ds_s", "G") != fgh || idnRefOf(bs, "ds_s", "H") != fgh {
			t.Errorf("run %d: F, G and H are %s %s %s", run, fgh, idnRefOf(bs, "ds_s", "G"), idnRefOf(bs, "ds_s", "H"))
		}
		if counts.Dropped != 1 || counts.NewObjects != 1 {
			t.Errorf("run %d: %+v", run, counts)
		}
		if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_problem p JOIN "s_%d".calc_record c ON c.rid = p.rid WHERE p.kind = 'identity_conflict' AND c.key = 'E'`, s.ref.KGNo, s.ref.KGNo)); n != 1 {
			t.Errorf("run %d: the conflict of E is told %d times", run, n)
		}
		s.idnUndo(t, l)
	}
}

func reversed(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[len(rows)-1-i] = r
	}
	return out
}

func rotated(rows []string, n int) []string {
	return append(append([]string{}, rows[n:]...), rows[:n]...)
}

var _ = strings.Join
