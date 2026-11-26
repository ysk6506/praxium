package pgingest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

const idnClaimSource = `{"id":"src_idc","name":"idc","adapter":"file","datasets":[
 {"id":"ds_s","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"serial"},{"name":"manufacturer"},{"name":"mac"},{"name":"flag"}]}]}`

// two feeds read the one dataset: the first makes the servers, the second only finds them; the rows whose flag is off are held.
const idnClaimMapping = `{"refinements":[{"id":"r_s","input":"ds_s","ops":[{"op":"filter","column":"flag","in":["on"]}]}],
"feeds":[
 {"id":"f_s","dataset":"r_s","object_type":"PhysicalServer","creates_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"asset_id"}],
  "identifiers":[{"scheme":"bmc_mac","column":"mac"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]},
 {"id":"f_t","dataset":"r_s","object_type":"PhysicalServer","creates_objects":false,"basis":"observed",
  "attributes":[{"attribute":"name","column":"asset_id"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]}]}`

func idnClaims(rows ...string) pgiTexts {
	return pgiTexts{"ds_s": "asset_id,serial,manufacturer,mac,flag\n" + idnJoin(rows...)}
}

// [REQ-885]
func TestIdnTwoObjectsThatSayTheyHaveOneIdentifierGetItByTheRuleAndTheLoserIsToldAndAFeedThatMakesNoObjectFindsTheOnesThereAre(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnClaimSource, idnClaimMapping))
	l1, _ := idnPipeline(s, t, idnClaims("A,sn1,Zorvane,aa:aa:aa:aa:aa:01,on", "B,sn2,Zorvane,aa:aa:aa:aa:aa:02,on"), rdfT0, 0)
	a1 := idnBounds(s, t)
	refA := idnRefOf(a1, "ds_s", "A")
	// the feed that makes none finds the object the other made, in the same run
	for _, b := range a1 {
		if b.Feed == 1 && b.Ref != idnRefOf(a1, "ds_s", b.Key) && b.Key == "A" && b.Ref != refA {
			t.Errorf("the second feed binds A to %s, not to %s", b.Ref, refA)
		}
	}
	if len(a1) != 4 {
		t.Fatalf("%+v", a1)
	}
	idnApply(s, t)
	k := s.ref.KGNo
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".object_identifier (obj_no, scheme, value, scope, seq_from) SELECT c.obj_no, '%s', cl.value, '', 1 FROM "s_%d".calc_claim cl
		JOIN "s_%d".calc_bind c ON c.rid = cl.rid AND c.feed_no = cl.feed_no`, k, idnScheme(s, "bmc_mac"), k, k))
	s.rdfSettle(t, l1)

	_, counts := idnPipelineAfter(s, t, idnClaims("A,sn1,Zorvane,aa:aa:aa:aa:aa:01,on", "B,sn2,Zorvane,aa:aa:aa:aa:aa:02,on",
		"C,sn3,Zorvane,aa:aa:aa:aa:aa:01,on",                                       // says it has the identifier A has
		"D,sn4,Zorvane,aa:aa:aa:aa:aa:09,on", "E,sn5,Zorvane,aa:aa:aa:aa:aa:09,on", // two new objects that say they have the same one
		"H,snH,Zorvane,aa:aa:aa:aa:aa:08,off", // held, and there is no object of its key
	), 0)
	if counts.Lost != 2 {
		t.Errorf("%+v", counts)
	}
	lost := func(key string) int {
		return s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_ref r JOIN "s_%d".calc_record c ON c.rid = r.rid WHERE r.role = %d AND r.state = %d AND c.key = '%s'`,
			k, k, pgschema.RefRoleClaim, pgschema.RefLostClaim, key))
	}
	bs := idnBounds(s, t)
	// the holder keeps it; of two that have none, the object with the smaller id keeps it
	if lost("C") != 1 || lost("A") != 0 {
		t.Errorf("C lost %d, A lost %d", lost("C"), lost("A"))
	}
	d, e := idnRefOf(bs, "ds_s", "D"), idnRefOf(bs, "ds_s", "E")
	if d == "" || e == "" || d == e {
		t.Fatalf("%s %s", d, e)
	}
	if (d < e && (lost("D") != 0 || lost("E") != 1)) || (e < d && (lost("E") != 0 || lost("D") != 1)) {
		t.Errorf("D (%s) lost %d, E (%s) lost %d", d, lost("D"), e, lost("E"))
	}
	// a held record with no object of its key is left held, and makes none
	if idnRefOf(bs, "ds_s", "H") != "" {
		t.Errorf("a held record is bound to an object it made")
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_ref r JOIN "s_%d".calc_record c ON c.rid = r.rid WHERE r.role = %d AND r.state = %d AND c.key = 'H'`,
		k, k, pgschema.RefRoleIdentity, pgschema.RefHeld)); n != 2 {
		t.Errorf("H has %d held references, the two feeds each have one", n)
	}
}

// idnScheme is the name an identifier scheme is kept by.
func idnScheme(s *rdfSetup, short string) string {
	for name := range s.plan.SchemeNumbers() {
		if strings.HasSuffix(name, short) {
			return name
		}
	}
	return short
}
