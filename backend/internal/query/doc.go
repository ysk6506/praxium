// Package query is the reference implementation of the first three closed
// queries over the KG storage port: find, get and neighbors (kg-queries.md 3,
// ADR-0026). It reads one snapshot of a KG through graph.Reader, filters every
// answer through a read-permission port (access-control.md 7), and wraps every
// answer in the envelope of kg-queries.md 4. It is the simple, slow and
// obvious version: it scans the snapshot instead of using indexes. A faster
// engine must give the same answers.
//
// The package knows no network, no clock of its own (the kernel.Clock is
// injected) and no storage product; it imports the kernel, the model and the
// graph unit only. Its tests also build a world in the in-memory store.
package query
