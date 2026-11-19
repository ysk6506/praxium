package ingest

// The form of the definition of an HTTP source and of a PostgreSQL source (ADR-0090, sources-and-adapters.md 2). These
// are data and the check of them: nothing here opens a connection or knows the network. An adapter is given the values.

// The adapters a source definition names.
const (
	AdapterFile     = "file"
	AdapterHTTP     = "http"
	AdapterPostgres = "postgres"
)

// The kinds of the authentication of an HTTP source.
const (
	AuthNone         = "none"
	AuthBearer       = "bearer"
	AuthBasic        = "basic"
	AuthHeader       = "header"
	AuthTokenRequest = "token_request"
)

// The kinds of the pagination of an HTTP dataset.
const (
	PageNone       = "none"
	PagePage       = "page"
	PageOffset     = "offset"
	PageCursor     = "cursor"
	PageNextURL    = "next_url"
	PageLinkHeader = "link_header"
)

// The methods of an HTTP request and the places a cursor may be sent.
const (
	MethodGet  = "GET"
	MethodPost = "POST"
	InQuery    = "query"
	InBody     = "body"
)

// The modes of the TLS of a PostgreSQL connection. A definition has no way to lower the check of the certificate; whether
// disable is allowed is for the operator's setting.
const (
	SSLVerifyFull = "verify-full"
	SSLDisable    = "disable"
)

// The values that stand where a definition does not give one (ADR-0090). They are starting values to be measured, and
// every one of them can be set in the definition.
const (
	DefaultHTTPTimeoutSeconds  = 30
	DefaultRequestsPerSecond   = 5.0
	DefaultMaxResponseBytes    = 16 << 20
	DefaultMaxRetryWaitSeconds = 60
	DefaultMaxPages            = 1000
	DefaultPostgresPort        = 5432
	DefaultConnectSeconds      = 10
	DefaultStatementSeconds    = 60
	DefaultLockSeconds         = 5
)

// MaxResponseBytesLimit is the most a definition may set max_response_bytes to (ADR-0097). The adapter reads a response as
// a stream, so the memory of a read does not depend on it; the limit bounds how long one response holds a connection and
// how much a source may send.
const MaxResponseBytesLimit = 256 << 20

// SecretRef names a secret of the store; the definition holds the name and never the value. It is read where the
// adapter builds its request, and only there.
type SecretRef struct {
	SecretID string `json:"secret_id"`
}

// Connection is where and how a source is reached. The fields of the HTTP adapter and those of the PostgreSQL adapter
// are in one type so that a field of the other adapter is told as that; a file source has none.
type Connection struct {
	// HTTP.
	BaseURL string `json:"base_url,omitempty"`
	// CAPEM is the certificate or certificates that sign the server's: the only ones trusted when it is given, in place
	// of the system's. HTTP and PostgreSQL both have it.
	CAPEM string `json:"ca_pem,omitempty"`
	// TimeoutSeconds is how long one request may take.
	TimeoutSeconds    int               `json:"timeout_seconds,omitempty"`
	RequestsPerSecond float64           `json:"requests_per_second,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	Auth              *Auth             `json:"auth,omitempty"`
	// MaxResponseBytes is the most one response may have after it is unpacked.
	MaxResponseBytes int `json:"max_response_bytes,omitempty"`
	// MaxRetryWaitSeconds is the longest the reader waits for one retry, whatever Retry-After says.
	MaxRetryWaitSeconds int `json:"max_retry_wait_seconds,omitempty"`

	// PostgreSQL.
	Host                    string     `json:"host,omitempty"`
	Port                    int        `json:"port,omitempty"`
	Database                string     `json:"database,omitempty"`
	Credential              *SecretRef `json:"credential,omitempty"`
	SSLMode                 string     `json:"sslmode,omitempty"`
	ConnectTimeoutSeconds   int        `json:"connect_timeout_seconds,omitempty"`
	StatementTimeoutSeconds int        `json:"statement_timeout_seconds,omitempty"`
	LockTimeoutSeconds      int        `json:"lock_timeout_seconds,omitempty"`
}

// Auth is how an HTTP source is signed in to. Kind says which of the other fields are read: bearer and header send a
// token secret, basic sends a password secret, token_request asks for a token with a password secret first.
type Auth struct {
	Kind       string     `json:"kind"`
	Credential *SecretRef `json:"credential,omitempty"`
	// Header and Prefix are for the kind header: the name of the header and the text before the token.
	Header string `json:"header,omitempty"`
	Prefix string `json:"prefix,omitempty"`
	// Request, TokenPath, SendHeader and SendPrefix are for the kind token_request.
	Request    *TokenRequest `json:"request,omitempty"`
	TokenPath  string        `json:"token_path,omitempty"`
	SendHeader string        `json:"send_header,omitempty"`
	// SendPrefix is the text before the token in the header it is sent in; it is "Bearer " when it is not given and
	// may be given as the empty text.
	SendPrefix *string `json:"send_prefix,omitempty"`
}

// TokenRequest is the request that gives a token: a POST to a path of the same host, in the format json or form. A
// value of the body that is exactly {{credential.<field>}} is replaced by that field of the secret, as a value and never
// as a part of a text.
type TokenRequest struct {
	Path   string            `json:"path"`
	Format string            `json:"format"`
	Body   map[string]string `json:"body,omitempty"`
}

// Request is the request an HTTP dataset reads with: the method (GET unless the dataset says it reads with POST), the
// path under the base address, the query, and for a POST the body.
type Request struct {
	Method string            `json:"method,omitempty"`
	Path   string            `json:"path"`
	Query  map[string]string `json:"query,omitempty"`
	Body   map[string]any    `json:"body,omitempty"`
}

// Pagination is how an HTTP dataset is read page after page. Kind says which of the other fields are read:
//
//	none        one response
//	page        page_param, size_param, size, first_page (0 or 1), total_path (optional)
//	offset      offset_param, size_param, size, total_path (optional)
//	cursor      cursor_param, cursor_in (query or body), next_cursor_path, size_param and size (optional)
//	next_url    next_path, size_param and size (optional)
//	link_header rel (default next), size_param and size (optional)
type Pagination struct {
	Kind           string `json:"kind"`
	PageParam      string `json:"page_param,omitempty"`
	OffsetParam    string `json:"offset_param,omitempty"`
	SizeParam      string `json:"size_param,omitempty"`
	Size           int    `json:"size,omitempty"`
	FirstPage      *int   `json:"first_page,omitempty"`
	TotalPath      string `json:"total_path,omitempty"`
	CursorParam    string `json:"cursor_param,omitempty"`
	CursorIn       string `json:"cursor_in,omitempty"`
	NextCursorPath string `json:"next_cursor_path,omitempty"`
	NextPath       string `json:"next_path,omitempty"`
	Rel            string `json:"rel,omitempty"`
	// MaxPages is the most requests one read of the dataset makes; the read that goes over it is not complete.
	MaxPages int `json:"max_pages,omitempty"`
}
