package pgconn

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Pool is one database with the limits of its roles: the connection pool of database/sql and the settings that
// every transaction of a role starts with. Make one with Open and close it with Close.
type Pool struct {
	db     *sql.DB
	limits map[Role]Limits
}

// Open connects, checks the connection and brings the schema up to date with the migrations of this build. The
// DSN is never part of an error. Closing the Pool closes the connections.
func Open(ctx context.Context, cfg Config) (*Pool, error) {
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
	p := &Pool{db: db, limits: rc.limits}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot reach the PostgreSQL server")
	}
	steps, err := embedded()
	if err == nil {
		err = migrate(ctx, p, steps)
	}
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return p, nil
}

// DB is the pool of database/sql, for the statements a unit runs inside Tx or reads with.
func (p *Pool) DB() *sql.DB { return p.db }

// Close closes the connections. The Pool cannot be used afterwards.
func (p *Pool) Close() error { return p.db.Close() }

// Tx runs fn in one transaction with the limits of the role. A reading transaction is read-only at repeatable
// read, so everything fn reads is one state; a writing one is read committed and locks what it needs.
func (p *Pool) Tx(ctx context.Context, role Role, readOnly bool, fn func(*sql.Tx) error) error {
	opts := &sql.TxOptions{}
	if readOnly {
		opts = &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
	}
	tx, err := p.db.BeginTx(ctx, opts)
	if err != nil {
		return DBErr(err, "begin")
	}
	defer func() { _ = tx.Rollback() }()
	l := p.limits[role]
	var unused [3]string
	// set_config with is_local true is SET LOCAL: it ends with the transaction, so it cannot leak through a pool.
	err = tx.QueryRowContext(ctx, `SELECT set_config('statement_timeout', $1, true), set_config('lock_timeout', $2, true),
		set_config('idle_in_transaction_session_timeout', $3, true)`, millis(l.Statement), millis(l.LockWait), millis(l.IdleInTx)).
		Scan(&unused[0], &unused[1], &unused[2])
	if err != nil {
		return DBErr(err, "set the limits of the role")
	}
	if err := fn(tx); err != nil {
		return err
	}
	return DBErr(tx.Commit(), "commit")
}

func millis(d time.Duration) string { return strconv.FormatInt(d.Milliseconds(), 10) }

// SQLSTATE classes this unit tells apart.
const (
	sqlUniqueViolation = "23505"
	sqlLockNotAvail    = "55P03"
	sqlQueryCanceled   = "57014"
	sqlIdleInTxTimeout = "25P03"
)

// DBErr gives an error from the database or the driver a code of this backend. An error that already has a code
// passes unchanged. The text of a driver error never carries the DSN.
func DBErr(err error, what string) error {
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
