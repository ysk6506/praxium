// Package kernel is the foundation every other unit may read. It holds typed
// IDs, errors with stable codes, the Clock, Rng and ID generator ports with
// their implementations, and the stable content hash.
//
// The kernel is the only unit that reads the wall clock or randomness
// (REQ-001). Those reads live in entropy.go; everything else receives a Clock
// or an Rng as an argument, so a run with the same inputs gives the same
// result. The kernel imports no other unit and no module outside the standard
// library.
package kernel
