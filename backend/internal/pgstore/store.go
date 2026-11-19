package pgstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
)

// shard is one database with the limits of its roles, held by the pool of the connection base. The router hands
// out shards; today there is one.
type shard struct {
	pool *pgconn.Pool
}

// router finds the storage of a KG. Every access to storage goes through it (kg-storage.md 11, REQ-270): it is
// what lets a KG move from one database to another later. locate answers for one KG; all lists every shard, for
// what has no KG id yet (looking a KG up by name, listing KGs). With one database the answer is always the same.
type router interface {
	locate(kg graph.KGID) (*shard, error)
	all() []*shard
}

type singleRouter struct{ sh *shard }

func (r singleRouter) locate(graph.KGID) (*shard, error) { return r.sh, nil }
func (r singleRouter) all() []*shard                     { return []*shard{r.sh} }

// tx runs fn in one transaction on the shard with the limits of the role (see pgconn.Pool.Tx).
func (sh *shard) tx(ctx context.Context, role Role, readOnly bool, fn func(*sql.Tx) error) error {
	return sh.pool.Tx(ctx, role, readOnly, fn)
}

// dbErr gives an error from the database or the driver a code of this backend (see pgconn.DBErr).
func dbErr(err error, what string) error { return pgconn.DBErr(err, what) }

// notFound turns "no rows" into the not-found code of the graph unit and any other error into dbErr's.
func notFound(err error, what string, kg graph.KGID) error {
	if errors.Is(err, sql.ErrNoRows) {
		return kernel.Errorf(graph.CodeKGNotFound, "no KG %q", kg)
	}
	return dbErr(err, what)
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
