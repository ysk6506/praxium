// Package readcheck holds the checks of the read path that need a store with
// history: reads at an older change number or version, waiting for a change
// number, cursors that outlive a restore, and the HTTP routes over the same
// store (ADR-0027). Units above the storage port may not import an adapter, so
// their own tests run on graph.ScratchStore, which keeps the latest state only;
// what needs the reference store of adapter.memstore is tested here, where
// both are allowed. The package has no production code.
package readcheck
