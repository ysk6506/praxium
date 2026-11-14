package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/cli"
	"github.com/ysk6506/praxium/backend/internal/contract"
	"github.com/ysk6506/praxium/backend/internal/devauth"
	"github.com/ysk6506/praxium/backend/internal/devload"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/httpapi"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/query"
)

// exitFailure is the exit code of a command that was understood and failed.
const exitFailure = 1

// wiring is everything the commands that talk to the outside world need on top of the kernel's parts:
// the environment and files, the listener and the stop signal of the server, and the client's HTTP client.
// The tests pass fixed ones.
type wiring struct {
	deps
	env cli.Env
	// listen opens the server's listener; signals gives the context that ends on a stop signal.
	listen  func(addr string) (net.Listener, error)
	signals func() (context.Context, context.CancelFunc)
	// client is the HTTP client of the client commands; nil means a default one.
	client *http.Client
	// open opens a truth file for the development loader; nil means the whole file read through env.
	open func(path string) (io.ReadCloser, error)
	// random gives n random bytes as hex for the secrets of access tokens; nil means the kernel's entropy.
	random func(n int) string
	// webBuilt says whether the program has the web UI inside; nil means ask the embedded files.
	webBuilt func() bool
}

// systemWiring is the wiring of the real process.
func systemWiring() wiring {
	return wiring{
		deps:   deps{clock: kernel.SystemClock{}, ids: kernel.NewRandomIDs(kernel.NewSystemRng())},
		env:    cli.Env{Lookup: kernel.LookupEnv, ReadFile: os.ReadFile},
		open:   systemOpen,
		listen: func(addr string) (net.Listener, error) { return net.Listen("tcp", addr) },
		signals: func() (context.Context, context.CancelFunc) {
			return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		},
	}
}

// execute runs a command line: the commands of the API and its client first, then everything run knows.
func execute(args []string, stdout, stderr io.Writer, w wiring) int {
	if code, handled := runHelp(args, stdout, stderr); handled {
		return code
	}
	if code, handled := runNotYet(args, stderr); handled {
		return code
	}
	if len(args) > 0 {
		if code, handled := runCommand(args, stdout, stderr, w); handled {
			return code
		}
	}
	return run(args, stdout, stderr, w.deps)
}

// runCommand handles the commands that are more than a bare role name. handled
// is false for everything else, which the caller leaves to run.
func runCommand(args []string, stdout, stderr io.Writer, w wiring) (code int, handled bool) {
	switch {
	case isOfflineAdmin(args):
		return runAdminOffline(args, stdout, stderr, w), true
	case args[0] == "api":
		return runAPICommand(args, stdout, stderr, w)
	case args[0] == "demo":
		return runDemo(args[1:], stdout, stderr, w), true
	case args[0] == "local":
		return runLocal(args[1:], stdout, stderr, w), true
	case args[0] == "dev":
		return runDev(args[1:], stdout, stderr, w), true
	case args[0] == "ingest":
		return runIngest(args[1:], stdout, stderr, w), true
	case args[0] == "contract":
		return runContract(args[1:], stdout, stderr), true
	case cli.IsClientCommand(args[0]) || cli.IsClientFlag(args[0]):
		return cli.Run(args, w.env, stdout, stderr, w.client), true
	}
	return 0, false
}

// runAPICommand handles `praxium api ...`: serve and routes run, an unknown word is a usage error, and help
// (or no word) is left to the help handler.
func runAPICommand(args []string, stdout, stderr io.Writer, w wiring) (int, bool) {
	if len(args) < 2 || helpFlagWord(args[1]) {
		return 0, false
	}
	if args[1] == "serve" || args[1] == "routes" {
		return runAPI(args[1:], stdout, stderr, w), true
	}
	return usageFail(stderr, kernel.Errorf(kernel.CodeInvalidValue, "api has no command %q", args[1]), "api"), true
}

// envDevToken names the variable that may carry the development token; the
// token file flag is the safer way and wins over it.
const envDevToken = "PRAXIUM_DEV_TOKEN"

const apiServeUsage = "usage: praxium api serve [-addr host:port] [-mode production|development] [-store file|postgres|memory] [-tls-cert F -tls-key F | -behind-proxy] [flags]"

