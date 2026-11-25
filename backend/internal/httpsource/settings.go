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
	pins     []string
	auth     ingest.Auth
	token    tokenSettings
}

// tokenSettings are where the token is asked for when the definition says it with a whole address (auth.request.url) and
// what is the token request's own. The origin of that address is the token host's; it is another one than the API's when
// cross is set, and the credentials then go to it and the token to the API (ADR-0106).
type tokenSettings struct {
	// url says that the address is given as a whole; path is its path, and scheme, host and port its origin.
	url     bool
	scheme  string
	host    string
	port    int
	path    string
	rawPath string
	// cross says that the origin is not the one of base_url.
	cross bool
	// caPEM is the certificate authority of the token host, for the cross origin only; headers are the headers of the token
	// request.
	caPEM   string
	headers map[string]string
}

func readSettings(def ingest.Source) (settings, error) {
	if def.Adapter != ingest.AdapterHTTP || def.Connection == nil {
		return settings{}, kernel.Errorf(kernel.CodeInvalidValue, "source %s is not an http source with a connection", def.ID)
	}
	cn := def.Connection
	u, host, port, err := addressOf(def.ID, "base_url", cn.BaseURL)
	if err != nil {
		return settings{}, err
	}
	s := settings{
		id: def.ID, scheme: u.Scheme, host: host, port: port, basePath: strings.TrimRight(u.Path, "/"),
		timeout:  secondsOr(cn.TimeoutSeconds, ingest.DefaultHTTPTimeoutSeconds),
		maxBytes: intOr(cn.MaxResponseBytes, ingest.DefaultMaxResponseBytes),
		maxWait:  secondsOr(cn.MaxRetryWaitSeconds, ingest.DefaultMaxRetryWaitSeconds),
		headers:  cn.Headers, caPEM: cn.CAPEM, auth: ingest.Auth{Kind: ingest.AuthNone},
	}
	if cn.TLS != nil {
		s.pins = cn.TLS.Pins
	}
	rps := cn.RequestsPerSecond
	if rps <= 0 {
		rps = ingest.DefaultRequestsPerSecond
	}
	s.interval = time.Duration(float64(time.Second) / rps)
	if cn.Auth != nil {
		s.auth = *cn.Auth
	}
	if err := s.readToken(); err != nil {
		return settings{}, err
	}
	return s, nil
}

// readToken reads where the token is asked for: the headers of the token request, and the address when it is a whole one. The
// host is normalized the way the host of base_url is, so that two spellings of one host are one origin (and are told apart
// from another host).
func (s *settings) readToken() error {
	tr := s.auth.Request
	if s.auth.Kind != ingest.AuthTokenRequest || tr == nil {
		return nil
	}
	s.token.headers = tr.Headers
	if tr.URL == "" {
		return nil
	}
	t, err := s.tokenAddress(tr)
	if err != nil {
		return err
	}
	s.token = t
	return nil
}

// tokenAddress reads the whole address of the token request.
func (s *settings) tokenAddress(tr *ingest.TokenRequest) (tokenSettings, error) {
	u, host, port, err := addressOf(s.id, "auth.request.url", tr.URL)
	if err != nil {
		return tokenSettings{}, err
	}
	t := tokenSettings{url: true, scheme: u.Scheme, host: host, port: port, path: u.Path, rawPath: u.RawPath, headers: tr.Headers}
	t.cross = t.scheme != s.scheme || t.host != s.host || t.port != s.port
	if t.cross {
		t.caPEM = tr.CAPEM
	}
	return t, nil
}

// addressOf reads an address of the definition (base_url or auth.request.url) the way a request is made to it: http or https,
// a host that is allowed in the form the client has it, and a port from 1 to 65535. The host and the port are what the
// secret is opened for.
func addressOf(id, field, raw string) (*url.URL, string, int, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, "", 0, kernel.Errorf(kernel.CodeInvalidValue, "source %s: %s is not an http or https address", id, field)
	}
	host, _, bad := egress.NormalizeHost(u.Hostname())
	if bad != "" {
		return nil, "", 0, kernel.Errorf(kernel.CodeInvalidValue, "source %s: %s has a host that is not allowed", id, field)
	}
	port, ok := portOf(u)
	if !ok {
		return nil, "", 0, kernel.Errorf(kernel.CodeInvalidValue, "source %s: %s has a port that is not from 1 to 65535", id, field)
	}
	return u, host, port, nil
}

// portOf is the port of an address, the one of its scheme when none is written; it is not ok when the port written is not from
// 1 to 65535.
func portOf(u *url.URL) (int, bool) {
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		return n, err == nil && n >= 1 && n <= 65535
	}
	if u.Scheme == "https" {
		return 443, true
	}
	return 80, true
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
	return originAt(s.scheme, host, port, s.basePath)
}

// tokenOrigin is the address the token is asked for at when it is another origin than the API's: the host the secret was
// opened for at the port it was opened for, and the path of auth.request.url.
func (s settings) tokenOrigin(host string, port int) *url.URL {
	u := originAt(s.token.scheme, host, port, s.token.path)
	u.RawPath = s.token.rawPath
	return u
}

func originAt(scheme, host string, port int, path string) *url.URL {
	h := host
	if strings.Contains(h, ":") {
		h = "[" + h + "]"
	}
	if !(scheme == "https" && port == 443) && !(scheme == "http" && port == 80) {
		h += ":" + strconv.Itoa(port)
	}
	return &url.URL{Scheme: scheme, Host: h, Path: path}
}

func (s settings) needsSecret() bool {
	return s.auth.Kind != ingest.AuthNone && s.auth.Kind != ""
}

// notes are what a check says about the source besides how it went: that nothing is encrypted, and that a pin takes a
// certificate whatever its dates are.
func (s settings) notes() []ingest.CheckNote {
	var out []ingest.CheckNote
	if s.scheme == "http" {
		out = append(out, ingest.CheckNote{Code: ingest.NotePlaintext, Message: "the credentials and the data of this source cross the network without encryption (http)"})
	} else if s.token.url && s.token.scheme == "http" {
		// The API is encrypted, but the request that asks for the token carries the secret of the source (ADR-0105).
		out = append(out, ingest.CheckNote{Code: ingest.NotePlaintext, Message: "the secret of this source crosses the network without encryption in the request for the token (http)"})
	}
	if len(s.pins) > 0 {
		out = append(out, ingest.CheckNote{Code: ingest.NotePinExpiry, Message: "the certificate of the server is accepted by its public key alone: its dates, its name and its chain are not looked at, so an expired certificate is accepted"})
	}
	return out
}
