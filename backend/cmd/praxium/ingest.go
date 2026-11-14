package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/devload"
	"github.com/ysk6506/praxium/backend/internal/filesource"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/memstore"
	"github.com/ysk6506/praxium/backend/internal/model"
)

const ingestUsage = `usage: praxium ingest run -source <source.json> -mapping <mapping.json> -input <dataset>=<file> [-input ...] [-times N]
  One ingestion run of a file source (ADR-0033): capture, parse, raw record changes, refinement, feeds,
  identification, links and apply, into a KG in a store that lives as long as this command. -times repeats
  the run on the same files in the same store to show that a second run changes nothing.
  The files are only read. The definitions are JSON data (docs/design-docs/ingestion.md).
`

// ingestFlags is what the command line of one ingestion run says.
type ingestFlags struct {
	source, mapping string
	inputs          map[string]string
	times           int
}

func parseIngestRun(args []string) (ingestFlags, error) {
	f := ingestFlags{inputs: map[string]string{}, times: 1}
	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) {
			return f, kernel.Errorf(kernel.CodeInvalidValue, "%s wants a value", args[i])
		}
		if err := f.set(args[i], args[i+1]); err != nil {
			return f, err
		}
	}
	if f.source == "" || f.mapping == "" || len(f.inputs) == 0 {
		return f, kernel.Errorf(kernel.CodeInvalidValue, "ingest run needs -source, -mapping and at least one -input")
	}
	return f, nil
}

// set takes one flag and its value.
func (f *ingestFlags) set(flag, v string) error {
	switch flag {
	case "-source":
		f.source = v
	case "-mapping":
		f.mapping = v
	case "-input":
		ds, file, ok := strings.Cut(v, "=")
		if !ok || ds == "" || file == "" {
			return kernel.Errorf(kernel.CodeInvalidValue, "-input wants <dataset>=<file>, got %q", v)
		}
		f.inputs[ds] = file
	case "-times":
		if _, err := fmt.Sscanf(v, "%d", &f.times); err != nil || f.times < 1 || f.times > 20 {
			return kernel.Errorf(kernel.CodeInvalidValue, "-times wants a number from 1 to 20, got %q", v)
		}
	default:
		return kernel.Errorf(kernel.CodeInvalidValue, "unexpected argument %q", flag)
	}
	return nil
}

// runIngest is `praxium ingest ...`: only `run` exists.
func runIngest(args []string, stdout, stderr io.Writer, w wiring) int {
	if len(args) < 1 || args[0] != "run" {
		fmt.Fprint(stderr, ingestUsage)
		return exitUsage
	}
	f, err := parseIngestRun(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "praxium: %v\n%s", err, ingestUsage)
		return exitUsage
	}
	if err := ingestRun(f, stdout, w); err != nil {
		fmt.Fprintf(stderr, "praxium: %v\n", err)
		return exitFailure
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

func ingestRun(f ingestFlags, stdout io.Writer, w wiring) error {
	ctx := context.Background()
	reg, err := model.LoadStandard()
	if err != nil {
		return err
	}
	plan, err := loadPlan(reg, f, w)
	if err != nil {
		return err
	}
	store := memstore.New()
	kg, err := devload.NewKG(ctx, store, reg, w.ids, w.clock, "ingested")
	if err != nil {
		return err
	}
	opener := w.open
	if opener == nil {
		opener = func(p string) (io.ReadCloser, error) { return openTruth(w, p) }
	}
	runner := ingest.Runner{Store: store, Reg: reg, Clock: w.clock, IDs: w.ids, States: ingest.NewMemStateStore(),
		Fetcher: filesource.Source{Open: opener, Files: f.inputs}}
	for i := 0; i < f.times; i++ {
		rep, err := runner.Run(ctx, plan, kg.ID)
		if err != nil {
			return err
		}
		printReport(stdout, rep)
	}
	return nil
}

// printReport prints one run: a line for the run, a line for each dataset and the problems it set aside.
func printReport(out io.Writer, r ingest.Report) {
	fmt.Fprintf(out, "ingest source=%s run=%d objects=%d links=%d volatile=%d retired_objects=%d retired_links=%d changed=%t new_version=%t version=%s graph_hash=%s\n",
		r.SourceID, r.Run, r.Objects, r.Links, r.VolatileValues, r.RetiredObjects, r.RetiredLinks, r.Changed, r.NewVersion, r.VersionID, r.GraphHash)
	for _, d := range r.Datasets {
		fmt.Fprintf(out, "  dataset %s collected=%t received=%d unreadable=%d read=%d used=%d filtered=%d problem=%d unused=%d raw_added=%d raw_modified=%d raw_unchanged=%d raw_deleted=%d raw_unseen=%d\n",
			d.Dataset, d.Collected, d.Received, d.Unreadable, d.Read, d.Used, d.Filtered, d.Problem, d.Unused,
			d.Raw.Added, d.Raw.Modified, d.Raw.Unchanged, d.Raw.Deleted, d.Raw.Unseen)
	}
	kinds := make([]string, 0, len(r.ProblemCounts))
	for k := range r.ProblemCounts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		fmt.Fprintf(out, "  problems %s=%d\n", k, r.ProblemCounts[k])
	}
	for i, p := range r.Problems {
		if i == 20 {
			fmt.Fprintf(out, "  ... %d more listed in the report\n", len(r.Problems)-i)
			break
		}
		fmt.Fprintf(out, "  problem %s dataset=%s key=%s line=%d: %s\n", p.Kind, p.Dataset, p.Key, p.Line, p.Reason)
	}
}
