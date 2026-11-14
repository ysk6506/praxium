package main

import (
	"context"
	"log/slog"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/filejournal"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/memstore"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// The stores `api serve` can use (-store).
const (
	storeMemory   = "memory"
	storePostgres = "postgres"
	storeFile     = "file"
)

// openStore makes the store the server reads. The memory store is the default and lives as long as the process.
// The PostgreSQL store takes its DSN from a file, never from an argument or the environment, so the secret is in
// no process list and no log; the DSN is not part of any message here. The file store keeps the KG in a data folder
// (-store-data-dir) by an append-only log and snapshots (ADR-0034). The returned function closes the store; for the
// file store that takes the last snapshot, so its error matters.
func openStore(ctx context.Context, o serveOptions, d wiring) (graph.Store, func() error, error) {
	return openStoreLogged(ctx, o, d, slog.New(slog.DiscardHandler))
}

// openStoreLogged is openStore with a log for what the recovery of a file store did.
func openStoreLogged(ctx context.Context, o serveOptions, d wiring, log *slog.Logger) (graph.Store, func() error, error) {
	if o.storeDataDir != "" && o.store != storeFile {
		return nil, nil, kernel.Errorf(kernel.CodeInvalidValue, "-store-data-dir belongs to -store file")
	}
	switch o.store {
	case storeMemory:
		if o.storeDSNFile != "" {
			return nil, nil, kernel.Errorf(kernel.CodeInvalidValue, "-store-dsn-file belongs to -store postgres")
		}
		return memstore.New(), func() error { return nil }, nil
	case storePostgres:
		return openPostgres(ctx, o, d)
	case storeFile:
		return openFile(o, log)
	}
	return nil, nil, kernel.Errorf(kernel.CodeInvalidValue, "unknown -store %q: use memory, postgres or file", o.store)
}

// openFile opens the built-in file store: the in-memory store with the file journal of the data folder under it.
// Only one process can have a folder open; a second one is refused.
func openFile(o serveOptions, log *slog.Logger) (graph.Store, func() error, error) {
	if o.storeDSNFile != "" {
		return nil, nil, kernel.Errorf(kernel.CodeInvalidValue, "-store-dsn-file belongs to -store postgres")
	}
	if o.storeDataDir == "" {
		return nil, nil, kernel.Errorf(kernel.CodeInvalidValue, "-store file needs -store-data-dir: the folder that holds the data")
	}
	disk, err := openDataDir(o.storeDataDir)
	if err != nil {
		return nil, nil, err
	}
	j, err := filejournal.Open(disk)
	if err != nil {
		return nil, nil, err
	}
	st, err := memstore.Open(j, memstore.Options{})
	if err != nil {
		return nil, nil, err
	}
	logRecovery(log, j.Report())
	return st, st.Close, nil
}

// logRecovery says what the recovery of the data folder did. A dropped tail and a fall back to an older snapshot
// are warnings: the first is a last record nobody was told had worked (or one damaged on disk), the second means
// the newest snapshot was damaged.
func logRecovery(log *slog.Logger, rep filejournal.Report) {
	log.Info("praxium file store recovered", slog.Uint64("snapshot", rep.SnapshotSeq),
		slog.Int64("replayed", int64(rep.Replayed)), slog.Uint64("last_record", rep.LastRecord))
	if rep.DroppedTail {
		log.Warn("praxium file store dropped the last record: it was cut and no answer had been returned for it",
			slog.Int64("dropped_bytes", rep.DroppedBytes))
	}
	if len(rep.SkippedSnapshots) > 0 {
		log.Warn("praxium file store recovered from an older snapshot: the newer one is damaged",
			slog.String("skipped", strings.Join(rep.SkippedSnapshots, ",")))
	}
}

func openPostgres(ctx context.Context, o serveOptions, d wiring) (graph.Store, func() error, error) {
	if o.storeDSNFile == "" {
		return nil, nil, kernel.Errorf(kernel.CodeInvalidValue, "-store postgres needs -store-dsn-file: the file that holds the connection string")
	}
	raw, err := d.env.ReadFile(o.storeDSNFile)
	if err != nil {
		return nil, nil, kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read the DSN file")
	}
	st, err := pgstore.Open(ctx, pgstore.Config{DSN: strings.TrimSpace(string(raw))})
	if err != nil {
		return nil, nil, err
	}
	return st, st.Close, nil
}
