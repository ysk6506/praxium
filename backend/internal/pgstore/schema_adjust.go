package pgstore

import (
	"context"
	"database/sql"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// adjustment is what the first transaction of an adjustment leaves for the second part.
type adjustment struct {
	rec        SchemaRecord
	concurrent []pgschema.Step
	online     bool
}

// AdjustStorageSchema brings the storage schema of a KG up to the model after the model changed. It compares the
// manifest it kept with the one of the model (pgschema.Diff) and follows only an addition: a new type, a new
// property, a new volatile property, a change of the search set, a change that touches no table. The steps that
// run inside a transaction run in one, under the lock of the KG's schema work; the indexes that are built or
// dropped on tables that have rows run after it, one statement at a time, without blocking the writers. A
// change that is not an addition is refused (pgschema.CodeNotAdditive) and the database is as it was.
//
// An index that a stopped run left half built (invalid) is dropped and built again, and an index that the
// manifest does not know is dropped; the state is ok when no index is left to do. A KG whose statistics are
// still to be made is not adjusted (FinishStorageStats first). A run that finds another one building indexes
// of the same KG is a conflict, unless that run has said nothing for longer than buildingStale.
func (s *Store) AdjustStorageSchema(ctx context.Context, kg graph.KGID, m SchemaModel) (SchemaRecord, error) {
	return s.adjustStorageSchema(ctx, kg, m, buildPlan)
}

func (s *Store) adjustStorageSchema(ctx context.Context, kg graph.KGID, m SchemaModel, plan planner) (SchemaRecord, error) {
	if _, err := hashesOfModel(m); err != nil {
		return SchemaRecord{}, err
	}
	sh, err := s.rt.locate(kg)
	if err != nil {
		return SchemaRecord{}, err
	}
	var adj adjustment
	err = locked(ctx, sh, kg, func(tx *sql.Tx) error {
		var err error
		adj, err = adjustInTx(ctx, tx, kg, m, plan)
		return err
	})
	if err != nil || !adj.online {
		return adj.rec, err
	}
	if err := buildOnline(ctx, sh, adj); err != nil {
		return adj.rec, err
	}
	return settledRecord(ctx, sh, kg)
}

// settledRecord reads the record of a KG from the primary, after the indexes were built.
func settledRecord(ctx context.Context, sh *shard, kg graph.KGID) (SchemaRecord, error) {
	var rec SchemaRecord
	err := sh.tx(ctx, migrationRole, false, func(tx *sql.Tx) error {
		var err error
		rec, _, err = readRecord(ctx, tx, kg)
		return err
	})
	return rec, err
}

// adjustInTx is the first part: the checks, the steps that run in the transaction and the record. The record
// is left alone when there is nothing to do.
func adjustInTx(ctx context.Context, tx *sql.Tx, kg graph.KGID, m SchemaModel, plan planner) (adjustment, error) {
	rec, found, err := readRecord(ctx, tx, kg)
	if err := adjustable(rec, found, err, kg); err != nil {
		return adjustment{}, err
	}
	next, delta, err := adjustPlan(ctx, tx, rec, m, plan)
	if err != nil {
		return adjustment{}, err
	}
	inTx, concurrent := splitPhases(delta.Steps)
	online := len(concurrent) > 0 || rec.State != string(pgschema.StateOK)
	if len(inTx) == 0 && !online && CheckSchemaMatch(rec, m) == nil {
		return adjustment{rec: rec}, nil
	}
	if err := runSteps(ctx, tx, inTx, pgschema.InTx); err != nil {
		return adjustment{}, err
	}
	if err := runQuerySteps(ctx, tx, next); err != nil {
		return adjustment{}, err
	}
	state, result := string(pgschema.StateOK), delta.Result
	if online {
		state, result.State = stateBuilding, pgschema.StateIndexPending
	}
	if err := writeRecord(ctx, tx, recordWrite{kg: kg, manifest: result, state: state, claim: online}); err != nil {
		return adjustment{}, err
	}
	rec, _, err = readRecord(ctx, tx, kg)
	return adjustment{rec: rec, concurrent: concurrent, online: online}, err
}

// adjustPlan is the plan of the model as it is now (with the token of the installation, for the query schema) and the steps that take the
// schema of the record to it.
func adjustPlan(ctx context.Context, tx *sql.Tx, rec SchemaRecord, m SchemaModel, plan planner) (pgschema.Plan, pgschema.Delta, error) {
	var err error
	if m.token, err = readInstallToken(ctx, tx); err != nil {
		return pgschema.Plan{}, pgschema.Delta{}, err
	}
	next, err := plan(m, rec.KGNo)
	if err != nil {
		return pgschema.Plan{}, pgschema.Delta{}, err
	}
	delta, err := pgschema.Diff(rec.Manifest, next.Manifest)
	return next, delta, err
}

// adjustable says whether the record of a KG may be adjusted now.
func adjustable(rec SchemaRecord, found bool, err error, kg graph.KGID) error {
	switch {
	case err != nil:
		return err
	case !found:
		return kernel.Errorf(kernel.CodeNotFound, "the KG %s has no storage schema", kg)
	case rec.State == string(pgschema.StateStatsPending):
		return kernel.Errorf(kernel.CodeConflict, "the statistics of the storage schema of the KG %s are not made yet; make them first", kg)
	case rec.State == stateBuilding && !rec.Stale:
		return kernel.Errorf(kernel.CodeConflict, "another run is building the indexes of the storage schema of the KG %s", kg)
	}
	return nil
}

// splitPhases cuts the steps of a delta into the ones that run in the transaction and the ones that run after it.
func splitPhases(steps []pgschema.Step) (inTx, concurrent []pgschema.Step) {
	for _, st := range steps {
		if st.Phase == pgschema.InTx {
			inTx = append(inTx, st)
		} else {
			concurrent = append(concurrent, st)
		}
	}
	return inTx, concurrent
}

// FinishStorageStats makes the statistics objects of a schema that was made without them (SchemaModel.WithStats
// false), once the first load is in. The model must be the one the schema was made from. A schema whose
// statistics are made is left as it is.
func (s *Store) FinishStorageStats(ctx context.Context, kg graph.KGID, m SchemaModel) (SchemaRecord, error) {
	sh, err := s.rt.locate(kg)
	if err != nil {
		return SchemaRecord{}, err
	}
	var out SchemaRecord
	err = locked(ctx, sh, kg, func(tx *sql.Tx) error {
		var err error
		out, err = finishStatsInTx(ctx, tx, kg, m)
		return err
	})
	return out, err
}

func finishStatsInTx(ctx context.Context, tx *sql.Tx, kg graph.KGID, m SchemaModel) (SchemaRecord, error) {
	rec, found, err := readRecord(ctx, tx, kg)
	switch {
	case err != nil:
		return SchemaRecord{}, err
	case !found:
		return SchemaRecord{}, kernel.Errorf(kernel.CodeNotFound, "the KG %s has no storage schema", kg)
	case rec.State != string(pgschema.StateStatsPending):
		return rec, nil
	}
	if err := CheckSchemaMatch(rec, m); err != nil {
		return SchemaRecord{}, err
	}
	p, err := buildPlan(SchemaModel{Registry: m.Registry, Search: m.Search}, rec.KGNo)
	if err != nil {
		return SchemaRecord{}, err
	}
	if err := runSteps(ctx, tx, p.Deferred, pgschema.InTx); err != nil {
		return SchemaRecord{}, err
	}
	done := rec.Manifest
	done.State = pgschema.StateOK
	if err := writeRecord(ctx, tx, recordWrite{kg: kg, manifest: done, state: string(pgschema.StateOK)}); err != nil {
		return SchemaRecord{}, err
	}
	rec, _, err = readRecord(ctx, tx, kg)
	return rec, err
}
