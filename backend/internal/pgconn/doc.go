// Package pgconn is the connection base of the PostgreSQL adapters (ADR-0083): the pool and its settings, the
// transaction helper that sets the time limits of a role, the numbered migrations and their runner, the check
// of how a connection string protects the link, and the helpers that give a test an empty database. It knows
// neither the graph nor the model: the adapters that implement a port (pgstore, later the others) import it and
// keep their own tables and statements.
//
// All the migration files of the shared schema live here, in one numbered line. The runner applies them in
// order, each in its own transaction under a transaction-level lock, and checks them by checksum, so a step
// that has been applied is never edited.
//
// The driver is github.com/lib/pq behind database/sql. Nothing here uses a session lock or LISTEN/NOTIFY, so a
// connection pool that gives out connections per transaction does not break it.
package pgconn
