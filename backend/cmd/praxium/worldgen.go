package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/worldgen"
)

const worldgenHelp = `Generates a synthetic data center (a "world") from a seed: the truth of the world, and optionally views of it
as files, a stream of changes and a shape card (docs/design-docs/worldgen.md). praxium demo uses the same generator.
  praxium worldgen [flags]            generate a synthetic world
  praxium worldgen validate <truth.jsonl>
  praxium worldgen params             list the parameters

flags:
  -seed N          seed of the world (default 1)
  -scale N         about this many objects and links (default 100000)
  -out DIR         write files here (default: write nothing, only count)
  -set name=value  set any parameter (see "params"); may repeat
  -views           write the source views
  -changes N       plan N changes and write the change stream
  -shape           compute and write the shape card
  -shape-in FILE   set parameters from a shape card

example:
  praxium worldgen -scale 500 -seed 7 -out /tmp/world
`

// dirStore creates files under a directory and never outside it.
type dirStore struct{ root string }

func (d dirStore) Create(name string) (io.WriteCloser, error) {
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "file name %q leaves the output directory", name)
	}
	path := filepath.Join(d.root, clean)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.Create(path)
}

type setFlags []string

func (s *setFlags) String() string     { return strings.Join(*s, ",") }
func (s *setFlags) Set(v string) error { *s = append(*s, v); return nil }

// runWorldgen is the worldgen subcommand: it returns the exit code.
func runWorldgen(args []string, stdout, stderr io.Writer, d deps) int {
	if len(args) > 0 {
		switch args[0] {
		case "validate":
			return worldgenValidate(args[1:], stdout, stderr)
		case "params":
			fmt.Fprintln(stdout, strings.Join(worldgen.ParamNames(), "\n"))
			return exitOK
		}
	}
	code, err := worldgenRun(args, stdout, d)
	if err != nil {
		if code == exitUsage {
			return usageFail(stderr, err, "worldgen")
		}
		fmt.Fprintf(stderr, "worldgen: %v\n", err)
	}
	return code
}

func worldgenRun(args []string, stdout io.Writer, d deps) (int, error) {
	fs := flag.NewFlagSet("worldgen", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	seed := fs.Uint64("seed", 1, "")
	scale := fs.Int64("scale", 100000, "")
	out := fs.String("out", "", "")
	views := fs.Bool("views", false, "")
	changes := fs.Int("changes", 0, "")
	shape := fs.Bool("shape", false, "")
	shapeIn := fs.String("shape-in", "", "")
	var sets setFlags
	fs.Var(&sets, "set", "")
	if err := fs.Parse(args); err != nil {
		return exitUsage, flagProblem(err)
	}
	if fs.NArg() > 0 {
		return exitUsage, kernel.Errorf(kernel.CodeInvalidValue, "bad arguments %v", args)
	}
	p := worldgen.DefaultParams(*seed, *scale)
	p.Changes.Events = *changes
	if err := applyParams(&p, *shapeIn, sets, stdout); err != nil {
		return exitUsage, err
	}
	reg, err := model.LoadStandard()
	if err != nil {
		return 1, err
	}
	start := d.clock.Now()
	w, err := worldgen.Build(reg, p)
	if err != nil {
		return exitUsage, err
	}
	var st worldgen.Store = discard{}
	if *out != "" {
		st = dirStore{*out}
	}
	m, err := w.Run(st, worldgen.RunOptions{Truth: *out != "", Views: *views && *out != "", Changes: *changes > 0 && *out != "", Shape: *shape})
	if err != nil {
		return 1, err
	}
	elapsed := d.clock.Now().Sub(start)
	fmt.Fprintf(stdout, "worldgen seed=%d sites=%d racks=%d objects=%d links=%d content_hash=%s elapsed_ms=%d peak_rss_kb=%d\n",
		p.Seed, m.Layout.Sites, m.Layout.Racks, m.Counts.Objects, m.Counts.Links, m.ContentHash, elapsed.Milliseconds(), peakRSSKB())
	return exitOK, nil
}

// applyParams applies a shape card and then the -set values.
func applyParams(p *worldgen.Params, shapeIn string, sets setFlags, stdout io.Writer) error {
	if shapeIn != "" {
		raw, err := os.ReadFile(shapeIn)
		if err != nil {
			return err
		}
		var card worldgen.ShapeCard
		if err := json.Unmarshal(raw, &card); err != nil {
			return kernel.Errorf(kernel.CodeInvalidValue, "shape card %s: %v", shapeIn, err)
		}
		applied, unsupported := worldgen.ApplyShape(&card, p)
		for _, a := range applied {
			fmt.Fprintf(stdout, "shape: set %s\n", a)
		}
		for _, u := range unsupported {
			fmt.Fprintf(stdout, "shape: not supported: %s\n", u)
		}
	}
	for _, s := range sets {
		name, value, ok := strings.Cut(s, "=")
		if !ok {
			return kernel.Errorf(kernel.CodeInvalidValue, "-set wants name=value, got %q", s)
		}
		if err := p.Set(name, value); err != nil {
			return err
		}
	}
	return nil
}

type discard struct{}

func (discard) Create(string) (io.WriteCloser, error) { return nopCloser{io.Discard}, nil }

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

func worldgenValidate(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		return usageFail(stderr, kernel.Errorf(kernel.CodeInvalidValue, "validate wants one truth file"), "worldgen")
	}
	f, err := os.Open(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "worldgen: %v\n", err)
		return 1
	}
	defer f.Close()
	reg, err := model.LoadStandard()
	if err != nil {
		fmt.Fprintf(stderr, "worldgen: %v\n", err)
		return 1
	}
	rep, err := worldgen.Validate(reg, f)
	if err != nil {
		fmt.Fprintf(stderr, "worldgen: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "objects=%d links=%d problems=%d\n", rep.Objects, rep.Links, len(rep.Problems)+int(rep.Dropped))
	for _, p := range rep.Problems {
		fmt.Fprintln(stderr, p)
	}
	if !rep.OK() {
		return 1
	}
	return exitOK
}

// peakRSSKB reads the peak resident set size of the process (Linux), 0 if unknown.
func peakRSSKB() int64 {
	raw, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(line, "VmHWM:"); ok {
			n, _ := strconv.ParseInt(strings.Fields(rest)[0], 10, 64)
			return n
		}
	}
	return 0
}
