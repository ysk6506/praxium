package pgstore

import (
	"context"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-311]
func TestIdentityRecordsAreKeptWithTheirVersionsAndFirstWriterWins(t *testing.T) {
	ctx := context.Background()
	rec := NewTestStore(t).Records()
	if _, _, err := rec.Get(ctx, "subject", "ann"); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Fatalf("a missing record: %v", err)
	}
	if v, err := rec.Put(ctx, "subject", "ann", []byte(`{"v":1}`), 0); err != nil || v != 1 {
		t.Fatalf("create: %d, %v", v, err)
	}
	if _, err := rec.Put(ctx, "subject", "ann", []byte("again"), 0); kernel.CodeOf(err) != kernel.CodeConflict {
		t.Fatalf("a second create: %v", err)
	}
	if v, err := rec.Put(ctx, "subject", "ann", []byte(`{"v":2}`), 1); err != nil || v != 2 {
		t.Fatalf("update: %d, %v", v, err)
	}
	if _, err := rec.Put(ctx, "subject", "ann", []byte("stale"), 1); kernel.CodeOf(err) != kernel.CodeConflict {
		t.Fatalf("an update with an old version: %v", err)
	}
	if _, err := rec.Put(ctx, "subject", "ghost", []byte("x"), 3); kernel.CodeOf(err) != kernel.CodeConflict {
		t.Fatalf("an update of a record that is not there: %v", err)
	}
	if _, err := rec.Put(ctx, "subject", "bad", []byte("a\x00b"), 0); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("text with NUL: %v", err)
	}
	var ids []string
	if err := rec.Scan(ctx, "subject", func(id string, data []byte, v int64) error {
		ids = append(ids, id)
		if string(data) != `{"v":2}` || v != 2 {
			t.Errorf("scan gave %q at %d", data, v)
		}
		return nil
	}); err != nil || len(ids) != 1 || ids[0] != "ann" {
		t.Fatalf("scan: %v, %v", ids, err)
	}
}

// [REQ-311]
func TestIdentityRecordsSurviveReopeningTheDatabase(t *testing.T) {
	ctx := context.Background()
	dsn := NewTestDatabase(t)
	first, err := Open(ctx, Config{DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := first.Records().Put(ctx, "token", "t1", []byte("kept"), 0); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	second, err := Open(ctx, Config{DSN: dsn})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() {
		if err := second.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()
	if data, v, err := second.Records().Get(ctx, "token", "t1"); err != nil || string(data) != "kept" || v != 1 {
		t.Fatalf("after reopening: %q at %d, %v", data, v, err)
	}
	if err := second.Records().Scan(ctx, "token", func(string, []byte, int64) error { return kernel.Errorf(kernel.CodeInternal, "stop") }); kernel.CodeOf(err) != kernel.CodeInternal {
		t.Fatalf("the error of the function of a scan: %v", err)
	}
}
