// Package filejournal is the file journal of the built-in file store (ADR-0034): an append-only log of records and
// periodic snapshots in one data folder, so that a process with one executable and one folder keeps the KG across
// restarts. It knows nothing of the graph: a record is bytes, a snapshot is a stream of bytes, and the in-memory store
// (adapter.memstore) decides what they hold.
//
// The folder is reached through the Disk interface, never through the operating system: the composition root gives the
// real one (the same way the file adapter of the ingestion pipeline is given an opener), and the tests give a fake
// disk that cuts a write at any byte, loses what was not synced and stops between a rename and the sync of the folder.
// The package imports no other adapter: it meets the store only through method signatures made of plain types.
package filejournal
