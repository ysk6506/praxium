// Package graph is the knowledge-graph core (ADR-0009, ADR-0025). A KG is a
// sequence of applied change batches: objects and links with the interval of
// change numbers in which each row was valid, the volatile values beside them,
// the identifier history, and the closed list of ordering events. A version
// is a sealed point on that sequence.
//
// The unit holds the domain (ids, rows, batches, validation against the type
// registry, the content hash and the graph hash that is kept incrementally),
// the applier that turns one batch into one or more atomic chunks, and the
// storage port (Catalog, Writer, Reader, Feed). It imports only the kernel
// and the model. The store persists one chunk atomically; every decision of
// what a chunk contains is made here, so every adapter gives the same ids,
// numbers, hashes and version ids for the same sequence of batches.
//
// Adapters are checked by the conformance suite in the conformance folder.
package graph
