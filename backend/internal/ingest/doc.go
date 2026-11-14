// Package ingest is the ingestion pipeline of a vertical slice (ADR-0033): one file source, taken from
// collection to the KG in the stages the design documents name.
//
//	capture (Fetcher, an adapter port) -> parse -> raw record changes -> refine -> feed (mapping)
//	-> identify -> link -> apply (graph.Applier, then seal)
//
// Every stage is a function of its input: the same files, definitions and state give the same batch. The clock
// and the id generator come in from the kernel's ports; the package reads no file and no environment, because the
// bytes of a file arrive as a Capture that the composition root and an adapter produced.
//
// The definitions are data (JSON): a source with its datasets, and a mapping with refinements, feeds and link
// definitions. They are checked against the type registry when they are compiled, so a feed that names a property,
// an identity key or a link binding the standard model does not have never runs.
//
// What the slice leaves out is listed in ADR-0033: schedules, stream sources, push, most refinement operations,
// the mapping helpers, the safety devices and the ordering step that gives ingest_seq.
package ingest
