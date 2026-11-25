package ingest

import (
	"encoding/json"
	"reflect"
	"slices"
	"sort"
)

// What a change of a source definition touches decides who may save it (ADR-0100, source-editing.md 1.3). A change of the
// connection is a change of where the requests go, with what secret and how much they ask of the source; a change of the
// mapping leaves all of that as it was. This file is the one place that tells them apart. It is a pure function of the two
// definitions: nothing here reads a store or knows a person, and the store of the definitions asks it when it saves.
//
// It is built the safe way round: the fields that are known to be a change of the mapping are listed (changeClasses), and
// two definitions that differ in anything else are a change of the connection. A field that is added to a definition is
// then a change of the connection until someone decides it is not, and a test fails until it is in the list.

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

// The class of a field of a definition for ClassifyChange. A field that is not in changeClasses is a change of the connection.
const (
	classMapping = "mapping" // a change of it is a change of the mapping
	classLimit   = "limit"   // a change that lowers it is a change of the mapping, one that raises it is not
	classKept    = "kept"    // a change of it is a change of the connection
)

// changeClasses is the class of every field of the types a definition is made of, as "Type.Field". Each of the other
// fields of those types is classKept; they are written down so that a test can tell a field that was decided from one that
// was forgotten.
var changeClasses = map[string]string{
	"Source.Version": classKept, "Source.ID": classKept, "Source.Name": classMapping, "Source.Adapter": classKept, "Source.Collector": classKept,
	"Source.Connection": classKept, "Source.Disabled": classMapping, "Source.Datasets": classMapping, "Source.Limits": classLimit,
	// The webhooks have their own comparison (webhookReasons) and their own table (webhookClasses): a webhook is the place
	// a call is let in, so every change of one is a change of the connection but a limit that is lowered.
	"Source.Webhooks": classKept,

	"Connection.BaseURL": classKept, "Connection.CAPEM": classKept, "Connection.TLS": classKept, "Connection.TimeoutSeconds": classLimit, "Connection.RequestsPerSecond": classLimit,
	"Connection.Headers": classKept, "Connection.Auth": classKept, "Connection.MaxResponseBytes": classLimit, "Connection.MaxRetryWaitSeconds": classLimit,
	"Connection.Host": classKept, "Connection.Port": classKept, "Connection.Database": classKept, "Connection.Credential": classKept, "Connection.SSLMode": classKept,
	"Connection.ConnectTimeoutSeconds": classLimit, "Connection.StatementTimeoutSeconds": classLimit, "Connection.LockTimeoutSeconds": classLimit,

	"Auth.Kind": classKept, "Auth.Credential": classKept, "Auth.Header": classKept, "Auth.Prefix": classKept, "Auth.Request": classKept, "Auth.TokenPath": classKept,
	"Auth.TokenFrom": classKept, "Auth.ExpiresInPath": classKept, "Auth.Send": classKept, "Auth.SendHeader": classKept, "Auth.SendPrefix": classKept, "Auth.Headers": classKept,
	"TokenRequest.Path": classKept, "TokenRequest.URL": classKept, "TokenRequest.CAPEM": classKept, "TokenRequest.Headers": classKept, "TokenRequest.Format": classKept, "TokenRequest.CredentialsIn": classKept, "TokenRequest.Body": classKept,
	"SecretHeader.Name": classKept, "SecretHeader.From": classKept, "SecretHeader.Prefix": classKept,
	"SecretRef.SecretID": classKept,

	"Dataset.ID": classKept, "Dataset.Name": classMapping, "Dataset.Kind": classMapping, "Dataset.ReceiveMode": classMapping, "Dataset.Parser": classMapping,
	"Dataset.NaturalKey": classMapping, "Dataset.Deletion": classMapping, "Dataset.Columns": classMapping, "Dataset.Delimiter": classMapping,
	"Dataset.IgnoreUndeclaredColumns": classMapping, "Dataset.IgnoreRowsWithoutKey": classMapping, "Dataset.Sheet": classMapping,
	// The request, the pages and post_reads of a dataset that reads with GET are the mapping (where in the source the records
	// are); of a dataset that reads with POST they are kept (what is sent with the secret). Which it is, is decided in datasetForm.
	"Dataset.Request": classKept, "Dataset.RecordsPath": classMapping, "Dataset.Pagination": classKept, "Dataset.PostReads": classKept,
	"Dataset.Table": classMapping, "Dataset.SQL": classMapping, "Dataset.KeyCollation": classMapping,
	// How a response is read (json or json_lines), what a record with no array is, and how many elements one record may have (ADR-0109).
	// The first two say where the records are and how what is seen is told; the third is a limit, and a limit that is raised is not the mapping.
	"Dataset.ResponseFormat": classMapping, "Dataset.MissingArray": classMapping, "Dataset.MaxElements": classLimit,
	"Request.Method": classKept, "Request.Path": classKept, "Request.Query": classKept, "Request.Body": classKept,
	"Pagination.Kind": classKept, "Pagination.PageParam": classKept, "Pagination.OffsetParam": classKept, "Pagination.SizeParam": classKept, "Pagination.Size": classLimit,
	"Pagination.FirstPage": classKept, "Pagination.TotalPath": classKept, "Pagination.CursorParam": classKept, "Pagination.CursorIn": classKept,
	"Pagination.NextCursorPath": classKept, "Pagination.NextPath": classKept, "Pagination.Rel": classKept, "Pagination.MaxPages": classLimit,
	"Limits.MaxUncompressedBytes": classLimit, "Limits.MaxRowsPerSheet": classLimit, "Limits.MaxColumnsPerSheet": classLimit, "Limits.MaxCellChars": classLimit,
	"Limits.MaxSharedStrings": classLimit, "Limits.MaxRowsPerDataset": classLimit,
}

