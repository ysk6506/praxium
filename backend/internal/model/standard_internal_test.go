package model

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func TestRequireStandard(t *testing.T) {
	// [REQ-107] Only std definitions ship with the product: one in another namespace stops the load.
	tests := []struct {
		name   string
		damage func(d *Definitions)
		fail   bool
	}{
		{"[REQ-107] all std", func(d *Definitions) {}, false},
		{"[REQ-107] object type in org", func(d *Definitions) { d.ObjectTypes = []ObjectType{{Namespace: NamespaceOrg}} }, true},
		{"[REQ-107] link type in ext", func(d *Definitions) { d.LinkTypes = []LinkType{{Namespace: NamespaceExt}} }, true},
		{"[REQ-107] action type in org", func(d *Definitions) { d.ActionTypes = []ActionType{{Namespace: NamespaceOrg}} }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Definitions
			tt.damage(&d)
			err := requireStandard(d)
			if tt.fail != (err != nil) {
				t.Fatalf("requireStandard() = %v, want failure %v", err, tt.fail)
			}
			if err != nil && kernel.CodeOf(err) != CodeNotStandard {
				t.Fatalf("code = %s, want %s", kernel.CodeOf(err), CodeNotStandard)
			}
		})
	}
}
