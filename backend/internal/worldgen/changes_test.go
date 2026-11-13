package worldgen

import (
	"bytes"
	"strings"
	"testing"
)

func changeParams(p *Params) {
	p.Sites = 2
	p.Changes.Events = 400
	p.Imperfections.LateEventRatio = 0.3
	p.Imperfections.DuplicateEventRatio = 0.1
	p.Imperfections.UnparseableRatio = 0.02
}

// Replaying the stream the way a reader should (skip garbage, ignore repeats,
// ignore what is older than what was applied) ends in the mutated truth.
func TestChangeStreamReplayReproducesTheMutatedTruth(t *testing.T) {
	for _, seed := range []uint64{31, 32, 33} {
		w := smallWorld(t, seed, 40000, changeParams)
		m, st := runAll(t, w)
		res, err := ReplayChanges(bytes.NewReader(st.Files[ChangesFile].Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		exp, err := ReadExpected(bytes.NewReader(st.Files[ExpectedFile].Bytes()))
		if err != nil || len(exp) == 0 {
			t.Fatalf("expected state: %v, %d entries", err, len(exp))
		}
		for _, d := range CheckReplay(res, exp) {
			t.Errorf("seed %d: %s", seed, d)
		}
		if res.Dups != m.Imperfections.Counts["duplicate_event"] || res.Rejected != m.Imperfections.ByFile["unparseable_line "+ChangesFile] {
			t.Errorf("seed %d: replay saw %d dups, %d rejected; manifest says %v", seed, res.Dups, res.Rejected, m.Imperfections.Counts)
		}
		if res.Late == 0 || res.Applied == 0 {
			t.Errorf("seed %d: late %d, applied %d", seed, res.Late, res.Applied)
		}
	}
}

func TestPlanCoversEveryKindAndIsRepeatable(t *testing.T) {
	w := smallWorld(t, 34, 40000, func(p *Params) { changeParams(p); p.Changes.Events = 600 })
	a, err := w.Plan()
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, c := range a {
		kinds[c.Kind]++
	}
	for _, k := range []string{"vm_move", "replace", "link_change", "retire", "flap", "add"} {
		if kinds[k] == 0 {
			t.Errorf("no %s change in %v", k, kinds)
		}
	}
	b, _ := w.Plan()
	if len(a) != len(b) || a[len(a)-1].Seq != b[len(b)-1].Seq {
		t.Fatal("the plan is not repeatable")
	}
	for i := 1; i < len(a); i++ {
		if a[i].Ops[0].At <= a[i-1].Ops[0].At {
			t.Fatalf("source times must grow with the plan: %d", i)
		}
	}
}

// A late event does not overwrite a newer value; a repeated event does nothing.
func TestReplayLateValueProtection(t *testing.T) {
	stream := strings.Join([]string{
		`{"event_id":"e1","source_time":"2026-01-01T00:00:10Z","op":"modified","kind":"object","type":"ot.virtual_machine","id":"x","props":{"power_state":"on"}}`,
		`{"event_id":"e2","source_time":"2026-01-01T00:00:30Z","op":"modified","kind":"object","type":"ot.virtual_machine","id":"x","props":{"power_state":"off"}}`,
		`{"event_id":"e3","source_time":"2026-01-01T00:00:20Z","op":"modified","kind":"object","type":"ot.virtual_machine","id":"x","props":{"power_state":"suspended"}}`,
		`{"event_id":"e2","source_time":"2026-01-01T00:00:30Z","op":"modified","kind":"object","type":"ot.virtual_machine","id":"x","props":{"power_state":"off"}}`,
		`{"event_id":"e4","source_time":"tru`,
		`{"event_id":"e5","source_time":"2026-01-01T00:01:00Z","op":"deleted","kind":"object","type":"ot.virtual_machine","id":"x"}`,
		``,
	}, "\n")
	res, err := ReplayChanges(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	x := res.Entities["x"]
	if res.Applied != 3 || res.Late != 1 || res.Dups != 1 || res.Rejected != 1 || x.Present || x.Props["power_state"] != "off" {
		t.Fatalf("%+v %+v", res, x)
	}
}

func TestCheckReplayReportsDifferences(t *testing.T) {
	res := &Replayed{Entities: map[string]*Entity{feedID("object", "a"): {Present: true, Props: map[string]string{"p": "1"}}}}
	exp := []Expected{
		{Kind: "object", ID: "a", Final: "present", Props: nil},
		{Kind: "object", ID: "b", Final: "present"},
		{Kind: "object", ID: "a", Final: "absent"},
	}
	if d := CheckReplay(res, exp); len(d) != 2 {
		t.Fatalf("%v", d)
	}
	if _, err := ReadExpected(strings.NewReader("{broken")); err == nil {
		t.Fatal("broken expected file")
	}
}

func TestNoChangesWhenNothingToChange(t *testing.T) {
	w := smallWorld(t, 35, 3000, func(p *Params) { p.VMsPerHost = 0; p.Changes.Events = 10 })
	plan, err := w.Plan()
	if err != nil || plan != nil {
		t.Fatalf("%v %v", plan, err)
	}
	w2 := smallWorld(t, 35, 3000, nil)
	if plan, _ := w2.Plan(); plan != nil {
		t.Fatal("no events were asked for")
	}
}
