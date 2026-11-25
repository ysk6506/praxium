package ingest

import (
	"crypto/x509"
	"encoding/pem"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// normalizeConnection checks the connection of the source for its adapter and fills the defaults. The connection it gives
// back is a copy: the one the document was read into is not changed.
func (s Source) normalizeConnection(c *collector) Source {
	switch s.Adapter {
	case AdapterFile:
		if s.Connection != nil {
			c.add(ecAdConnNotFile, ptr("connection"), Params{"source": s.ID, "adapter": s.Adapter},
				"source %s: a file source has no connection (the files are given to the run)", s.ID)
		}
	case AdapterHTTP, AdapterPostgres:
		if s.Connection == nil {
			c.add(ecAdConnMissing, ptr("connection"), Params{"source": s.ID, "adapter": s.Adapter}, "source %s: an %s source needs a connection", s.ID, s.Adapter)
			return s
		}
		cn := *s.Connection
		cn.checkFields(c, s)
		if s.Adapter == AdapterHTTP {
			cn.normalizeHTTP(c, s.ID)
		} else {
			cn.normalizePostgres(c, s.ID)
		}
		s.Connection = &cn
	}
	return s
}

type namedSet struct {
	name string
	set  bool
}

func (cn Connection) httpFields() []namedSet {
	return []namedSet{{"base_url", cn.BaseURL != ""}, {"timeout_seconds", cn.TimeoutSeconds != 0}, {"requests_per_second", cn.RequestsPerSecond != 0},
		{"headers", cn.Headers != nil}, {"auth", cn.Auth != nil},
		{"max_response_bytes", cn.MaxResponseBytes != 0}, {"max_retry_wait_seconds", cn.MaxRetryWaitSeconds != 0}}
}

func (cn Connection) postgresFields() []namedSet {
	return []namedSet{{"host", cn.Host != ""}, {"port", cn.Port != 0}, {"database", cn.Database != ""}, {"credential", cn.Credential != nil},
		{"sslmode", cn.SSLMode != ""}, {"connect_timeout_seconds", cn.ConnectTimeoutSeconds != 0},
		{"statement_timeout_seconds", cn.StatementTimeoutSeconds != 0}, {"lock_timeout_seconds", cn.LockTimeoutSeconds != 0}}
}

// checkFields tells each field that is the other adapter's.
func (cn Connection) checkFields(c *collector, s Source) {
	other := cn.postgresFields()
	if s.Adapter == AdapterPostgres {
		other = cn.httpFields()
	}
	for _, f := range other {
		if f.set {
			c.add(ecAdConnField, ptr("connection", f.name), Params{"source": s.ID, "adapter": s.Adapter, "field": f.name},
				"source %s: connection.%s is not a field of the %s adapter", s.ID, f.name, s.Adapter)
		}
	}
}

// require records that a field the connection needs is not given.
func (c *collector) require(source, adapter, field string, given bool) bool {
	if !given {
		c.add(ecAdConnRequired, ptr("connection", field), Params{"source": source, "adapter": adapter, "field": field},
			"source %s: the %s adapter needs connection.%s", source, adapter, field)
	}
	return given
}

func (cn *Connection) normalizeHTTP(c *collector, id string) {
	if c.require(id, AdapterHTTP, "base_url", cn.BaseURL != "") {
		if reason := baseURLProblem(cn.BaseURL); reason != "" {
			c.add(ecAdBaseURL, ptr("connection", "base_url"), Params{"source": id, "reason": reason}, "source %s: base_url %s", id, reason)
		}
	}
	cn.checkCA(c, id)
	cn.checkHeaders(c, id)
	c.intOr(id, "timeout_seconds", &cn.TimeoutSeconds, 1, 3600, DefaultHTTPTimeoutSeconds)
	cn.RequestsPerSecond = defaultedF(c.checkRange(ecAdRange, Params{"source": id}, "requests_per_second", ptr("connection", "requests_per_second"), cn.RequestsPerSecond, numBounds{0.01, 1000}), cn.RequestsPerSecond, DefaultRequestsPerSecond)
	c.intOr(id, "max_response_bytes", &cn.MaxResponseBytes, 1<<10, MaxResponseBytesLimit, DefaultMaxResponseBytes)
	c.intOr(id, "max_retry_wait_seconds", &cn.MaxRetryWaitSeconds, 1, 3600, DefaultMaxRetryWaitSeconds)
	a := Auth{Kind: AuthNone}
	if cn.Auth != nil {
		a = *cn.Auth
	}
	cn.Auth = a.normalize(c, id)
}

// intOr checks a whole number of the connection that is optional and gives it its default when it was not given or was
// refused (a refused one has its error).
func (c *collector) intOr(id, field string, v *int, lo, hi, def int) {
	if !c.checkRange(ecAdRange, Params{"source": id}, field, ptr("connection", field), float64(*v), numBounds{float64(lo), float64(hi)}) || *v == 0 {
		*v = def
	}
}

func defaultedF(ok bool, v, def float64) float64 {
	if !ok || v == 0 {
		return def
	}
	return v
}

// baseURLProblem says why a text is no base address, or "". It ends a sentence that starts with the field.
func baseURLProblem(raw string) string {
	if hasControl(raw) {
		return "has a space or a control character"
	}
	if strings.ContainsAny(raw, "{}") {
		return "has a { or a }"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" {
		return "is not an address"
	}
	return parsedURLProblem(u)
}

func parsedURLProblem(u *url.URL) string {
	switch {
	case u.Scheme != "http" && u.Scheme != "https":
		return "has to begin with http:// or https://"
	case u.Hostname() == "":
		return "has no host"
	case u.User != nil:
		return "has a user name or a password in it; credentials come from a secret"
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return "has a query or a fragment; the base address ends in a path, and the query is the request's"
	case !portOK(u.Port()):
		return "has a port that is not from 1 to 65535"
	case u.Path != "" && !pathOK(u.Path):
		return "has a path with //, a backslash, or a . or .. step"
	}
	return ""
}

func portOK(p string) bool {
	if p == "" {
		return true
	}
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535
}

// pathOK tells whether a path is plain: no empty step, no backslash and no step that goes up or stays.
func pathOK(p string) bool {
	if strings.Contains(p, `\`) || strings.Contains(p, "//") {
		return false
	}
	for _, step := range strings.Split(p, "/") {
		if step == "." || step == ".." {
			return false
		}
	}
	return true
}

// checkCA checks the certificates the definition holds; the key of a certificate is no part of one.
func (cn Connection) checkCA(c *collector, id string) {
	if cn.CAPEM == "" {
		return
	}
	if reason := caProblem(cn.CAPEM); reason != "" {
		c.add(ecAdCAPEM, ptr("connection", "ca_pem"), Params{"source": id, "reason": reason}, "source %s: ca_pem %s", id, reason)
	}
}

func caProblem(text string) string {
	rest, n := []byte(text), 0
	for {
		block, more := pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			return "has a " + clip(block.Type, idClipAt) + " in it; only certificates belong in a definition, and a private key never does"
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return "has a certificate that cannot be read"
		}
		n++
		rest = more
	}
	if n == 0 {
		return "has no certificate in PEM form"
	}
	if strings.TrimSpace(string(rest)) != "" {
		return "has text after the last certificate"
	}
	return ""
}

func (cn Connection) checkHeaders(c *collector, id string) {
	checkHeaderTexts(c, id, []any{"connection", "headers"}, cn.Headers)
}

func (cn *Connection) normalizePostgres(c *collector, id string) {
	if c.require(id, AdapterPostgres, "host", cn.Host != "") {
		if reason := hostProblem(cn.Host); reason != "" {
			c.add(ecAdHost, ptr("connection", "host"), Params{"source": id, "reason": reason}, "source %s: host %s", id, reason)
		}
	}
	if cn.Port != 0 && (cn.Port < 1 || cn.Port > 65535) {
		c.add(ecAdPort, ptr("connection", "port"), Params{"source": id, "port": cn.Port}, "source %s: port %d is not from 1 to 65535", id, cn.Port)
	} else if cn.Port == 0 {
		cn.Port = DefaultPostgresPort
	}
	if c.require(id, AdapterPostgres, "database", cn.Database != "") && (len(cn.Database) > 63 || strings.ContainsFunc(cn.Database, func(r rune) bool { return r < ' ' })) {
		c.add(ecAdConnRequired, ptr("connection", "database"), Params{"source": id, "adapter": AdapterPostgres, "field": "database"},
			"source %s: connection.database is not a database name (at most 63 bytes, no control character)", id)
	}
	cn.checkPostgresCredential(c, id)
	cn.checkSSL(c, id)
	cn.checkCA(c, id)
	c.intOr(id, "connect_timeout_seconds", &cn.ConnectTimeoutSeconds, 1, 300, DefaultConnectSeconds)
	c.intOr(id, "statement_timeout_seconds", &cn.StatementTimeoutSeconds, 1, 3600, DefaultStatementSeconds)
	c.intOr(id, "lock_timeout_seconds", &cn.LockTimeoutSeconds, 1, 300, DefaultLockSeconds)
}

// checkPostgresCredential: the account and the password come from a secret, so a connection with none is refused.
func (cn Connection) checkPostgresCredential(c *collector, id string) {
	if cn.Credential == nil || cn.Credential.SecretID == "" {
		c.add(ecAdConnRequired, ptr("connection", "credential"), Params{"source": id, "adapter": AdapterPostgres, "field": "credential"},
			"source %s: the postgres adapter needs connection.credential, the secret of the account and its password (a connection with no password is not read)", id)
		return
	}
	c.checkID(ecAdSecretID, ptr("connection", "credential", "secret_id"), "secret", cn.Credential.SecretID, Params{"source": id, "field": "connection.credential.secret_id"})
}

func (cn *Connection) checkSSL(c *collector, id string) {
	supported := []string{SSLVerifyFull, SSLDisable}
	switch {
	case cn.SSLMode == "":
		cn.SSLMode = SSLVerifyFull
	case !containsString(supported, cn.SSLMode):
		c.add(ecAdSSLMode, ptr("connection", "sslmode"), Params{"source": id, "sslmode": clip(cn.SSLMode, idClipAt), "supported": supported},
			"source %s: sslmode %q is none of verify-full, disable", id, clip(cn.SSLMode, idClipAt))
	case cn.SSLMode == SSLDisable && cn.CAPEM != "":
		c.add(ecAdCAPEM, ptr("connection", "ca_pem"), Params{"source": id, "reason": "is given with sslmode disable, which uses no TLS"}, "source %s: ca_pem is given with sslmode disable, which uses no TLS", id)
	}
}

// hostProblem says why a text is not one host name or IP address, or "".
func hostProblem(h string) string {
	switch {
	case len(h) > 253:
		return "is longer than 253 bytes"
	case strings.ContainsAny(h, ",/@\\?#=[]'\"{}") || hasControl(h):
		return "has a character a host does not have (a comma, a slash, @ and the like); give one name or one IP address"
	case strings.Contains(h, ":"):
		if _, err := netip.ParseAddr(h); err != nil {
			return "has a colon and is no IPv6 address; the port is its own field"
		}
	}
	return ""
}
