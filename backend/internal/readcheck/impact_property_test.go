package readcheck

import (
	"encoding/json"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/query"
)

// itAll runs a downstream question and reads every page of both readings.
func (w *impactWorld) itAll(in query.ImpactInput) (facts, assumed map[string]string) {
	w.t.Helper()
	facts, assumed = map[string]string{}, map[string]string{}
	in.MaxItems = query.MaxItemsCap
	for {
		cursor := in.Cursor
		in.Common = w.itCommon()
		in.Common.Limit, in.Common.Cursor = query.MaxLimit, cursor
		a, err := w.eng.Impact(w.ctx, in, query.AllowAll())
		if err != nil {
			w.t.Fatal(err)
		}
		for _, it := range a.Data.FactsOnly.Items {
			facts[it.ObjectID] = it.Result
		}
		for _, it := range a.Data.WithAssumptions.Items {
			assumed[it.ObjectID] = it.Result
		}
		if a.Meta.ResultComplete.NextCursor == nil {
			return facts, assumed
		}
		in.Cursor = *a.Meta.ResultComplete.NextCursor
	}
}

// itSample picks n distinct objects of the given types, deterministically from the seed.
func (w *impactWorld) itSample(seed uint64, n int, types ...string) []string {
	var pool []string
	for _, typ := range types {
		ids := w.itKGIDs(typ)
		keys := make([]string, 0, len(ids))
		for _, id := range ids {
			keys = append(keys, id)
		}
		pool = append(pool, itSortedStrings(keys)...)
	}
	rng := kernel.NewSeededRng(seed)
	var out []string
	for len(out) < n && len(pool) > 0 {
		i := int(rng.Uint64() % uint64(len(pool)))
		out = append(out, pool[i])
		pool = append(pool[:i], pool[i+1:]...)
	}
	return out
}

func itSortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Failing more objects never lets an object that was down come back (kg-queries.md 7: monotone).
//
// [REQ-300]
func TestImpactAMoreFailuresNeverBringBackAnObjectThatWasDown(t *testing.T) {
	w := loadImpactWorld(t, itSeed, itScale)
	for round := uint64(1); round <= 3; round++ {
		more := w.itSample(round, 6, "RackPdu", "Switch", "PhysicalServer", "Fan", "PowerSupply")
		less := more[:3]
		factsLess, assumedLess := w.itAll(query.ImpactInput{Dead: less})
		factsMore, assumedMore := w.itAll(query.ImpactInput{Dead: more})
		for id, r := range factsLess {
			if r == query.ResultDown && factsMore[id] != query.ResultDown && !itIsSeed(more, id) {
				t.Fatalf("round %d, facts only: %s was down with %d failures and is %q with %d", round, id, len(less), factsMore[id], len(more))
			}
		}
		for id, r := range assumedLess {
			if r == query.ResultDown && assumedMore[id] != query.ResultDown && !itIsSeed(more, id) {
				t.Fatalf("round %d, with assumptions: %s was down with %d failures and is %q with %d", round, id, len(less), assumedMore[id], len(more))
			}
		}
		// what is down in the facts is down with assumptions too
		for id, r := range factsMore {
			if r == query.ResultDown && assumedMore[id] != query.ResultDown {
				t.Fatalf("round %d: %s is down in the facts and %q with assumptions", round, id, assumedMore[id])
			}
		}
	}
}

func itIsSeed(seeds []string, id string) bool {
	for _, s := range seeds {
		if s == id {
			return true
		}
	}
	return false
}

