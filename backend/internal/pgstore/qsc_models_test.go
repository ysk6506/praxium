package pgstore

import (
	"context"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The model the tests of other packages make their KGs from (pgingest's pgiOpen) is the model of a server less the query schema, and
// nothing else: the statistics objects are made as a server makes them, so the plans those tests watch are a server's plans (ADR-0113).
//
// [REQ-626]
func TestQscTheModelOfTheTestsOfOtherPackagesIsTheModelOfAServerWithItsStatisticsAndWithoutTheQuerySchema(t *testing.T) {
	light, err := TestOnlyModelsWithoutQuerySchema()
	if err != nil {
		t.Fatal(err)
	}
	server, err := StandardModels(true)
	if err != nil {
		t.Fatal(err)
	}
	var h kernel.Hash
	m, err := light(h)
	if err != nil {
		t.Fatal(err)
	}
	s, err := server(h)
	if err != nil {
		t.Fatal(err)
	}
	if !m.WithStats || !s.WithStats {
		t.Errorf("the statistics objects: tests %v, server %v", m.WithStats, s.WithStats)
	}
	if !m.noQuery || s.noQuery {
		t.Errorf("without the query schema: tests %v, server %v", m.noQuery, s.noQuery)
	}
	if m.Registry.Hash() != s.Registry.Hash() || len(m.Search) != len(s.Search) {
		t.Errorf("the model of the tests is not the model of a server: %v and %v", m.Registry.Hash(), s.Registry.Hash())
	}
	// a model without the query schema reads no token of the installation, so its plan has none (no transaction is needed for it)
	got, err := readToken(context.Background(), nil, m)
	if err != nil || got.token != "" {
		t.Errorf("the model without the query schema read a token %q: %v", got.token, err)
	}
	p, err := buildPlan(got, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Query) != 0 {
		t.Errorf("the plan of the model of the tests has %d steps of a query schema", len(p.Query))
	}
}
