package pgsource

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// noFile is a path no file is at (a file of /dev/null is no folder). It stands where the driver would look in the home
// folder of the server's user: the client certificate (~/.postgresql/postgresql.crt and its key) and .pgpass. A
// definition has no client certificate (ADR-0093), and the password of the account is always given, so nothing of the
// home folder may reach the connection.
const noFile = "/dev/null/praxium-none"

// applicationName is what the source sees in its list of sessions, so that its operator knows whose connection it is.
const applicationName = "praxium-source"

// tlsKeys makes the name that a TLS configuration is registered with in the driver. The registry of the driver is
// shared by the whole process, so every read has a name of its own and removes it when its connection is closed.
var tlsKeys atomic.Uint64

// plaintextConnector marks every connection it makes as one without TLS, so that adapter.egress lets it go only to the
// loopback or where the operator allowed connections without TLS (sslmode disable, sources-and-adapters.md 8).
type plaintextConnector struct {
	inner *pq.Connector
}

func (c plaintextConnector) Connect(ctx context.Context) (driver.Conn, error) {
	return c.inner.Connect(egress.WithPlaintext(ctx))
}

func (c plaintextConnector) Driver() driver.Driver { return c.inner.Driver() }

// settings are the values of the connection of the source with the defaults of the definition applied.
type settings struct {
	host      string
	port      int
	database  string
	disable   bool
	caPEM     string
	connect   time.Duration
	statement time.Duration
	lock      time.Duration
}

func settingsOf(cn *ingest.Connection) settings {
	s := settings{host: cn.Host, port: cn.Port, database: cn.Database, disable: cn.SSLMode == ingest.SSLDisable, caPEM: cn.CAPEM}
	if s.port == 0 {
		s.port = ingest.DefaultPostgresPort
	}
	s.connect = secondsOr(cn.ConnectTimeoutSeconds, ingest.DefaultConnectSeconds)
	s.statement = secondsOr(cn.StatementTimeoutSeconds, ingest.DefaultStatementSeconds)
	s.lock = secondsOr(cn.LockTimeoutSeconds, ingest.DefaultLockSeconds)
	return s
}

func secondsOr(n, def int) time.Duration {
	if n <= 0 {
		n = def
	}
	return time.Duration(n) * time.Second
}

// open makes the pool of one read: the secret is opened for this source and this address, the connection configuration
// is filled in here (never read from a string or from the environment), the dialer is the one of adapter.egress, and the
// TLS settings are registered under a name of this read. The pool has at most one connection and is made again for
// every read; close it when the read is over.
func (r *Reader) open(ctx context.Context, purpose secrets.Purpose) (*session, error) {
	st := r.st
	opened, err := r.secrets.Open(ctx, r.conn.Credential.SecretID, secrets.Use{SourceID: r.src.ID, Host: st.host, Port: st.port, Purpose: purpose})
	if err != nil {
		return nil, openError(err)
	}
	defer opened.Close()
	acct, serr := accountOf(opened)
	if serr != nil {
		return nil, serr
	}
	// The address that was allowed is the address that is connected to (ADR-0071 decision 5).
	addr := opened.Address()
	cfg := pq.Config{
		Host: addr.Host, Port: uint16(addr.Port), User: acct.user, Password: acct.password, Database: st.database,
		SSLMode: pq.SSLModeDisable, SSLSNI: true, SSLMinProtocolVersion: pq.SSLProtocolVersionTLS12,
		MinProtocolVersion: pq.ProtocolVersion30, MaxProtocolVersion: pq.ProtocolVersion30,
		ConnectTimeout: st.connect, ClientEncoding: "UTF8", Datestyle: "ISO, MDY",
		ApplicationName: applicationName, SSLCert: noFile, SSLKey: noFile, Passfile: noFile,
	}
	sess := &session{r: r, st: st, unregister: func() {}}
	if !st.disable {
		if err := sess.useTLS(&cfg); err != nil {
			return nil, err
		}
	}
	conn, err := pq.NewConnectorConfig(cfg)
	if err != nil {
		sess.unregister()
		return nil, sourceErr(ingest.SrcInternal, "the connection could not be set up", err)
	}
	conn.Dialer(r.dialer)
	var connector driver.Connector = conn
	if st.disable {
		connector = plaintextConnector{inner: conn}
	}
	sess.db = sql.OpenDB(connector)
	sess.db.SetMaxOpenConns(1)
	sess.db.SetMaxIdleConns(1)
	return sess, nil
}

// useTLS registers the TLS settings of egress (TLS 1.2 or later, the certificate and the host name always verified, only
// the certificates of ca_pem trusted when it is given) under the name of this read, and makes the connection ask for
// exactly those. The driver's own modes are never used: require, prefer and verify-ca turn the verification off, and
// verify-full reads root.crt of the home folder.
func (s *session) useTLS(cfg *pq.Config) error {
	tlsCfg, err := egress.TLSConfig(s.st.caPEM)
	if err != nil {
		return classify(context.Background(), err)
	}
	key := "praxium-source-" + strconv.FormatUint(tlsKeys.Add(1), 10)
	if err := pq.RegisterTLSConfig(key, tlsCfg); err != nil {
		return sourceErr(ingest.SrcInternal, "the TLS settings could not be registered", err)
	}
	s.unregister = func() { _ = pq.RegisterTLSConfig(key, nil) }
	cfg.SSLMode = pq.SSLMode("pqgo-" + key)
	return nil
}
