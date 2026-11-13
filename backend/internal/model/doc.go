// Package model is the type registry and the standard model as data (ADR-0007,
// ADR-0020). It defines what a definition of the model is: namespaces, layers,
// object types with their properties and identity keys, value types, traits,
// link types with the closed meanings and their bindings, action types, and
// the closed lists that the rest of the product reads (channels, evidence
// grades, bases). It checks every rule the design documents state and builds
// an immutable Registry that answers lookups by id, by stable number and by
// name.
//
// The unit has no storage and no HTTP. The standard model is the JSON files in
// the standard folder, embedded in the program; Standard loads and checks them.
// Definitions that add org or ext types go through NewRegistry on top of the
// same rules. The unit imports only the kernel and the standard library.
package model
