// Package identity is the built-in authentication of Praxium (ADR-0044): the principals (users and service
// accounts), the access tokens they hold and the service that turns a token into a subject. A token is a random
// secret with a public identifier; the server keeps only the SHA-256 of the secret, and the secret is shown once,
// when the token is issued. There are no passwords and no sessions.
//
// The data lives behind the small Store port, which knows only records of bytes with a version, so one port serves
// the file store, PostgreSQL and the in-memory store of the tests, and the adapters need not know this unit. The
// unit reads the kernel and access, and nothing that touches the outside world.
package identity
