package main

import (
	"fmt"
	"io"
	"runtime"

	"github.com/ysk6506/praxium/backend/internal/webui"
)

// What a build knows about itself. A release build sets all of them (scripts/release-build.sh, ADR-0039):
//
//	go build -ldflags "-X main.version=0.1.0 -X main.commit=<sha> -X main.buildDate=<UTC time of the commit>"
//
// A build that sets none says "dev" and "unknown". version itself is declared in main.go.
var (
	commit    = "unknown"
	buildDate = "unknown"
)

// runVersion prints what `praxium version` shows: the version, the commit, the build date, the Go version and
// whether the web UI is inside. The build date is the commit's time, not the time of the build, so the same commit
// gives the same program (ADR-0039).
func runVersion(stdout io.Writer) int {
	ui := "not included (placeholder only)"
	if webui.Built(webui.Files()) {
		ui = "included"
	}
	fmt.Fprintf(stdout, "praxium %s\ncommit:  %s\nbuilt:   %s\ngo:      %s\nweb ui:  %s\n",
		version, commit, buildDate, runtime.Version(), ui)
	return exitOK
}
