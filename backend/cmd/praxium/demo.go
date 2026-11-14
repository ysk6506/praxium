package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/devauth"
	"github.com/ysk6506/praxium/backend/internal/devload"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/webui"
	"github.com/ysk6506/praxium/backend/internal/worldgen"
)

// The defaults of `praxium demo` (ADR-0038).
const (
	demoDefaultAddr  = "127.0.0.1:8080"
	demoDefaultScale = 20000
	demoDefaultSeed  = 1
	demoSubject      = "demo-user"
	// demoTokenBytes is the size of the random token: 16 bytes, 32 hex characters.
	demoTokenBytes = 16
)

const demoUsage = `usage: praxium demo [-addr host:port] [-data-dir dir] [-scale N] [-seed N]
  Starts a server with a synthetic data center already loaded and the web UI, for a first look.
  The data is made up and nothing is connected to a real system. It is not for production.
  -addr      where to listen (default 127.0.0.1:8080)
  -data-dir  keep the KG in this folder, so it is still there at the next start (default: in memory, gone at exit)
  -scale     about this many objects and links (default 20000)
  -seed      seed of the synthetic world (default 1)
`

type demoOptions struct {
	addr, dataDir string
	scale         int64
	seed          uint64
}

func parseDemo(args []string) (demoOptions, error) {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o demoOptions
	fs.StringVar(&o.addr, "addr", demoDefaultAddr, "")
	fs.StringVar(&o.dataDir, "data-dir", "", "")
	fs.Int64Var(&o.scale, "scale", demoDefaultScale, "")
	fs.Uint64Var(&o.seed, "seed", demoDefaultSeed, "")
	if err := fs.Parse(args); err != nil {
		return o, kernel.Wrap(kernel.CodeInvalidValue, err, "bad flags for demo")
	}
	if fs.NArg() != 0 {
		return o, kernel.Errorf(kernel.CodeInvalidValue, "demo takes no arguments")
	}
	if o.scale < 1 {
		return o, kernel.Errorf(kernel.CodeInvalidValue, "-scale wants a number of at least 1")
	}
	return o, nil
}

// runDemo is `praxium demo`: the shortest way to a running Praxium with a data center in it.
func runDemo(args []string, stdout, stderr io.Writer, w wiring) int {
	o, err := parseDemo(args)
	if err != nil {
		fmt.Fprintf(stderr, "praxium: %v\n%s", err, demoUsage)
		return exitUsage
	}
	if err := serveDemo(o, stdout, stderr, w); err != nil {
		fmt.Fprintf(stderr, "praxium: %v\n", err)
		return exitFailure
	}
	fmt.Fprintln(stdout, "praxium demo stopped")
	return exitOK
}

// serveDemo is the development server of `api serve` with a token made for this run and a world synthesized in the
// process: no truth file and no token file touch the disk. The token comes from the kernel's entropy and exists
// only in this process and in the address printed to the terminal.
func serveDemo(o demoOptions, stdout, stderr io.Writer, w wiring) error {
	token := kernel.NewToken(demoTokenBytes)
	authn, err := devauth.New(devauth.Config{
		Mode: devauth.ModeDevelopment, Token: token, SubjectID: demoSubject, Roles: []access.Role{access.RoleViewer},
	})
	if err != nil {
		return err
	}
	so := serveOptions{addr: o.addr, mode: devauth.ModeDevelopment, store: storeMemory, web: true}
	if o.dataDir != "" {
		so.store, so.storeDataDir = storeFile, o.dataDir
	}
	if !isLoopbackAddr(o.addr) {
		fmt.Fprintf(stderr, "warning: %s is not a loopback address. The demo token is weak protection: it is printed to a terminal "+
			"and travels over plain HTTP. Anyone who can reach this address and learns the token can read the demo data.\n", o.addr)
	}
	built := webui.Built(webui.Files())
	if w.webBuilt != nil {
		built = w.webBuilt()
	}
	return runServer(serverPlan{
		opts: so, authn: authn, listenFirst: true,
		load: func(ctx context.Context, store graph.Store, reg *model.Registry) (*devload.Report, error) {
			return demoLoad(ctx, store, reg, o, w, stdout)
		},
		ready: func(addr net.Addr, loaded *devload.Report) { announceDemo(stdout, o, addr, token, loaded, built) },
	}, stderr, w)
}