// limitCompared lists the fields of classLimit that a comparison of "is it raised" exists for (connectionLimits, limitReasons,
// paginationRaised). A field of classLimit that is not here has no such comparison, so it is read as classKept: any change
// of it is a change of the connection, until the comparison is written and the field is put here.
var limitCompared = map[string]bool{
	"Source.Limits":                      true,
	"Connection.TimeoutSeconds":          true,
	"Connection.RequestsPerSecond":       true,
	"Connection.MaxResponseBytes":        true,
	"Connection.MaxRetryWaitSeconds":     true,
	"Connection.ConnectTimeoutSeconds":   true,
	"Connection.StatementTimeoutSeconds": true,
	"Connection.LockTimeoutSeconds":      true,
	"Dataset.MaxElements":                true,
	"Pagination.Size":                    true,
	"Pagination.MaxPages":                true,
	"Limits.MaxUncompressedBytes":        true,
	"Limits.MaxRowsPerSheet":             true,
	"Limits.MaxColumnsPerSheet":          true,
	"Limits.MaxCellChars":                true,
	"Limits.MaxSharedStrings":            true,
	"Limits.MaxRowsPerDataset":           true,
}

// changeClassOf is the class of a field for the comparison: a field of classLimit that has no comparison of a raise is classKept.
func changeClassOf(key string) string {
	c := changeClasses[key]
	if c == classLimit && !limitCompared[key] {
		return classKept
	}
	return c
}

// ClassifyChange tells whether going from the definition before to the definition after is a change of the connection or
// of the mapping only. A source that is new is compared with an empty one (before is the zero Source, which has no id), and
// a new source is always a change of the connection. Both definitions are given their defaults first, so that a field
// that is written out and one that is left to its default are the same.
//
// A change of the mapping is a change of the names, the columns, the keys and the rules of deletion of the datasets, of the
// place of the records, of the request and the pages of a dataset that reads with GET, of what a PostgreSQL dataset reads, a
// limit that is lowered, and a source that is stopped. Any other difference is a change of the connection: among them a
// source that is new or has another id, the adapter, the collector, every field of the connection, a limit that is raised
// (a size of a page of a new dataset over pageSizeWithoutAsk included), the request and the pages of a dataset that reads
// with POST, a body that has a placeholder, and a source that is started again.
func ClassifyChange(before, after Source) Change {
	b, a := changeForm(before), changeForm(after)
	var reasons []string
	if before.ID == "" || b.ID != a.ID {
		reasons = append(reasons, "new_source")
	}
	if before.Adapter != after.Adapter {
		reasons = append(reasons, "adapter")
	}
	if b.Disabled && !a.Disabled {
		reasons = append(reasons, "enabled")
	}
	reasons = append(reasons, connectionReasons(b.Connection, a.Connection)...)
	reasons = append(reasons, limitReasons(b.Limits, a.Limits)...)
	reasons = append(reasons, webhookReasons(b.Webhooks, a.Webhooks)...)
	old := map[string]Dataset{}
	for _, d := range b.Datasets {
		old[d.ID] = d
	}
	for _, d := range a.Datasets {
		reasons = append(reasons, datasetReasons(old[d.ID], d)...)
	}
	if len(reasons) == 0 && !reflect.DeepEqual(sourceKept(b), sourceKept(a)) {
		reasons = append(reasons, "other")
	}
	reasons = uniqueSorted(reasons)
	if len(reasons) == 0 {
		return Change{Kind: ChangeMapping}
	}
	return Change{Kind: ChangeConnection, Reasons: reasons}
}

// pageSizeWithoutAsk is the size of a page that a dataset may have without the ability to run sources, when it is new: the
// sizes the examples of the definitions use. A bigger one asks more of the source than the usual page does.
const pageSizeWithoutAsk = 1000

