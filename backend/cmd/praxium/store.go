package main

import (
	"context"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/memstore"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// The stores `api serve` can use (-store).
const (
	storeMemory   = "memory"
	storePostgres = "postgres"
)

// openStore makes the store the server reads. The memory store is the default and lives as long as the process.
// The PostgreSQL store takes its DSN from a file, never from an argument or the environment, so the secret is in
// no process list and no log; the DSN is not part of any message here. The returned function closes the store.
func openStore(ctx context.Context, o serveOptions, d wiring) (graph.Store, func(), error) {
	switch o.store {
	case storeMemory:
		if o.storeDSNFile != "" {
			return nil, nil, kernel.Errorf(kernel.CodeInvalidValue, "-store-dsn-file belongs to -store postgres")
		}
		return memstore.New(), func() {}, nil
	case storePostgres:
		return openPostgres(ctx, o, d)
	}
	return nil, nil, kernel.Errorf(kernel.CodeInvalidValue, "unknown -store %q: use memory or postgres", o.store)
}

func openPostgres(ctx context.Context, o serveOptions, d wiring) (graph.Store, func(), error) {
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
	return st, func() { _ = st.Close() }, nil
}
