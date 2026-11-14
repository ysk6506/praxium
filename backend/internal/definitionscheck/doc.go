// Package definitionscheck is the conformance suite of the definitions store (ADR-0049): the cases that every store
// must pass, so that the memory store, the file store and the database store that comes later all keep the same rules.
// It is production code of its own because it cannot import "testing"; only its tests import the stores it is run on.
package definitionscheck
