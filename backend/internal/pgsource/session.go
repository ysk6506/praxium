package pgsource

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// session is the one connection of one read, with the settings it was made from. The pool behind it has room for one
// connection and the read holds it from the first statement to the end (db.Conn), so that the driver is called only from
// the goroutines of this read: database/sql opens a connection in a goroutine of its own only for a caller that waits
// for one, and nobody waits.
type session struct {
	r          *Reader
	st         settings
	db         *sql.DB
	conn       *sql.Conn
	tr         *egress.Tracker
	unregister func()
	budget     int64 // the bytes one transaction may read
	dead       bool
}

// connectBudget is the bytes the connection may read before the first transaction: the answers of the start-up.
const connectBudget = 1 << 20

// cancelGrace is how long the driver has, after the read is cancelled or runs out of time, to send the cancel request
// to the source and read the answer, before every connection is closed.
const cancelGrace = 2 * time.Second

// close ends the connection and removes the TLS settings of this read from the registry of the driver. The cancel
// request of a statement that was cut short is sent while the connection is still open, so this comes after.
func (s *session) close() {
	_ = s.catch(func() error {
		if s.tr != nil {
			s.tr.CloseAll()
		}
		if s.conn != nil {
			_ = s.conn.Close()
		}
		if s.db != nil {
			_ = s.db.Close()
		}
		return nil
	})
	s.unregister()
}

// guard runs a call of the driver. A panic of the driver (it has no recover, and a source that sends a message that is
// cut short or has a wrong length can make it panic) is the end of this read and of nothing else: the connection is
// closed and never used again, and the error says only that the answer of the source was not what the protocol says. The
// value of the panic is dropped, since it may hold bytes that the source sent.
func (s *session) guard(fn func() error) error {
	if s.dead {
		return sourceErr(ingest.SrcResponseFormat, "the answer of the source was not what the protocol of PostgreSQL says", nil)
	}
	return s.catch(fn)
}

// catch runs a call of the driver and turns a panic of it into the end of the read (guard says it). It runs when the
// connection is dead too, which is what the closing of the connection needs.
func (s *session) catch(fn func() error) (err error) {
	defer func() {
		if recover() != nil {
			s.dead = true
			if s.tr != nil {
				s.tr.CloseAll()
			}
			err = sourceErr(ingest.SrcResponseFormat, "the answer of the source was not what the protocol of PostgreSQL says", nil)
		}
	}()
	return fn()
}

// connect makes the connection, so that a failure of the name, the connection, the TLS or the sign-in is told before any
// statement is, and the check can say which of them it was.
func (s *session) connect(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, s.st.connect+connectSlack)
	defer cancel()
	stop := s.closeLater(cctx)
	defer stop()
	err := s.guard(func() error {
		conn, err := s.db.Conn(cctx)
		if err != nil {
			return err
		}
		s.conn = conn
		return conn.PingContext(cctx)
	})
	if err != nil && ctx.Err() == nil && cctx.Err() != nil {
		return sourceErr(ingest.SrcTimeout, "the source did not answer within the time of the connection", nil)
	}
	return classify(ctx, err)
}

// closeLater closes every connection of the read a little after the context ends (cancelled or out of time), whatever
// the source does: the driver is given cancelGrace to ask the source to stop the statement first. The returned function
// ends the wait when the call is over.
func (s *session) closeLater(ctx context.Context) func() {
	over := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		grace, cancel := context.WithTimeout(context.Background(), cancelGrace)
		defer cancel()
		select {
		case <-grace.Done():
			s.tr.CloseAll()
		case <-over:
		}
	})
	return func() {
		close(over)
		stop()
	}
}

// connectSlack is what the client waits beyond connect_timeout_seconds for the driver to give up by itself.
const connectSlack = 5 * time.Second

// txSlack is what a transaction may take beyond its statement time limits: the client's own bound, for a source that
// stops answering and never says so. Past it (and cancelGrace) every connection is closed.
const txSlack = 10 * time.Second

// stmtsPerTx is the most statements one transaction runs (the limits, the description, the count, a page, a group).
const stmtsPerTx = 5

// readTx runs fn in one short read-only transaction (repeatable read, so that the statements of one page see one state
// of the source) with the time limits of the source set for this transaction alone. Nothing is held between two
// transactions at the source, and no setting of the session is changed: the limits are set with is_local. The
// transaction has a time of its own and a budget of bytes to read (s.budget); when either runs out every connection is
// closed, so that a read that waits on a source that went quiet returns.
func (s *session) readTx(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) error {
	txctx, cancel := context.WithTimeout(ctx, stmtsPerTx*s.st.statement+txSlack)
	defer cancel()
	s.tr.Window(s.budget)
	stop := s.closeLater(txctx)
	defer stop()
	err := s.guard(func() error { return s.runTx(txctx, fn) })
	if err != nil && ctx.Err() == nil && txctx.Err() != nil {
		return sourceErr(ingest.SrcTimeout, "the source did not finish the page within the time the read may take", nil)
	}
	return err
}

func (s *session) runTx(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) error {
	tx, err := s.conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
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
const limitsSQL = `SELECT pg_catalog.set_config('statement_timeout', $1, true), pg_catalog.set_config('lock_timeout', $2, true), pg_catalog.set_config('idle_in_transaction_session_timeout', $3, true)`

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
