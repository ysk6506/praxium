package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/filesource"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/xlsx"
)

// exitProblems is the exit code of `ingest run -fail-on-problem` when the run set problems aside (ADR-0041).
const exitProblems = 20

const ingestRunUsage = "usage: praxium ingest run -source <source.json> -mapping <mapping.json> -input <dataset>=<file> [-input ...] [-data-dir <folder>] [-fail-on-problem] [-times N]"

const ingestRunBody = `Reads files into a KG: capture, parse, raw record changes, refinement, feeds, identification, links and apply
(ADR-0033). The files are only read. The source definition says which datasets there are and how each file is parsed
(CSV, JSON Lines, or a sheet of an Excel workbook); the mapping definition says which objects and links they make.
Both are JSON files. The format of both, field by field, is in docs/guides/ingest-definitions.md ("praxium ingest init"
writes a copy of that guide, ingest-definitions.md, next to the examples, so you have it with the program). "praxium
ingest inspect -input <file> -emit-source" drafts the source definition of a file.

Without -data-dir the KG lives in memory only while this command runs. With -data-dir <folder> the KG is built in
that folder (the built-in file store) and what the run learned (which rows it saw, how often a row was missing) is
kept next to it, so the next run goes on from here: a row that stays away from complete runs is retired only after
several of them. Look at the result with: praxium local -data-dir <folder>. A folder that a server has open is locked:
stop the server and run again.

A dataset with no -input is not collected in this run and keeps what it had. A -input that names a dataset the source
does not define is an error.

Reading the output:
  ingest ...            one line per run. objects, links: what the run made or kept alive. volatile: values of
                        attributes that change all the time and keep no history. retired_objects, retired_links:
                        closed in this run. changed: whether the KG changed; new_version: whether a version was
                        sealed. version, graph_hash: the sealed version and a hash of the whole graph; the same
                        files give the same hash.
  dataset ...           one line per dataset: the rows of the file. received = read + unreadable. read = used +
                        filtered (by a rule of the mapping) + problem + unused (no feed uses the row).
                        raw_added, raw_modified, raw_unchanged, raw_deleted, raw_unseen: what changed against the
                        previous run (unseen: not in the file this time, not yet retired).
                        not_read="...": the file of this dataset was not read at all, and why (the workbook has no
                        such sheet, or a column the dataset declares is not in the header); every count is then 0,
                        and the same text is a shape_changed problem.
                        For a workbook: sheet and sheet_state (visible, hidden, very_hidden); hidden_rows_read and
                        hidden_columns_read: hidden rows and columns the mapping read; skipped_<reason>=N: rows that
                        were passed over and are no part of the counts above:
                          skipped_blank_row    the row has nothing in the columns that are read
                          skipped_hidden_row   the row is hidden and the sheet says skip_hidden_rows
                          skipped_after_end    the row is after the end of the data: stop_after_blank_rows blank
                                               rows came first, so what follows is not read
  problems kind=N       how many problems of each kind; problem ... lines list the first 20 with the row and why.
                        Kinds: shape_changed (see not_read), unreadable_line, duplicate_key, undeclared_column (a
                        column of the file no dataset column reads; ignore_undeclared_columns turns it off),
                        cell_error, formula_without_value (the cell has an error, or a formula never calculated),
                        invalid_value, missing_identity, link_target_missing, link_target_ambiguous, cardinality,
                        source_conflict, identity_conflict, incomplete_capture.
  last line             completed without problems, or completed with N problems (see "problem" lines above).

Exit codes:
  0    the run finished (also when it set problems aside; the problems are listed and counted on the last line)
  1    the run failed (a file or definition could not be read, the data folder is locked or does not fit, ...)
  2    the command line is wrong (including a -input for a dataset the source does not define)
  20   the run finished with problems and -fail-on-problem was given

example:
  praxium ingest init -out example
  praxium ingest run -data-dir data -source example/csv/source.json -mapping example/csv/mapping.json \
    -input ds_racks=example/csv/racks.csv -input ds_servers=example/csv/servers.csv
`

// inputList is the repeated -input <dataset>=<file>.
type inputList map[string]string

