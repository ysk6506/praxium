package graphcheck_test

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/graphcheck"
	"github.com/ysk6506/praxium/backend/internal/memstore"
)

// The change preview on the reference store. The subtest name carries the tag of the requirement.
//
// [REQ-410]
func TestPreviewSuiteOnTheReferenceStore(t *testing.T) {
	factory := func(graphcheck.TB) graph.Store { return memstore.New() }
	cases := graphcheck.PreviewCases()
	if len(cases) != 5 {
		t.Fatalf("the preview suite has %d cases, this test expects 5", len(cases))
	}
	for _, c := range cases {
		t.Run("[REQ-410] "+c.Name, func(t *testing.T) { c.Run(t, factory) })
	}
}
