package pgconn

import (
	"context"
	"database/sql"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// PoolKind names one of the three pools of a Pool.
type PoolKind string

// The pools: writes go to the primary server; the read pool serves the built-in reads and the saved queries; the
// query pool is for the free-form queries of people (nothing uses it yet).
const (
	WritePool PoolKind = "write"
	ReadPool  PoolKind = "read"
	QueryPool PoolKind = "query"
)

// Pool is a database with the limits of its roles: three pools of connections (write, read, query) and the settings
// that every transaction of a role starts with. Make one with Open and close it with Close.
type Pool struct {
	db     *Handle // the write pool
	read   *Handle
	query  *Handle
	limits map[Role]Limits
	hook   func(ctx context.Context, tx *sql.Tx) error
	closed atomic.Bool
}

// Open connects, checks the connection and the version of the server (18 or newer), and brings the schema up to date
// with the migrations of this build. The DSN is never part of an error. Closing the Pool closes the connections.
func Open(ctx context.Context, cfg Config) (*Pool, error) {
	rc, err := cfg.resolve()
	if err != nil {
		return nil, err
	}
	p, err := build(rc)
	if err != nil {
		return nil, err
	}
	if err := p.start(ctx, rc); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}

// build makes the three pools of a resolved configuration; no connection is opened yet.
func build(rc resolved) (*Pool, error) {
	lim := newLimiter(rc.Clock, rc.NewConnsPerSecond)
	readDSN := rc.ReadDSN
	if readDSN == "" {
		readDSN = rc.DSN
	}
	mk := func(dsn string, write bool, size int) (*Handle, error) {
		c, err := newConnector(dsn, write, rc.ConnectTimeout)
		if err != nil {
			return nil, err
		}
		return newHandle(limitedConnector{inner: c, lim: lim}, size, rc.MaxIdleConns, rc.ConnMaxLifetime), nil
	}
	w, err := mk(rc.DSN, true, rc.MaxOpenConns)
	if err != nil {
		return nil, err
	}
	r, err := mk(readDSN, false, rc.ReadConns)
	if err != nil {
		_ = w.close()
		return nil, err
	}
	q, err := mk(readDSN, false, rc.QueryConns)
	if err != nil {
		_ = w.close()
		_ = r.close()
		return nil, err
	}
	return &Pool{db: w, read: r, query: q, limits: rc.limits, hook: rc.BeforeCommit}, nil
}

// start reaches the server(s), applies the version gate and runs the migrations.
func (p *Pool) start(ctx context.Context, rc resolved) error {
	if err := p.db.PingContext(ctx); err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "cannot reach the PostgreSQL server")
	}
	if err := gate(ctx, rc.ReadVersion, p.db); err != nil {
		return err
	}
	if rc.ReadDSN != "" && rc.ReadDSN != rc.DSN {
		if err := p.read.PingContext(ctx); err != nil {
			return kernel.Wrap(kernel.CodeInternal, err, "cannot reach the PostgreSQL server of the read pool")
		}
		if err := gate(ctx, rc.ReadVersion, p.read); err != nil {
			return err
		}
	}
	steps, err := embedded()
	if err != nil {
		return err
	}
	return migrate(ctx, p, steps)
}

// DB is the write pool, a handle that follows the renewal of its connections. Do not keep what it gives out.
func (p *Pool) DB() *Handle { return p.db }

// Generation is how many times the pool of that kind was renewed.
func (p *Pool) Generation(kind PoolKind) uint64 {
	if h := p.handle(kind); h != nil {
		return h.Generation()
	}
	return 0
}

func (p *Pool) handle(kind PoolKind) *Handle {
	switch kind {
	case WritePool:
		return p.db
	case ReadPool:
		return p.read
	case QueryPool:
		return p.query
	}
	return nil
}

