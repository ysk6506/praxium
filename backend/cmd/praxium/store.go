package main

import (
	"context"
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
		return openFile(o)
	}
	return nil, nil, kernel.Errorf(kernel.CodeInvalidValue, "unknown -store %q: use memory, postgres or file", o.store)
}

// openFile opens the built-in file store: the in-memory store with the file journal of the data folder under it.
// Only one process can have a folder open; a second one is refused.
func openFile(o serveOptions) (graph.Store, func() error, error) {
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
	return st, st.Close, nil
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