func (l inputList) String() string { return fmt.Sprint(map[string]string(l)) }

func (l inputList) Set(v string) error {
	ds, file, ok := strings.Cut(v, "=")
	if !ok || ds == "" || file == "" {
		return fmt.Errorf("-input wants <dataset>=<file>, got %q", v)
	}
	l[ds] = file
	return nil
}

// ingestFlags is what the command line of one ingestion run says.
type ingestFlags struct {
	source, mapping, dataDir string
	inputs                   inputList
	times                    int
	failOnProblem            bool
}

func newIngestRunFlags(f *ingestFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("ingest run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&f.source, "source", "", "the source definition (JSON): the datasets and how each file is parsed")
	fs.StringVar(&f.mapping, "mapping", "", "the mapping definition (JSON): which objects and links the datasets make")
	fs.Var(f.inputs, "input", "the file of a dataset, <dataset>=<file>; repeat it for every dataset")
	fs.StringVar(&f.dataDir, "data-dir", "", "keep the KG and the ingestion state in this folder (default: in memory, gone when the command ends)")
	fs.IntVar(&f.times, "times", 1, "run N times on the same files, to show that a second run changes nothing (1 to 20)")
	fs.BoolVar(&f.failOnProblem, "fail-on-problem", false, "exit with code 20 when the run set problems aside (default: exit 0)")
	return fs
}

// ingestHelpEntries are the help entries of this file's commands.
func ingestHelpEntries(out map[string]helpEntry) {
	out["ingest"] = helpEntry{usage: "usage: praxium ingest init|inspect|run ...", body: `Bring in your data: files in, a KG out.
  init      write example definitions, sample files and the guide to the definitions, to try the steps
  inspect   look at a file before writing a mapping for it (-emit-source drafts its source definition)
  run       read files into a KG (-data-dir keeps the result)
The two definition files (source.json: the datasets and how each file is read; mapping.json: the objects and links they
make) are described field by field in docs/guides/ingest-definitions.md; "praxium ingest init" writes a copy of it
(ingest-definitions.md) so it is with the program.
Run "praxium ingest <command> --help" for the flags.
`}
	out["ingest run"] = helpEntry{usage: ingestRunUsage, body: ingestRunBody, flags: func() *flag.FlagSet { return newIngestRunFlags(&ingestFlags{inputs: inputList{}}) }}
}

func parseIngestRun(args []string) (ingestFlags, error) {
	f := ingestFlags{inputs: inputList{}}
	fs := newIngestRunFlags(&f)
	if err := fs.Parse(args); err != nil {
		return f, flagProblem(err)
	}
	if fs.NArg() > 0 {
		return f, kernel.Errorf(kernel.CodeInvalidValue, "unexpected argument %q", fs.Arg(0))
	}
	if f.times < 1 || f.times > 20 {
		return f, kernel.Errorf(kernel.CodeInvalidValue, "-times wants a number from 1 to 20, got %d", f.times)
	}
	if f.source == "" || f.mapping == "" || len(f.inputs) == 0 {
		return f, kernel.Errorf(kernel.CodeInvalidValue, "ingest run needs -source, -mapping and at least one -input")
	}
	return f, nil
}

// inputProblem is a -input the source does not fit: a usage error, found before anything is read.
type inputProblem struct{ msg string }

func (e inputProblem) Error() string { return e.msg }

// runIngest is `praxium ingest ...`: run, inspect and init.
func runIngest(args []string, stdout, stderr io.Writer, w wiring) int {
	if len(args) >= 1 && args[0] == "inspect" {
		return runInspect(args[1:], stdout, stderr, w)
	}
	if len(args) >= 1 && args[0] == "init" {
		return runInit(args[1:], stdout, stderr)
	}
	if len(args) < 1 || args[0] != "run" {
		fmt.Fprint(stderr, "praxium: ingest needs init, inspect or run\n"+commandHelps()["ingest"].usage+"\nFor details: praxium ingest --help\n")
		return exitUsage
	}
	f, err := parseIngestRun(args[1:])
	if err != nil {
		return usageFail(stderr, err, "ingest run")
	}
	problems, err := ingestRun(f, stdout, stderr, w)
	var bad inputProblem
	switch {
	case errors.As(err, &bad):
		return usageFail(stderr, err, "ingest run")
	case err != nil:
		fmt.Fprintf(stderr, "praxium: %s\n", errText(err))
		return exitFailure
	case problems > 0 && f.failOnProblem:
		return exitProblems
	}
	return exitOK
}

// readDefinition reads a definition file whole.
func readDefinition(w wiring, path string) (io.ReadCloser, error) {
	in, err := openTruth(w, path)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read "+path)
	}
	return in, nil
}

