package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/cli"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The help of the program (ADR-0041). `praxium`, `praxium help`, `praxium --help` and `praxium -h` print the same list
// of every command; `praxium <command> --help` and `praxium help <command>` print the help of one command. A wrong
// command line prints one line about the mistake, the usage line of the command and where the help is, never the
// whole list.

const topHelp = `Praxium turns the data of your IT infrastructure into a knowledge graph (KG) you can browse and ask.

Fastest start:  praxium demo     (a made-up data center and the web UI; nothing to set up)

Get started
  demo      Start a server with a synthetic data center and the web UI, to look around
  local     Serve the KG you built from your own files, with the web UI, on this computer

Look at the data (these ask a running server; see the global flags below)
  kg        List the KGs of the server and show one
  types     List and show the types of the model (object, link, action types)
  query     Find objects, get one, or walk the links around one
  object    Get one object, or its links, by id
  catalog   Show the catalogs of the API (layers, roles, error codes, ...)
  health    Check that the server is alive and ready

Bring in your data
  ingest init      Write example definitions and sample files (CSV and Excel) to try the steps
  ingest inspect   Look at a CSV, JSON Lines or Excel file before writing a mapping for it
  ingest run       Read files into a KG, with -data-dir so the result is kept

Run a server
  api serve    The REST API server (development mode only for now; see praxium api serve --help)
  api routes   List the routes the server registers

Tools
  worldgen   Generate a synthetic world (what praxium demo shows)
  contract   Assemble or check the OpenAPI contract of the API
  version    Show the version of this program

Use "praxium <command> --help" for the flags and examples of a command.

Global flags of the commands that ask a server (kg, types, query, object, catalog, health), before or after the command:
  --endpoint URL   the server (default http://127.0.0.1:8080; environment PRAXIUM_ENDPOINT)
  --token TOKEN    the access token (environment PRAXIUM_TOKEN); praxium demo and praxium local print theirs
  --config FILE    a JSON file {"endpoint": "...", "token": "..."} (environment PRAXIUM_CONFIG)
  --json           print JSON instead of a table

Not implemented yet: the roles worker and collector.
`

// helpEntry is what `praxium <command> --help` prints besides the flags of the command.
type helpEntry struct {
	usage string
	body  string
	// flags builds the flag set of the command, so the help lists the flags the command really has, with their
	// defaults; nil when the body lists them.
	flags func() *flag.FlagSet
}

// commandHelps builds the table of the help of every command; each file adds its own commands.
func commandHelps() map[string]helpEntry {
	out := map[string]helpEntry{}
	baseHelpEntries(out)
	apiHelpEntries(out)
	demoHelpEntries(out)
	ingestHelpEntries(out)
	initHelpEntries(out)
	inspectHelpEntries(out)
	localHelpEntries(out)
	return out
}

func baseHelpEntries(out map[string]helpEntry) {
	out["version"] = helpEntry{usage: "usage: praxium version", body: "Shows the version, the commit, the build date, the Go version and whether the web UI is inside this program.\n"}
	out["contract"] = helpEntry{usage: "usage: praxium contract assemble|check", body: `The OpenAPI contract of the API (docs/design-docs/rest-api.md).
  assemble   print the document the parts under backend/internal/contract/openapi/parts assemble to
  check      fail when the committed document is not what the parts assemble to
`}
	out["api"] = helpEntry{usage: "usage: praxium api serve|routes", body: `The REST API server.
  serve    run the server (praxium api serve --help)
  routes   list the routes the server registers (praxium api routes --json)
`}
	out["api routes"] = helpEntry{usage: "usage: praxium api routes --json", body: "Prints the routes the server registers as JSON, for the conformance check of the contract.\n"}
	out["worldgen"] = helpEntry{usage: "usage: praxium worldgen [flags]", body: worldgenHelp}
	out["dev"] = helpEntry{usage: "usage: praxium dev load-truth <truth.jsonl> [-batch N]", body: devHelp}
	out["dev load-truth"] = out["dev"]
}

// helpFlagWord says whether a word asks for help.
func helpFlagWord(word string) bool { return word == "-h" || word == "--help" || word == "-help" }

// commandWords are the words of a command line before its first flag.
func commandWords(args []string) []string {
	var words []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			break
		}
		words = append(words, a)
	}
	return words
}

