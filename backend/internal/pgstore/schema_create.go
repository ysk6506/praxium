package pgstore

import (
	"context"
	"database/sql"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// planner is the function that makes the plan; pgschema.Build in production. A test gives one whose plan is
// broken at a statement, to see that nothing is left behind.
type planner func(model SchemaModel, kgNo int64) (pgschema.Plan, error)

func buildPlan(m SchemaModel, kgNo int64) (pgschema.Plan, error) {
	return pgschema.Build(m.Registry, pgschema.Options{KGNo: kgNo, Search: m.Search, WithStats: m.WithStats, InstallToken: m.token})
}

// readToken gives the model the token of the installation, which makes the plan have a query schema (ADR-0113). A model that has no
// query schema (SchemaModel.noQuery) is left without it, and its plan is the tables only.
func readToken(ctx context.Context, tx *sql.Tx, m SchemaModel) (SchemaModel, error) {
	if m.noQuery {
		return m, nil
	}
	var err error
	m.token, err = readInstallToken(ctx, tx)
	return m, err
}

// CreateStorageSchema makes the storage schema of a KG from the model: it takes the number of the KG from the
// sequence, runs the statements of the plan, and writes the record with the manifest last, all in one transaction
// of the migration role on the write pool, under the lock of the KG's schema work. It either makes everything or
// leaves nothing. Called again for a schema that was made from the same model it changes nothing and returns the
// record; for one made from another model it is a conflict (AdjustStorageSchema follows a change of the model).
// Two runs at once take turns, so both succeed and one of them works.
func (s *Store) CreateStorageSchema(ctx context.Context, kg graph.KGID, m SchemaModel) (SchemaRecord, error) {
	return s.createStorageSchema(ctx, kg, m, buildPlan)
}

func (s *Store) createStorageSchema(ctx context.Context, kg graph.KGID, m SchemaModel, plan planner) (SchemaRecord, error) {
	if kg.IsZero() {
		return SchemaRecord{}, kernel.Errorf(kernel.CodeInvalidValue, "the storage schema needs the id of a KG")
	}
	if _, err := hashesOfModel(m); err != nil {
		return SchemaRecord{}, err
	}
	sh, err := s.rt.locate(kg)
	if err != nil {
		return SchemaRecord{}, err
	}
	var out SchemaRecord
	err = locked(ctx, sh, kg, func(tx *sql.Tx) error {
		var err error
		out, err = createInTx(ctx, tx, kg, m, plan)
		return err
	})
	return out, err
}

func createInTx(ctx context.Context, tx *sql.Tx, kg graph.KGID, m SchemaModel, plan planner) (SchemaRecord, error) {
	have, found, err := readRecord(ctx, tx, kg)
	if err != nil {
		return SchemaRecord{}, err
	}
	if found {
		if err := CheckSchemaMatch(have, m); err != nil {
			return SchemaRecord{}, kernel.Wrap(kernel.CodeConflict, err, "the KG has a storage schema already")
		}
		return have, nil
	}
	var kgNo int64
	if err := tx.QueryRowContext(ctx, `SELECT nextval('kg_no_seq')`).Scan(&kgNo); err != nil {
		return SchemaRecord{}, dbErr(err, "take the number of the KG")
	}
	if m, err = readToken(ctx, tx, m); err != nil {
		return SchemaRecord{}, err
	}
	p, err := plan(m, kgNo)
	if err != nil {
		return SchemaRecord{}, err
	}
	if err := runSteps(ctx, tx, p.Steps, pgschema.InTx); err != nil {
		return SchemaRecord{}, err
	}
	if err := runQuerySteps(ctx, tx, p); err != nil {
		return SchemaRecord{}, err
	}
	state := string(p.Manifest.State)
	if err := writeRecord(ctx, tx, recordWrite{kg: kg, manifest: p.Manifest, state: state, insert: true}); err != nil {
		return SchemaRecord{}, err
	}
	rec, _, err := readRecord(ctx, tx, kg)
	return rec, err
}

// runSteps runs the steps that belong to a phase, in order; a step of the other phase is a mistake of the
// caller, not something to skip.
func runSteps(ctx context.Context, tx *sql.Tx, steps []pgschema.Step, phase pgschema.Phase) error {
	for _, st := range steps {
		if st.Phase != phase {
			return kernel.Errorf(kernel.CodeInternal, "the step %s %s cannot run here", st.What, st.Name)
		}
		if _, err := tx.ExecContext(ctx, st.SQL); err != nil {
			return dbErr(err, "run the step that makes the "+st.What+" "+st.Name)
		}
	}
	return nil
}

// DropStorageSchema drops the storage schema of a KG with everything in it, and its record, in one transaction. A
// KG that has none is left as it is. The number of the KG is not given again.
func (s *Store) DropStorageSchema(ctx context.Context, kg graph.KGID) error {
	sh, err := s.rt.locate(kg)
	if err != nil {
		return err
	}
	if err := s.detachSources(ctx, sh, kg); err != nil {
		return err
	}
	return locked(ctx, sh, kg, func(tx *sql.Tx) error {
		rec, found, err := readRecord(ctx, tx, kg)
		if err != nil || !found {
			return err
		}
		if err := dropQuerySchema(ctx, tx, rec.KGNo); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+pgschema.QuoteIdent(rec.Schema())+` CASCADE`); err != nil {
			return dbErr(err, "drop the storage schema")
		}
		return dropStateRows(ctx, tx, kg)
	})
}
