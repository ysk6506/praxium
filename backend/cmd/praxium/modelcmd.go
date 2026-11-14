package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

const modelUsage = "usage: praxium model check|apply|show|export|history ..."

const modelBody = `Your own properties on the standard types (organization properties, docs/guides/org-model.md).
You write them in a file; the standard model stays as it is and the file adds properties to its types.
  check     read the file and say what is wrong and what would change; nothing is stored
  apply     check the file and store it in the data folder (a server must not have the folder open)
  show      the stored organization properties (of the newest definition set, or -set N)
  export    the stored properties as a file you can edit and apply again
  history   the definition sets, newest first: when, what was said, and how many properties changed
praxium ingest run, local, demo and api serve -store file read the stored properties when they start, so a change
takes effect when they are started again. Without a data folder, or with a folder that has none, the standard model
alone is used. Run "praxium model <command> --help" for the flags.
`

const (
	modelCheckUsage   = "usage: praxium model check -file <org-model.json> [-data-dir <folder>] [--json]"
	modelApplyUsage   = "usage: praxium model apply -file <org-model.json> -data-dir <folder> [-m <message>] [--json]"
	modelShowUsage    = "usage: praxium model show -data-dir <folder> [-set N] [--json]"
	modelExportUsage  = "usage: praxium model export -data-dir <folder> [-set N]"
	modelHistoryUsage = "usage: praxium model history -data-dir <folder> [-limit N] [--json]"
)

const modelCheckBody = `Reads the file, checks it against the standard model and, with -data-dir, against what is stored there. It stores
nothing and takes no lock, so it also works while a server has the folder open. All the errors are listed at once, then
the warnings, then what would change: properties added, compatible changes (display names, description, aliases,
enum values added, sensitivity raised, a property deprecated, a rename that keeps the old name as an alias) and
breaking changes, which apply refuses. Without -data-dir the file is checked against the standard model alone.
Exit codes: 0 the file is fine, 1 it has errors or breaking changes, 2 the command line is wrong.

example:
  praxium model check -file org-model.json -data-dir data
`

const modelApplyBody = `Checks the file as "model check" does and, when it has no errors and no breaking change, stores it in the data folder
as a new definition set, in one atomic write: the revisions, the set, the numbers given to new properties and the head.
Nothing is written when nothing changed, and apply says so. Breaking changes are refused with what to do instead; there is
no flag that forces them. The file says the whole state: a stored property the file leaves out is a breaking change (to
retire a property, set its status to deprecated).

The folder must not be open in another praxium (a server, local, demo or an ingest run): a server reads the definitions only
when it starts, so apply does not change them under it. Stop it, apply, and start it again.
Exit codes: 0 stored, or nothing to store; 1 refused or failed; 2 the command line is wrong.

example:
  praxium model apply -file org-model.json -data-dir data -m "add the department"
`

const modelShowBody = `Prints the stored organization properties: for each type or trait, its properties with their value types, status,
number and display names. Takes no lock. -set N shows an older definition set (see "model history").

example:
  praxium model show -data-dir data
`

const modelExportBody = `Prints the stored organization properties as a file, with the id and the number of every property. Applying that file
again changes nothing, so it is the way to rename a property (change "name"; the old name stays as an alias), to
retire one (status deprecated) or to go back to an earlier set (-set N, then apply). Takes no lock.

example:
  praxium model export -data-dir data > org-model.json
`

const modelHistoryBody = `Lists the definition sets, newest first: D-N, when, the hash (first 8 characters), the properties added and changed
against the set before, and the message of the apply. Takes no lock.

example:
  praxium model history -data-dir data
`

// modelFlags is what the command line of a model command says.
type modelFlags struct {
	file, dataDir, message, store, dsnFile string
	set                                    int64
	limit                                  int
	asJSON                                 bool
}

