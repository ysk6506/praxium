// Package pgstore is the PostgreSQL implementation of the graph storage port (ADR-0032). It passes the
// conformance suite of the graph unit, as the in-memory reference does, and it is the store a server can keep
// its KGs in across restarts.
//
// Every write of a chunk runs in one transaction that locks the head row of its KG first, verifies the
// generation, the lease and the expected head, writes every row of the call, moves the head and, only with the
// last chunk of an apply, the readable_through that readers see. Readers take one read-only transaction per call
// at a fixed change number, so a reader sees the same state however much is applied after it.
//
// The change number is the counter of the head row, not a sequence. Nothing here uses a session lock or
// LISTEN/NOTIFY, so a connection pool that gives out connections per transaction does not break it. Every access
// to storage finds its storage by the KG id through the router (the answer is always the one database for now).
//
// The driver is github.com/lib/pq behind database/sql. The schema is made by the migrations in migrations/,
// which the runner in this package applies in order under a transaction-level lock and checks by checksum.
package pgstore
