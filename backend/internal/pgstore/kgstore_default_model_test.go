package pgstore

import (
	"context"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// A store opened without a model holds the standard model with its statistics objects, the same model that
// StandardModels(true) gives: Open is the one way to open the storage, so this is the default it keeps.
// [REQ-369]
func TestV2sOpenWithoutModelsHoldsTheStandardModelWithItsStatistics(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, Config{DSN: NewTestDatabase(t)})
	if err != nil {
		t.Fatalf("open without models: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	std, err := StandardModels(true)
	if err != nil {
		t.Fatalf("the standard model: %v", err)
	}
	want, err := std(kernel.Hash{})
	if err != nil {
		t.Fatalf("the standard model: %v", err)
	}
	got, err := st.models(kernel.Hash{})
	if err != nil {
		t.Fatalf("the model of the store: %v", err)
	}
	if got.Registry == nil || got.Registry.Hash() != want.Registry.Hash() {
		t.Fatalf("the store without models does not hold the standard model")
	}
	if !got.WithStats {
		t.Fatalf("the store without models does not hold the statistics objects of the standard model")
	}
}
