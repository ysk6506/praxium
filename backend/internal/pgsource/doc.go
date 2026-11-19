// Package pgsource is the PostgreSQL adapter of the new ingestion flow (ADR-0093, sources-and-adapters.md 2.3 and 7.2).
// It reads the datasets of a source that someone else runs: a database it only reads, in which it opens no long
// transaction, changes no setting, and leaves nothing behind. It stands on the read port of core.ingest
// (PageReader, and the optional Previewer and Observer).
//
// A dataset is read page by page in the order of its natural key: every page is one short read-only transaction with
// its time limits set for that transaction alone, the statement is the table or the sql of the definition wrapped as a
// sub query, and what comes after a page is the last key (the token of the page). Between two pages nothing is held at
// the source, so a read that stops can go on from the token and a read that is slow never keeps the source's
// vacuum from running.
//
// Where the connection goes is decided by adapter.egress alone: the driver is given its dialer (names are resolved and
// judged there), and its TLS settings come from egress.TLSConfig. The driver reads nothing from the environment: no
// PG variable, no service file, no .pgpass and no certificate of the home folder reaches a connection, because the
// connection is made from a configuration that is filled in here and never from a connection string. The password is
// read from the opened secret in one place (credential.go), is used when the connection is made, and is in no error,
// no event and no sample. What the driver says in its own words is never passed on: a failure becomes an
// ingest.SourceError whose kind comes from the SQLSTATE or from the kind of the egress error.
package pgsource
