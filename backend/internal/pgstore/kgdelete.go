package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The delete of a KG in steps (ADR-0125). Dropping a storage schema in one transaction locks every relation in it and the
// types of its tables, about twice what making it took, and every table the model or the ingestion adds makes it more; the
// lock table that every connection shares has room for a KG only if it takes a part of it. So a delete is a chain of
// transactions, each well inside 80% of the lock table of the default settings:
//
//  1. mark: the KG gets its row in kg_deleting (its row of the list is locked first, so a transaction of the ingestion that
//     holds the row is done before and one that comes after sees the mark). From then on the KG is closed to reads, writes
//     and ingestion and is left out of the list.
//  2. the sources, one to a transaction (detachSources, ADR-0098), with their runs.
//  3. the tables of the computation (pgschema.ComputeTables), computeDropBatch to a transaction. No run is left to write them:
//     the runs went with their sources, and a transaction that was writing one holds its lock until it ends.
//  4. the query schema and its roles (ADR-0113), in a transaction of their own: their views and functions lock about 2,200,
//     and the storage schema with them would not fit.
//  5. last: the rest of the storage schema, the state rows and the row of the list (the mark goes with it).
//
// Every step can be run again: a delete that stopped in the middle is ended by DeleteKG called again, by the server when it
// starts (ResumeAllWork) and by CreateKG for a KG that would clash with the one being deleted.

// computeDropBatch is how many tables of the computation one transaction drops. A table with its indexes, its row and array
// types and its TOAST table is about a dozen locks, so a batch is a few hundred whatever the number of the tables grows to.
const computeDropBatch = 8

// The steps of a delete, as deleteSeen is told them.
const (
	deleteStepMark    = "mark"
	deleteStepSource  = "source"
	deleteStepCompute = "compute"
	deleteStepQuery   = "query"
	deleteStepLast    = "last"
)

// seen tells the hook of the tests, if there is one, that a transaction of a delete is at its end.
func (s *Store) seen(ctx context.Context, step string, tx *sql.Tx) error {
	if s.deleteSeen == nil {
		return nil
	}
	return s.deleteSeen(ctx, step, tx)
}