const apiServeBody = `Runs the REST API server (docs/design-docs/rest-api.md) and, unless -web=false, the web UI at / (ADR-0035).

Production mode (the default) signs callers in with access tokens that "praxium admin" makes (ADR-0044); there are no
passwords. It needs a store that survives a restart and an administrator, so the first time:
  1. praxium admin bootstrap -name root -store file -store-data-dir /var/lib/praxium    (prints the first token, once)
  2. praxium api serve -store file -store-data-dir /var/lib/praxium
The users and tokens are kept beside the KG: in the data folder (-store file) or the database (-store postgres). Production
refuses the memory store, the development token flags and PRAXIUM_DEV_TOKEN. Access tokens must not cross a network in
plain text: on an address that is not loopback it needs -tls-cert and -tls-key, or -behind-proxy when a reverse proxy in
front ends TLS (then it believes the last address in X-Forwarded-For as the caller's).

To look around without any of this use "praxium demo" (a made-up data center) or "praxium local" (your own KG, built
with praxium ingest run). For development use -mode development: it takes one access token from -dev-token-file (a file
with the token; the environment variable PRAXIUM_DEV_TOKEN also works, the file wins) and treats a caller with it as the
subject -dev-subject with the roles -dev-roles.

-store file keeps the KG in a data folder (-store-data-dir, ADR-0034); -store postgres reads the connection string
from a file (-store-dsn-file) so no secret is in the process list.

example:
  praxium admin bootstrap -name root -store file -store-data-dir /var/lib/praxium
  praxium api serve -addr 127.0.0.1:8080 -store file -store-data-dir /var/lib/praxium
  praxium worldgen -scale 500 -seed 7 -out /tmp/world
  printf 'dev-token-not-a-secret' > /tmp/dev.tok
  praxium api serve -mode development -dev-token-file /tmp/dev.tok -dev-truth /tmp/world/truth.jsonl
`

// apiHelpEntries are the help entries of this file's commands.
func apiHelpEntries(out map[string]helpEntry) {
	out["api serve"] = helpEntry{usage: apiServeUsage, body: apiServeBody, flags: func() *flag.FlagSet { return newServeFlags(&serveOptions{}) }}
}

// runAPI handles `praxium api serve` and `praxium api routes`; runCommand sends nothing else here.
func runAPI(args []string, stdout, stderr io.Writer, d wiring) int {
	if args[0] == "routes" {
		return runRoutes(args[1:], stdout, stderr)
	}
	if err := serveAPI(args[1:], stderr, d); err != nil {
		fmt.Fprintf(stderr, "praxium: %s\n", errText(err))
		var bad usageProblem
		if errors.As(err, &bad) {
			fmt.Fprintf(stderr, "%s\nFor details: praxium api serve --help\n", apiServeUsage)
		}
		return exitFailure
	}
	return exitOK
}

// runRoutes prints the routes the server registers, for the conformance check.
func runRoutes(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "--json" {
		return usageFail(stderr, kernel.Errorf(kernel.CodeInvalidValue, "api routes wants --json"), "api routes")
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"routes": httpapi.Routes()}); err != nil {
		fmt.Fprintf(stderr, "praxium: %s\n", errText(err))
		return exitFailure
	}
	return exitOK
}

type serveOptions struct {
	addr, mode, tokenFile, subject, roles string
	// truth is a developer convenience of development mode (production never starts): a truth file of the generator
	// to load into the development store. It is not the ingestion pipeline (ADR-0027).
	truth string
	// store is "memory" (the default), "postgres" or "file"; storeDSNFile is the file that holds the PostgreSQL DSN and
	// storeDataDir the data folder of the file store.
	store, storeDSNFile, storeDataDir string
	// web is whether the server also serves the embedded web UI at / (ADR-0035).
	web bool
	// tlsCert and tlsKey are the files of the certificate and key the server speaks TLS with; behindProxy says that a
	// reverse proxy in front ends TLS and appends the caller's address to X-Forwarded-For (ADR-0044).
	tlsCert, tlsKey string
	behindProxy     bool
	// devFlag is the first development-only flag that was given (without its dash), "" when none was.
	devFlag string
	// modeSet is whether -mode was on the command line: a start with no -mode at all is told how to get going.
	modeSet bool
}

// usageProblem is an error of the command line of api serve: the message is followed by the usage line.
type usageProblem struct{ error }

func (e usageProblem) Unwrap() error { return e.error }