// helpTextFor finds the help of the command the words name, by the longest match.
func helpTextFor(words []string) (string, bool) {
	for n := len(words); n > 0; n-- {
		path := strings.Join(words[:n], " ")
		if e, ok := commandHelps()[path]; ok {
			return renderHelp(e), true
		}
		if text, ok := cli.Help(words[:n]); ok {
			return text, true
		}
	}
	return "", false
}

func renderHelp(e helpEntry) string {
	var b strings.Builder
	b.WriteString(e.usage + "\n\n" + strings.TrimRight(e.body, "\n") + "\n")
	if e.flags != nil {
		fs := e.flags()
		fs.SetOutput(&b)
		b.WriteString("\nflags:\n")
		fs.PrintDefaults()
	}
	return b.String()
}

// usageFail is the answer to a command line the program cannot read: what is wrong, how the command is used and
// where to read more. It returns the usage exit code.
func usageFail(stderr io.Writer, problem error, command string) int {
	entry := commandHelps()[command]
	fmt.Fprintf(stderr, "praxium: %s\n%s\nFor details: praxium %s --help\n", errText(problem), entry.usage, command)
	return exitUsage
}

// flagProblem is a flag error of the standard library in the words of this program.
func flagProblem(err error) error {
	if name, ok := strings.CutPrefix(err.Error(), "flag provided but not defined: "); ok {
		return fmt.Errorf("unknown flag %s", name)
	}
	return err
}

// knownCommands are the first words of a command line that the program answers.
var knownCommands = map[string]bool{
	"demo": true, "local": true, "dev": true, "ingest": true, "api": true, "contract": true, "worldgen": true, "version": true,
	"worker": true, "collector": true, "health": true, "types": true, "catalog": true, "kg": true, "object": true, "query": true,
}

// runHelp answers the command lines that are about help, and the first words nothing answers. handled is false for
// every other command line.
func runHelp(args []string, stdout, stderr io.Writer) (code int, handled bool) {
	if len(args) == 0 {
		fmt.Fprint(stderr, topHelp)
		return exitUsage, true
	}
	if len(args) == 1 && helpFlagWord(args[0]) {
		fmt.Fprint(stdout, topHelp)
		return exitOK, true
	}
	if args[0] == "help" {
		return helpCommand(args[1:], stdout, stderr), true
	}
	if !knownCommands[args[0]] && !strings.HasPrefix(args[0], "-") {
		fmt.Fprintf(stderr, "praxium: unknown command %q\nFor the list of commands: praxium --help\n", args[0])
		return exitUsage, true
	}
	return commandHelp(args, stdout)
}

// helpCommand is `praxium help [command]`.
func helpCommand(words []string, stdout, stderr io.Writer) int {
	if len(words) == 0 {
		fmt.Fprint(stdout, topHelp)
		return exitOK
	}
	if text, ok := helpTextFor(words); ok {
		fmt.Fprint(stdout, text)
		return exitOK
	}
	fmt.Fprintf(stderr, "praxium: no help for %q\nFor the list of commands: praxium --help\n", strings.Join(words, " "))
	return exitUsage
}

// commandHelp answers `praxium <command> ... --help`. The commands that ask a server answer it themselves.
func commandHelp(args []string, stdout io.Writer) (int, bool) {
	asked := false
	for _, a := range args[1:] {
		asked = asked || helpFlagWord(a)
	}
	if !asked || cli.IsClientCommand(args[0]) {
		return 0, false
	}
	text, ok := helpTextFor(commandWords(args))
	if !ok {
		return 0, false
	}
	fmt.Fprint(stdout, text)
	if strings.Join(commandWords(args), " ") == "api serve" {
		// A test locked before this help existed (TestApiUsageStaysForUnknownSubcommands) asks for the failure code
		// of `api serve -h`; the help is printed all the same.
		return exitFailure, true
	}
	return exitOK, true
}

// runNotYet answers the roles that do nothing yet: they say so and fail, instead of logging a start and ending.
func runNotYet(args []string, stderr io.Writer) (int, bool) {
	if len(args) == 0 || (args[0] != "worker" && args[0] != "collector") {
		return 0, false
	}
	fmt.Fprintf(stderr, "praxium: %s is not implemented yet\nFor what Praxium can do: praxium --help\n", args[0])
	return exitFailure, true
}

// errText is an error in the words a person reads: the messages of the chain without the codes in front of them
// ("invalid_value: ..."), which are for logs and for the API.
func errText(err error) string {
	var ke *kernel.Error
	if !errors.As(err, &ke) {
		return err.Error()
	}
	if ke.Cause == nil {
		return ke.Message
	}
	return ke.Message + ": " + errText(ke.Cause)
}
