// Package kgread is the use case of reading a KG, apart from any transport (ADR-0062, kg-mcp.md 8). It takes the caller
// as a subject and the question as raw JSON (or as the typed input of a query) and answers with a result or with the
// code of the catalogue the refusal becomes. The REST adapter and the MCP adapter that follows it both call it, so a
// question has one reading and one set of codes. It writes no HTTP and imports no adapter: where the caller comes from,
// the headers, the query string and the status of an answer stay with the adapters.
package kgread
