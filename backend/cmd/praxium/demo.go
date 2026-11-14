package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

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

const demoUsage = "usage: praxium demo [-addr host:port] [-data-dir dir] [-scale N] [-seed N]"

const demoBody = `Starts a server with a synthetic data center already loaded, and the web UI, for a first look. It is the quickest way
to see what Praxium does: run it, open the address it prints in a browser, and click through the types, the tables and
the links between objects. The data is made up and nothing is connected to a real system. It is not for production.

The address it prints carries a token made for this run (after the #, which the browser never sends to the server), so
there is nothing to set up and no token to type. Press Ctrl+C to stop. The same server answers the command line:
  praxium --endpoint http://127.0.0.1:8080 --token <the token it prints> query find --type PhysicalServer --limit 3

With -data-dir the KG is kept in that folder and used again at the next start; without it the data is in memory and
gone when the demo stops. For your own data see "praxium ingest init" and "praxium local".
`

// demoHelpEntries are the help entries of this file's commands.
func demoHelpEntries(out map[string]helpEntry) {
	out["demo"] = helpEntry{usage: demoUsage, body: demoBody, flags: func() *flag.FlagSet { return newDemoFlags(&demoOptions{}) }}
}

type demoOptions struct {
	addr, dataDir string
	scale         int64
	seed          uint64
}

func newDemoFlags(o *demoOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.addr, "addr", demoDefaultAddr, "where to listen, host:port; an address other than this computer's own is warned about")
	fs.StringVar(&o.dataDir, "data-dir", "", "keep the KG in this folder, so it is still there at the next start (default: in memory, gone at exit)")
	fs.Int64Var(&o.scale, "scale", demoDefaultScale, "about this many objects and links (500 or more)")
	fs.Uint64Var(&o.seed, "seed", demoDefaultSeed, "seed of the synthetic world")
	return fs
}

func parseDemo(args []string) (demoOptions, error) {
	var o demoOptions
	fs := newDemoFlags(&o)
	if err := fs.Parse(args); err != nil {
		return o, kernel.Wrap(kernel.CodeInvalidValue, flagProblem(err), "bad flags for demo")
	}
	if fs.NArg() != 0 {
		return o, kernel.Errorf(kernel.CodeInvalidValue, "demo takes no arguments")
	}
	if err := checkAddr(o.addr); err != nil {
		return o, err
	}
	if o.scale < 1 {
		return o, kernel.Errorf(kernel.CodeInvalidValue, "-scale wants a number of at least 1")
	}
	return o, nil
}

// checkAddr says what is wrong with a listen address that is not host:port, before anything else is said about it.
func checkAddr(addr string) error {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return kernel.Errorf(kernel.CodeInvalidValue, "-addr wants host:port, such as 127.0.0.1:8080, got %q", addr)
	}
	return nil
}

// runDemo is `praxium demo`: the shortest way to a running Praxium with a data center in it.
func runDemo(args []string, stdout, stderr io.Writer, w wiring) int {
	o, err := parseDemo(args)
	if err != nil {
		return usageFail(stderr, err, "demo")
	}
	if err := serveDemo(o, stdout, stderr, w); err != nil {
		fmt.Fprintf(stderr, "praxium: %s\n", errText(err))
		return exitFailure
	}
	fmt.Fprintln(stdout, "praxium demo stopped")
	return exitOK
}

// session is what `demo` and `local` share: a development server on this computer with a token made for the run,
// and the address to open that carries it (ADR-0038, ADR-0041). They differ in what they load, in what they say and
// in the mark the address carries for the web UI.
type session struct {
	addr, dataDir string
	// load fills the store before the server answers; announce says where to go once it listens.
	load     func(ctx context.Context, store graph.Store, reg *model.Registry) (*devload.Report, error)
	announce func(out io.Writer, addr net.Addr, token string, loaded *devload.Report, webBuilt bool)
	// warnAbout is the name used in the warning for an address that is not the loopback.
	warnAbout string
}

