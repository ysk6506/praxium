package mcpapi

import (
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/kgread"
)

// Path is the one address of the interface.
const Path = "/mcp"

// wellKnownPrefix is the address under which a client looks for the metadata of authorization. The server publishes
// none while the token is its own (ADR-0062 5), and so answers a JSON 404 there and not the page of the web UI.
const wellKnownPrefix = "/.well-known/"

// DefaultMaxBodyBytes is the limit of a request body when the configuration gives none.
const DefaultMaxBodyBytes int64 = 1 << 20

// Config is everything the handler needs from outside; the composition root builds it.
type Config struct {
	// Enabled says whether the interface is on. Off, /mcp and /.well-known/ answer a JSON 404 (ADR-0062 3).
	Enabled bool
	// Gate turns a token into a subject: the same code as the REST server's (kgread.Gate).
	Gate kgread.Gate
	// ServiceAccountsOnly makes the interface refuse a token whose subject is not a service account: true in production
	// mode. In the other modes the token of a run (a person with the viewer role) is taken.
	ServiceAccountsOnly bool
	// AllowedOrigins are the origins a browser may call from; empty (the default) means no browser origin.
	AllowedOrigins []string
	// LoopbackBound says the server listens on a loopback address, so only a loopback Host is taken (DNS rebinding).
	LoopbackBound bool
	// TrustProxy says a proxy in front appends the caller's address to X-Forwarded-For, as for the REST server.
	TrustProxy bool
	// MaxBodyBytes limits a request body; zero means DefaultMaxBodyBytes.
	MaxBodyBytes int64
	// Info and Directory are for the tools; Tools are further tools of the registry (only the tests add any).
	Info      Info
	Directory access.Directory
	Tools     []Tool
	Clock     kernel.Clock
	IDs       kernel.IDGenerator
	Log       *slog.Logger
}

// Handler is the http.Handler of /mcp and of /.well-known/. It is safe for concurrent use. It keeps no session.
type Handler struct {
	cfg     Config
	reg     *Registry
	counts  *counters
	origins map[string]bool
	max     int64
	nextRPC atomic.Int64
}

// New builds the handler.
func New(cfg Config) (*Handler, error) {
	if cfg.Clock == nil || cfg.IDs == nil || cfg.Log == nil || cfg.Gate.Authz == nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the MCP handler needs a clock, an id generator, a logger and a gate with an authorizer")
	}
	h := &Handler{cfg: cfg, counts: newCounters(), origins: map[string]bool{}, max: cfg.MaxBodyBytes}
	if h.max <= 0 {
		h.max = DefaultMaxBodyBytes
	}
	for _, o := range cfg.AllowedOrigins {
		h.origins[strings.TrimSpace(o)] = true
	}
	h.reg = DefaultRegistry(ToolDeps{Info: cfg.Info, Directory: cfg.Directory, Counts: h.counts.snapshot})
	for _, t := range cfg.Tools {
		h.reg.Add(t)
	}
	return h, nil
}

// Match says whether the handler owns an address: /mcp and what is under it, and /.well-known/. The server gives these
// to the handler before the web UI sees them.
func (h *Handler) Match(path string) bool {
	return path == Path || strings.HasPrefix(path, Path+"/") || path == strings.TrimSuffix(wellKnownPrefix, "/") || strings.HasPrefix(path, wellKnownPrefix)
}

// Counts gives the numbers of requests counted since the start (see counters).
func (h *Handler) Counts() map[string]int64 { return h.counts.snapshot() }

// ServeHTTP checks a request in the order of kg-mcp.md 3: the address, the method, Origin and Host, the token, then the
// media type and the version, and last the body.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	trace := w.Header().Get("X-Trace-Id")
	if trace == "" {
		trace = h.cfg.IDs.Next("tr")
	}
	a := answer{w: w, trace: trace}
	if r.URL.Path != Path || !h.cfg.Enabled {
		a.problem("not_found.resource", map[string]any{"resource": "route"})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		a.problem("invalid_input.method_not_allowed", map[string]any{"method": cleanShort(r.Method), "allowed": []string{http.MethodPost}})
		return
	}
	if !h.originOK(a, r) {
		return
	}
	subject, ok := h.authenticate(a, r)
	if !ok {
		return
	}
	h.serveMessage(a, r, subject)
}

// serveMessage runs the steps after the token: the media type, what the caller accepts, the body, the version.
func (h *Handler) serveMessage(a answer, r *http.Request, subject access.Subject) {
	if !isJSONMedia(r.Header.Get("Content-Type")) {
		a.problem("invalid_input.unsupported_media_type", map[string]any{"content_type": cleanShort(r.Header.Get("Content-Type"))})
		return
	}
	if !acceptsJSON(r.Header.Get("Accept")) {
		a.problem("invalid_input.not_acceptable", map[string]any{"accept": cleanShort(r.Header.Get("Accept"))})
		return
	}
	body, ok := h.readBody(a, r)
	if !ok {
		return
	}
	req, rej := h.parseMessage(body)
	if rej != nil {
		a.rpcError(http.StatusBadRequest, nil, *rej)
		return
	}
	h.route(a, r, req, subject)
}

// readBody reads the body up to the limit; one over it is answered 413.
func (h *Handler) readBody(a answer, r *http.Request) ([]byte, bool) {
	if r.ContentLength > h.max {
		a.problem("invalid_input.body_too_large", map[string]any{"max_bytes": h.max})
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(a.w, r.Body, h.max))
	if err != nil {
		if _, tooBig := err.(*http.MaxBytesError); tooBig {
			a.problem("invalid_input.body_too_large", map[string]any{"max_bytes": h.max})
		} else {
			a.rpcError(http.StatusBadRequest, nil, rpcError{Code: codeParse, Message: "The body could not be read."})
		}
		return nil, false
	}
	return body, true
}

// parseMessage reads the body into a request: strictly (parseStrict), one message and not a batch.
func (h *Handler) parseMessage(body []byte) (*rpcRequest, *rpcError) {
	tree, err := parseStrict(body)
	switch err {
	case nil:
	case errSyntax:
		return nil, &rpcError{Code: codeParse, Message: "The body is not valid JSON."}
	case errTooDeep:
		return nil, &rpcError{Code: codeInvalidRequest, Message: "The JSON is nested too deeply."}
	default:
		return nil, &rpcError{Code: codeInvalidRequest, Message: "A member name is repeated in one object or holds a NUL character."}
	}
	return readRequest(tree)
}

// isJSONMedia is true for application/json, with parameters allowed (a charset).
func isJSONMedia(contentType string) bool {
	media, _, _ := strings.Cut(contentType, ";")
	return strings.EqualFold(strings.TrimSpace(media), jsonMediaType)
}

// acceptsJSON is the rule of the REST server: no Accept, */*, application/* or application/json (with a quality above
// zero) takes JSON; a request that lists only other types is refused.
func acceptsJSON(header string) bool {
	if strings.TrimSpace(header) == "" {
		return true
	}
	for _, part := range strings.Split(header, ",") {
		media, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		media = strings.ToLower(strings.TrimSpace(media))
		if (media == "*/*" || media == "application/*" || media == jsonMediaType) && qualityOf(params) > 0 {
			return true
		}
	}
	return false
}

// qualityOf reads q=... from the parameters of a media range; the default is 1.
func qualityOf(params string) float64 {
	for _, p := range strings.Split(params, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(p), "=")
		if ok && strings.EqualFold(key, "q") {
			q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				return 0
			}
			return q
		}
	}
	return 1
}
