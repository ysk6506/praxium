package pgstore

import (
	"fmt"
	"time"

	"github.com/ysk6506/praxium/backend/internal/pgconn"
)

// Role and Limits are the roles and the time limits of the connection base (pgconn). The names stay here because
// the Config below uses them and the store runs every transaction under one of them.
type (
	Role   = pgconn.Role
	Limits = pgconn.Limits
)

// The roles this store uses: RoleApplier for everything that writes and RoleAPIRead for every read.
const (
	RoleApplier = pgconn.RoleApplier
	RoleAPIRead = pgconn.RoleAPIRead
)

// defaultPollInterval is how often WaitReadable looks at the head when the configuration names none.
const defaultPollInterval = 50 * time.Millisecond

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

// conn is the part of the Config that the connection base reads.
func (c Config) conn() pgconn.Config {
	return pgconn.Config{DSN: c.DSN, Limits: c.Limits, MaxOpenConns: c.MaxOpenConns, MaxIdleConns: c.MaxIdleConns, ConnMaxLifetime: c.ConnMaxLifetime}
}

// poll is the polling interval with its default filled in.
func (c Config) poll() time.Duration {
	if c.PollInterval > 0 {
		return c.PollInterval
	}
	return defaultPollInterval
}
