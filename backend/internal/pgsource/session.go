package pgsource

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

// session is the one pool of one read, with the settings it was made from.
type session struct {
	r          *Reader
	st         settings
	db         *sql.DB
	unregister func()
}

// close ends the connection and removes the TLS settings of this read from the registry of the driver. The cancel
// request of a statement that was cut short is sent while the connection is still open, so this comes after.
func (s *session) close() {
	if s.db != nil {
		_ = s.db.Close()
	}
	s.unregister()
}

// connect makes the first connection, so that a failure of the name, the connection, the TLS or the sign-in is told
// before any statement is, and the check can say which of them it was.
func (s *session) connect(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.st.connect+connectSlack)
	defer cancel()
	return classify(ctx, s.db.PingContext(ctx))
}

// connectSlack is what the client waits beyond connect_timeout_seconds for the driver to give up by itself.
const connectSlack = 5 * time.Second

// txSlack is what a transaction may take beyond its statement time limits: the client's own bound, for a source that
// stops answering and never says so.
const txSlack = 10 * time.Second

// stmtsPerTx is the most statements one transaction runs (the limits, the description, the count, a page, a group).
const stmtsPerTx = 5

// readTx runs fn in one short read-only transaction (repeatable read, so that the statements of one page see one state
// of the source) with the time limits of the source set for this transaction alone. Nothing is held between two
// transactions at the source, and no setting of the session is changed: the limits are set with is_local.
func (s *session) readTx(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) error {
	ctx, cancel := context.WithTimeout(ctx, stmtsPerTx*s.st.statement+txSlack)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := runStmt(ctx, tx, limitsSQL, millis(s.st.statement), millis(s.st.lock), millis(s.st.statement))
	if err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// limitsSQL sets the three time limits for the transaction (is_local), the statement, the wait for a lock and the wait
// of a transaction between two statements. It is the first statement of every transaction.
const limitsSQL = `SELECT set_config('statement_timeout', $1, true), set_config('lock_timeout', $2, true), set_config('idle_in_transaction_session_timeout', $3, true)`

func millis(d time.Duration) string { return strconv.FormatInt(d.Milliseconds(), 10) }

// stmtRows is the rows of a prepared statement, closed together with it.
type stmtRows struct {
	*sql.Rows
	st *sql.Stmt
}

// Close closes the rows and then the statement.
func (r *stmtRows) Close() error {
	err := r.Rows.Close()
	if cerr := r.st.Close(); err == nil {
		err = cerr
	}
	return err
}

// runStmt runs every statement of this package, and only as a prepared statement: the server then reads it as one
// statement (the protocol of a prepared statement takes no second one), whatever the user's sql holds, and the first
// statement of a read, which has no argument, is no exception (the driver would send a statement without arguments as a
// simple query, which takes several). It is the one place that runs a statement (a test of the sources keeps it so).
func runStmt(ctx context.Context, tx *sql.Tx, query string, args ...any) (*stmtRows, error) {
	st, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	rows, err := st.QueryContext(ctx, args...)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	return &stmtRows{Rows: rows, st: st}, nil
}
