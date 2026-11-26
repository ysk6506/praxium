package pgingest

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// [REQ-891] [REQ-898]
func TestExpAnObjectThatARecordOfAnotherSourceStandsUnderIsNotACandidateAndOneThatNoRecordStandsUnderIs(t *testing.T) {
	mapping := strings.Replace(idnServerMapping, `"creates_objects":true,`, `"creates_objects":true,"retires_objects":true,`, 1)
	s := rdfNew(t, pgiPlan(t, idnServerSource, mapping))
	l1, _ := idnPipeline(s, t, idnServers("A,sn1,Zorvane,u1"), rdfT0, 0)
	expApply(s, t)
	s.rdfSettle(t, l1)
	obj := expObjOf(s, t, "ds_s", "A")
	// a record of a second source stands under the object too
	other := s.e.attach(t, s.kg, pgiPlan(t, idnOtherSource, idnOtherMapping))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate) VALUES ($1, $2, 'B1', $3, $4, 0, 1, 0)`, pgiTable(other, pgschema.TableRawRecord)),
		other.SrcNo, other.Datasets["ds_b"], rdfHash("B1"), textArray([]string{"B1", "sn1", "Zorvane"}))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_ref (src_no, dataset_no, key, role, def_no, sub, target_no, state, retirable) VALUES ($1, $2, 'B1', %d, 0, 0, $3, %d, true)`,
		s.ref.KGNo, pgschema.RefRoleIdentity, pgschema.RefResolved), other.SrcNo, other.Datasets["ds_b"], obj)
	// the record of the first source says another serial: it leaves the object
	l, _ := idnPipeline(s, t, idnServers("A,sn1x,Zorvane,u1x"), rdfT0.Add(time.Hour), 0)
	start := ingest.ExpandStart{Feeds: expFeeds(s)}
	if _, err := ingest.RunExpand(s.e.ctx, s.e.st, l, start); err != nil {
		t.Fatal(err)
	}
	if why := s.e.count(t, fmt.Sprintf(`SELECT why FROM "s_%d".calc_object WHERE obj_no = $1`, s.ref.KGNo), obj); int16(why) != pgschema.ObjectWhyOldRef {
		t.Errorf("an object a record of another source stands under is %d", why)
	}
	// the record of the other source goes: nothing stands under it, and a reference of the first source says it may be retired
	s.e.exec(t, fmt.Sprintf(`DELETE FROM "s_%d".record_ref WHERE src_no = $1`, s.ref.KGNo), other.SrcNo)
	expReset(s, t, l)
	if _, err := ingest.RunExpand(s.e.ctx, s.e.st, l, start); err != nil {
		t.Fatal(err)
	}
	if why := s.e.count(t, fmt.Sprintf(`SELECT why FROM "s_%d".calc_object WHERE obj_no = $1`, s.ref.KGNo), obj); int16(why) != pgschema.ObjectWhyRetire {
		t.Errorf("an object no record stands under is %d", why)
	}
	// and when the reference that is kept is not retirable (no feed that retires made it), it is not
	s.e.exec(t, fmt.Sprintf(`UPDATE "s_%d".record_ref SET retirable = false`, s.ref.KGNo))
	expReset(s, t, l)
	if _, err := ingest.RunExpand(s.e.ctx, s.e.st, l, start); err != nil {
		t.Fatal(err)
	}
	if why := s.e.count(t, fmt.Sprintf(`SELECT why FROM "s_%d".calc_object WHERE obj_no = $1`, s.ref.KGNo), obj); int16(why) != pgschema.ObjectWhyOldRef {
		t.Errorf("an object that no retiring feed made, that no record stands under, is %d", why)
	}
}
