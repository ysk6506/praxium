package pgconn

import (
	"context"
	"database/sql/driver"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// libpqUnsupportedEnv are the variables the driver refuses to start with (connector.go of lib/pq, fromEnv).
var libpqUnsupportedEnv = []string{"PGREQUIRESSL", "PGSSLCOMPRESSION", "PGREALM", "PGGSSENCMODE", "PGGSSDELEGATION", "PGGSSLIB",
	"PGREQUIREAUTH", "PGCHANNELBINDING", "PGSSLCERTMODE", "PGSSLCRL", "PGSSLCRLDIR", "PGREQUIREPEER"}

// driverDebugEnv is the variable that makes the driver write what it sends and receives to the standard error.
const driverDebugEnv = "PQGO_DEBUG"

// driverEnvNames are the environment variables pq.NewConfig reads: the env tags of pq.Config. The driver reads the
// whole environment and lets it fill whatever the connection string does not name; there is no way to switch that
// off, so the connection is made only when none of them is set.
func driverEnvNames() []string {
	t := reflect.TypeOf(pq.Config{})
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if tag := f.Tag.Get("env"); f.IsExported() && tag != "" && tag != "-" {
			out = append(out, tag)
		}
	}
	out = append(out, libpqUnsupportedEnv...)
	// PQGO_DEBUG is read by the driver when it starts, outside Config: set, it writes every message the driver sends
	// (the password included) and every row it reads to the standard error, for this connection and for the connections
	// of the sources, which use the same driver (ADR-0093).
	out = append(out, driverDebugEnv)
	sort.Strings(out)
	return out
}

// refuseDriverEnv is an error when a variable the driver would read is set: a variable of the shell must not change
// where the server connects or how (the connection string is the one place, and it is a secret kept in a file).
func refuseDriverEnv() error {
	for _, name := range driverEnvNames() {
		if _, set := kernel.LookupEnv(name); set {
			return kernel.Errorf(kernel.CodeInvalidValue, "the environment variable %s is set: Praxium takes the PostgreSQL connection from the "+
				"connection string only, and the driver would let this variable fill in what the string does not say; unset it and start again", name)
		}
	}
	return nil
}

// newConnector reads a connection string into a connector. forceWrite makes the connector accept only a server that
// takes writes (target_session_attrs=read-write, put in here and never read from the string). Every refusal is
// worded without the text of the string.
func newConnector(dsn string, forceWrite bool, connectTimeout time.Duration) (*pq.Connector, error) {
	if err := refuseDriverEnv(); err != nil {
		return nil, err
	}
	cfg, err := pq.NewConfig(dsn)
	if err != nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the DSN cannot be read as a PostgreSQL connection string (its text is not shown)")
	}
	if len(cfg.Runtime) > 0 {
		keys := make([]string, 0, len(cfg.Runtime))
		for k := range cfg.Runtime {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the connection string has keys the driver does not know (%s): it would send them to the server as "+
			"startup parameters, so they are refused", strings.Join(keys, ", "))
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = connectTimeout
	}
	if forceWrite {
		if cfg.TargetSessionAttrs != "" && cfg.TargetSessionAttrs != pq.TargetSessionAttrsReadWrite {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "target_session_attrs of the connection string for writes is not yours to set: "+
				"Praxium sets it to read-write itself so that writes go to the primary server only")
		}
		cfg.TargetSessionAttrs = pq.TargetSessionAttrsReadWrite
	}
	return pq.NewConnectorConfig(cfg)
}

// limitedConnector opens connections through a driver.Connector, no faster than the limiter allows.
type limitedConnector struct {
	inner driver.Connector
	lim   *limiter
}

func (c limitedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := c.lim.wait(ctx); err != nil {
		return nil, err
	}
	return c.inner.Connect(ctx)
}

func (c limitedConnector) Driver() driver.Driver { return c.inner.Driver() }
