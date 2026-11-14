package query

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// impactPlan is the input of impact once it has been checked.
type impactPlan struct {
	in       ImpactInput
	upstream bool
	mode     string
	maxItems int
	budget   time.Duration
	deadLink map[graph.LinkID]bool
}

// impactLimits are the model limits that every answer of impact states (kg-queries.md 14).
var impactLimits = []string{
	"Links that mean connected_to are not followed: reachability to a network anchor is not calculated, so a network path that is lost without a member of a group failing is not found",
	"Time is not modelled: how long a battery or the cooling carries on, how long a failover takes, and the load that moves to the members left",
	"Only the objects named as failed fail; the observed state of an object (oper_state) is not read",
}

// planImpact checks the input of impact and fills in the defaults.
func planImpact(in ImpactInput) (*impactPlan, error) {
	p := &impactPlan{in: in, mode: in.Mode, maxItems: in.MaxItems, budget: in.TimeBudget, deadLink: map[graph.LinkID]bool{}}
	switch in.Direction {
	case "", DirectionDownstream:
	case DirectionUpstream:
		p.upstream = true
	default:
		return nil, inputf("direction", "direction %q is not downstream or upstream", in.Direction)
	}
	switch p.mode {
	case "":
		p.mode = ModeBoth
	case ModeBoth, ModeFactsOnly, ModeWithAssumptions:
	default:
		return nil, inputf("mode", "mode %q is not both, facts_only or with_assumptions", in.Mode)
	}
	if err := p.checkLimits(); err != nil {
		return nil, err
	}
	if in.DeadScopeID != "" {
		return nil, kernel.Errorf(CodeScopeUnsupported, "dead_scope_id needs stored scopes, which do not exist yet")
	}
	return p, p.checkSeeds()
}

func (p *impactPlan) checkLimits() error {
	in := p.in
	if in.MaxHops < 0 {
		return inputf("max_hops", "max_hops %d is negative", in.MaxHops)
	}
	if p.maxItems == 0 {
		p.maxItems = DefaultMaxItems
	}
	if p.maxItems < 1 || p.maxItems > MaxItemsCap {
		return inputf("max_items", "max_items %d is outside 1 to %d", p.maxItems, MaxItemsCap)
	}
	if p.budget < 0 {
		return inputf("time_budget", "time_budget is negative")
	}
	if p.budget == 0 {
		p.budget = DefaultTimeBudget
	}
	return nil
}

func (p *impactPlan) checkSeeds() error {
	in := p.in
	if p.upstream {
		if in.TargetObjectID == "" {
			return inputf("target_object_id", "upstream needs target_object_id")
		}
		if len(in.Dead)+len(in.DeadLinks)+len(in.SiteFailure) > 0 {
			return inputf("dead", "upstream takes target_object_id, not dead, dead_links or site_failure")
		}
		return nil
	}
	if in.TargetObjectID != "" {
		return inputf("target_object_id", "target_object_id is for direction upstream")
	}
	if len(in.Dead)+len(in.DeadLinks)+len(in.SiteFailure) == 0 {
		return inputf("dead", "give at least one of dead, dead_links or site_failure")
	}
	if len(in.Dead) > MaxSeeds || len(in.DeadLinks) > MaxSeeds || len(in.SiteFailure) > MaxSeeds {
		return inputf("dead", "at most %d objects or links may be named", MaxSeeds)
	}
	for _, id := range in.DeadLinks {
		lid, err := kernel.ParseID[graph.LinkKind](id)
		if err != nil {
			return inputf("dead_links", "%q is not a link id", id)
		}
		p.deadLink[lid] = true
	}
	return nil
}

// policies are the readings the mode asks for; the one that believes every
// link comes last and is added only when some link is weak.
func (p *impactPlan) policies(g *depGraph) []policy {
	var out []policy
	if p.mode != ModeWithAssumptions {
		out = append(out, pFacts)
	}
	if p.mode != ModeFactsOnly {
		out = append(out, pAssumed)
	}
	if g.weakLinks > 0 {
		out = append(out, pReal)
	}
	return out
}

func (p *impactPlan) fingerprint(c Common) string {
	return fingerprint("impact", p.in.Direction, sortedCopy(p.in.Dead), sortedCopy(p.in.DeadLinks), sortedCopy(p.in.SiteFailure),
		p.in.TargetObjectID, p.mode, p.in.MaxHops, p.maxItems, c.View, c.KGVersionID, seqPart(c.ChangeSeq), c.Shape, int64(c.MaxStaleness))
}

func sortedCopy(in []string) []any {
	s := append([]string(nil), in...)
	sort.Strings(s)
	return strs(s)
}

// Impact answers impact: what a failure reaches, or what a target depends on.
func (e *Engine) Impact(ctx context.Context, in ImpactInput, f ReadFilter) (Answer[ImpactData], error) {
	var zero Answer[ImpactData]
	plan, err := planImpact(in)
	if err != nil {
		return zero, err
	}
	fp := plan.fingerprint(in.Common)
	r, err := e.open(ctx, in.Common, fp, f)
	if err != nil {
		return zero, err
	}
	g, err := e.loadGraph(ctx, r, loadOptions{deadLinks: plan.deadLink, maxStale: r.c.MaxStaleness, now: e.clock.Now()})
	if err != nil {
		return zero, err
	}
	if err := checkCut(plan, g); err != nil {
		return zero, err
	}
	var data ImpactData
	var rc ResultComplete
	var warn []string
	if plan.upstream {
		data, rc, warn, err = e.upstreamAnswer(r, g, plan, fp)
	} else {
		data, rc, warn, err = e.downstreamAnswer(r, g, plan, fp)
	}
	if err != nil {
		return zero, err
	}
	env, err := e.envelope(ctx, r, rc)
	if err != nil {
		return zero, err
	}
	env.ModelLimits, env.Warnings = impactLimits, warn
	return Answer[ImpactData]{Meta: env, Data: data}, nil
}

// checkCut refuses a dead link that is not in the KG.
func checkCut(p *impactPlan, g *depGraph) error {
	var missing []string
	for id := range p.deadLink {
		if !g.cutFound[id] {
			missing = append(missing, id.String())
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return inputf("dead_links", "no link %s", strings.Join(missing, ", "))
	}
	return nil
}

// objectIndex finds an object of the graph that the caller may read.
func (g *depGraph) objectIndex(field, id string) (int32, error) {
	oid, err := kernel.ParseID[graph.ObjectKind](id)
	if err != nil {
		return 0, kernel.Errorf(CodeObjectNotFound, "no object %q", id)
	}
	x, ok := g.byID[oid]
	if !ok || !g.nodes[x].visible {
		return 0, kernel.Errorf(CodeObjectNotFound, "no object %s", id)
	}
	return x, nil
}

func (g *depGraph) objectIndexes(field string, ids []string) ([]int32, error) {
	var out []int32
	seen := map[int32]bool{}
	for _, id := range ids {
		x, err := g.objectIndex(field, id)
		if err != nil {
			return nil, err
		}
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out, nil
}

func (g *depGraph) ref(x int32) ObjectRef {
	nd := &g.nodes[x]
	return ObjectRef{ObjectID: nd.idText, ObjectType: nd.typeName, DisplayName: nd.name}
}

func (e *Engine) now() func() time.Time { return e.clock.Now }
