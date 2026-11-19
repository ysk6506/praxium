package pgstore

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The first spelling of the storage's names (ADR-0089 built it beside the old tables and called it the second
// storage). The storage is the Store and Open makes it (ADR-0094); the tests that were locked when the first
// spelling was the only one still use these names, and rewriting them only to drop the names would put about thirty
// locked tests into the list of the change. The change that has to touch those tests anyway removes this file.

// StoreV2 is the Store.
type StoreV2 = Store

// V2Options are the settings that OpenV2 takes in the place of the fields of Config.
type V2Options struct {
	// Models gives the model of a KG. It is required.
	Models ModelProvider
	// LeaseClock is whose time a lease runs out by. The zero value is the database's.
	LeaseClock LeaseClock
}

// OpenV2 is Open with the model given in the options, which must name one.
func OpenV2(ctx context.Context, cfg Config, opts V2Options) (*Store, error) {
	if opts.Models == nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the storage needs a provider of the model")
	}
	cfg.Models, cfg.LeaseClock = opts.Models, opts.LeaseClock
	return Open(ctx, cfg)
}

// NewTestStoreV2 is NewTestStoreWith with the clock of the database, the one a server uses.
func NewTestStoreV2(tb TB) *Store { return NewTestStoreWith(tb, LeaseClockDB) }

// NewTestStoreV2With is NewTestStoreWith.
func NewTestStoreV2With(tb TB, clock LeaseClock) *Store { return NewTestStoreWith(tb, clock) }
