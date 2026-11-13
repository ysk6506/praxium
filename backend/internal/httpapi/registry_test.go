package httpapi_test

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

func mustTraitID(t *testing.T, raw string) model.TraitID {
	t.Helper()
	id, err := kernel.ParseID[model.TraitKind](raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// differentRegistry is the standard model with one more trait, so its hash differs.
func differentRegistry(t *testing.T) *model.Registry {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	defs := reg.Definitions()
	defs.Traits = append(defs.Traits, model.Trait{ID: mustTraitID(t, "tr.extra_for_test"), Name: "ExtraForTest", Description: "added by a test"})
	other, err := model.NewRegistry(defs)
	if err != nil {
		t.Fatal(err)
	}
	return other
}
