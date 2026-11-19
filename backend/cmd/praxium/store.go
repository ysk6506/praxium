package main

import (
	"context"
	"flag"
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
	if o.storeReadDSNFile != "" && o.store != storePostgres {
		return nil, nil, kernel.Errorf(kernel.CodeInvalidValue, "-store-read-dsn-file belongs to -store postgres")
	}
	switch o.store {
	case storeMemory:
		if o.storeDSNFile != "" {
			return nil, nil, kernel.Errorf(kernel.CodeInvalidValue, "-store-dsn-file belongs to -store postgres")
		}
		return memstore.New(), func() error { return nil }, nil
	case storePostgres:
		warnDBTLS(o, d, log)
		st, closeStore, err := openPostgres(ctx, o, d)
		if err != nil {
			return nil, nil, err
		}
		resumeWork(ctx, st, log)
		return st, closeStore, nil
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
		return nil, nil, lockHint(err)
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
	cfg := pgstore.Config{DSN: strings.TrimSpace(string(raw)), MaxOpenConns: o.storeWriteConns, ReadConns: o.storeReadConns,
		QueryConns: o.storeQueryConns, NewConnsPerSecond: o.storeNewConns}
	if o.storeReadDSNFile != "" {
		readRaw, err := d.env.ReadFile(o.storeReadDSNFile)
		if err != nil {
			return nil, nil, kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read the read DSN file")
		}
		cfg.ReadDSN = strings.TrimSpace(string(readRaw))
	}
	st, err := pgstore.Open(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	return st, st.Close, nil
}

// workResumer is a store that can end the abandon or restore a server left half done (the PostgreSQL store).
type workResumer interface {
	ResumeAllWork(ctx context.Context) ([]graph.KGID, error)
}

// resumeWork ends the abandon or restore that a KG's head says a server left half done (ADR-0094): the KGs are
// whole before the server answers. A KG whose work cannot be ended is a warning, not a reason to stay down; it stays
// closed or refusing writes as its head says, and the next lease of it tries again.
func resumeWork(ctx context.Context, st graph.Store, log *slog.Logger) {
	r, ok := st.(workResumer)
	if !ok {
		return
	}
	resumed, err := r.ResumeAllWork(ctx)
	for _, kg := range resumed {
		log.Info("praxium ended the work a server left half done on a KG", slog.String("kg", kg.String()))
	}
	if err != nil {
		log.Warn("praxium could not end the work half done on a KG", slog.String("error", err.Error()))
	}
}

// lockHint says what to do about a data folder another process has open: stop that process. The folder holds one
// writer at a time (ADR-0034).
func lockHint(err error) error {
	if kernel.CodeOf(err) != filejournal.CodeLocked {
		return err
	}
	return kernel.Wrap(filejournal.CodeLocked, err, "another praxium has this data folder open (a server such as praxium local, or another ingest run); stop it and run this again")
}

// addPoolFlags adds the flags of the PostgreSQL pools to a command that opens the PostgreSQL store (ADR-0084).
func addPoolFlags(fs *flag.FlagSet, o *serveOptions) {
	fs.StringVar(&o.storeReadDSNFile, "store-read-dsn-file", "", "-store postgres: the file that holds the connection string of the read pool (default: the one of -store-dsn-file)")
	fs.IntVar(&o.storeWriteConns, "store-write-conns", 0, "-store postgres: the most connections of the write pool (default 8)")
	fs.IntVar(&o.storeReadConns, "store-read-conns", 0, "-store postgres: the most connections of the read pool (default 8)")
	fs.IntVar(&o.storeQueryConns, "store-query-conns", 0, "-store postgres: the most connections of the pool of free-form queries (default 4)")
	fs.IntVar(&o.storeNewConns, "store-new-conns-per-second", 0, "-store postgres: the most new connections all the pools open in a second (default 20)")
}