// demoLoad fills the store with the synthetic world, unless it already holds the KG: a data folder that was filled
// by an earlier start is used as it is. A KG whose first load never finished is not: the folder must be removed.
func demoLoad(ctx context.Context, store graph.Store, reg *model.Registry, o demoOptions, w wiring, stdout io.Writer) (*devload.Report, error) {
	kg, err := store.GetKG(ctx, graph.MainAlias)
	switch {
	case err == nil && kg.State == graph.StateBuilding:
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the data folder %s holds a demo KG whose load did not finish; remove the folder and start again", o.dataDir)
	case err == nil:
		return nil, nil
	case kernel.CodeOf(err) != graph.CodeKGNotFound:
		return nil, err
	}
	fmt.Fprintf(stdout, "Synthesizing a data center (scale %d, seed %d) and loading it...\n", o.scale, o.seed)
	truth, err := synthesize(reg, o)
	if err != nil {
		return nil, err
	}
	return loadTruthFrom(ctx, store, reg, w, truth, 0)
}

// synthesize generates the world in memory and gives its truth as a reader.
func synthesize(reg *model.Registry, o demoOptions) (io.Reader, error) {
	world, err := worldgen.Build(reg, worldgen.DefaultParams(o.seed, o.scale))
	if err != nil {
		return nil, err
	}
	files := worldgen.NewMemStore()
	if _, err := world.Run(files, worldgen.RunOptions{Truth: true}); err != nil {
		return nil, err
	}
	return strings.NewReader(files.Files[worldgen.TruthFile].String()), nil
}

// isLoopbackAddr says whether a listen address is reachable from this machine only.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// demoURL is the address to open: the token and the demo mark sit in the fragment, which the browser never sends,
// so no server log has it. The web UI takes them and replaces the address (ADR-0038).
func demoURL(addr net.Addr, token string) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return ""
	}
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	u := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: "/"}
	return u.String() + "#/connect?token=" + token + "&demo=1"
}

// announceDemo prints what a person needs to start looking: what this is, where to go, how to stop.
func announceDemo(out io.Writer, o demoOptions, addr net.Addr, token string, loaded *devload.Report, webBuilt bool) {
	fmt.Fprintln(out, "Praxium demo: the data is synthetic and not connected to any real system. This is not for production.")
	switch {
	case loaded != nil:
		fmt.Fprintf(out, "Loaded %d objects and %d links.\n", loaded.Objects, loaded.Links)
	default:
		fmt.Fprintf(out, "Using the KG kept in %s; nothing was loaded again.\n", o.dataDir)
	}
	where := demoURL(addr, token)
	if webBuilt {
		fmt.Fprintln(out, "Open this address in a browser (the token is after the #, which a browser does not send to the server):")
		fmt.Fprintln(out, where)
	} else {
		host := strings.SplitN(strings.TrimPrefix(where, "http://"), "/", 2)[0]
		fmt.Fprintln(out, "This build does not include the web UI. Build with scripts/build-with-web.sh, or use a release executable, to browse in a browser.")
		fmt.Fprintf(out, "The API and the command line work as they are. The token of this run is %s\n", token)
		fmt.Fprintf(out, "  praxium --endpoint http://%s --token %s kg list\n", host, token)
		fmt.Fprintf(out, "  praxium --endpoint http://%s --token %s query find --type PhysicalServer --limit 3\n", host, token)
	}
	if o.dataDir == "" {
		fmt.Fprintln(out, "The data is in memory and is gone when the demo stops. Press Ctrl+C to stop.")
	} else {
		fmt.Fprintf(out, "The data is kept in %s. Press Ctrl+C to stop.\n", o.dataDir)
	}
}
