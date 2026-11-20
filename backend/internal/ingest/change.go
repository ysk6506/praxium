package ingest

import (
	"reflect"
	"sort"
)

// What a change of a source definition touches decides who may save it (ADR-0100, source-editing.md 1.3). A change of the
// connection is a change of where the requests go, with what secret and how much they ask of the source; a change of the
// mapping leaves all of that as it was. This file is the one place that tells them apart. It is a pure function of the two
// definitions: nothing here reads a store or knows a person, and the store of the definitions asks it when it saves.

// ChangeKind is what a change of a definition touches.
type ChangeKind string

const (
	// ChangeMapping touches the datasets, the columns and the mapping only: the ability to edit definitions is enough.
	ChangeMapping ChangeKind = "mapping"
	// ChangeConnection touches where the source is reached, how it is signed in to, what is sent to it or how much is asked
	// of it: the ability to run sources is needed as well.
	ChangeConnection ChangeKind = "connection"
)

// Change is the answer of ClassifyChange: the kind, and what made it a change of the connection (empty for a mapping
// change), as short stable names in order.
type Change struct {
	Kind    ChangeKind
	Reasons []string
}

// ClassifyChange tells whether going from the definition before to the definition after is a change of the connection or
// of the mapping only. A source that is new is compared with an empty one (before is the zero Source, which has no id), and
// a new source is always a change of the connection.
//
// A change of the connection is one of: a source that is new; the adapter; any field of the connection (the address, the secret, the
// certificates, the headers, the authentication with all of its fields, the host, the port, the database, the mode of
// TLS); a limit that is raised (timeouts, requests per second, response size, wait of a retry, pages, the size of a page);
// a dataset that reads with POST (post_reads, and any change of its request) or that is new and POSTs; a request body that
// has a placeholder of a secret or of the token and is not the body it was. Everything else is a change of the mapping.
func ClassifyChange(before, after Source) Change {
	var reasons []string
	if before.ID == "" {
		reasons = append(reasons, "new_source")
	}
	if before.Adapter != after.Adapter {
		reasons = append(reasons, "adapter")
	}
	reasons = append(reasons, connectionReasons(before.Connection, after.Connection)...)
	old := map[string]Dataset{}
	for _, d := range before.Datasets {
		old[d.ID] = d
	}
	for _, d := range after.Datasets {
		reasons = append(reasons, datasetReasons(old[d.ID], d)...)
	}
	reasons = uniqueSorted(reasons)
	if len(reasons) == 0 {
		return Change{Kind: ChangeMapping}
	}
	return Change{Kind: ChangeConnection, Reasons: reasons}
}

// limit is a number of the connection that bounds what is asked of the source: a bigger one is more.
type limit struct {
	name     string
	def      int
	get      func(*Connection) int
	floatVal func(*Connection) float64
}

var connectionLimits = []limit{
	{name: "timeout_seconds", def: DefaultHTTPTimeoutSeconds, get: func(c *Connection) int { return c.TimeoutSeconds }},
	{name: "requests_per_second", floatVal: func(c *Connection) float64 {
		if c.RequestsPerSecond <= 0 {
			return DefaultRequestsPerSecond
		}
		return c.RequestsPerSecond
	}},
	{name: "max_response_bytes", def: DefaultMaxResponseBytes, get: func(c *Connection) int { return c.MaxResponseBytes }},
	{name: "max_retry_wait_seconds", def: DefaultMaxRetryWaitSeconds, get: func(c *Connection) int { return c.MaxRetryWaitSeconds }},
	{name: "connect_timeout_seconds", def: DefaultConnectSeconds, get: func(c *Connection) int { return c.ConnectTimeoutSeconds }},
	{name: "statement_timeout_seconds", def: DefaultStatementSeconds, get: func(c *Connection) int { return c.StatementTimeoutSeconds }},
	{name: "lock_timeout_seconds", def: DefaultLockSeconds, get: func(c *Connection) int { return c.LockTimeoutSeconds }},
}

