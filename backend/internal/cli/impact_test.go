package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/cli"
)

const impactCliAnswer = `{"data":{"direction":"downstream","seeds":[{"object_id":"obj_pdu","display_name":"PDU A"}],
"facts_only":{"counts_by_result":{"down":1,"degraded":2},"counts_by_type":[{"object_type":"PhysicalServer","count":3}],
"counts_by_parent":[],"groups_touched":[],"unfilled_channel_objects":4,
"items":[{"object_id":"obj_s1","object_type":"PhysicalServer","display_name":"S1","result":"down","also":[],
"lost_channels":["power"],"hops":2,"assumption_based":false,"weakest_grade":"fact",
"path":[{"object_id":"obj_pdu","display_name":"PDU A"},{"object_id":"obj_s1","display_name":"S1","link":{"link_type":"POWERED_BY"}}]}]},
"computation":{"complete":false,"stopped_by":"time_budget","rounds":3,"max_hops":0,"max_items":5000,"time_budget_seconds":30,"weak_links":0,"stale_inputs":0,"items_over_cap":false}},"meta":{}}`

func itCliImpactServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, r.URL.Path+" "+string(raw))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, impactCliAnswer)
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies
}

func itCliImpactRun(srv *httptest.Server, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	all := append([]string{"--endpoint", srv.URL, "--token", "test-token-not-real-0001"}, args...)
	code := cli.Run(all, fixedEnv(nil, nil), &out, &errOut, nil)
	return code, out.String(), errOut.String()
}

// [REQ-306]
func TestImpactCommandAsksTheRouteAndPrintsSummaryGroupsAndPaths(t *testing.T) {
	srv, bodies := itCliImpactServer(t)
	code, out, errOut := itCliImpactRun(srv, "query", "impact", "obj_pdu", "--mode", "facts_only", "--max-hops", "4", "--site-failure", "obj_site")
	if code != 0 {
		t.Fatalf("%d %q", code, errOut)
	}
	last := (*bodies)[len(*bodies)-1]
	for _, want := range []string{"/v1/kgs/main/queries:impact", `"dead":["obj_pdu"]`, `"site_failure":["obj_site"]`, `"mode":"facts_only"`, `"max_hops":4`} {
		if !strings.Contains(last, want) {
			t.Errorf("the request lacks %s: %s", want, last)
		}
	}
	for _, want := range []string{"facts only: down 1, degraded 2", "channel-empty objects 4", "down (1 shown)", "PDU A -[POWERED_BY]-> S1", "NOT complete, stopped by time_budget"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "channel-empty") > strings.Index(out, "down (1 shown)") {
		t.Error("the summary comes before the groups")
	}
	_, js, _ := itCliImpactRun(srv, "--json", "query", "impact", "obj_pdu")
	var data map[string]any
	if err := json.Unmarshal([]byte(js), &data); err != nil || data["direction"] != "downstream" {
		t.Errorf("--json prints the data: %v %q", err, js)
	}
}

// [REQ-306]
func TestImpactCommandUpstreamTakesATargetAndRefusesAMixedOrEmptyQuestion(t *testing.T) {
	srv, bodies := itCliImpactServer(t)
	if code, _, errOut := itCliImpactRun(srv, "query", "impact", "--upstream", "obj_x"); code != 0 {
		t.Fatalf("%d %q", code, errOut)
	}
	if last := (*bodies)[len(*bodies)-1]; !strings.Contains(last, `"direction":"upstream"`) || !strings.Contains(last, `"target_object_id":"obj_x"`) {
		t.Errorf("%s", last)
	}
	for _, args := range [][]string{{"query", "impact"}, {"query", "impact", "--upstream", "obj_x", "obj_y"}} {
		if code, _, errOut := itCliImpactRun(srv, args...); code == 0 || !strings.Contains(errOut, "query impact needs") {
			t.Errorf("%v: %d %q", args, code, errOut)
		}
	}
	text, ok := cli.Help([]string{"query", "impact"})
	if !ok || !strings.Contains(text, "how to read the output") || !strings.Contains(text, "examples:") {
		t.Errorf("the help explains the output: %q", text)
	}
}
