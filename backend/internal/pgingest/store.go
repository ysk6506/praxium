package pgingest

import (
	"context"
	"database/sql"
	"embed"
	"errors"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
)

// ownSources holds the Go files of this unit, for the test that scans them for what may not stay on a session
// (pgconn.SessionStateViolations). Nothing else reads it.
//
//go:embed *.go
var ownSources embed.FS

// catalogTable is the list of the KGs (migration 0004 of adapter.pgconn). It is not a table of the ingestion, so
// adapter.pgschema does not name it; this unit reads the number of a KG from it, and nothing else.
const (
	catalogTable = "kg_catalog"
	catalogKGID  = "kg_id"
	catalogKGNo  = "kg_no"
	// closedKGs is every KG that is being deleted or made: the two marks (migrations 0009 and 0010 of adapter.pgconn, ADR-0125,
	// ADR-0126), keyed by catalogKGID.
	closedKGs = "(SELECT kg_id FROM kg_deleting UNION ALL SELECT kg_id FROM kg_making)"
)

// The roles this unit runs its transactions under: the applier for the record of runs and the sources, bulk for the
// pages (long statements, a long wait for a lock).
const (
	roleApplier = pgconn.RoleApplier
	roleBulk    = pgconn.RoleBulk
)

// Store is the record of runs, the sources of the KGs and the receiving of a source, on one database.
type Store struct {
	pool *pgconn.Pool
	// identSeen is the hook the tests of the identification watch its statements by (see identObserver); it is nil in the code that runs.
	identSeen identObserver
	// expTest is the hook the tests of the expansion use (expand.go); it is nil in the code that runs.
	expTest *expHook
	// rcpSeen is the hook the tests of the computation of the objects watch its statements by; it is nil in the code that runs.
	rcpSeen identObserver
}

var _ ingest.RunLedger = (*Store)(nil)

// New makes the Store of a pool of the connection base. The pool is the caller's: closing it is the caller's too.
func New(pool *pgconn.Pool) *Store { return &Store{pool: pool} }

// tx runs fn in one transaction of the role on the write pool: the record of runs is read there too, because a read
// that follows a write must not meet a replica that has not caught up.
func (s *Store) tx(ctx context.Context, role pgconn.Role, fn func(*sql.Tx) error) error {
	return s.pool.Tx(ctx, role, false, fn)
}

func dbErr(err error, what string) error { return pgconn.DBErr(err, what) }

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// kgNoOf reads the number of a KG, taking a share lock on its row of the list so that a KG that is deleted at the
// same time is deleted before or after this transaction and not in the middle of it.
func kgNoOf(ctx context.Context, tx *sql.Tx, kg graph.KGID) (int64, error) {
	var no int64
	err := tx.QueryRowContext(ctx, "SELECT "+catalogKGNo+" FROM "+catalogTable+" WHERE "+catalogKGID+" = $1 FOR SHARE", kg.String()).Scan(&no)
	if isNoRows(err) {
		return 0, kernel.Errorf(graph.CodeKGNotFound, "no KG %q", kg)
	}
	if err != nil {
		return 0, dbErr(err, "read the number of the KG")
	}
	return no, refuseClosed(ctx, tx, kg.String())
}

// refuseClosed refuses a KG that is being deleted (ADR-0125) or made (ADR-0126): from the first transaction of a delete, and until
// the last transaction of a making, the ingestion neither starts nor goes on for it. Called after the statement that took the share
// lock on the row of the list, it is a statement of its own and so sees a mark that the delete wrote before the lock was given (the
// delete locks the row to write it); a making writes its mark with the row of the list.
func refuseClosed(ctx context.Context, tx *sql.Tx, kgID string) error {
	var closed bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM "+closedKGs+" d WHERE d."+catalogKGID+" = $1)", kgID).Scan(&closed); err != nil {
		return dbErr(err, "look for the mark of a KG that is being made or deleted")
	}
	if closed {
		return kernel.Errorf(graph.CodeKGNotFound, "KG %q is being made or deleted", kgID)
	}
	return nil
}
