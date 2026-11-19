// Package sourcecheck has tests only: the acceptance of the source adapters against a source that is not honest, and
// against TLS that works (ADR-0093). The tests open sockets (a PostgreSQL server that sends the bytes a test chooses,
// and a TLS terminator in front of the test server), which no unit that has product code may do, so they are here,
// in files that name what they import in architecture.yaml; this package has no product code and no unit imports it.
package sourcecheck
