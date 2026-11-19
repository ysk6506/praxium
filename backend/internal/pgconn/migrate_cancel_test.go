package pgconn

import (
	"context"
	"errors"
	"testing"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-270]
func TestRetryLocksStopsAtOnceWhenTheContextIsCancelledWhileItWaitsForALock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tries := 0
	err := retryLocks(ctx, func() error {
		tries++
		return &pq.Error{Code: sqlLockNotAvail}
	})
	if kernel.CodeOf(err) != kernel.CodeInternal || !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled wait gave %v, want the cancellation as an internal error", err)
	}
	if tries != 1 {
		t.Errorf("the step ran %d times, a cancelled migration must not try again", tries)
	}
}