func newModelFlags(command string, f *modelFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("model "+command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if command == "check" || command == "apply" {
		fs.StringVar(&f.file, "file", "", "the file of organization properties (JSON)")
	}
	fs.StringVar(&f.dataDir, "data-dir", "", "the data folder that holds the definitions (definitions.json)")
	fs.StringVar(&f.store, "store", "file", "where the definitions are kept: file (the data folder); postgres is not there yet")
	fs.StringVar(&f.dsnFile, "store-dsn-file", "", "with -store postgres, the file that holds the connection string")
	if command == "apply" {
		fs.StringVar(&f.message, "m", "", "the message of this change, kept in the history")
	}
	if command == "show" || command == "export" {
		fs.Int64Var(&f.set, "set", 0, "the definition set to read, as a number (default: the newest)")
	}
	if command == "history" {
		fs.IntVar(&f.limit, "limit", 20, "the most sets to list")
	}
	fs.BoolVar(&f.asJSON, "json", false, "print JSON instead of text")
	return fs
}

// modelHelpEntries are the help entries of this file's commands.
func modelHelpEntries(out map[string]helpEntry) {
	entry := func(command, usage, body string) {
		out["model "+command] = helpEntry{usage: usage, body: body, flags: func() *flag.FlagSet { return newModelFlags(command, &modelFlags{}) }}
	}
	out["model"] = helpEntry{usage: modelUsage, body: modelBody}
	entry("check", modelCheckUsage, modelCheckBody)
	entry("apply", modelApplyUsage, modelApplyBody)
	entry("show", modelShowUsage, modelShowBody)
	entry("export", modelExportUsage, modelExportBody)
	entry("history", modelHistoryUsage, modelHistoryBody)
}

func parseModel(command string, args []string) (modelFlags, error) {
	var f modelFlags
	fs := newModelFlags(command, &f)
	if err := fs.Parse(args); err != nil {
		return f, flagProblem(err)
	}
	if fs.NArg() > 0 {
		return f, kernel.Errorf(kernel.CodeInvalidValue, "unexpected argument %q", fs.Arg(0))
	}
	switch f.store {
	case "file", "":
	case "postgres":
		return f, kernel.Errorf(kernel.CodeUnsupported, "the PostgreSQL store of the definitions is not there yet: use -data-dir with the built-in file store")
	default:
		return f, kernel.Errorf(kernel.CodeInvalidValue, "unknown -store %q: the definitions are kept in a data folder (-data-dir)", f.store)
	}
	if (command == "check" || command == "apply") && f.file == "" {
		return f, kernel.Errorf(kernel.CodeInvalidValue, "model %s needs -file <org-model.json>", command)
	}
	if command != "check" && f.dataDir == "" {
		return f, kernel.Errorf(kernel.CodeInvalidValue, "model %s needs -data-dir <folder>: the folder that holds the definitions", command)
	}
	if f.set < 0 || f.limit < 0 {
		return f, kernel.Errorf(kernel.CodeInvalidValue, "-set and -limit want a number from 1")
	}
	return f, nil
}

// runModel is `praxium model ...`.
func runModel(args []string, stdout, stderr io.Writer, w wiring) int {
	if len(args) == 0 || !strings.Contains(" check apply show export history ", " "+args[0]+" ") {
		return usageFail(stderr, kernel.Errorf(kernel.CodeInvalidValue, "model needs check, apply, show, export or history"), "model")
	}
	command := args[0]
	f, err := parseModel(command, args[1:])
	if err != nil {
		return usageFail(stderr, err, "model "+command)
	}
	var runErr error
	switch command {
	case "check":
		runErr = modelCheck(f, stdout, w)
	case "apply":
		runErr = modelApply(f, stdout, w)
	case "show":
		runErr = modelShow(f, stdout, w)
	case "export":
		runErr = modelExport(f, stdout, w)
	default:
		runErr = modelHistory(f, stdout, w)
	}
	if runErr != nil {
		fmt.Fprintf(stderr, "praxium: %s\n", errText(runErr))
		return exitFailure
	}
	return exitOK
}

// printJSON writes a value as indented JSON.
func printJSON(out io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(data))
	return err
}
