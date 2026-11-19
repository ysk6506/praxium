package pgconn

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Role is the kind of caller a transaction runs for. Each role has its own time limits (postgres-operations.md
// 3, point 7): a transaction sets them with SET LOCAL, so they hold behind a pool that gives out connections per
// transaction and do not leak to the next user of the connection.
type Role string

// The roles. The KG store (pgstore) uses RoleApplier for everything that writes, RoleAPIRead for every read and
// RoleMigration for the migration runner. RoleBulk and RoleDryRun are named here so that a deployment can set
// their limits now; the callers that use them come with the loading of large sources and the dry run.
const (
	RoleApplier   Role = "applier"
	RoleAPIRead   Role = "api_read"
	RoleBulk      Role = "bulk"
	RoleDryRun    Role = "dry_run"
	RoleMigration Role = "migration"
)

// Roles lists the roles in a fixed order.
func Roles() []Role {
	return []Role{RoleApplier, RoleAPIRead, RoleBulk, RoleDryRun, RoleMigration}
}

// Limits are the time limits of a role. A zero limit means none (PostgreSQL's own meaning of 0).
type Limits struct {
	// Statement is the longest a single statement may run.
	Statement time.Duration
	// LockWait is the longest a statement waits for a lock.
	LockWait time.Duration
	// IdleInTx is the longest a transaction may sit idle between two statements.
	IdleInTx time.Duration
}

// DefaultLimits returns the limits a role gets when the configuration names none. The values are assumptions
// that stay to be measured in the closed beta (postgres-operations.md 8): short for the readers, longer for
// the applier, long for what loads a lot.
func DefaultLimits(r Role) Limits {
	switch r {
	case RoleAPIRead:
		return Limits{Statement: 30 * time.Second, LockWait: 2 * time.Second, IdleInTx: 15 * time.Second}
	case RoleBulk:
		return Limits{Statement: 10 * time.Minute, LockWait: 5 * time.Second, IdleInTx: time.Minute}
	case RoleDryRun:
		return Limits{Statement: 5 * time.Minute, LockWait: 2 * time.Second, IdleInTx: 30 * time.Second}
	case RoleMigration:
		return Limits{Statement: 5 * time.Minute, LockWait: 3 * time.Second, IdleInTx: time.Minute}
	}
	return Limits{Statement: time.Minute, LockWait: 5 * time.Second, IdleInTx: 30 * time.Second}
}

// Config is what Open needs. DSN is a secret: it never appears in an error or in the text of the Config. What only
// one user of the connection cares about (how often the KG store looks at a head, say) is not here: it stays with
// that user.
type Config struct {
	// DSN is a PostgreSQL connection string, in URL or key=value form. Name sslmode in it.
	DSN string
	// Limits override the default limits of the roles named in it; a role that is not in the map keeps its default.
	Limits map[Role]Limits
	// ReadDSN is the connection string of the read pool and the query pool. Empty means DSN is used for them too
	// (the read pool may go to a replica in a later step; today it may be the same server).
	ReadDSN string
	// MaxOpenConns is the size of the write pool (default 8); ReadConns is the size of the read pool (default 8) and
	// QueryConns the size of the pool of free-form queries (default 4). MaxIdleConns caps the idle connections of
	// each pool (default 4) and ConnMaxLifetime is how long a connection lives (default 30 minutes).
	MaxOpenConns    int
	ReadConns       int
	QueryConns      int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	// ConnectTimeout is the longest a connection attempt waits (default 5 seconds). The driver waits for ever when
	// the connection string names none, so a value is always set.
	ConnectTimeout time.Duration
	// NewConnsPerSecond limits how many new connections the pools open together (default 20), so that all the pools
	// of all the servers do not reconnect at once after a failure (postgres-ha.md, rule 10).
	NewConnsPerSecond int
	// Clock is the time of the limit on new connections (default the system clock).
	Clock kernel.Clock
	// ReadVersion reads server_version_num of a server; it is replaced in tests to give the version gate a server
	// of version 17 (default: SHOW server_version_num).
	ReadVersion func(ctx context.Context, db *Handle) (int, error)
	// BeforeCommit is a hook for tests: it runs after the work of a transaction and before its COMMIT.
	BeforeCommit func(ctx context.Context, tx *sql.Tx) error
}

// String is the Config without its secret, for logs.
func (c Config) String() string {
	return fmt.Sprintf("pgconn.Config{DSN: <redacted>, MaxOpenConns: %d, ReadConns: %d, QueryConns: %d, MaxIdleConns: %d}",
		c.MaxOpenConns, c.ReadConns, c.QueryConns, c.MaxIdleConns)
}

// GoString keeps %#v from printing the DSN.
func (c Config) GoString() string { return c.String() }

// resolved is a Config with every default filled in and every limit of every role known.
type resolved struct {
	Config
	limits map[Role]Limits
}

// resolve fills the defaults and refuses what cannot be right. The messages never carry the DSN.
func (c Config) resolve() (resolved, error) {
	if c.DSN == "" {
		return resolved{}, kernel.Errorf(kernel.CodeInvalidValue, "the PostgreSQL store needs a DSN")
	}
	known := map[Role]bool{}
	limits := map[Role]Limits{}
	for _, r := range Roles() {
		known[r] = true
		limits[r] = DefaultLimits(r)
	}
	for _, r := range sortedRoles(c.Limits) {
		l := c.Limits[r]
		if !known[r] {
			return resolved{}, kernel.Errorf(kernel.CodeInvalidValue, "unknown role %q in the limits", r)
		}
		if l.Statement < 0 || l.LockWait < 0 || l.IdleInTx < 0 {
			return resolved{}, kernel.Errorf(kernel.CodeInvalidValue, "the limits of role %q cannot be negative", r)
		}
		limits[r] = l
	}
	c.MaxOpenConns = orInt(c.MaxOpenConns, 8)
	c.ReadConns = orInt(c.ReadConns, 8)
	c.QueryConns = orInt(c.QueryConns, 4)
	c.MaxIdleConns = orInt(c.MaxIdleConns, 4)
	c.ConnMaxLifetime = orDuration(c.ConnMaxLifetime, 30*time.Minute)
	c.ConnectTimeout = orDuration(c.ConnectTimeout, 5*time.Second)
	c.NewConnsPerSecond = orInt(c.NewConnsPerSecond, 20)
	if c.Clock == nil {
		c.Clock = kernel.SystemClock{}
	}
	return resolved{Config: c, limits: limits}, nil
}

func sortedRoles(m map[Role]Limits) []Role {
	out := make([]Role, 0, len(m))
	for r := range m {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func orInt(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

func orDuration(v, def time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return def
}
