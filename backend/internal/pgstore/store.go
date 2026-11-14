package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// shard is one database with the limits of its roles. The router hands out shards; today there is one.
type shard struct {
	db     *sql.DB
	limits map[Role]Limits
}

// router finds the storage of a KG. Every access to storage goes through it (kg-storage.md 11, REQ-263): it is
// what lets a KG move from one database to another later. locate answers for one KG; all lists every shard, for
// what has no KG id yet (looking a KG up by name, listing KGs). With one database the answer is always the same.
type router interface {
	locate(kg graph.KGID) (*shard, error)
	all() []*shard
}

type singleRouter struct{ sh *shard }

func (r singleRouter) locate(graph.KGID) (*shard, error) { return r.sh, nil }
func (r singleRouter) all() []*shard                     { return []*shard{r.sh} }

// Store is the PostgreSQL implementation of graph.Store. Make one with Open and close it with Close.
type Store struct {
	rt      router
	poll    time.Duration
	closers []func() error
}

var _ graph.Store = (*Store)(nil)

// Open connects, checks the connection and brings the schema up to date with the migrations of this build. The
// DSN is never part of an error. Closing the Store closes the pool.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	rc, err := cfg.resolve()
	if err != nil {
		return nil, err
	}
	conn, err := pq.NewConnector(rc.DSN)
	if err != nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the DSN cannot be read as a PostgreSQL connection string (its text is not shown)")
	}
	db := sql.OpenDB(conn)
	db.SetMaxOpenConns(rc.MaxOpenConns)
	db.SetMaxIdleConns(rc.MaxIdleConns)
	db.SetConnMaxLifetime(rc.ConnMaxLifetime)
	sh := &shard{db: db, limits: rc.limits}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot reach the PostgreSQL server")
	}
	steps, err := embedded()
	if err == nil {
		err = migrate(ctx, sh, steps)
	}
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return newStore(singleRouter{sh}, rc.PollInterval, db.Close), nil
}

func newStore(rt router, poll time.Duration, closers ...func() error) *Store {
	return &Store{rt: rt, poll: poll, closers: closers}
}

// Close closes the pool. The Store cannot be used afterwards.
func (s *Store) Close() error {
	var first error
	for _, c := range s.closers {
		if err := c(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// tx runs fn in one transaction on the shard with the limits of the role. A reading transaction is read-only at
// repeatable read, so everything fn reads is one state; a writing one is read committed and locks what it needs.
func (sh *shard) tx(ctx context.Context, role Role, readOnly bool, fn func(*sql.Tx) error) error {
	opts := &sql.TxOptions{}
	if readOnly {
		opts = &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
	}
	tx, err := sh.db.BeginTx(ctx, opts)
	if err != nil {
		return dbErr(err, "begin")
	}
	defer func() { _ = tx.Rollback() }()
	l := sh.limits[role]
	var unused [3]string
	// set_config with is_local true is SET LOCAL: it ends with the transaction, so it cannot leak through a pool.
	err = tx.QueryRowContext(ctx, `SELECT set_config('statement_timeout', $1, true), set_config('lock_timeout', $2, true),
		set_config('idle_in_transaction_session_timeout', $3, true)`, millis(l.Statement), millis(l.LockWait), millis(l.IdleInTx)).
		Scan(&unused[0], &unused[1], &unused[2])
	if err != nil {
		return dbErr(err, "set the limits of the role")
	}
	if err := fn(tx); err != nil {
		return err
	}
	return dbErr(tx.Commit(), "commit")
}

func millis(d time.Duration) string { return strconv.FormatInt(d.Milliseconds(), 10) }

// SQLSTATE classes this unit tells apart.
const (
	sqlUniqueViolation = "23505"
	sqlLockNotAvail    = "55P03"
	sqlQueryCanceled   = "57014"
	sqlIdleInTxTimeout = "25P03"
)

// dbErr gives an error from the database or the driver a code of this backend. An error that already has a code
// passes unchanged. The text of a driver error never carries the DSN.
func dbErr(err error, what string) error {
	if err == nil {
		return nil
	}
	var coded *kernel.Error
	if errors.As(err, &coded) {
		return err
	}
	var pe *pq.Error
	if errors.As(err, &pe) {
		switch string(pe.Code) {
		case sqlUniqueViolation:
			return kernel.Wrap(kernel.CodeConflict, err, what+": a row with that key is already there")
		case sqlLockNotAvail, sqlQueryCanceled, sqlIdleInTxTimeout:
			return kernel.Wrap(kernel.CodeInternal, err, what+": a time limit of the role passed")
		}
	}
	return kernel.Wrap(kernel.CodeInternal, err, what)
}

// notFound turns "no rows" into the not-found code of the graph unit and any other error into dbErr's.
func notFound(err error, what string, kg graph.KGID) error {
	if errors.Is(err, sql.ErrNoRows) {
		return kernel.Errorf(graph.CodeKGNotFound, "no KG %q", kg)
	}
	return dbErr(err, what)
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
