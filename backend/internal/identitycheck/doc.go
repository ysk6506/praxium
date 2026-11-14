// Package identitycheck is the conformance suite of the identity Store port (ADR-0044): what any store of the
// identity data must do, as cases the store's own test runs. It is production code of its own because a non-test
// file may not import "testing"; it imports nothing but the kernel, and only its tests import the stores they check
// (the in-memory store, the file store and the PostgreSQL store), as graphcheck does for the storage port.
package identitycheck
