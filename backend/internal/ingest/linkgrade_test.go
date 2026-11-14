package ingest

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// [REQ-363]
// When both feeds a link is made from are approximate the link is assumed, and the basis stays what the link
// definition says: only the grade follows the lowest feed.
func TestALinkOfTwoApproximateFeedsIsAssumedAndKeepsItsBasis(t *testing.T) {
	plain := newRig(t, sourceJSON, mappingJSON)
	plain.files["ds_racks"], plain.files["ds_servers"], plain.files["ds_vms"] = racksV1, serversV1, vmsV1
	plain.run()
	wantBasis := linksOf(plain, "SV1")[0].Basis
	mp := strings.Replace(mappingJSON, `"id":"f_server",`, `"approximate":true,"id":"f_server",`, 1)
	mp = strings.Replace(mp, `"id":"f_rack",`, `"approximate":true,"id":"f_rack",`, 1)
	r := newRig(t, sourceJSON, mp)
	r.files["ds_racks"], r.files["ds_servers"], r.files["ds_vms"] = racksV1, serversV1, vmsV1
	r.run()
	links := linksOf(r, "SV1")
	if len(links) != 1 {
		t.Fatalf("links %+v, want one", links)
	}
	if links[0].Grade != model.GradeAssumed {
		t.Errorf("grade %s, want %s", links[0].Grade, model.GradeAssumed)
	}
	if links[0].Basis != wantBasis {
		t.Errorf("basis %s, want %s", links[0].Basis, wantBasis)
	}
}
