package cli_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const impactCliUpstream = `{"data":{"direction":"upstream","target":{"object_id":"obj_t","display_name":"Server T"},
"upstream":{"total":2,"single_points_of_failure":1,"counts_by_type":[{"object_type":"RackPdu","count":2}],"counts_by_effect":{},
"items":[{"object_id":"obj_p","object_type":"RackPdu","display_name":"PDU P","hops":1,"effects":{"facts_only":"down","with_assumptions":"down"},"single_point_of_failure":true,"assumption_based":false,
"path":[{"object_id":"obj_p","display_name":"PDU P"},{"object_id":"obj_t","display_name":"Server T","link":{"link_type":"POWERED_BY"}}]},
{"object_id":null,"object_type":"","display_name":"","hops":2,"effects":{},"single_point_of_failure":false,"assumption_based":true,"path":[{"object_id":null,"hidden":true},{"object_id":"obj_t","display_name":"Server T","link":{"link_type":"PART_OF"}}]}]},
"computation":{"complete":true,"stopped_by":null,"rounds":4,"max_hops":0,"max_items":5000,"time_budget_seconds":30,"weak_links":2,"stale_inputs":1,"items_over_cap":true}},"meta":{}}`

const impactCliQuiet = `{"data":{"direction":"downstream","seeds":[],"facts_only":{"counts_by_result":{},"counts_by_type":[],"counts_by_parent":[],"groups_touched":[
{"display_name":"G","group_type":"PowerRedundancyGroup","mode":"count","value":"up","alive":1,"unknown":0,"n":2,"min_alive":1,"slack":0}],"unfilled_channel_objects":0,
"items":[{"object_id":"obj_u","object_type":"Rack","display_name":"R","result":"unknown","lost_channels":[],"unfilled_channels":["cooling"],"hops":1,"assumption_based":true,"path":[]}]},
"computation":{"complete":true,"stopped_by":null,"rounds":1,"max_hops":0,"max_items":5000,"time_budget_seconds":30,"weak_links":0,"stale_inputs":0,"items_over_cap":false}},"meta":{}}`

func itCliImpactAnswering(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// [REQ-306]
func TestImpactCommandPrintsAnUpstreamAnswerWithItsSinglePointsAndHiddenSteps(t *testing.T) {
	srv := itCliImpactAnswering(t, impactCliUpstream)
	code, out, errOut := itCliImpactRun(srv, "query", "impact", "--upstream", "obj_t", "--time-budget", "PT5S")
	if code != 0 {
		t.Fatalf("%d %q", code, errOut)
	}
	for _, want := range []string{"depends on, for obj_t", "2 objects, 1 single points of failure", "SINGLE POINT OF FAILURE",
		"alone it causes: facts_only=down, with_assumptions=down", "(hidden) -[PART_OF]-> Server T", "alone it causes: -", "items over the cap of 5000", "stale inputs 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
}

// [REQ-306]
func TestImpactCommandSaysWhenNothingChangesAndListsUnfilledChannels(t *testing.T) {
	srv := itCliImpactAnswering(t, impactCliQuiet)
	code, out, errOut := itCliImpactRun(srv, "query", "impact", "--dead-link", "lnk_1")
	if code != 0 {
		t.Fatalf("%d %q", code, errOut)
	}
	for _, want := range []string{"nothing changes", "redundancy groups touched", "unknown (1 shown)", "unfilled cooling", "assumption-based"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
}
