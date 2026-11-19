package httpsource

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// settings are the values of a source's connection with the defaults filled in: what a read needs to know about where
// it goes and how fast and how big it may be. A definition that came from ParseAdapterSource has them all; a zero value
// takes the starting value of ADR-0090, so that a Source built by hand reads the same.
type settings struct {
	id       string
	scheme   string
	host     string
	port     int
	basePath string
	timeout  time.Duration
	interval time.Duration
	maxBytes int
	maxWait  time.Duration
	headers  map[string]string
	caPEM    string
	auth     ingest.Auth
}

func readSettings(def ingest.Source) (settings, error) {
	if def.Adapter != ingest.AdapterHTTP || def.Connection == nil {
		return settings{}, kernel.Errorf(kernel.CodeInvalidValue, "source %s is not an http source with a connection", def.ID)
	}
	cn := def.Connection
	u, err := url.Parse(cn.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return settings{}, kernel.Errorf(kernel.CodeInvalidValue, "source %s: base_url is not an http or https address", def.ID)
	}
	host, _, bad := egress.NormalizeHost(u.Hostname())
	if bad != "" {
		return settings{}, kernel.Errorf(kernel.CodeInvalidValue, "source %s: base_url has a host that is not allowed", def.ID)
	}
	s := settings{
		id: def.ID, scheme: u.Scheme, host: host, port: portOf(u), basePath: strings.TrimRight(u.Path, "/"),
		timeout:  secondsOr(cn.TimeoutSeconds, ingest.DefaultHTTPTimeoutSeconds),
		maxBytes: intOr(cn.MaxResponseBytes, ingest.DefaultMaxResponseBytes),
		maxWait:  secondsOr(cn.MaxRetryWaitSeconds, ingest.DefaultMaxRetryWaitSeconds),
		headers:  cn.Headers, caPEM: cn.CAPEM, auth: ingest.Auth{Kind: ingest.AuthNone},
	}
	rps := cn.RequestsPerSecond
	if rps <= 0 {
		rps = ingest.DefaultRequestsPerSecond
	}
	s.interval = time.Duration(float64(time.Second) / rps)
	if cn.Auth != nil {
		s.auth = *cn.Auth
	}
	return s, nil
}

func portOf(u *url.URL) int {
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			return n
		}
	}
	if u.Scheme == "https" {
		return 443
	}
	return 80
}

func secondsOr(n, def int) time.Duration { return time.Duration(intOr(n, def)) * time.Second }

func intOr(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}

// origin is the address requests go to: the scheme, the host and the port, and the path of the base address. The host
// is the one the secret was opened for when there is one (ADR-0071 decision 5: what was checked is what is connected to).
func (s settings) origin(host string, port int) *url.URL {
	h := host
	if strings.Contains(h, ":") {
		h = "[" + h + "]"
	}
	if !(s.scheme == "https" && port == 443) && !(s.scheme == "http" && port == 80) {
		h += ":" + strconv.Itoa(port)
	}
	return &url.URL{Scheme: s.scheme, Host: h, Path: s.basePath}
}

func (s settings) needsSecret() bool {
	return s.auth.Kind != ingest.AuthNone && s.auth.Kind != ""
}
