// Package secrets is the secret store of Praxium (ADR-0071, ADR-0018): the passwords and tokens that the connections
// to the source systems need, kept with envelope encryption. The unit holds the rules and the cryptography; where the
// bytes live is behind the small Store port, and where the master key comes from is behind KeyProvider.
//
// A value is written and never read back. Three ports split what may be done (Service.Writer, Opener and Describer give them out as separate values): Writer (create, new version, retire,
// delete, change the scope or the allowed addresses), Opener (decrypt, the one place that checks the use) and
// Describer (names, kinds, versions, scope and addresses, never a value). A caller gets only the ports it needs.
//
// Every version has its own 32-byte data key. The data key encrypts the fields once with AES-256-GCM and is itself
// wrapped by the master key with AES-256-GCM; the authenticated data bind each ciphertext to the format, the secret
// and the version (and, for the wrapped key, the master key id). The scope (the source ids that may use the secret)
// and the allowed addresses, with the whole list of versions (which exist, which are retired, their ciphertext), are
// bound by one authentication tag under the master key, so a record edited by hand or put together from pieces of
// different times does not open. Only a copy of the whole record from an earlier time still opens. The unit reads randomness only through kernel.ByteSource and imports nothing but the kernel
// and the standard library.
//
// Deleting a secret or retiring a version does not ask whether a source still refers to it: the caller checks that
// first (a later change joins the sources to this unit). Nothing here reads the environment, a file or the clock.
package secrets
