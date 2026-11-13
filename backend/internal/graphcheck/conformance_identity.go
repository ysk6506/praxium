package graphcheck

import (
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
)

func identityCases() []Case {
	return []Case{
		{"identifiers_are_looked_up_and_keep_their_history", caseIdentifiers},
		{"identifier_naming_two_objects_in_one_scope_is_refused", caseIdentifierTaken},
		{"identity_events_are_recorded_and_not_rewound", caseIdentityEvents},
	}
}

func (e *env) scheme(name string) model.IdentifierSchemeID {
	e.tb.Helper()
	s, ok := e.fx.Reg.IdentifierSchemeByName(name)
	if !ok {
		e.tb.Fatalf("no identifier scheme %s", name)
	}
	return s.ID
}

func caseIdentifiers(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 2))
	one, two := e.fx.ServerID("SN-000000"), e.fx.ServerID("SN-000001")
	scheme := e.scheme("bmc_mac")
	def, _ := e.fx.Reg.IdentifierScheme(scheme)
	value, err := model.NormalizeValue(def.Normalizer, "AA-BB-CC-00-00-01")
	fatalIf(tb, err, "normalize the mac")
	e.apply(graph.Batch{Identifiers: []graph.IdentifierWrite{{Object: one, Scheme: scheme, Value: "AA-BB-CC-00-00-01", Scope: "bmc-a"}}})
	s := e.snap(graph.ReadLatest, 0)
	recs, err := s.Identifiers(e.ctx, scheme.String(), value)
	fatalIf(tb, err, "lookup")
	check(tb, len(recs) == 1 && recs[0].Object == one && recs[0].SeqFrom == 2 && recs[0].SeqTo == 0, "records %+v", recs)
	e.apply(graph.Batch{Identifiers: []graph.IdentifierWrite{
		{Object: one, Scheme: scheme, Value: value, Scope: "bmc-a", Close: true},
		{Object: two, Scheme: scheme, Value: value, Scope: "bmc-a"}}})
	now := e.snap(graph.ReadLatest, 0)
	recs, err = now.Identifiers(e.ctx, scheme.String(), value)
	fatalIf(tb, err, "lookup")
	check(tb, len(recs) == 1 && recs[0].Object == two, "the identifier should name the second server now: %+v", recs)
	hist, err := now.IdentifierHistory(e.ctx, scheme.String(), value)
	fatalIf(tb, err, "history")
	check(tb, len(hist) == 2 && hist[0].Object == one && hist[0].SeqTo == 3 && hist[1].SeqFrom == 3, "history %+v", hist)
	past, err := e.snap(graph.ReadSeq, 2).Identifiers(e.ctx, scheme.String(), value)
	fatalIf(tb, err, "lookup in the past")
	check(tb, len(past) == 1 && past[0].Object == one, "in the past the identifier named the first server: %+v", past)
	res := e.apply(graph.Batch{Identifiers: []graph.IdentifierWrite{{Object: two, Scheme: scheme, Value: value, Scope: "bmc-a"}}})
	check(tb, !res.Changed(), "writing an identifier that is already held made a change")
}

func caseIdentifierTaken(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 2))
	one, two := e.fx.ServerID("SN-000000"), e.fx.ServerID("SN-000001")
	scheme := e.scheme("redfish_system_id")
	e.apply(graph.Batch{Identifiers: []graph.IdentifierWrite{{Object: one, Scheme: scheme, Value: "sys-1", Scope: "bmc-a"}}})
	before := e.head()
	_, err := e.a.Apply(e.ctx, e.kg.ID, e.lease, graph.Batch{Identifiers: []graph.IdentifierWrite{{Object: two, Scheme: scheme, Value: "sys-1", Scope: "bmc-a"}}})
	wantCode(tb, "same identifier, same scope", err, graph.CodeIdentifierTaken)
	check(tb, e.head().AppliedSeq == before.AppliedSeq, "a refused identifier moved the head")
	e.apply(graph.Batch{Identifiers: []graph.IdentifierWrite{{Object: two, Scheme: scheme, Value: "sys-1", Scope: "bmc-b"}}})
	_, err = e.a.Apply(e.ctx, e.kg.ID, e.lease, graph.Batch{Identifiers: []graph.IdentifierWrite{{Object: e.fx.ServerID("SN-404"), Scheme: scheme, Value: "x"}}})
	wantCode(tb, "identifier of an object that is not there", err, graph.CodeBatchInvalid)
}

func caseIdentityEvents(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 2))
	one, two := e.fx.ServerID("SN-000000"), e.fx.ServerID("SN-000001")
	e.apply(graph.Batch{Events: []graph.IdentityEvent{{Kind: graph.EventMerge, Retired: two, Into: one}}})
	_, err := e.a.Apply(e.ctx, e.kg.ID, e.lease, graph.Batch{Events: []graph.IdentityEvent{{Kind: graph.EventMerge, Retired: one, Into: one}}})
	wantCode(tb, "merge into itself", err, graph.CodeBatchInvalid)
	events, err := e.snap(graph.ReadLatest, 0).IdentityEvents(e.ctx)
	fatalIf(tb, err, "events")
	check(tb, len(events) == 1 && events[0].Seq == 2 && events[0].Retired == two && events[0].Into == one, "events %+v", events)
	before, err := e.snap(graph.ReadSeq, 1).IdentityEvents(e.ctx)
	fatalIf(tb, err, "events in the past")
	check(tb, len(before) == 0, "an event shows in a state before it: %+v", before)
}
