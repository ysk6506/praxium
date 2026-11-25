package pgingest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// [REQ-880]
func TestIdnTheRecordsOfARunAreBoundToObjectsWhoseIdsAreTheOnesTheKeysGive(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	_, counts := s.idnPipeline(t, pgiAll(), rdfT0, 0)
	bs := s.idnBound(t)
	if len(bs) != 7 {
		t.Fatalf("%d records are bound: %+v", len(bs), bs)
	}
	if counts.NewObjects != 7 || counts.Unresolved != 0 {
		t.Errorf("%+v", counts)
	}
	seen := map[string]bool{}
	for _, b := range bs {
		if b.Ref == "" || !b.IsNew {
			t.Errorf("%+v", b)
		}
		seen[b.Ref] = true
	}
	if len(seen) != 7 {
		t.Errorf("%d objects for 7 records", len(seen))
	}
	// a server's id is the id of its key, which the receiving made: the same one the current flow gives
	if got, want := idnRefOf(bs, "ds_servers", "SV1"), s.e.text(t, `SELECT cand_ref FROM `+pgiTable(s.ref, pgschema.TableStageKey)+` k JOIN `+pgiTable(s.ref, pgschema.TableStageRecord)+
		` r ON r.dataset_no = k.dataset_no AND r.page = k.page AND r.row_no = k.row_no WHERE r.key = 'SV1'`); got != want {
		t.Errorf("the id of SV1 is %s, the key gives %s", got, want)
	}
}

// idnSite is the places a test uses: the racks of two floors of one building.
const idnSite = "row_id,site,building,floor,room,rack,tag\n"

func idnPlaces(rows ...string) pgiTexts {
	return pgiTexts{"ds_places": idnSite + idnJoin(rows...), "ds_servers": pgiServers[:strings.Index(pgiServers, "\n")+1], "ds_vms": ""}
}

// [REQ-880] [REQ-881]
func TestIdnAnObjectWhoseKeyHasAParentIsMadeOnceTheParentIsAndItsIdComesFromGo(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, idnMapping))
	_, counts := s.idnPipeline(t, idnPlaces("1,HQ,B1,F1,R1,rack a,RK1", "2,HQ,B1,F1,R1,rack b,RK2", "3,HQ,B1,F2,R2,rack c,RK3"), rdfT0, 0)
	if counts.NewObjects != 9 || counts.Unresolved != 0 || counts.Rounds < 5 {
		t.Fatalf("%+v", counts)
	}
	k := s.ref.KGNo
	// every key under a parent is bound in the context of the object its parent is: rack a is in the room that room R1 is
	var wrong int
	s.e.exec(t, "SELECT 1")
	wrong = s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_newkey n JOIN "s_%d".calc_newobj o ON o.obj_no = n.obj_no WHERE n.ctx_no <> 0 AND NOT EXISTS (SELECT 1 FROM "s_%d".calc_newobj p WHERE p.obj_no = n.ctx_no)`, k, k, k))
	if wrong != 0 {
		t.Errorf("%d keys are bound in a context that no object of the run is", wrong)
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_newobj`, k)); n != 9 {
		t.Errorf("%d objects", n)
	}
	// the three records of the places that name the same site are one object, and so are the two that name the same room
	rows := s.e.count(t, fmt.Sprintf(`SELECT count(DISTINCT b.obj_no) FROM "s_%d".calc_bind b WHERE b.feed_no = 0`, k))
	if rows != 1 {
		t.Errorf("the site is %d objects", rows)
	}
}
