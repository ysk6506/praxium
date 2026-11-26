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
// query schema (SchemaModel.noQuery) is left without it, and its plan has no query schema (the tables, and the statistics objects
// when the model makes them).
func readToken(ctx context.Context, tx *sql.Tx, m SchemaModel) (SchemaModel, error) {
	if m.noQuery {
		return m, nil
	}
	var err error
	m.token, err = readInstallToken(ctx, tx)
	return m, err
}

// CreateStorageSchema makes the storage schema of a KG from the model: it takes the number of the KG from the
// sequence, runs the statements of the plan, and writes the record with the manifest, in one transaction of the
// migration role on the write pool, under the lock of the KG's schema work; then it makes the query schemas, one to a
// transaction (ADR-0126). It either makes everything or leaves nothing: a query schema that fails deletes what was
// made. Called again for a schema that was made from the same model it changes nothing and returns the record; for one
// made from another model it is a conflict (AdjustStorageSchema follows a change of the model). Two runs at once take
// turns, so both succeed and one of them works.
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
	if err := s.endClashingMakings(ctx, sh, kg, "", false); err != nil {
		return SchemaRecord{}, err
	}
	var out made
	err = locked(ctx, sh, kg, func(tx *sql.Tx) error {
		var err error
		if out, err = createInTx(ctx, tx, kg, m, plan); err != nil {
			return err
		}
		return s.schemaStep(ctx, schemaStepMake, tx)
	})
	if err != nil {
		return SchemaRecord{}, err
	}
	if err := s.makeRest(ctx, sh, kg, out.plan); err != nil {
		return SchemaRecord{}, err
	}
	return out.rec, nil
}

// createInTx is the first transaction of a making: the storage schema, its record and, when the plan has a query schema, the mark
// of the making (ADR-0126).
func createInTx(ctx context.Context, tx *sql.Tx, kg graph.KGID, m SchemaModel, plan planner) (made, error) {
	have, found, err := readRecord(ctx, tx, kg)
	if err != nil {
		return made{}, err
	}
	if found {
		return joinMaking(ctx, tx, have, m, plan)
	}
	var kgNo int64
	if err := tx.QueryRowContext(ctx, `SELECT nextval('kg_no_seq')`).Scan(&kgNo); err != nil {
		return made{}, dbErr(err, "take the number of the KG")
	}
	if m, err = readToken(ctx, tx, m); err != nil {
		return made{}, err
	}
	p, err := plan(m, kgNo)
	if err != nil {
		return made{}, err
	}
	if err := runSteps(ctx, tx, p.Steps, pgschema.InTx); err != nil {
		return made{}, err
	}
	state := string(p.Manifest.State)
	if err := writeRecord(ctx, tx, recordWrite{kg: kg, manifest: p.Manifest, state: state, insert: true}); err != nil {
		return made{}, err
	}
	if len(p.Query) > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO kg_making (kg_id, expires_at) VALUES ($1, now() + make_interval(secs => $2))`,
			kg.String(), makingTerm.Seconds()); err != nil {
			return made{}, dbErr(err, "mark the KG as being made")
		}
	}
	rec, _, err := readRecord(ctx, tx, kg)
	return made{rec: rec, plan: p}, err
}

// joinMaking is the first transaction of a making that finds the record. One of another model is a conflict. One of the same model
// is the record, with the plan of its query schema when another run is making it still: the two take turns over the query schemas
// and both end. A making whose term is over is deleted before (endClashingMakings), so only a run at that very moment meets one; it
// is a conflict.
func joinMaking(ctx context.Context, tx *sql.Tx, have SchemaRecord, m SchemaModel, plan planner) (made, error) {
	if err := CheckSchemaMatch(have, m); err != nil {
		return made{}, kernel.Wrap(kernel.CodeConflict, err, "the KG has a storage schema already")
	}
	var over bool
	err := tx.QueryRowContext(ctx, `SELECT expires_at < now() FROM kg_making WHERE kg_id = $1`, have.KG.String()).Scan(&over)
	switch {
	case isNoRows(err):
		return made{rec: have}, nil
	case err != nil:
		return made{}, dbErr(err, "read the mark of a KG being made")
	case over:
		return made{}, kernel.Errorf(kernel.CodeConflict, "the making of the KG %s stopped and is deleted first; make it again", have.KG)
	}
	if m, err = readToken(ctx, tx, m); err != nil {
		return made{}, err
	}
	p, err := plan(m, have.KGNo)
	return made{rec: have, plan: p}, err
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

// DropStorageSchema drops the storage schema of a KG with everything in it, and its record, in the steps of a delete
// after the mark (ADR-0125): the sources, the tables of the computation, and the rest in the last transaction. It leaves
// the row of the list, and writes no mark, as it always did; a call that stopped in the middle is ended by a call again.
// A KG that has none is left as it is. The number of the KG is not given again.
func (s *Store) DropStorageSchema(ctx context.Context, kg graph.KGID) error {
	sh, err := s.rt.locate(kg)
	if err != nil {
		return err
	}
	return s.dropInSteps(ctx, sh, kg, func(tx *sql.Tx) error { return dropSchemaRest(ctx, tx, kg) })
}