func (l limit) value(c *Connection) float64 {
	if l.floatVal != nil {
		return l.floatVal(c)
	}
	if v := l.get(c); v > 0 {
		return float64(v)
	}
	return float64(l.def)
}

// connectionReasons names the parts of the connection that changed, and the limits that went up.
func connectionReasons(before, after *Connection) []string {
	if before == nil && after == nil {
		return nil
	}
	if before == nil || after == nil {
		return []string{"connection"}
	}
	var out []string
	differs := func(name string, a, b any) {
		if !reflect.DeepEqual(a, b) {
			out = append(out, "connection."+name)
		}
	}
	differs("base_url", before.BaseURL, after.BaseURL)
	differs("ca_pem", before.CAPEM, after.CAPEM)
	differs("headers", headersOf(before), headersOf(after))
	differs("auth", authOf(before), authOf(after))
	differs("host", before.Host, after.Host)
	differs("port", portOfPostgres(before), portOfPostgres(after))
	differs("database", before.Database, after.Database)
	differs("credential", before.Credential, after.Credential)
	differs("sslmode", sslOf(before), sslOf(after))
	for _, l := range connectionLimits {
		if l.value(after) > l.value(before) {
			out = append(out, "connection."+l.name)
		}
	}
	return out
}

func headersOf(c *Connection) map[string]string {
	if len(c.Headers) == 0 {
		return nil
	}
	return c.Headers
}

// authOf is the authentication a connection has, with none for the connection that gives no authentication.
func authOf(c *Connection) Auth {
	if c.Auth == nil {
		return Auth{Kind: AuthNone}
	}
	if c.Auth.Kind == "" {
		a := *c.Auth
		a.Kind = AuthNone
		return a
	}
	return *c.Auth
}

func portOfPostgres(c *Connection) int {
	if c.Host != "" && c.Port == 0 {
		return DefaultPostgresPort
	}
	return c.Port
}

func sslOf(c *Connection) string {
	if c.Host != "" && c.SSLMode == "" {
		return SSLVerifyFull
	}
	return c.SSLMode
}

// datasetReasons names what makes the change of one dataset a change of the connection. before is the zero Dataset for a
// dataset that is new.
func datasetReasons(before, after Dataset) []string {
	name := "datasets." + after.ID
	var out []string
	if after.PostReads && (!before.PostReads || !reflect.DeepEqual(before.Request, after.Request)) {
		out = append(out, name+".post_reads")
	}
	if after.Request != nil && after.Request.Method == MethodPost && !after.PostReads {
		out = append(out, name+".post_request")
	}
	if after.Request != nil && hasBodyPlaceholder(after.Request.Body) && !reflect.DeepEqual(requestBody(before), after.Request.Body) {
		out = append(out, name+".placeholder")
	}
	if paginationRaised(before.Pagination, after.Pagination) {
		out = append(out, name+".pagination")
	}
	return out
}

func requestBody(d Dataset) map[string]any {
	if d.Request == nil {
		return nil
	}
	return d.Request.Body
}

func hasBodyPlaceholder(body map[string]any) bool {
	if len(body) == 0 {
		return false
	}
	found := false
	WalkPlaceholders(body, func(Placeholder) { found = true })
	return found
}

// paginationRaised tells whether the pages a dataset reads are more than before: a higher number of pages (a dataset that is new
// is compared with the starting number), or a bigger page than the dataset had.
func paginationRaised(before, after *Pagination) bool {
	if after == nil {
		return false
	}
	pages := func(p *Pagination) int {
		if p == nil || p.MaxPages <= 0 {
			return DefaultMaxPages
		}
		return p.MaxPages
	}
	if pages(after) > pages(before) {
		return true
	}
	return before != nil && after.Size > before.Size
}

func uniqueSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := in[:0:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
