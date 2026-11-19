package pgconn

import (
	"net/netip"
	"path/filepath"
	"strings"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// TLSState says how a connection string protects the link to the database. It is what production mode looks at
// before it starts (ADR-0064): a link that leaves the machine must check the identity of the server.
type TLSState struct {
	// SSLMode is the sslmode the driver would use. Empty means the driver's default, which is require.
	SSLMode string
	// Remote is true when at least one of the hosts is not on this machine (not a loopback address, not a Unix
	// socket). A connection string that names several hosts is remote when any one of them is.
	Remote bool
	// Verified is true when the mode checks the certificate of the server: verify-full or verify-ca. A mode of
	// the form pqgo-NAME runs a custom TLS configuration that cannot be read here, so it does not count.
	Verified bool
}

// Acceptable is whether production mode starts with this state and no explicit permission: the link stays on
// this machine, or the server certificate is checked.
func (s TLSState) Acceptable() bool { return !s.Remote || s.Verified }

// InspectTLS reads the connection string with the parser of the driver the store uses, so it sees what the driver
// will see (URL and key=value forms, several hosts, the driver's own default, PG* environment variables). It
// connects to nothing. The error never carries the string.
func InspectTLS(dsn string) (TLSState, error) {
	cfg, err := pq.NewConfig(dsn)
	if err != nil {
		return TLSState{}, kernel.Errorf(kernel.CodeInvalidValue, "the DSN cannot be read as a PostgreSQL connection string (its text is not shown)")
	}
	st := TLSState{SSLMode: string(cfg.SSLMode)}
	st.Verified = cfg.SSLMode == pq.SSLModeVerifyFull || cfg.SSLMode == pq.SSLModeVerifyCA
	st.Remote = !onThisMachine(cfg.Host, cfg.Hostaddr)
	for _, m := range cfg.Multi {
		st.Remote = st.Remote || !onThisMachine(m.Host, m.Hostaddr)
	}
	return st, nil
}

// onThisMachine is whether a connection to the host stays on this machine. A host address, when given, is where the
// driver connects, so it decides. A name counts only when it is the literal "localhost" or a loopback address: a
// name that merely resolves to one is not trusted.
func onThisMachine(host string, addr netip.Addr) bool {
	if addr.IsValid() {
		return addr.IsLoopback()
	}
	host = strings.TrimSpace(host)
	switch {
	case host == "" || strings.EqualFold(host, "localhost"):
		return true // the driver's default host is localhost
	case filepath.IsAbs(host) || strings.HasPrefix(host, "@"):
		return true // a Unix socket
	}
	ip, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil && ip.IsLoopback()
}
