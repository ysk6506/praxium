// Package pgcheck holds the acceptance checks of the new PostgreSQL storage (ADR-0089) that need more than one
// unit: the current ingestion and the current query engine run on it and on the memory store, and must agree.
// Units above the storage port may not import an adapter, so these checks live here. The package has no
// production code.
package pgcheck
