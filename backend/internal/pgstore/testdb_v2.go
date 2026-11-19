package pgstore

import "context"

// NewTestStoreV2 opens a StoreV2 on a new empty test database with the standard model (without the statistics
// objects). The store is closed and the database dropped when the test ends.
func NewTestStoreV2(tb TB) *StoreV2 {
	tb.Helper()
	dsn := NewTestDatabase(tb)
	if dsn == "" {
		return nil
	}
	models, err := StandardModels(false)
	if err != nil {
		tb.Fatalf("cannot load the standard model: %v", err)
		return nil
	}
	st, err := OpenV2(context.Background(), Config{DSN: dsn}, V2Options{Models: models})
	if err != nil {
		tb.Fatalf("cannot open the test store: %v", err)
		return nil
	}
	tb.Cleanup(func() { _ = st.Close() })
	return st
}
