package main

import (
	"log/slog"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/devauth"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// dbTLSState reads how the connection string of -store-dsn-file protects the link to the database (ADR-0064). It
// says false when there is nothing to judge: the store is not PostgreSQL, or the file cannot be read (opening the
// store reports that, in the same words, a moment later).
func dbTLSState(o serveOptions, d wiring) (pgstore.TLSState, bool, error) {
	if o.store != storePostgres || o.storeDSNFile == "" {
		return pgstore.TLSState{}, false, nil
	}
	raw, err := d.env.ReadFile(o.storeDSNFile)
	if err != nil {
		return pgstore.TLSState{}, false, nil
	}
	st, err := pgstore.InspectTLS(strings.TrimSpace(string(raw)))
	return st, err == nil, err
}

// checkDBTLS is the production-mode guard for the link to PostgreSQL: a database on another machine must be reached
// over TLS that checks the certificate of the server, or the operator must say that the link is protected some other
// way (-allow-unverified-db-tls). A database on this machine (loopback address, Unix socket) needs nothing. Like the
// other guards it runs before the store is opened.
func checkDBTLS(o serveOptions, d wiring) error {
	st, ok, err := dbTLSState(o, d)
	if err != nil || !ok || st.Acceptable() || o.allowUnverifiedDBTLS {
		return err
	}
	mode := "not set, so the driver uses require"
	if st.SSLMode != "" {
		mode = st.SSLMode
	}
	return kernel.Errorf(kernel.CodeInvalidValue,
		"production mode (-mode production) will not start: the PostgreSQL connection in -store-dsn-file reaches a host on another machine "+
			"and does not check the server certificate (sslmode: %s), so the data and the credentials could be read or changed on the way. "+
			"Either put sslmode=verify-full (or verify-ca) in the connection string, with sslrootcert=FILE if the certificate is not from a CA this machine trusts, "+
			"or, if the link is protected in another way (a private network, a tunnel), start with -allow-unverified-db-tls", mode)
}

// warnDBTLS says in the start log that production runs on a link that does not check the server, because the
// operator allowed it. Nothing is said for a link that is fine, and nothing outside production mode.
func warnDBTLS(o serveOptions, d wiring, log *slog.Logger) {
	if strings.ToLower(strings.TrimSpace(o.mode)) != devauth.ModeProduction || !o.allowUnverifiedDBTLS {
		return
	}
	if st, ok, _ := dbTLSState(o, d); ok && !st.Acceptable() {
		log.Warn("praxium PostgreSQL connection does not check the server certificate (allowed by -allow-unverified-db-tls)",
			slog.String("sslmode", st.SSLMode))
	}
}