func loadPlan(reg *model.Registry, f ingestFlags, w wiring) (*ingest.Plan, error) {
	sin, err := readDefinition(w, f.source)
	if err != nil {
		return nil, err
	}
	defer sin.Close()
	src, err := ingest.ParseSource(sin)
	if err != nil {
		return nil, err
	}
	min, err := readDefinition(w, f.mapping)
	if err != nil {
		return nil, err
	}
	defer min.Close()
	mp, err := ingest.ParseMapping(min)
	if err != nil {
		return nil, err
	}
	return ingest.Compile(reg, src, mp)
}

// checkInputs compares the -input datasets with the datasets the source defines: a name the source does not define
// is an error that lists the names it does; a dataset without a file is a note, since it is simply not collected.
func checkInputs(plan *ingest.Plan, inputs inputList) (notes []string, err error) {
	defined := map[string]bool{}
	var names []string
	for _, ds := range plan.Source.Datasets {
		defined[ds.ID] = true
		names = append(names, ds.ID)
	}
	var unknown []string
	for ds := range inputs {
		if !defined[ds] {
			unknown = append(unknown, ds)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		return nil, inputProblem{fmt.Sprintf("-input names %s, which source %s does not define; the datasets it defines are: %s",
			quoteAll(unknown), plan.Source.ID, strings.Join(names, ", "))}
	}
	for _, name := range names {
		if _, ok := inputs[name]; !ok {
			notes = append(notes, fmt.Sprintf("note: dataset %s has no -input, so it is not collected in this run (what it had before stays as it is)", name))
		}
	}
	return notes, nil
}

func quoteAll(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("dataset %q", n)
	}
	return strings.Join(quoted, ", ")
}

// ingestRun runs the pipeline as many times as asked and prints each run, then the last lines: whether there were
// problems and whether the result is kept. It returns the problems of the last run.
func ingestRun(f ingestFlags, stdout, stderr io.Writer, w wiring) (int, error) {
	ctx := context.Background()
	reg, err := model.LoadStandard()
	if err != nil {
		return 0, err
	}
	plan, err := loadPlan(reg, f, w)
	if err != nil {
		return 0, err
	}
	notes, err := checkInputs(plan, f.inputs)
	if err != nil {
		return 0, err
	}
	for _, n := range notes {
		fmt.Fprintln(stdout, n)
	}
	target, err := openIngestTarget(ctx, reg, w, f.dataDir, stderr)
	if err != nil {
		return 0, err
	}
	target.reg = reg
	problems, runErr := runPlan(ctx, f, plan, target, stdout, w)
	if err := target.close(); err != nil && runErr == nil {
		runErr = err
	}
	if runErr != nil {
		return 0, runErr
	}
	printEnd(stdout, problems, f.dataDir)
	return problems, nil
}

func runPlan(ctx context.Context, f ingestFlags, plan *ingest.Plan, t ingestTarget, stdout io.Writer, w wiring) (int, error) {
	opener := w.open
	if opener == nil {
		opener = func(p string) (io.ReadCloser, error) { return openTruth(w, p) }
	}
	runner := ingest.Runner{Store: t.store, Reg: t.reg, Clock: w.clock, IDs: w.ids, States: t.states,
		Fetcher: filesource.Source{Open: opener, Files: f.inputs}, Workbooks: xlsx.Reader{}}
	problems := 0
	for i := 0; i < f.times; i++ {
		rep, err := runner.Run(ctx, plan, t.kg.ID)
		if err != nil {
			return 0, err
		}
		printReport(stdout, rep)
		problems = countProblems(rep)
	}
	return problems, nil
}
