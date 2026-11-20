// Package pgingest is the PostgreSQL side of the new ingestion flow (ADR-0086, ADR-0098): the record of runs, the
// sources put to a KG, and the receiving step that stores the pages of a source into the receiving partition of that
// source. The tables are those of ADR-0095 and every name and column is taken from adapter.pgschema.
//
// A page is one transaction: the attempt of the run is checked first, the rows of the page go in with COPY, and the
// number of the page and the token to go on after it are written to the row of the run in the same transaction. A server
// that woke up late holds an attempt that another server has taken over, and its write is refused before it writes anything.
//
// COPY is the text format of the statement COPY ... FROM STDIN, which the driver sends when a statement that begins with
// COPY is prepared inside a transaction and executed once for each row. This unit does not import the driver: it makes the
// text of the statement itself and passes the cells as strings, bytes, numbers and nil, the way the driver reads them.
package pgingest
