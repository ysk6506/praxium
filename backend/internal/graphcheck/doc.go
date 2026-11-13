// Package graphcheck is the conformance suite of the KG storage port
// (ADR-0025): the cases every adapter of graph.Store must pass. The suite
// takes a TB, the small part of *testing.T it needs, so that no non-test file
// imports "testing"; an adapter's test runs Cases() as subtests against its
// own store. The suite is a unit of its own so that its coverage is measured
// by its own tests, which run it against the in-memory reference store.
package graphcheck
