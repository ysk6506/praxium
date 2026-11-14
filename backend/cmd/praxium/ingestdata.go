package main

import (
	"context"
	"io"
	"log/slog"

	"github.com/ysk6506/praxium/backend/internal/devload"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/memstore"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// mainKGName is the name of the KG that ingestion builds in a data folder. Every reader asks for the alias main,
// which is the production KG, so the name is the alias (ADR-0041).
const mainKGName = "main"

// ingestTarget is where a run writes: the store with the KG, and the state the pipeline keeps between runs.
type ingestTarget struct {
	store  graph.Store
	kg     graph.KG
	states ingest.StateStore
	close  func() error
	reg    *model.Registry // set by the run, so the plan step takes few parameters
}

// openIngestTarget gives a run its KG. Without a data folder the store and the state live in memory as long as the
// command. With one, the KG is in the folder's file store (ADR-0034) and the ingestion state is the file next to it.
func openIngestTarget(ctx context.Context, reg *model.Registry, w wiring, dataDir string, stderr io.Writer) (ingestTarget, error) {
	if dataDir == "" {
		store := memstore.New()
		kg, err := devload.NewKG(ctx, store, reg, w.ids, w.clock, mainKGName)
		return ingestTarget{store: store, kg: kg, states: ingest.NewMemStateStore(), close: func() error { return nil }}, err
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	store, closeStore, err := openFile(serveOptions{store: storeFile, storeDataDir: dataDir}, log)
	if err != nil {
		return ingestTarget{}, err
	}
	t, err := bindFolder(ctx, reg, w, store, dataDir)
	if err != nil {
		_ = closeStore()
		return ingestTarget{}, err
	}
	t.close = closeStore
	return t, nil
}

// bindFolder finds or makes the KG of the folder and the state that belongs to it. The two must fit: a KG and a state
// that never met (a folder of praxium demo, or a state file that was deleted), a state without its KG, and a state of
// another KG are refused before anything is changed, since going on would make objects twice or retire the wrong ones.
func bindFolder(ctx context.Context, reg *model.Registry, w wiring, store graph.Store, dir string) (ingestTarget, error) {
	disk, err := openDataDir(dir)
	if err != nil {
		return ingestTarget{}, err
	}
	states, err := ingest.OpenFileStateStore(stateDisk{disk})
	if err != nil {
		return ingestTarget{}, err
	}
	kg, err := store.GetKG(ctx, graph.MainAlias)
	switch {
	case err == nil && states.KG() == "":
		return ingestTarget{}, kernel.Errorf(kernel.CodeConflict,
			"%s holds a KG that praxium ingest did not build (a folder of praxium demo, or %s was removed); ingest into an empty folder", dir, ingest.StateFileName)
	case err == nil && states.KG() != kg.ID.String():
		return ingestTarget{}, kernel.Errorf(kernel.CodeConflict, "%s belongs to KG %s but the folder holds KG %s; the folder does not fit its state, use another folder", ingest.StateFileName, states.KG(), kg.ID)
	case err == nil:
		return ingestTarget{store: store, kg: kg, states: states}, nil
	case kernel.CodeOf(err) != graph.CodeKGNotFound:
		return ingestTarget{}, err
	case states.KG() != "":
		return ingestTarget{}, kernel.Errorf(kernel.CodeConflict, "%s remembers KG %s but the folder holds no KG (its data was removed); remove %s to start over", ingest.StateFileName, states.KG(), ingest.StateFileName)
	}
	kg, err = devload.NewKG(ctx, store, reg, w.ids, w.clock, mainKGName)
	if err != nil {
		return ingestTarget{}, err
	}
	if err := states.Bind(kg.ID.String()); err != nil {
		return ingestTarget{}, err
	}
	return ingestTarget{store: store, kg: kg, states: states}, nil
}
