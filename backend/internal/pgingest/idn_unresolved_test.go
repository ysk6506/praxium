package pgingest

import (
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// putPlace keeps a raw record of the places that a run before read, with a reference that was not solved.
func (s *rdfSetup) idnPutPlace(t *testing.T, key string, declRev int, cells ...string) {
	t.Helper()
	k := s.ref.KGNo
	s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate) VALUES ($1, $2, $3, $4, $5, $6, 1, 0)`, pgiTable(s.ref, pgschema.TableRawRecord)),
		s.ref.SrcNo, s.rdfNo("ds_places"), key, rdfHash(key), textArray(cells), declRev)
	for feed := 0; feed < 5; feed++ {
		s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_ref (src_no, dataset_no, key, role, def_no, sub, state) VALUES ($1, $2, $3, 1, $4, 0, %d)`, k, pgschema.RefUnresolved),
			s.ref.SrcNo, s.rdfNo("ds_places"), key, feed)
	}
}

// [REQ-880] [REQ-884]
func TestIdnARecordWhoseReferenceIsNotSolvedIsReadAgainFromItsCellsAndBoundWithoutTouchingTheOnesThatAre(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, idnMapping))
	s.idnPutPlace(t, "1", 0, "1", "HQ", "B1", "F1", "R1", "rack a", "RK1")
	s.idnPutPlace(t, "2", 0, "2", "HQ", "B1", "F1", "R1", "rack b", "RK2")
	// a record kept under another declaration than the dataset has now is not read: its references stay as they are
	s.idnPutPlace(t, "3", 7, "3", "HQ", "B1", "F2", "R2", "rack c", "RK3")
	// the places are not given this time, so the comparison leaves them out and only the references say they are to be looked at
	l, counts := s.idnPipeline(t, pgiTexts{}, rdfT0, 0)
	if counts.Seeded != 2 || counts.Rebuilt != 2 || counts.NewObjects != 6 || counts.Unresolved != 0 {
		// two racks of one room: a site, a building, a floor, a room and the two racks
		t.Fatalf("%+v", counts)
	}
	bs := s.idnBound(t)
	if len(bs) != 10 {
		t.Errorf("%d records are bound: %+v", len(bs), bs)
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_ref r JOIN "s_%d".calc_record c ON c.rid = r.rid WHERE c.key = '3'`, s.ref.KGNo, s.ref.KGNo)); n != 0 {
		t.Errorf("the record of the other declaration has %d references in the run", n)
	}
	_ = l
	_ = ingest.RunComputing
}
