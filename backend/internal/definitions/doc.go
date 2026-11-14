// Package definitions is the store of definitions that no one reads by kind (ADR-0049): items with a kind (an opaque
// string) and a normalized JSON body, immutable revisions, definition sets (D-n) with a content hash, the head of the
// main line that moves only by compare-and-swap, a ledger of numbers that never goes down, branches that hold only the
// items they change, proposals with their approvals, merging, and restoring an earlier set as a new proposal.
//
// What a body means is for other units to know. This unit imports neither the model nor the ingestion pipeline, so
// the first users (source and mapping definitions, organization attributes) and the ones after them share it. A write
// is one atomic step of the Store port: the revisions, the set, the ledger, the head and the state of a branch and its
// proposal all happen or none does, so a database adapter can make it one transaction. Time and new IDs come in
// through the Config; the same inputs give the same result.
package definitions
