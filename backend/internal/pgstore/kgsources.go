package pgstore

import (
	"context"
	"database/sql"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// detachSources takes the sources off a KG that is about to be deleted, one source to a transaction, before its storage schema
// goes (ADR-0098, REQ-684). A source has partitions (the raw records and the four tables it is received into), and every one of
// them is a table of the schema with its own locks: dropping a schema that still has the partitions of many sources in one
// transaction takes about 68 locks more for each source, and a source count in the hundreds needs more locks than any
// reasonable max_locks_per_transaction gives. A partition is dropped here in a transaction of its own, with the row of the
// source in the state of sources (which takes its datasets and its runs with it). The rows that the runs of a source left in the
// tables that are not partitioned go with the schema.
//
// A KG that is not there has no sources to take off, and the delete says so itself.
func (s *Store) detachSources(ctx context.Context, sh *shard, kg graph.KGID) error {
	var kgNo int64
	var sources []int64
	err := sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT kg_no FROM kg_catalog WHERE kg_id = $1`, kg.String()).Scan(&kgNo)
		if isNoRows(err) {
			return nil
		}
		if err != nil {
			return dbErr(err, "read the number of the KG")
		}
		rows, err := tx.QueryContext(ctx, `SELECT `+pgschema.ColSrcNo+` FROM `+pgschema.TableSourceState+` WHERE `+pgschema.ColKGID+` = $1 ORDER BY `+pgschema.ColSrcNo, kg.String())
		if err != nil {
			return dbErr(err, "list the sources of the KG")
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var no int64
			if err := rows.Scan(&no); err != nil {
				return dbErr(err, "list the sources of the KG")
			}
			sources = append(sources, no)
		}
		return dbErr(rows.Err(), "list the sources of the KG")
	})
	if err != nil {
		return err
	}
	for _, src := range sources {
		if err := s.detachSource(ctx, sh, kg, kgNo, src); err != nil {
			return err
		}
	}
	return nil
}

// detachSource drops the partitions of one source and its state in one transaction, under the lock of the schema work of the
// KG, as every other change of the schema is made.
func (s *Store) detachSource(ctx context.Context, sh *shard, kg graph.KGID, kgNo, src int64) error {
	steps, err := pgschema.DropSourcePartitionSteps(kgNo, src)
	if err != nil {
		return err
	}
	return locked(ctx, sh, kg, func(tx *sql.Tx) error {
		if err := runSteps(ctx, tx, steps, pgschema.InTx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+pgschema.TableSourceState+` WHERE `+pgschema.ColSrcNo+` = $1`, src); err != nil {
			return dbErr(err, "delete the state of the source")
		}
		return s.seen(ctx, deleteStepSource, tx)
	})
}

// dropStateRows deletes what the shared tables keep of a KG whose schema was just dropped: the state of its storage schema, and
// any source that was put to it after its sources were taken off (its datasets and its runs go with it; its partitions were
// in the schema).
func dropStateRows(ctx context.Context, tx *sql.Tx, kg graph.KGID) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+pgschema.TableSourceState+` WHERE `+pgschema.ColKGID+` = $1`, kg.String()); err != nil {
		return dbErr(err, "delete the sources of the KG")
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM kg_schema_state WHERE kg_id = $1`, kg.String())
	return dbErr(err, "delete the state of the storage schema")
}
