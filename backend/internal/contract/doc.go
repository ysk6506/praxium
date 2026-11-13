// Package contract is the unit that holds the shapes Praxium sends out: the
// REST contract (an OpenAPI 3.1 document in JSON), the closed catalogues the
// contract names (problem codes, reason codes), the registry of planned
// routes, and the small checker that validates a response against the
// contract (ADR-0016, ADR-0024).
//
// The contract comes before the server. The document is written as several
// JSON files under openapi/parts, assembled deterministically into
// openapi/openapi.json by Assemble (the command `praxium contract assemble`),
// and served as it is. The assembled file is a build product: it is never
// edited by hand, and a test and a structure check fail when it differs from
// what the parts assemble to.
//
// The unit may read the kernel and nothing else; the units that build the
// answers read the contract, never the other way round.
package contract
