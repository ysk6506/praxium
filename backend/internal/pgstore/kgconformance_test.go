package pgstore_test

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/graphcheck"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// [REQ-132] [REQ-135] [REQ-246] [REQ-247] [REQ-248] [REQ-249] [REQ-270]
func TestV2StorePassesTheSuite(t *testing.T) {
	// The caller's clock decides when a lease has run out, as the suite moves a clock of its own.
	factory := func(tb graphcheck.TB) graph.Store { return pgstore.NewTestStoreWith(tb, pgstore.LeaseClockCaller) }
	for _, c := range graphcheck.Cases() {
		t.Run(c.Name, func(t *testing.T) { c.Run(t, factory) })
	}
}

// The change preview on the new storage gives what the reference store gives.
//
// [REQ-410]
func TestV2StorePassesThePreviewSuite(t *testing.T) {
	factory := func(tb graphcheck.TB) graph.Store { return pgstore.NewTestStoreWith(tb, pgstore.LeaseClockCaller) }
	for _, c := range graphcheck.PreviewCases() {
		t.Run("[REQ-410] "+c.Name, func(t *testing.T) { c.Run(t, factory) })
	}
}
