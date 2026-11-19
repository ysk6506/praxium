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
	// ReadDSN is the connection string of the read pool; empty means DSN is used for it too.
	ReadDSN string
	// MaxOpenConns is the size of the write pool (default 8), ReadConns of the read pool (8) and QueryConns of the
	// pool of free-form queries (4); MaxIdleConns and ConnMaxLifetime size them all (defaults 4 and 30 minutes).
	MaxOpenConns    int
	ReadConns       int
	QueryConns      int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	// NewConnsPerSecond limits how fast the pools open new connections (default 20).
	NewConnsPerSecond int
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
	return pgconn.Config{DSN: c.DSN, ReadDSN: c.ReadDSN, Limits: c.Limits, MaxOpenConns: c.MaxOpenConns, ReadConns: c.ReadConns,
		QueryConns: c.QueryConns, MaxIdleConns: c.MaxIdleConns, ConnMaxLifetime: c.ConnMaxLifetime, NewConnsPerSecond: c.NewConnsPerSecond}
}

// poll is the polling interval with its default filled in.
func (c Config) poll() time.Duration {
	if c.PollInterval > 0 {
		return c.PollInterval
	}
	return defaultPollInterval
}