// serveSession is the development server of `api serve` with a token made for this run: no token file touches the
// disk. The token comes from the kernel's entropy and exists only in this process and in the address printed to the
// terminal. The port is taken before the load, so a port in use fails at once and not after a long load.
func serveSession(s session, stdout, stderr io.Writer, w wiring) error {
	token := kernel.NewToken(demoTokenBytes)
	authn, err := devauth.New(devauth.Config{
		Mode: devauth.ModeDevelopment, Token: token, SubjectID: demoSubject, Roles: []access.Role{access.RoleViewer},
	})
	if err != nil {
		return err
	}
	so := serveOptions{addr: s.addr, mode: devauth.ModeDevelopment, store: storeMemory, web: true}
	if s.dataDir != "" {
		so.store, so.storeDataDir = storeFile, s.dataDir
	}
	if !isLoopbackAddr(s.addr) {
		fmt.Fprintf(stderr, "warning: %s is not a loopback address. The %s token is weak protection: it is printed to a terminal "+
			"and travels over plain HTTP. Anyone who can reach this address and learns the token can read the data.\n", s.addr, s.warnAbout)
	}
	built := webui.Built(webui.Files())
	if w.webBuilt != nil {
		built = w.webBuilt()
	}
	return runServer(serverPlan{
		opts: so, authn: authn, listenFirst: true, load: s.load,
		ready: func(addr net.Addr, loaded *devload.Report) { s.announce(stdout, addr, token, loaded, built) },
	}, stderr, w)
}

// serveDemo is the session of `praxium demo`: a world synthesized in the process.
func serveDemo(o demoOptions, stdout, stderr io.Writer, w wiring) error {
	return serveSession(session{
		addr: o.addr, dataDir: o.dataDir, warnAbout: "demo",
		load: func(ctx context.Context, store graph.Store, reg *model.Registry) (*devload.Report, error) {
			return demoLoad(ctx, store, reg, o, w, stdout)
		},
		announce: func(out io.Writer, addr net.Addr, token string, loaded *devload.Report, built bool) {
			announceDemo(out, o, addr, token, loaded, built)
		},
	}, stdout, stderr, w)
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
	var rep *devload.Report
	err = reportWhile(stdout, progressEvery, "still synthesizing and loading", func() error {
		truth, err := synthesize(reg, o)
		if err != nil {
			return err
		}
		rep, err = loadTruthFrom(ctx, store, reg, w, truth, 0)
		return err
	})
	return rep, err
}

// progressEvery is how often a long start says it is still at work.
const progressEvery = 5 * time.Second

// reportWhile runs fn and says every so often, on its own line, that it is still running, so a long start (a large
// -scale) is not mistaken for a hang. A fn that ends before the first interval prints nothing.
func reportWhile(out io.Writer, every time.Duration, what string, fn func() error) error {
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for n := 1; ; n++ {
			select {
			case <-done:
				return
			case <-ticker.C:
				fmt.Fprintf(out, "  %s (%s)...\n", what, time.Duration(n)*every)
			}
		}
	}()
	err := fn()
	close(done)
	<-finished
	return err
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
func demoURL(addr net.Addr, token string) string { return connectURL(addr, token, "demo=1") }

// connectURL is the address of the web UI that carries the token and a mark of the mode in its fragment.
func connectURL(addr net.Addr, token, mark string) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return ""
	}
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	u := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: "/"}
	return u.String() + "#/connect?token=" + token + "&" + mark
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
	announceWhere(out, demoURL(addr, token), token, webBuilt)
	if o.dataDir == "" {
		fmt.Fprintln(out, "The data is in memory and is gone when the demo stops. Press Ctrl+C to stop.")
	} else {
		fmt.Fprintf(out, "The data is kept in %s. Press Ctrl+C to stop.\n", o.dataDir)
	}
}

// announceWhere says where to go: the address of the web UI, or, for a build without it, the token and the commands
// of the command line that work as they are.
func announceWhere(out io.Writer, where, token string, webBuilt bool) {
	if webBuilt {
		fmt.Fprintln(out, "Open this address in a browser (the token is after the #, which a browser does not send to the server):")
		fmt.Fprintln(out, where)
		return
	}
	host := strings.SplitN(strings.TrimPrefix(where, "http://"), "/", 2)[0]
	fmt.Fprintln(out, "This build does not include the web UI. Build with scripts/build-with-web.sh, or use a release executable, to browse in a browser.")
	fmt.Fprintf(out, "The API and the command line work as they are. The token of this run is %s\n", token)
	fmt.Fprintf(out, "  praxium --endpoint http://%s --token %s kg list\n", host, token)
	fmt.Fprintf(out, "  praxium --endpoint http://%s --token %s query find --type PhysicalServer --limit 3\n", host, token)
}
