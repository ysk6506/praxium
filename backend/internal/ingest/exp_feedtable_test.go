package ingest

import (
	"context"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Every expansion is given the feeds (ADR-0121): without them every reference is of a feed that is not known and keeps its object, so an
// expansion of the records of the run that was not given them would keep the objects that only a feed that adds values stands under.

// [REQ-950] [REQ-981]
func TestExpAnExpansionOfTheRecordsOfTheRunIsRefusedWithoutTheFeedsAsTheExpansionOfEverythingIs(t *testing.T) {
	for _, full := range []bool{false, true} {
		if err := (ExpandStart{Full: full}).Validate(); kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Errorf("an expansion (everything: %t) with no feeds: %v", full, err)
		}
		if err := (ExpandStart{Full: full, Feeds: expOneFeed}).Validate(); err != nil {
			t.Errorf("an expansion (everything: %t) with the feeds: %v", full, err)
		}
	}
	// the run is not begun when the start is refused
	f := &expFake{stage: RunComputing, total: 2}
	if _, err := RunExpand(context.Background(), f, RunLease{No: 1}, ExpandStart{Rows: 3}); kernel.CodeOf(err) != kernel.CodeInvalidValue || len(f.calls) != 0 {
		t.Errorf("an expansion with no feeds: %v, the calls %v", err, f.calls)
	}
}
