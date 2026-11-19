package pgconn

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync"
	"time"
)

// Handle is one pool of connections that can be thrown away and made again: a *sql.DB behind a generation number.
// Renewing it swaps in a new *sql.DB over the same connector and closes the old one; a transaction that is still
// running on the old one finishes there (its connection is closed when it is given back).
type Handle struct {
	mu   sync.Mutex
	db   *sql.DB
	gen  uint64
	make func() *sql.DB
}

func newHandle(conn driver.Connector, open, idle int, life time.Duration) *Handle {
	h := &Handle{}
	h.make = func() *sql.DB {
		db := sql.OpenDB(conn)
		db.SetMaxOpenConns(open)
		db.SetMaxIdleConns(min(idle, open))
		db.SetConnMaxLifetime(life)
		return db
	}
	h.db = h.make()
	return h
}

// current is the pool to use now and its generation.
func (h *Handle) current() (*sql.DB, uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.db, h.gen
}

// Generation counts how many times the pool was renewed.
func (h *Handle) Generation() uint64 {
	_, g := h.current()
	return g
}

// renew throws the connections away, unless the pool was renewed already since the caller looked at generation seen:
// goroutines that meet the same failure together renew once.
func (h *Handle) renew(seen uint64) bool {
	h.mu.Lock()
	if h.gen != seen {
		h.mu.Unlock()
		return false
	}
	old := h.db
	h.db = h.make()
	h.gen++
	h.mu.Unlock()
	_ = old.Close()
	return true
}

func (h *Handle) close() error {
	db, _ := h.current()
	return db.Close()
}

// BeginTx, QueryRowContext and PingContext are those of the current *sql.DB. A caller
// that wants the pool to follow a failure uses Pool.Tx, which renews the pool when the failure says so.
func (h *Handle) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	db, _ := h.current()
	return db.BeginTx(ctx, opts)
}

func (h *Handle) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	db, _ := h.current()
	return db.QueryRowContext(ctx, q, args...)
}

func (h *Handle) PingContext(ctx context.Context) error {
	db, _ := h.current()
	return db.PingContext(ctx)
}
