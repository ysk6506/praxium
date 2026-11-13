// Package cli is the command line client of the API (rest-api.md 5-5). It
// speaks only HTTP to a running server, through the same contract every other
// client uses, and never reads the server's code or the database. It reads
// no environment itself: the composition root passes a lookup function and a
// file reader, so tests give fixed ones.
//
// The commands of this step are health, types (list and get) and catalog. The
// output is a table for people or JSON (--json) for scripts, and the exit
// code says what kind of problem the server answered (ExitCode).
package cli
