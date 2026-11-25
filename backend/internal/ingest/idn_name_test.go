package ingest

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
)

// [REQ-884] An object that has neither a name attribute nor a value of its own in the key is not made.
func TestIdnObjectNameIsEmptyWithoutNameOrOwnKey(t *testing.T) {
	fp := &feedPlan{}
	if got := objectName(fp, Row{}, nil); got != "" {
		t.Fatalf("name = %q, want empty", got)
	}
	own := []graph.KeyPart{{Value: "rack-1"}}
	if got := objectName(fp, Row{}, own); got != "rack-1" {
		t.Fatalf("name = %q, want the first own key value", got)
	}
}
