package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ysk6506/praxium/backend/internal/devload"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/memstore"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// devKGName is the name of the production KG a development server creates when it loads a truth file.
const devKGName = "dev"

const devUsage = `usage: praxium dev load-truth <truth.jsonl> [-batch N]
  A developer convenience and test path, NOT the ingestion pipeline: it applies the ground truth of
  "praxium worldgen" to a KG in a store that lives as long as this command, and prints what it loaded.
  To read the KG, start the API with: praxium api serve -mode development -dev-truth <truth.jsonl>
`

// openTruth opens a truth file: the wiring's opener, else the whole file from the environment's reader.
func openTruth(w wiring, path string) (io.ReadCloser, error) {
	if w.open != nil {
		return w.open(path)
	}
	data, err := w.env.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// systemOpen opens a real file for reading.
func systemOpen(path string) (io.ReadCloser, error) { return os.Open(path) }

// loadDevStore creates the in-memory store of a development server and, when a truth file is named, a production
// KG filled from it. Without a file the store is empty: there is no KG to read until one is loaded.
func loadDevStore(ctx context.Context, reg *model.Registry, w wiring, truth string, batch int) (*memstore.Store, *devload.Report, error) {
	store := memstore.New()
	if truth == "" {
		return store, nil, nil
	}
	in, err := openTruth(w, truth)
	if err != nil {
		return nil, nil, kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read the truth file")
	}
	defer in.Close()
	kg, err := devload.NewKG(ctx, store, reg, w.ids, w.clock, devKGName)
	if err != nil {
		return nil, nil, err
	}
	rep, err := devload.Loader{Store: store, Reg: reg, Clock: w.clock, IDs: w.ids, BatchItems: batch}.Load(ctx, kg.ID, in)
	if err != nil {
		return nil, nil, err
	}
	return store, &rep, nil
}

// reportLine is the one line a load prints.
func reportLine(rep devload.Report) string {
	return fmt.Sprintf("devload objects=%d links=%d volatile=%d identifiers=%d not_ingestible_objects=%d not_ingestible_links=%d "+
		"unkeyed=%d dangling_links=%d batches=%d chunks=%d change_seq=%d version=%s graph_hash=%s",
		rep.Objects, rep.Links, rep.VolatileValues, rep.Identifiers, rep.ObjectsNotIngestible, rep.LinksNotIngestible,
		rep.ObjectsUnkeyed, rep.LinksDangling, rep.Batches, rep.Chunks, rep.ChangeSeq, rep.VersionID, rep.GraphHash)
}

// runDev is `praxium dev ...`: the developer commands. Only load-truth exists.
func runDev(args []string, stdout, stderr io.Writer, w wiring) int {
	if len(args) < 2 || args[0] != "load-truth" {
		fmt.Fprint(stderr, devUsage)
		return exitUsage
	}
	file, batch, err := parseLoadTruth(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "praxium: %v\n%s", err, devUsage)
		return exitUsage
	}
	reg, err := model.LoadStandard()
	if err != nil {
		fmt.Fprintf(stderr, "praxium: %v\n", err)
		return exitFailure
	}
	start := w.clock.Now()
	_, rep, err := loadDevStore(context.Background(), reg, w, file, batch)
	if err != nil {
		fmt.Fprintf(stderr, "praxium: %v\n", err)
		return exitFailure
	}
	fmt.Fprintf(stdout, "%s elapsed_ms=%d peak_rss_kb=%d\n", reportLine(*rep), w.clock.Now().Sub(start).Milliseconds(), peakRSSKB())
	return exitOK
}

func parseLoadTruth(args []string) (file string, batch int, err error) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-batch" && i+1 < len(args):
			if _, scanErr := fmt.Sscanf(args[i+1], "%d", &batch); scanErr != nil || batch < 1 {
				return "", 0, kernel.Errorf(kernel.CodeInvalidValue, "-batch wants a number of at least 1, got %q", args[i+1])
			}
			i++
		case file == "" && len(args[i]) > 0 && args[i][0] != '-':
			file = args[i]
		default:
			return "", 0, kernel.Errorf(kernel.CodeInvalidValue, "unexpected argument %q", args[i])
		}
	}
	if file == "" {
		return "", 0, kernel.Errorf(kernel.CodeInvalidValue, "load-truth needs a truth file")
	}
	return file, batch, nil
}
