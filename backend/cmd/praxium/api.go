package main

import (
	"context"
	"encoding/json"
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
	case args[0] == "api" && len(args) >= 2 && (args[1] == "serve" || args[1] == "routes"):
		return runAPI(args[1:], stdout, stderr, w), true
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

// envDevToken names the variable that may carry the development token; the
// token file flag is the safer way and wins over it.
const envDevToken = "PRAXIUM_DEV_TOKEN"

const apiUsage = "usage: praxium api serve [-addr host:port] [-mode development|production] [-store memory|postgres|file] [-store-dsn-file file] [-store-data-dir dir]\n" +
	"                          [-dev-token-file file] [-dev-subject id] [-dev-roles r1,r2] [-dev-truth file]\n" +
	"       praxium api routes --json\n"

// runAPI handles `praxium api serve` and `praxium api routes`; runCommand sends nothing else here.
func runAPI(args []string, stdout, stderr io.Writer, d wiring) int {
	if args[0] == "routes" {
		return runRoutes(args[1:], stdout, stderr)
	}
	if err := serveAPI(args[1:], stderr, d); err != nil {
		fmt.Fprintf(stderr, "praxium: %v\n", err)
		return exitFailure
	}
	return exitOK
}

// runRoutes prints the routes the server registers, for the conformance check.
func runRoutes(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "--json" {
		fmt.Fprint(stderr, apiUsage)
		return exitUsage
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"routes": httpapi.Routes()}); err != nil {
		fmt.Fprintf(stderr, "praxium: %v\n", err)
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
}

func parseServe(args []string) (serveOptions, error) {
	fs := flag.NewFlagSet("api serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o serveOptions
	fs.StringVar(&o.addr, "addr", "127.0.0.1:8080", "")
	fs.StringVar(&o.mode, "mode", devauth.ModeProduction, "")
	fs.StringVar(&o.tokenFile, "dev-token-file", "", "")
	fs.StringVar(&o.subject, "dev-subject", "dev-user", "")
	fs.StringVar(&o.roles, "dev-roles", "viewer", "")
	fs.StringVar(&o.truth, "dev-truth", "", "")
	fs.StringVar(&o.store, "store", storeMemory, "")
	fs.StringVar(&o.storeDSNFile, "store-dsn-file", "", "")
	fs.StringVar(&o.storeDataDir, "store-data-dir", "", "")
	if err := fs.Parse(args); err != nil {
		return o, kernel.Wrap(kernel.CodeInvalidValue, err, "bad flags for api serve")
	}
	if fs.NArg() != 0 {
		return o, kernel.Errorf(kernel.CodeInvalidValue, "api serve takes no arguments")
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

// requireRealAuthenticator is the fail-closed guard of the server (ADR-0024, ADR-0027). Only a development server
// may start: only the development authenticator exists, so production mode needs a real one (OIDC, TD-0010).
// The production store exists (-store postgres, TD-0011 is closed) and works in development mode. It refuses
// whatever else is configured.
func requireRealAuthenticator(o serveOptions) error {
	switch strings.ToLower(strings.TrimSpace(o.mode)) {
	case devauth.ModeDevelopment:
		return nil
	case devauth.ModeProduction:
		return kernel.Errorf(kernel.CodeInvalidValue,
			"mode production needs a real authenticator (TD-0010), which does not exist yet; the production store exists (-store postgres, TD-0011 is closed) "+
				"but a server never starts in production mode without the authenticator; use -mode development for a development server")
	}
	return kernel.Errorf(kernel.CodeInvalidValue, "unknown -mode %q: use development or production", o.mode)
}

func serveAPI(args []string, stderr io.Writer, d wiring) error {
	o, err := parseServe(args)
	if err != nil {
		return err
	}
	if err := requireRealAuthenticator(o); err != nil {
		return err
	}
	authn, err := authenticatorFor(o, d)
	if err != nil {
		return err
	}
	reg, err := model.LoadStandard()
	if err != nil {
		return err
	}
	log := logger(stderr)
	ctx := context.Background()
	store, closeStore, err := openStoreLogged(ctx, o, d, log)
	if err != nil {
		return err
	}
	defer func() {
		if err := closeStore(); err != nil {
			log.Error("praxium store did not close cleanly", slog.String("error", err.Error()))
		}
	}()
	rep, err := loadTruth(ctx, store, reg, d, o.truth, 0)
	if err != nil {
		return err
	}
	if rep != nil {
		log.Info("praxium dev truth loaded", slog.Int64("objects", rep.Objects), slog.Int64("links", rep.Links), slog.String("version", rep.VersionID))
	}
	queries, err := query.New(store, reg, d.clock)
	if err != nil {
		return err
	}
	srv, err := httpapi.New(httpapi.Config{
		Registry: reg, Authenticator: authn, Clock: d.clock, IDs: d.ids, Logger: log, Version: version,
		Queries: queries, KGs: store,
	})
	if err != nil {
		return err
	}
	ln, err := d.listen(o.addr)
	if err != nil {
		return kernel.Wrap(kernel.CodeInvalidValue, err, "cannot listen")
	}
	stopCtx, stop := d.signals()
	defer stop()
	log.Info("praxium api listening", slog.String("addr", ln.Addr().String()), slog.String("mode", o.mode),
		slog.Bool("dev_auth", authn != nil), slog.String("store", o.store), slog.String("version", version))
	return srv.Serve(stopCtx, ln)
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
		fmt.Fprintf(stderr, "praxium: %v\n", err)
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