func newServeFlags(o *serveOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("api serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.addr, "addr", "127.0.0.1:8080", "where to listen, host:port")
	fs.StringVar(&o.mode, "mode", devauth.ModeProduction, "development or production (built-in authentication; needs -store file or postgres)")
	fs.StringVar(&o.tokenFile, "dev-token-file", "", "development mode: the file that holds the access token (at least 16 characters; environment PRAXIUM_DEV_TOKEN works too)")
	fs.StringVar(&o.subject, "dev-subject", "dev-user", "development mode: the subject a caller with the token is")
	fs.StringVar(&o.roles, "dev-roles", "viewer", "development mode: the roles of that subject, comma separated")
	fs.StringVar(&o.truth, "dev-truth", "", "development mode: a truth file of praxium worldgen to load into the KG before serving")
	fs.StringVar(&o.store, "store", storeMemory, "memory, postgres or file")
	fs.StringVar(&o.storeDSNFile, "store-dsn-file", "", "-store postgres: the file that holds the connection string")
	fs.BoolVar(&o.web, "web", true, "serve the web UI at / (false: the API only)")
	fs.StringVar(&o.storeDataDir, "store-data-dir", "", "-store file: the data folder that keeps the KG (and, in production, the users and tokens)")
	fs.StringVar(&o.tlsCert, "tls-cert", "", "the PEM file of the TLS certificate; with -tls-key the server speaks HTTPS")
	fs.StringVar(&o.tlsKey, "tls-key", "", "the PEM file of the TLS private key")
	fs.BoolVar(&o.behindProxy, "behind-proxy", false, "a reverse proxy in front ends TLS and sets X-Forwarded-For; allows plain HTTP on a non-loopback address")
	return fs
}

func parseServe(args []string) (serveOptions, error) {
	var o serveOptions
	fs := newServeFlags(&o)
	if err := fs.Parse(args); err != nil {
		return o, usageProblem{kernel.Wrap(kernel.CodeInvalidValue, flagProblem(err), "bad flags for api serve")}
	}
	fs.Visit(func(f *flag.Flag) {
		o.modeSet = o.modeSet || f.Name == "mode"
		if o.devFlag == "" && strings.HasPrefix(f.Name, "dev-") {
			o.devFlag = f.Name
		}
	})
	if fs.NArg() != 0 {
		return o, usageProblem{kernel.Errorf(kernel.CodeInvalidValue, "api serve takes no arguments")}
	}
	return o, nil
}

// authenticatorFor builds the development authenticator when a token is
// configured and none otherwise. It refuses mode production with a token.
func authenticatorFor(o serveOptions, d wiring) (access.Authenticator, error) {
	token, err := devToken(o, d)
	if err != nil || token == "" {
		return nil, err
	}
	var roles []access.Role
	for _, name := range strings.Split(o.roles, ",") {
		role, ok := access.ParseRole(strings.TrimSpace(name))
		if !ok {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "unknown role %q in -dev-roles", name)
		}
		roles = append(roles, role)
	}
	return devauth.New(devauth.Config{Mode: o.mode, Token: token, SubjectID: o.subject, Roles: roles})
}

func devToken(o serveOptions, d wiring) (string, error) {
	if o.tokenFile != "" {
		data, err := d.env.ReadFile(o.tokenFile)
		if err != nil {
			return "", kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read the development token file")
		}
		return strings.TrimSpace(string(data)), nil
	}
	token, _ := d.env.Lookup(envDevToken)
	return strings.TrimSpace(token), nil
}

func serveAPI(args []string, stderr io.Writer, d wiring) error {
	o, err := parseServe(args)
	if err != nil {
		return err
	}
	if err := checkMode(o, d); err != nil {
		return err
	}
	plan := serverPlan{opts: o}
	if strings.ToLower(strings.TrimSpace(o.mode)) == devauth.ModeProduction {
		plan.auth = productionAuth(o, d)
	} else if plan.authn, err = authenticatorFor(o, d); err != nil {
		return err
	}
	plan.load = func(ctx context.Context, store graph.Store, reg *model.Registry) (*devload.Report, error) {
		return loadTruth(ctx, store, reg, d, o.truth, 0)
	}
	return runServer(plan, stderr, d)
}

// serverPlan is what one start of the server needs. `api serve` and `demo` both build one and share runServer, so
// the store, the server and the shutdown are assembled in one place.
type serverPlan struct {
	opts serveOptions
	// authn is the development authenticator; auth, when set, builds the authentication of a production server over
	// the store once it is open, and wins.
	authn access.Authenticator
	auth  func(ctx context.Context, store graph.Store, log *slog.Logger) (authKit, error)
	// load fills the store before the server answers; nil, or a nil report, loads nothing.
	load func(ctx context.Context, store graph.Store, reg *model.Registry) (*devload.Report, error)
	// listenFirst takes the address before the load, so a taken port fails at once; the default loads first.
	listenFirst bool
	// ready is called once the server listens and the load is done, with where it listens and what was loaded.
	ready func(addr net.Addr, loaded *devload.Report)
}

// listenError says why the address cannot be used; the common case, a port in use, names the way out.
func listenError(addr string, err error) error {
	if errors.Is(err, syscall.EADDRINUSE) {
		return kernel.Wrap(kernel.CodeInvalidValue, err, fmt.Sprintf("cannot listen on %s: the address is already in use; stop the program that uses it or choose another with -addr", addr))
	}
	return kernel.Wrap(kernel.CodeInvalidValue, err, "cannot listen")
}

func listenOn(d wiring, addr string) (net.Listener, error) {
	ln, err := d.listen(addr)
	if err != nil {
		return nil, listenError(addr, err)
	}
	return ln, nil
}

