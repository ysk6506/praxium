package pgstore_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// [REQ-310]
func TestIdentityRecordsFailWithAnErrorWhenTheirTableIsGoneOrThePoolIsClosed(t *testing.T) {
	ctx := context.Background()
	dsn := pgstore.NewTestDatabase(t)
	st, err := pgstore.Open(ctx, pgstore.Config{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	rec := st.Records()
	if _, err := rec.Put(ctx, "subject", "ann", []byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	ops := map[string]func() error{
		"get":    func() error { _, _, err := rec.Get(ctx, "subject", "ann"); return err },
		"create": func() error { _, err := rec.Put(ctx, "subject", "bob", []byte("x"), 0); return err },
		"update": func() error { _, err := rec.Put(ctx, "subject", "ann", []byte("y"), 1); return err },
		"scan":   func() error { return rec.Scan(ctx, "subject", func(string, []byte, int64) error { return nil }) },
	}
	damaged(t, admin, "identity_record", ops)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	for name, op := range ops {
		if err := op(); err == nil {
			t.Errorf("%s on a closed pool: no error", name)
		}
	}
}