// Close closes the connections of the three pools. The Pool cannot be used afterwards.
func (p *Pool) Close() error {
	p.closed.Store(true)
	var first error
	for _, h := range []*Handle{p.db, p.read, p.query} {
		if err := h.close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Tx runs fn in one transaction with the limits of the role: a reading transaction on the read pool, a writing one
// on the write pool. See TxOn.
func (p *Pool) Tx(ctx context.Context, role Role, readOnly bool, fn func(*sql.Tx) error) error {
	kind := WritePool
	if readOnly {
		kind = ReadPool
	}
	return p.TxOn(ctx, kind, role, readOnly, fn)
}

// TxOn runs fn in one transaction on the pool of that kind. A reading transaction is read-only at repeatable read, so
// everything fn reads is one state; a writing one is read committed and locks what it needs. When a failure says the
// role of the server changed or the connection is lost, the connections of that pool are thrown away (once, however
// many callers meet the failure) and the error comes back with the code CodeRoleChanged or CodeConnectionLost. A
// COMMIT that got no answer comes back as CodeOutcomeUnknown: the caller must find out whether it happened.
func (p *Pool) TxOn(ctx context.Context, kind PoolKind, role Role, readOnly bool, fn func(*sql.Tx) error) error {
	h := p.handle(kind)
	if h == nil || p.closed.Load() {
		return kernel.Errorf(kernel.CodeInternal, "the pool %q is not open", kind)
	}
	opts := &sql.TxOptions{}
	if readOnly {
		opts = &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
	}
	tx, gen, err := begin(ctx, h, opts)
	if err != nil {
		return settle(h, gen, Classify(err), err, "begin")
	}
	defer func() { _ = tx.Rollback() }()
	l := p.limits[role]
	var unused [3]string
	// set_config with is_local true is SET LOCAL: it ends with the transaction, so it cannot leak through a pool.
	err = tx.QueryRowContext(ctx, `SELECT set_config('statement_timeout', $1, true), set_config('lock_timeout', $2, true),
		set_config('idle_in_transaction_session_timeout', $3, true)`, millis(l.Statement), millis(l.LockWait), millis(l.IdleInTx)).
		Scan(&unused[0], &unused[1], &unused[2])
	if err != nil {
		return settle(h, gen, Classify(err), err, "set the limits of the role")
	}
	if err := fn(tx); err != nil {
		if k := kindOfWork(err, readOnly); k == KindRoleChanged || k == KindLost {
			return settle(h, gen, k, err, "the work of the transaction")
		}
		return err
	}
	return p.commit(ctx, h, gen, tx)
}

// begin starts a transaction. When the start fails because the connection is lost or the role of the server changed,
// the pool is renewed and the start is tried once more on the new one; nothing was written before the start, so
// the second try is safe. When another caller renewed the pool while this one tried, the start is tried once on the
// new pool. A start that the context of the caller ended is not tried again, and neither is any other failure.
// The generation comes back as the pool's current one on a success. On a failure of the retry it is the one the
// caller looked at before this call renewed the pool, so that the caller's renewal does not renew it a second time.
func begin(ctx context.Context, h *Handle, opts *sql.TxOptions) (*sql.Tx, uint64, error) {
	db, gen := h.current()
	tx, err := db.BeginTx(ctx, opts)
	if err == nil || ctx.Err() != nil || Classify(err) == KindCaller {
		return tx, gen, err
	}
	if k := Classify(err); k == KindLost || k == KindRoleChanged {
		h.renew(gen) // a no-op when another caller renewed the pool already
		db, now := h.current()
		tx, err = db.BeginTx(ctx, opts)
		if err != nil {
			return nil, gen, err
		}
		return tx, now, nil
	}
	if _, now := h.current(); now == gen {
		return tx, gen, err
	}
	db, gen = h.current()
	tx, err = db.BeginTx(ctx, opts)
	return tx, gen, err
}

// settle renews the pool when the kind of an error says so, and gives the error its code. An error of another
// kind is only given a code of this backend.
func settle(h *Handle, gen uint64, k Kind, err error, what string) error {
	switch k {
	case KindRoleChanged:
		h.renew(gen)
		return kernel.Wrap(CodeRoleChanged, err, what+": the server cannot take this work (the connections of the pool were thrown away)")
	case KindLost:
		h.renew(gen)
		return kernel.Wrap(CodeConnectionLost, err, what+": the connection is lost (the connections of the pool were thrown away)")
	}
	return DBErr(err, what)
}

// commit commits and sorts the answer. A success is a commit; an answer of the server and an end of the context
// before the COMMIT was sent are not; everything else is unknown, and the connections are thrown away.
func (p *Pool) commit(ctx context.Context, h *Handle, gen uint64, tx *sql.Tx) error {
	if p.hook != nil {
		if err := p.hook(ctx, tx); err != nil {
			return DBErr(err, "before the commit")
		}
	}
	err := tx.Commit()
	switch CommitOutcome(err) {
	case OutcomeCommitted:
		return nil
	case OutcomeNotCommitted:
		if k := Classify(err); k == KindRoleChanged {
			return settle(h, gen, k, err, "commit")
		}
		return DBErr(err, "commit")
	}
	h.renew(gen)
	return kernel.Wrap(CodeOutcomeUnknown, err, "commit got no answer: the transaction may or may not be committed (the connections of the pool were thrown away)")
}

func millis(d time.Duration) string { return strconv.FormatInt(d.Milliseconds(), 10) }
