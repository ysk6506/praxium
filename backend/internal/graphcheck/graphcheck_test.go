package graphcheck_test

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/graphcheck"
	"github.com/ysk6506/praxium/backend/internal/memstore"
)

// [REQ-132] [REQ-135] [REQ-246] [REQ-247] [REQ-248] [REQ-249]
func TestReferenceStorePassesTheSuite(t *testing.T) {
	cases := graphcheck.Cases()
	if len(cases) == 0 {
		t.Fatal("the suite has no cases")
	}
	factory := func(graphcheck.TB) graph.Store { return memstore.New() }
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) { c.Run(t, factory) })
	}
}
