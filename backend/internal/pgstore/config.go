package pgstore

import (
	"fmt"
	"sort"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Role is the kind of caller a transaction runs for. Each role has its own time limits (postgres-operations.md
// 3, point 7): a transaction sets them with SET LOCAL, so they hold behind a pool that gives out connections per
// transaction and do not leak to the next user of the connection.
type Role string

// The roles. The store itself uses RoleApplier for everything that writes, RoleAPIRead for every read and
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

// Config is what Open needs. DSN is a secret: it never appears in an error or in the text of the Config.
type Config struct {
	// DSN is a PostgreSQL connection string, in URL or key=value form. Name sslmode in it.
	DSN string
	// Limits override the default limits of the roles named in it; a role that is not in the map keeps its default.
	Limits map[Role]Limits
	// MaxOpenConns, MaxIdleConns and ConnMaxLifetime size the pool of database/sql (defaults 16, 4 and 30 minutes).
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	// PollInterval is how often WaitReadable looks at the head (default 50 ms). There is no LISTEN/NOTIFY.
	PollInterval time.Duration
}

// String is the Config without its secret, for logs.
func (c Config) String() string {
	return fmt.Sprintf("pgstore.Config{DSN: <redacted>, MaxOpenConns: %d, MaxIdleConns: %d, PollInterval: %s}",
		c.MaxOpenConns, c.MaxIdleConns, c.PollInterval)
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
	c.MaxOpenConns = orInt(c.MaxOpenConns, 16)
	c.MaxIdleConns = orInt(c.MaxIdleConns, 4)
	c.ConnMaxLifetime = orDuration(c.ConnMaxLifetime, 30*time.Minute)
	c.PollInterval = orDuration(c.PollInterval, 50*time.Millisecond)
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
