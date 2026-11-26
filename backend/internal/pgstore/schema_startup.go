package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// SchemaStartup says what the adjustment at the start of a server did with one KG whose schema was made by an
// older generator (ADR-0095): it was brought up to this build (Adjusted), or it was left as it is, for the reason.
type SchemaStartup struct {
	KG       graph.KGID
	Adjusted bool
	Reason   string
}

// AdjustOldSchemas is what a server does when it starts, after the migrations and the check of the storage
// format: every KG whose storage schema was made by an older generator of the plan is brought up to this build
// with AdjustStorageSchema, so that a new build does not refuse the KGs that the build before made (D19).
//
// Only an addition is followed. A KG whose schema differs from the model in anything but the generator (the
// model, its numbering or the search set), and one whose difference is not an addition, is left as it is, and
// stays refused by D19; so is one that another run is already adjusting (servers that start together take
// turns: the state "building" of ADR-0087 is the claim). A KG made by a newer generator than this build is not
// touched. A KG that could not be adjusted because of a failure does not stop the others: the failures are
// joined in the error returned, and the KG stays refused until it is adjusted.
//
// A KG of this generator whose query schemas are missing or stale (made by another plan, or left by an adjustment that
// stopped) gets them made again, one to a transaction, and only those (ADR-0126); it is said as adjusted. Servers that
// start together take turns over each query schema, and the one that comes second finds it made and leaves it.
func (s *Store) AdjustOldSchemas(ctx context.Context) ([]SchemaStartup, error) {
	kgs, err := s.ListKGs(ctx)
	if err != nil {
		return nil, err
	}
	var done []SchemaStartup
	var failed []error
	for _, kg := range kgs {
		out, err := s.adjustOldOne(ctx, kg.ID)
		if err != nil {
			failed = append(failed, kernel.Errorf(kernel.CodeOf(err), "cannot adjust the storage schema of KG %s: %v", kg.ID, err))
			continue
		}
		if out != nil {
			done = append(done, *out)
		}
	}
	return done, errors.Join(failed...)
}

// adjustOldOne handles one KG; nil means it is not of an older generator, its query schemas are as the plan says, and
// nothing was said about it.
func (s *Store) adjustOldOne(ctx context.Context, kg graph.KGID) (*SchemaStartup, error) {
	rec, err := s.StorageSchema(ctx, kg)
	if err != nil {
		return nil, err
	}
	if !needsAdjusting(rec) {
		return s.remakeQuery(ctx, kg, rec)
	}
	defer s.forget(kg)
	a, err := s.access(ctx, kg)
	if err != nil {
		return nil, err
	}
	left := func(reason string) (*SchemaStartup, error) { return &SchemaStartup{KG: kg, Reason: reason}, nil }
	want, err := hashesOfModel(a.model)
	if err != nil {
		return nil, err
	}
	if a.rec.RegistryHash != want.registry || a.rec.NumberingHash != want.numbering || a.rec.SearchHash != want.search {
		return left("the schema differs from the model in more than the generator")
	}
	_, err = s.AdjustStorageSchema(ctx, kg, a.model)
	switch {
	case err == nil:
		return &SchemaStartup{KG: kg, Adjusted: true}, nil
	case kernel.CodeOf(err) == pgschema.CodeNotAdditive:
		return left(fmt.Sprintf("the difference is not an addition: %v", err))
	case kernel.CodeOf(err) == kernel.CodeConflict:
		return left(fmt.Sprintf("another run holds the schema: %v", err))
	}
	return nil, err
}

// remakeQuery makes again the query schemas of a KG that are missing or stale (ADR-0126). A KG whose schema was not made from the
// model the server holds (D19 refuses it, and the plan of the model is not its plan) and a model without a query schema are left as
// they are; so is a KG whose query schemas all have the mark of the plan. nil means nothing was made.
func (s *Store) remakeQuery(ctx context.Context, kg graph.KGID, rec SchemaRecord) (*SchemaStartup, error) {
	defer s.forget(kg)
	a, err := s.access(ctx, kg)
	if err != nil {
		return nil, err
	}
	if CheckSchemaMatch(rec, a.model) != nil {
		return nil, nil
	}
	var p pgschema.Plan
	stale := false
	err = a.sh.tx(ctx, RoleAPIRead, false, func(tx *sql.Tx) error {
		m, err := readToken(ctx, tx, a.model)
		if err != nil {
			return err
		}
		if p, err = buildPlan(m, rec.KGNo); err != nil {
			return err
		}
		parts, err := queryParts(p)
		if err != nil {
			return err
		}
		for _, part := range parts {
			have, err := queryMark(ctx, tx, part.name)
			if err != nil {
				return err
			}
			stale = stale || have != part.mark
		}
		return nil
	})
	if err != nil || !stale {
		return nil, err
	}
	if err := s.makeQuerySchemas(ctx, a.sh, kg, p, false); err != nil {
		return nil, err
	}
	return &SchemaStartup{KG: kg, Adjusted: true}, nil
}

// needsAdjusting says whether a record is of an older generator, or of this one with the indexes not all built (the state is not ok: a
// run was stopped in the middle, and the server finishes it). A KG of a newer generator is left alone, and one whose statistics are still
// to be made waits for them (FinishStorageStats).
func needsAdjusting(rec SchemaRecord) bool {
	switch {
	case rec.GeneratorVersion < pgschema.GeneratorVersion:
		return true
	case rec.GeneratorVersion > pgschema.GeneratorVersion:
		return false
	}
	return rec.State != string(pgschema.StateOK) && rec.State != string(pgschema.StateStatsPending)
}
