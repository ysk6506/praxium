// Command praxium is the one executable of the backend. It is the composition
// root: it chooses one of four roles, wires the kernel and starts the role.
// Nothing else assembles the program (ARCHITECTURE.md).
//
// Usage:
//
//	praxium <api|worker|query|collector>
//	praxium api serve [flags]          the REST API on net/http
//	praxium api routes --json          the routes the server registers
//	praxium contract assemble|check    the OpenAPI document of the API
//	praxium health | types | catalog   the command line client of the API
//	praxium worldgen [flags]
//	praxium ingest run -source f -mapping f -input dataset=file
//	praxium --version
//
// A bare role name wires the kernel, logs one structured line and exits; only
// api serve does work so far, the other roles come in later changes.
package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// version is set at build time: go build -ldflags "-X main.version=1.2.3".
var version = "dev"

const usage = "usage: praxium <api|worker|query|collector>\n       praxium worldgen [flags]\n       praxium --version\n"

// Exit codes of the command.
const (
	exitOK    = 0
	exitUsage = 2
)

// Role is what one process of the executable does.
type Role string

// The four roles (ARCHITECTURE.md). Several can run in one process in
// development; in a deployment each runs on its own.
const (
	RoleAPI       Role = "api"
	RoleWorker    Role = "worker"
	RoleQuery     Role = "query"
	RoleCollector Role = "collector"
)

// ParseRole accepts exactly the four role names.
func ParseRole(name string) (Role, error) {
	switch role := Role(name); role {
	case RoleAPI, RoleWorker, RoleQuery, RoleCollector:
		return role, nil
	}
	return "", kernel.Errorf(kernel.CodeInvalidValue, "unknown role %q: want api, worker, query or collector", name)
}

// deps is everything the kernel gives the roles. The tests pass fixed ones.
type deps struct {
	clock kernel.Clock
	ids   kernel.IDGenerator
}

func main() {
	os.Exit(execute(os.Args[1:], os.Stdout, os.Stderr, systemWiring()))
}

// run is main without the process: it returns the exit code.
func run(args []string, stdout, stderr io.Writer, d deps) int {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-version") {
		fmt.Fprintf(stdout, "praxium %s\n", version)
		return exitOK
	}
	if len(args) > 0 && args[0] == "worldgen" {
		return runWorldgen(args[1:], stdout, stderr, d)
	}
	if len(args) != 1 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	role, err := ParseRole(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "%v\n%s", err, usage)
		return exitUsage
	}
	logger(stderr).Info("praxium started",
		slog.String("role", string(role)),
		slog.String("version", version),
		slog.String("instance", d.ids.Next("inst")),
		slog.Time("at", d.clock.Now()),
	)
	return exitOK
}

// logger writes one JSON object per line. The handler's own timestamp is
// dropped: the time of an event comes from the Clock, so a log line is
// reproducible in tests.
func logger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
}