// changeForm is a copy of a definition with its defaults filled in. It is read from the copy so that nothing of the argument
// is changed and no slice or map is shared with it. A definition that does not pass the checks is given as far as it goes.
func changeForm(s Source) Source {
	data, err := json.Marshal(s)
	if err != nil {
		return s
	}
	var c Source
	if json.Unmarshal(data, &c) != nil {
		return s
	}
	return c.normalize(newCollector(DocumentSource), allAdapters, true)
}

// clearClass sets to zero the fields of the struct that v points to which changeClasses gives one of the classes.
func clearClass(v any, typeName string, classes ...string) {
	rv := reflect.ValueOf(v).Elem()
	for i := 0; i < rv.NumField(); i++ {
		if slices.Contains(classes, changeClassOf(typeName+"."+rv.Type().Field(i).Name)) {
			rv.Field(i).SetZero()
		}
	}
}

// sourceKept is the definition with what is a change of the mapping, and every limit, taken out; the datasets are
// compared one by one, so they are out too.
func sourceKept(s Source) Source {
	if s.Connection != nil {
		c := *s.Connection
		clearClass(&c, "Connection", classMapping, classLimit)
		s.Connection = &c
	}
	s.Datasets, s.Webhooks = nil, nil
	clearClass(&s, "Source", classMapping, classLimit)
	return s
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
	b, a := *before, *after
	clearClass(&b, "Connection", classMapping, classLimit)
	clearClass(&a, "Connection", classMapping, classLimit)
	if !reflect.DeepEqual(b, a) {
		out = append(out, "connection")
	}
	differs := func(name string, x, y any) {
		if !reflect.DeepEqual(x, y) {
			out = append(out, "connection."+name)
		}
	}
	differs("base_url", before.BaseURL, after.BaseURL)
	differs("ca_pem", before.CAPEM, after.CAPEM)
	differs("tls", before.TLS, after.TLS)
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

// limitReasons names the limits of the readers of files that went up.
func limitReasons(before, after *Limits) []string {
	var b, a Limits
	if before != nil {
		b = *before
	}
	if after != nil {
		a = *after
	}
	b, a = b.WithDefaults(), a.WithDefaults()
	var out []string
	rb, ra := reflect.ValueOf(b), reflect.ValueOf(a)
	for i := 0; i < ra.NumField(); i++ {
		if ra.Field(i).Int() > rb.Field(i).Int() {
			out = append(out, "limits."+ra.Type().Field(i).Name)
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

// readsWithPost tells whether a dataset sends a body with the secret: it says post_reads or its method is POST.
func readsWithPost(d Dataset) bool {
	return d.PostReads || (d.Request != nil && d.Request.Method == MethodPost)
}

// datasetForm is a dataset with what is the mapping taken out. The request, the pages and post_reads of a dataset that
// reads with GET are the mapping too; those of a dataset that reads with POST stay, with the limits of the pages out.
func datasetForm(d Dataset, post bool) Dataset {
	clearClass(&d, "Dataset", classMapping, classLimit)
	if !post {
		d.Request, d.Pagination, d.PostReads = nil, nil, false
		return d
	}
	if d.Pagination != nil {
		p := *d.Pagination
		clearClass(&p, "Pagination", classLimit)
		d.Pagination = &p
	}
	return d
}

// datasetReasons names what makes the change of one dataset a change of the connection. before is the zero Dataset for a
// dataset that is new.
func datasetReasons(before, after Dataset) []string {
	name := "datasets." + after.ID
	existed := before.ID != ""
	if !existed {
		before.ID = after.ID
	}
	out := postReasons(name, before, after)
	if paginationRaised(before.Pagination, after.Pagination, existed) {
		out = append(out, name+".pagination")
	}
	if after.ElementLimit() > before.ElementLimit() {
		out = append(out, name+".max_elements")
	}
	post := readsWithPost(after)
	if len(out) == 0 && !reflect.DeepEqual(datasetForm(before, post), datasetForm(after, post)) {
		out = append(out, name+".other")
	}
	return out
}

// postReasons names what makes a dataset that reads with POST, or has a body with a placeholder, a change of the connection.
func postReasons(name string, before, after Dataset) []string {
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

// paginationRaised tells whether the pages a dataset reads are more than before: a higher number of pages, or a bigger page
// (a page that has no size is read at the size the source chooses, which is bigger than any that is written). A dataset that is
// new has no size to be compared with, so a size over pageSizeWithoutAsk is a raise.
func paginationRaised(before, after *Pagination, existed bool) bool {
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
	if !existed || before == nil {
		return after.Size > pageSizeWithoutAsk
	}
	return after.Size > before.Size || (before.Size > 0 && after.Size == 0)
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
