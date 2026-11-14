package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/ysk6506/praxium/backend/internal/devload"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

const localUsage = "usage: praxium local -data-dir <folder> [-addr host:port]"

const localBody = `Serves the KG you built from your own files, with the API and the web UI, on this computer (127.0.0.1 by default).
It opens the data folder of "praxium ingest run -data-dir <folder>", puts nothing made up into it, and prints an
address to open in a browser. The address carries a token made for this run (after the #, which the browser never
sends to the server). Press Ctrl+C to stop.

Local mode is for one person on their own computer. The sign-in is weak and it is not for production. The server holds
the data folder open, so run "praxium ingest run" for the same folder after you stop it.

If the folder holds no KG yet, it tells you to run "praxium ingest run -data-dir <folder> ..." first
("praxium ingest init" writes examples to start from).

example:
  praxium ingest init -out example
  praxium ingest run -data-dir data -source example/csv/source.json ...   (the commands "ingest init" prints)
  praxium local -data-dir data
`

type localOptions struct{ addr, dataDir string }

func newLocalFlags(o *localOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("local", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.addr, "addr", demoDefaultAddr, "where to listen, host:port; an address other than this computer's own is warned about")
	fs.StringVar(&o.dataDir, "data-dir", "", "the data folder that \"praxium ingest run -data-dir\" built (required)")
	return fs
}

// localHelpEntries are the help entries of this file's commands.
func localHelpEntries(out map[string]helpEntry) {
	out["local"] = helpEntry{usage: localUsage, body: localBody, flags: func() *flag.FlagSet { return newLocalFlags(&localOptions{}) }}
}

func parseLocal(args []string) (localOptions, error) {
	var o localOptions
	fs := newLocalFlags(&o)
	if err := fs.Parse(args); err != nil {
		return o, flagProblem(err)
	}
	if fs.NArg() != 0 {
		return o, kernel.Errorf(kernel.CodeInvalidValue, "local takes no arguments")
	}
	if err := checkAddr(o.addr); err != nil {
		return o, err
	}
	if o.dataDir == "" {
		return o, kernel.Errorf(kernel.CodeInvalidValue, "local needs -data-dir <folder>: the folder that praxium ingest run -data-dir built")
	}
	return o, nil
}

// runLocal is `praxium local`: the KG of a data folder, served with the web UI for the person who built it.
func runLocal(args []string, stdout, stderr io.Writer, w wiring) int {
	o, err := parseLocal(args)
	if err != nil {
		return usageFail(stderr, err, "local")
	}
	if err := serveLocal(o, stdout, stderr, w); err != nil {
		fmt.Fprintf(stderr, "praxium: %s\n", errText(err))
		return exitFailure
	}
	fmt.Fprintln(stdout, "praxium local stopped")
	return exitOK
}

// ingestHint is the way to a KG in a data folder, for the messages that find none.
func ingestHint(dir string) string {
	return fmt.Sprintf("first build one with: praxium ingest run -data-dir %s -source <source.json> -mapping <mapping.json> -input <dataset>=<file> (examples: praxium ingest init)", dir)
}

func serveLocal(o localOptions, stdout, stderr io.Writer, w wiring) error {
	if st, err := os.Stat(o.dataDir); err != nil || !st.IsDir() {
		return kernel.Errorf(kernel.CodeNotFound, "the data folder %s does not exist; %s", o.dataDir, ingestHint(o.dataDir))
	}
	return serveSession(session{
		addr: o.addr, dataDir: o.dataDir, warnAbout: "local-mode",
		load: func(ctx context.Context, store graph.Store, _ *model.Registry) (*devload.Report, error) {
			return nil, requireKG(ctx, store, o.dataDir)
		},
		announce: func(out io.Writer, addr net.Addr, token string, _ *devload.Report, built bool) {
			announceLocal(out, o, addr, token, built)
		},
	}, stdout, stderr, w)
}

// requireKG refuses a folder that holds no KG to serve, or one whose first load did not finish.
func requireKG(ctx context.Context, store graph.Store, dir string) error {
	kg, err := store.GetKG(ctx, graph.MainAlias)
	switch {
	case kernel.CodeOf(err) == graph.CodeKGNotFound:
		return kernel.Errorf(kernel.CodeNotFound, "the data folder %s holds no KG; %s", dir, ingestHint(dir))
	case err != nil:
		return err
	case kg.State == graph.StateBuilding:
		return kernel.Errorf(kernel.CodeInvalidValue, "the KG in %s was never completed (the first ingest run did not finish); run praxium ingest run again, or remove the folder", dir)
	}
	return nil
}

// announceLocal says what local mode is and where to go.
func announceLocal(out io.Writer, o localOptions, addr net.Addr, token string, webBuilt bool) {
	fmt.Fprintln(out, "Praxium local mode: for one person on their own computer. The sign-in is weak and this is not for production.")
	fmt.Fprintf(out, "Serving the KG kept in %s.\n", o.dataDir)
	announceWhere(out, connectURL(addr, token, "mode=local"), token, webBuilt)
	fmt.Fprintln(out, "Press Ctrl+C to stop. Stop it before you run praxium ingest run for this folder again.")
}
