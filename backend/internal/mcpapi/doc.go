// Package mcpapi is the MCP adapter of the read-only KG interface (ADR-0062, ADR-0068, kg-mcp.md): POST /mcp, the JSON-RPC
// layer, the handling of the three protocol versions the server speaks, the checks on where a request comes from and
// who sends it, and the registry of tools. It only carries: who may call, what a tool answers and the codes of a
// refusal are the use case's (core.kgread), so REST and MCP judge alike. The registry (tools.go and the files of the
// tools) knows nothing of HTTP and JSON-RPC; the transport files know nothing of what a tool does.
//
// The server is stateless: it makes no session, remembers no protocol version and answers with JSON only.
package mcpapi
