package pgstore

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgconn"
)

// [REQ-559]
func TestTheSourcesOfTheStoreLeaveNothingOnASessionAndWriteOnlyInTransactions(t *testing.T) {
	got, err := pgconn.SessionStateViolations(ownSources)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("the sources leave something on a session or write outside a transaction: %v", got)
	}
}
