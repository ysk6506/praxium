// Package egress is the one place where the server's own connections to a source leave the program
// (ADR-0091, sources-and-adapters.md section 8). The HTTP and the PostgreSQL source adapters take their
// dialer, their HTTP client and their TLS settings from here and nowhere else.
//
// The defence against server-side request forgery and DNS rebinding is one rule: the dialer resolves the name
// itself, judges every address the name resolved to, and connects to the literal address it judged. The name is
// never resolved a second time, so a name that answers differently the next time cannot move the connection to
// another place. If one address of the answer is refused, the whole answer is refused. The hook of the net
// package that sees the address at connect time needs the syscall package, which only the composition root may
// import, so it is not used.
//
// What the package does not offer is as deliberate as what it does: no proxy (the environment is not read),
// no way to turn off the verification of a certificate, no redirect that leaves the origin of the request, no
// log. A failure comes back as an *Error that names a kind and a fixed reason and carries neither the address
// that was refused, nor the query of a URL, nor the sentence of the standard library.
package egress
