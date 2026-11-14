package httpapi

import (
	"context"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/query"
)

// Defaults of the server's limits.
const (
	// DefaultMaxBodyBytes is the body size limit when the configuration gives none.
	DefaultMaxBodyBytes int64 = 1 << 20
	// DefaultGrace is how long a shutdown waits for requests in flight.
	DefaultGrace = 15 * time.Second
)

// Config is everything a Server needs from the outside. The composition root
// builds it; the tests pass fixed parts.
type Config struct {
	// Registry is the type registry the model routes read.
	Registry *model.Registry
	// Authenticator turns a bearer token into a subject. Nil means no one can
	// authenticate: only the routes open to everyone answer.
	Authenticator access.Authenticator
	// Authorizer judges abilities; nil means the role table.
	Authorizer access.Authorizer
	// Clock times the access log; IDs issue trace ids.
	Clock kernel.Clock
	IDs   kernel.IDGenerator
	// Logger receives the access log and the server's own messages.
	Logger *slog.Logger
	// MaxBodyBytes limits a request body; zero means DefaultMaxBodyBytes.
	MaxBodyBytes int64
	// Grace is the shutdown wait; zero means DefaultGrace.
	Grace time.Duration
	// Queries and KGs read the KGs; both nil means the server has no store and the KG routes answer 503.
	Queries *query.Engine
	KGs     KGSource
	// UI is the built web UI, served at / next to the API. Nil means the server serves the API only (ADR-0035).
	UI fs.FS
	// Version is the build version, reported by capabilities later and logged now.
	Version string
}

// Server serves the contract. Build it with New; it is safe for concurrent use.
type Server struct {
	reg      *model.Registry
	authn    access.Authenticator
	authz    access.Authorizer
	clock    kernel.Clock
	ids      kernel.IDGenerator
	log      *slog.Logger
	maxBody  int64
	grace    time.Duration
	version  string
	mux      *http.ServeMux
	handler  http.Handler
	lists    *registryLists
	queries  *query.Engine
	kgs      KGSource
	ui       fs.FS
	started  atomic.Bool
	draining atomic.Bool
}

// New builds a server and registers every route of the route table.
func New(cfg Config) (*Server, error) {
	if cfg.Registry == nil || cfg.Clock == nil || cfg.IDs == nil || cfg.Logger == nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a server needs a registry, a clock, an id generator and a logger")
	}
	s := &Server{
		reg: cfg.Registry, authn: cfg.Authenticator, authz: cfg.Authorizer, clock: cfg.Clock,
		ids: cfg.IDs, log: cfg.Logger, queries: cfg.Queries, kgs: cfg.KGs, ui: cfg.UI, maxBody: cfg.MaxBodyBytes, grace: cfg.Grace, version: cfg.Version,
		mux: http.NewServeMux(),
	}
	if s.authz == nil {
		s.authz = access.RoleTable{}
	}
	if s.maxBody <= 0 {
		s.maxBody = DefaultMaxBodyBytes
	}
	if s.grace <= 0 {
		s.grace = DefaultGrace
	}
	s.lists = buildRegistryLists(s.reg)
	for _, spec := range s.routeSpecs() {
		s.mux.HandleFunc(spec.method+" "+spec.path, s.wrap(spec))
	}
	s.handler = s.chain(http.HandlerFunc(s.dispatch))
	return s, nil
}

// Handler is the http.Handler of the whole API, middleware included.
func (s *Server) Handler() http.Handler { return s.handler }

// MarkStarted tells the server it is up: /v1/health/started answers 200 from now on.
// Serve does this itself; it is public for servers run through Handler alone.
func (s *Server) MarkStarted() { s.started.Store(true) }

// Serve serves on the listener until ctx ends, then stops taking new
// connections and waits up to the grace period for the requests in flight
// (graceful shutdown). While it waits, /v1/health/ready answers 503.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	s.started.Store(true)
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	s.draining.Store(true)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.grace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		return err
	}
	<-done
	return nil
}

// wrap turns a route into a handler with the checks every route shares:
// content negotiation, authentication, authorization.
func (s *Server) wrap(spec routeSpec) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := &call{s: s, w: w, r: r, trace: traceOf(r.Context())}
		if !acceptsJSON(r.Header.Get("Accept")) {
			c.fail("invalid_input.not_acceptable", map[string]any{"accept": r.Header.Get("Accept")})
			return
		}
		if !c.authorize(spec.roles) {
			return
		}
		spec.handle(c)
	}
}

// dispatch sends a request to its route, or answers 405 or 404 as a problem.
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	if _, pattern := s.mux.Handler(r); pattern != "" {
		s.mux.ServeHTTP(w, r)
		return
	}
	if s.serveUI(w, r) {
		return
	}
	c := &call{s: s, w: w, r: r, trace: traceOf(r.Context())}
	if allowed := s.allowedMethods(r); len(allowed) > 0 {
		w.Header().Set("Allow", joinMethods(allowed))
		c.fail("invalid_input.method_not_allowed", map[string]any{"method": r.Method, "allowed": allowed})
		return
	}
	c.notFound("route", c.r.URL.Path)
}

// allowedMethods lists the methods the request's path has routes for.
func (s *Server) allowedMethods(r *http.Request) []string {
	var out []string
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		probe := r.Clone(r.Context())
		probe.Method = m
		if _, pattern := s.mux.Handler(probe); pattern != "" {
			out = append(out, m)
		}
	}
	return out
}

func joinMethods(methods []string) string {
	out := ""
	for i, m := range methods {
		if i > 0 {
			out += ", "
		}
		out += m
	}
	return out
}
