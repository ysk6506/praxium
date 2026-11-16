package filejournal

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/graphcheck"
)

// The change preview on the journal store, with the log only and with a snapshot after every write.
//
// [REQ-410]
func TestFileStorePassesThePreviewSuite(t *testing.T) {
	for _, variant := range []struct {
		name string
		ckpt int64
	}{{"log only", 0}, {"snapshot after every write", 1}} {
		factory := func(tb graphcheck.TB) graph.Store {
			s, _, err := openStore(newFakeDisk(), variant.ckpt)
			if err != nil {
				tb.Fatalf("open: %v", err)
			}
			tb.Cleanup(func() {
				if err := s.Close(); err != nil {
					tb.Errorf("close: %v", err)
				}
			})
			return s
		}
		for _, c := range graphcheck.PreviewCases() {
			t.Run(variant.name+"/[REQ-410] "+c.Name, func(t *testing.T) { c.Run(t, factory) })
		}
	}
}
