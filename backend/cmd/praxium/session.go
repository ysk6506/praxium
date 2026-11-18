package main

import (
	"context"
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
)

// The defaults of `praxium local` (ADR-0041).
const (
	localDefaultAddr = "127.0.0.1:8080"
	localSubject     = "local-user"
	// localTokenBytes is the size of the random token: 16 bytes, 32 hex characters.
	localTokenBytes = 16
)

// checkAddr says what is wrong with a listen address that is not host:port, before anything else is said about it.
func checkAddr(addr string) error {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return kernel.Errorf(kernel.CodeInvalidValue, "-addr wants host:port, such as 127.0.0.1:8080, got %q", addr)
	}
	return nil
}

// session is a development server on this computer with a token made for the run, and the address to open that
// carries it (ADR-0041). `praxium local` is its one user; what it loads and what it says are the parts it is given.
type session struct {
	addr, dataDir string
	mcp           mcpOptions
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
	token := kernel.NewToken(localTokenBytes)
	authn, err := devauth.New(devauth.Config{
		Mode: devauth.ModeDevelopment, Token: token, SubjectID: localSubject, Roles: []access.Role{access.RoleViewer},
	})
	if err != nil {
		return err
	}
	so := serveOptions{addr: s.addr, mode: devauth.ModeDevelopment, store: storeMemory, web: true, mcp: s.mcp}
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

// connectURL is the address to open: the token and the mark of local mode sit in the fragment, which the browser
// never sends, so no server log has it. The web UI takes them and replaces the address (ADR-0042).
func connectURL(addr net.Addr, token string) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return ""
	}
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	u := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: "/"}
	return u.String() + "#/connect?token=" + token + "&mode=local"
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