// DeleteKG deletes a KG: its sources, its storage schema with everything in it, its state records and its row of the list, in
// the steps above. A KG that is not there is graph.CodeKGNotFound; a KG whose delete stopped in the middle is there until it
// ends, so a call again ends it. The number of the KG is not given again.
func (s *Store) DeleteKG(ctx context.Context, kg graph.KGID) error {
	sh, err := s.rt.locate(kg)
	if err != nil {
		return err
	}
	defer s.forget(kg)
	found, err := s.markDeleting(ctx, sh, kg)
	if err != nil {
		return err
	}
	if !found {
		return kernel.Errorf(graph.CodeKGNotFound, "no KG %q", kg)
	}
	return s.dropInSteps(ctx, sh, kg, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM kg_catalog WHERE kg_id = $1`, kg.String()); err != nil {
			return dbErr(err, "delete the KG from the list")
		}
		return dropSchemaRest(ctx, tx, kg)
	})
}

// markDeleting is the first step: it writes the mark of a KG that has a row of the list and says whether there is a KG to
// delete at all (a row of the list or a storage schema).
func (s *Store) markDeleting(ctx context.Context, sh *shard, kg graph.KGID) (bool, error) {
	var found bool
	err := locked(ctx, sh, kg, func(tx *sql.Tx) error {
		var no int64
		err := tx.QueryRowContext(ctx, `SELECT kg_no FROM kg_catalog WHERE kg_id = $1 FOR UPDATE`, kg.String()).Scan(&no)
		if err != nil && !isNoRows(err) {
			return dbErr(err, "lock the KG in the list")
		}
		listed := err == nil
		_, hasSchema, err := readRecord(ctx, tx, kg)
		if err != nil {
			return err
		}
		found = listed || hasSchema
		if listed {
			if _, err := tx.ExecContext(ctx, `INSERT INTO kg_deleting (kg_id, started_at) VALUES ($1, now()) ON CONFLICT (kg_id) DO NOTHING`,
				kg.String()); err != nil {
				return dbErr(err, "mark the KG as being deleted")
			}
		}
		return s.seen(ctx, deleteStepMark, tx)
	})
	return found, err
}

// dropInSteps runs the steps after the mark: the sources, the tables of the computation, the query schema, and last, the work
// the caller gives the last transaction.
func (s *Store) dropInSteps(ctx context.Context, sh *shard, kg graph.KGID, last func(tx *sql.Tx) error) error {
	if err := s.detachSources(ctx, sh, kg); err != nil {
		return err
	}
	if err := s.dropComputeTables(ctx, sh, kg); err != nil {
		return err
	}
	err := locked(ctx, sh, kg, func(tx *sql.Tx) error {
		rec, found, err := readRecord(ctx, tx, kg)
		if err != nil || !found {
			return err
		}
		if err := dropQuerySchema(ctx, tx, rec.KGNo); err != nil {
			return err
		}
		return s.seen(ctx, deleteStepQuery, tx)
	})
	if err != nil {
		return err
	}
	return locked(ctx, sh, kg, func(tx *sql.Tx) error {
		if err := last(tx); err != nil {
			return err
		}
		return s.seen(ctx, deleteStepLast, tx)
	})
}

// dropComputeTables drops the tables of the computation of the KG, computeDropBatch to a transaction. A table that is not
// there (a step that ran before, a schema of a generator that had none) is passed over.
func (s *Store) dropComputeTables(ctx context.Context, sh *shard, kg graph.KGID) error {
	tables := pgschema.ComputeTables()
	for start := 0; start < len(tables); start += computeDropBatch {
		batch := tables[start:min(start+computeDropBatch, len(tables))]
		err := locked(ctx, sh, kg, func(tx *sql.Tx) error {
			rec, found, err := readRecord(ctx, tx, kg)
			if err != nil || !found {
				return err
			}
			names := make([]string, len(batch))
			for i, table := range batch {
				names[i] = pgschema.Qualified(rec.Schema(), table)
			}
			if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+strings.Join(names, ", ")); err != nil {
				return dbErr(err, "drop the tables of the computation")
			}
			return s.seen(ctx, deleteStepCompute, tx)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// dropSchemaRest is the last step but the row of the list: the storage schema with what is left in it, and the state rows. A
// KG without a storage schema has nothing of it to drop.
func dropSchemaRest(ctx context.Context, tx *sql.Tx, kg graph.KGID) error {
	rec, found, err := readRecord(ctx, tx, kg)
	if err != nil || !found {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+pgschema.QuoteIdent(rec.Schema())+` CASCADE`); err != nil {
		return dbErr(err, "drop the storage schema")
	}
	return dropStateRows(ctx, tx, kg)
}

// errKGDeleting is the refusal of a read or a write of a KG that is being deleted: it is not there for the caller any more.
func errKGDeleting(kg graph.KGID) error {
	return kernel.Errorf(graph.CodeKGNotFound, "KG %s is being deleted", kg)
}

// endDeletes ends every delete that a mark says a server left half done and returns the KGs it ended. A KG whose delete
// cannot be ended does not stop the others: its error is in the one returned, which joins them.
func (s *Store) endDeletes(ctx context.Context) ([]graph.KGID, error) {
	var done []graph.KGID
	var failed []error
	for _, sh := range s.rt.all() {
		ids, err := markedKGs(ctx, sh, `SELECT kg_id FROM kg_deleting ORDER BY kg_id`)
		if err != nil {
			failed = append(failed, err)
			continue
		}
		for _, id := range ids {
			if err := s.endDelete(ctx, id); err != nil {
				failed = append(failed, kernel.Wrap(kernel.CodeOf(err), err, "cannot end the delete of KG "+id.String()))
				continue
			}
			done = append(done, id)
		}
	}
	return done, errors.Join(failed...)
}

// endClashingDeletes ends the deletes of the KGs that a new KG would clash with (its id, its name, or the one production
// KG), so that a KG being deleted does not keep its name from a new one.
func (s *Store) endClashingDeletes(ctx context.Context, sh *shard, kg graph.KG) error {
	ids, err := markedKGs(ctx, sh, `SELECT d.kg_id FROM kg_deleting d JOIN kg_catalog c ON c.kg_id = d.kg_id
		WHERE c.kg_id = $1 OR c.name = $2 OR ($3 AND c.role = 'production') ORDER BY d.kg_id`,
		kg.ID.String(), kg.Name, kg.Role == graph.RoleProduction)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.endDelete(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// endDelete ends one delete; a KG that another server deleted in the meantime is ended too.
func (s *Store) endDelete(ctx context.Context, kg graph.KGID) error {
	if err := s.DeleteKG(ctx, kg); err != nil && kernel.CodeOf(err) != graph.CodeKGNotFound {
		return err
	}
	return nil
}

// markedKGs reads the ids of the KGs that a query of the marks gives, on the write pool: a mark that was just written is
// there.
func markedKGs(ctx context.Context, sh *shard, query string, args ...any) ([]graph.KGID, error) {
	var out []graph.KGID
	err := sh.tx(ctx, RoleAPIRead, false, func(tx *sql.Tx) error {
		out = out[:0]
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return dbErr(err, "list the KGs being deleted")
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var text string
			if err := rows.Scan(&text); err != nil {
				return dbErr(err, "read a KG being deleted")
			}
			id, err := parseID[graph.KGKind](text)
			if err != nil {
				return err
			}
			out = append(out, id)
		}
		return dbErr(rows.Err(), "list the KGs being deleted")
	})
	return out, err
}
