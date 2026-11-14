package pgstore

import (
	"bytes"
	"context"
	"database/sql"
	"unicode/utf8"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Records is the PostgreSQL store of the identity data (ADR-0044): the records of the Store port of the identity
// unit, in the table identity_record of the same database as the KGs. It meets that port by its method signatures
// only, as this unit does not import the identity unit; the conformance suite of the port runs on it.
type Records struct{ rt router }

// Records returns the identity store of the database this Store is open on. It shares the pool, so closing the
// Store closes it too.
func (s *Store) Records() *Records { return &Records{rt: s.rt} }

// shard is where identity records live: the records belong to no KG, so any key finds the one database.
func (r *Records) shard() (*shard, error) { return r.rt.locate(graph.KGID{}) }

// Get returns a record and its version, or a not_found error.
func (r *Records) Get(ctx context.Context, kind, id string) ([]byte, int64, error) {
	sh, err := r.shard()
	if err != nil {
		return nil, 0, err
	}
	var data string
	var version int64
	err = sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT data, version FROM identity_record WHERE kind = $1 AND id = $2`, kind, id).Scan(&data, &version)
	})
	if isNoRows(err) {
		return nil, 0, kernel.Errorf(kernel.CodeNotFound, "no %s record %q", kind, id)
	}
	if err != nil {
		return nil, 0, dbErr(err, "read an identity record")
	}
	return []byte(data), version, nil
}

// Put writes the record when the stored version is expect (0 for a record that must not exist yet).
func (r *Records) Put(ctx context.Context, kind, id string, data []byte, expect int64) (int64, error) {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return 0, kernel.Errorf(kernel.CodeInvalidValue, "a record is UTF-8 text without NUL")
	}
	sh, err := r.shard()
	if err != nil {
		return 0, err
	}
	var version int64
	var wrote bool
	err = sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		var qerr error
		if expect == 0 {
			qerr = tx.QueryRowContext(ctx, `INSERT INTO identity_record (kind, id, version, data) VALUES ($1, $2, 1, $3)
				ON CONFLICT (kind, id) DO NOTHING RETURNING version`, kind, id, string(data)).Scan(&version)
		} else {
			qerr = tx.QueryRowContext(ctx, `UPDATE identity_record SET version = version + 1, data = $3
				WHERE kind = $1 AND id = $2 AND version = $4 RETURNING version`, kind, id, string(data), expect).Scan(&version)
		}
		if qerr == nil {
			wrote = true
		} else if !isNoRows(qerr) {
			return qerr
		}
		return nil
	})
	if err != nil {
		return 0, dbErr(err, "write an identity record")
	}
	if !wrote {
		return 0, kernel.Errorf(kernel.CodeConflict, "the %s record %q is not at version %d", kind, id, expect)
	}
	return version, nil
}

type recordRow struct {
	id      string
	data    string
	version int64
}

// Scan calls fn for every record of the kind in the order of the IDs. The records are read first, then fn is called
// with the transaction over, so fn may use the store.
func (r *Records) Scan(ctx context.Context, kind string, fn func(id string, data []byte, version int64) error) error {
	sh, err := r.shard()
	if err != nil {
		return err
	}
	var rows []recordRow
	err = sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		cur, err := tx.QueryContext(ctx, `SELECT id, data, version FROM identity_record WHERE kind = $1 ORDER BY id`, kind)
		if err != nil {
			return err
		}
		defer cur.Close()
		for cur.Next() {
			var row recordRow
			if err := cur.Scan(&row.id, &row.data, &row.version); err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return cur.Err()
	})
	if err != nil {
		return dbErr(err, "scan the identity records")
	}
	for _, row := range rows {
		if err := fn(row.id, []byte(row.data), row.version); err != nil {
			return err
		}
	}
	return nil
}
