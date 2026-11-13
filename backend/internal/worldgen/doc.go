// Package worldgen is the synthetic world generator (ADR-0021): a
// deterministic, seed-driven generator of an invented data-centre estate that
// conforms to the standard model. The world is the ground truth for tests,
// for the later steps (ingestion, queries, impact) and for scale validation
// without any real environment (query-engine-shape.md 6).
//
// One generation walks the estate once and hands every object and link to
// observers: the ground-truth writer (JSON Lines), the shape-card collector,
// and the renderers of the invented source systems (CSV tables, JSON pages,
// indented device configuration, fixed-layout command output, a change
// stream). Nothing is held for the whole world, so memory stays bounded at
// any scale. Everything derives from the injected kernel.Rng: the same
// parameters and the same Rng give byte-identical output.
//
// The unit imports only the kernel, the model and the standard library. It
// knows no file system: output goes to a Sink the caller provides.
package worldgen
