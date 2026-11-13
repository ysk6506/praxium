// Package devauth is the development authenticator: one static token from the
// configuration mapped to one subject and its roles. It exists so that the API
// and the command line client can be tried before the identity backend exists.
// It is off unless a token is configured, it refuses to be built in a mode
// named production, and it is recorded as debt to be replaced by the OIDC
// authenticator (ADR-0012, ADR-0024).
package devauth
