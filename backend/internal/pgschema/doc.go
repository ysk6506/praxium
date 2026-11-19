// Package pgschema plans the tables of one KG in PostgreSQL from the standard model (ADR-0085). It is pure: it
// turns a model registry and a few options into an ordered list of statements and a manifest, and it compares
// two manifests to find the steps that bring a stored schema up to a newer model. It knows neither the driver
// nor a database; adapter.pgstore runs the plan.
//
// One KG has one storage schema, s_<kg_no>, with a table per object type (t_<type_no>), a table of volatile
// values for the types that have them (s_<type_no>), and the fixed tables of the registry, the links, the
// history, the change records and the identity records. Every physical name is built from numbers, so a rename
// in the model never touches a table. The text that reads a value out of a document (expr.go) is made in one
// place, so an index, a statistic and a later view all spell it the same way and the planner can match them.
//
// Only additions can be followed: a new type, a new property, a new volatile property, a change of the
// search set. A change of a value type or of a number is refused, and so is anything that takes a column or a
// type away.
package pgschema