// runServer opens the store, loads, takes the address and serves until the stop signal. A plan with listenFirst takes
// the address before the load, so a taken port fails at once and not after a long load. The stop signal also ends a
// load in progress.
func runServer(p serverPlan, stderr io.Writer, d wiring) error {
	reg, err := model.LoadStandard()
	if err != nil {
		return err
	}
	tlsCfg, err := tlsConfigFor(p.opts, d)
	if err != nil {
		return err
	}
	log := logger(stderr)
	stopCtx, stop := d.signals()
	defer stop()
	store, closeStore, err := openStoreLogged(stopCtx, p.opts, d, log)
	if err != nil {
		return err
	}
	defer closeLogged(closeStore, log)
	kit, err := authFor(stopCtx, p, store, log)
	if err != nil {
		return err
	}
	defer closeLogged(kit.close, log)
	var ln net.Listener
	serving := false
	defer func() {
		if ln != nil && !serving {
			_ = ln.Close()
		}
	}()
	if p.listenFirst {
		if ln, err = listenOn(d, p.opts.addr); err != nil {
			return err
		}
	}
	rep, err := loadFor(stopCtx, p, store, reg, log)
	if err != nil {
		return err
	}
	if ln == nil {
		if ln, err = listenOn(d, p.opts.addr); err != nil {
			return err
		}
	}
	srv, err := newServer(p, kit, d, log, store, reg)
	if err != nil {
		return err
	}
	if tlsCfg != nil {
		ln = tls.NewListener(ln, tlsCfg)
	}
	log.Info("praxium api listening", slog.String("addr", ln.Addr().String()), slog.String("mode", p.opts.mode),
		slog.Bool("dev_auth", p.authn != nil), slog.String("auth", kit.name), slog.Bool("tls", tlsCfg != nil),
		slog.String("store", p.opts.store), slog.String("web_ui", uiState(p.opts)), slog.String("version", version))
	if p.ready != nil {
		p.ready(ln.Addr(), rep)
	}
	serving = true
	return srv.Serve(stopCtx, ln)
}

// authFor settles who may call the server: the built-in authentication of a production plan, else the development
// authenticator (nil: only the open routes answer).
func authFor(ctx context.Context, p serverPlan, store graph.Store, log *slog.Logger) (authKit, error) {
	if p.auth != nil {
		return p.auth(ctx, store, log)
	}
	kit := authKit{authn: p.authn, name: "none", close: func() error { return nil }}
	if p.authn != nil {
		kit.name = "development"
	}
	return kit, nil
}

// closeLogged closes the store; the file store takes its last snapshot here, so a failure is worth a line.
func closeLogged(closeStore func() error, log *slog.Logger) {
	if err := closeStore(); err != nil {
		log.Error("praxium store did not close cleanly", slog.String("error", err.Error()))
	}
}

// loadFor runs the plan's load and says what it loaded.
func loadFor(ctx context.Context, p serverPlan, store graph.Store, reg *model.Registry, log *slog.Logger) (*devload.Report, error) {
	if p.load == nil {
		return nil, nil
	}
	rep, err := p.load(ctx, store, reg)
	if rep != nil {
		log.Info("praxium dev truth loaded", slog.Int64("objects", rep.Objects), slog.Int64("links", rep.Links), slog.String("version", rep.VersionID))
	}
	return rep, err
}

// newServer wires the API server over the store.
func newServer(p serverPlan, kit authKit, d wiring, log *slog.Logger, store graph.Store, reg *model.Registry) (*httpapi.Server, error) {
	queries, err := query.New(store, reg, d.clock)
	if err != nil {
		return nil, err
	}
	return httpapi.New(httpapi.Config{
		Registry: reg, Authenticator: kit.authn, Directory: kit.dir, Limiter: kit.limiter, TrustProxy: p.opts.behindProxy,
		Clock: d.clock, IDs: d.ids, Logger: log, Version: version, Queries: queries, KGs: store, UI: uiFor(p.opts),
	})
}

// runContract handles `praxium contract assemble|check`: the same assembly the
// harness repeats to see whether the committed document is current.
func runContract(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || (args[0] != "assemble" && args[0] != "check") {
		fmt.Fprint(stderr, "usage: praxium contract assemble|check\n")
		return exitUsage
	}
	data, err := contract.Assemble(contract.Files(), contract.PartsDir)
	if err != nil {
		fmt.Fprintf(stderr, "praxium: %s\n", errText(err))
		return exitFailure
	}
	if args[0] == "assemble" {
		_, _ = stdout.Write(data)
		return exitOK
	}
	if string(data) != string(contract.OpenAPI()) {
		fmt.Fprint(stderr, "praxium: the committed contract is not what the parts assemble to; run `praxium contract assemble`\n")
		return exitFailure
	}
	return exitOK
}
