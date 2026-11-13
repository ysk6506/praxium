// Package memstore is the in-memory implementation of the graph storage port
// (ADR-0025). It is the reference the PostgreSQL adapter is written against
// and the store of tests and development mode. It passes the conformance
// suite of the graph unit. It keeps everything in process memory, loses it
// when the process ends and is not for production.
//
// One mutex guards the store, so every write is atomic and every read sees a
// whole state: that is what the PostgreSQL adapter gets from a transaction
// that locks the head row.
package memstore
