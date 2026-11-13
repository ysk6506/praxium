// Package contract is the unit that will hold the shapes Praxium sends out:
// the HTTP routes and their schemas, the MCP tool shapes and the file formats
// (ADR-0016). The contract comes before the server, and the server is checked
// against it.
//
// The unit is registered and empty on purpose. It may read the kernel and
// nothing else; the units that build the answers read the contract, never the
// other way round.
package contract
