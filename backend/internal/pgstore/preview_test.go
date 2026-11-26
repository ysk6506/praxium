package pgstore_test

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/graphcheck"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// The change preview on PostgreSQL gives what the reference store gives.
//
// [REQ-410]
func TestPostgresStorePassesThePreviewSuite(t *testing.T) {
	factory := func(tb graphcheck.TB) graph.Store { return pgstore.NewTestStore(tb) }
	for _, c := range graphcheck.PreviewCases() {
		t.Run("[REQ-410] "+c.Name, func(t *testing.T) { t.Parallel(); c.Run(t, factory) })
	}
}
