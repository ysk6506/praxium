// Package httpapi is the HTTP adapter of the API role: it serves the REST
// contract with the standard library's net/http and nothing else. The routes
// are one table (routes.go) that the contract's operations must equal; the
// handlers turn registry and catalogue data into the answer envelope; the
// middleware gives every request a trace id, a structured access log, a body
// limit, content negotiation and panic recovery; every error leaves as
// problem+json with a code of the contract's closed catalogue.
//
// The package builds no program: the composition root (cmd/praxium) wires an
// Authenticator, the type registry, a clock and a logger into a Server and
// owns the listener, signals and the end of the process.
package httpapi