// The order of the seeds, and a seed named twice, change nothing.
//
// [REQ-300]
func TestImpactTheOrderOfTheSeedsDoesNotChangeTheAnswerOnAGeneratedWorld(t *testing.T) {
	w := loadImpactWorld(t, itSeed, itScale)
	seeds := w.itSample(11, 5, "RackPdu", "Switch", "HypervisorHost", "Fan")
	reversed := make([]string, 0, len(seeds)+1)
	for i := len(seeds) - 1; i >= 0; i-- {
		reversed = append(reversed, seeds[i])
	}
	reversed = append(reversed, seeds[0])
	one := w.itImpact(query.ImpactInput{Dead: seeds, MaxItems: 100000})
	two := w.itImpact(query.ImpactInput{Dead: reversed, MaxItems: 100000})
	a, _ := json.Marshal([]any{one.Data.FactsOnly, one.Data.WithAssumptions})
	b, _ := json.Marshal([]any{two.Data.FactsOnly, two.Data.WithAssumptions})
	if string(a) != string(b) {
		t.Fatal("the answer depends on the order of the seeds")
	}
}

// What a failure of one object does to a target is the same whether it is asked downstream from the
// object or upstream from the target.
//
// [REQ-305]
func TestImpactDownstreamAndUpstreamAgreeOnWhatOneFailureDoesToATarget(t *testing.T) {
	w := loadImpactWorld(t, itSeed, itScale)
	targets := w.itSample(21, 3, "VirtualMachine", "HypervisorHost", "PhysicalServer", "Switch")
	checked := 0
	for _, target := range targets {
		in := query.ImpactInput{Direction: query.DirectionUpstream, TargetObjectID: target, Common: w.itCommon()}
		in.Common.Limit = query.MaxLimit
		up, err := w.eng.Impact(w.ctx, in, query.AllowAll())
		if err != nil {
			t.Fatal(err)
		}
		if up.Data.Upstream.Total == 0 || up.Meta.ResultComplete.Truncated {
			t.Fatalf("the upstream of %s: total %d, %+v", target, up.Data.Upstream.Total, up.Meta.ResultComplete)
		}
		for i, dep := range up.Data.Upstream.Items {
			if i >= 12 {
				break
			}
			facts, assumed := w.itAll(query.ImpactInput{Dead: []string{dep.ObjectID}})
			for reading, down := range map[string]map[string]string{query.ModeFactsOnly: facts, query.ModeWithAssumptions: assumed} {
				want := down[target]
				if want == "" {
					want = "none"
				}
				if got := dep.Effects[reading]; got != want {
					t.Fatalf("target %s, dependency %s (%s): upstream says %q, downstream %q", target, dep.DisplayName, reading, got, want)
				}
			}
			if dep.SinglePointOfFailure != (facts[target] == query.ResultDown) {
				t.Fatalf("target %s, dependency %s: single point %v, downstream %q", target, dep.DisplayName, dep.SinglePointOfFailure, facts[target])
			}
			checked++
		}
	}
	if checked < 24 {
		t.Fatalf("only %d pairs checked", checked)
	}
}

// A single point of failure of a target has the target in the objects that go down when it fails.
//
// [REQ-305]
func TestImpactEveryObjectThatGoesDownWithASeedNamesTheSeedAsItsSinglePoint(t *testing.T) {
	w := loadImpactWorld(t, itSeed, itScale)
	for _, seed := range w.itSample(31, 3, "RackPdu", "Switch", "HypervisorHost") {
		facts, _ := w.itAll(query.ImpactInput{Dead: []string{seed}})
		var down []string
		for id, r := range facts {
			if r == query.ResultDown {
				down = append(down, id)
			}
		}
		for i, target := range itSortedStrings(down) {
			if i >= 8 {
				break
			}
			in := query.ImpactInput{Direction: query.DirectionUpstream, TargetObjectID: target, Mode: query.ModeFactsOnly, Common: w.itCommon()}
			in.Common.Limit = query.MaxLimit
			up, err := w.eng.Impact(w.ctx, in, query.AllowAll())
			if err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, dep := range up.Data.Upstream.Items {
				found = found || dep.ObjectID == seed && dep.SinglePointOfFailure
			}
			if !found && !up.Meta.ResultComplete.Truncated {
				t.Fatalf("%s goes down with %s, which is not among its single points of failure", target, seed)
			}
		}
	}
}
