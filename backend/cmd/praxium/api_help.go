package main

import "flag"

const apiServeUsage = "usage: praxium api serve [-addr host:port] [-mode production|development] [-store file|postgres|memory] [-tls-cert F -tls-key F | -behind-proxy] [-mcp[=false]] [-mcp-allowed-origins list] [flags]"

const apiServeBody = `Runs the REST API server (docs/design-docs/rest-api.md) and, unless -web=false, the web UI at / (ADR-0035).

Production mode (the default) signs callers in with access tokens that "praxium admin" makes (ADR-0044); there are no
passwords. It needs a store that survives a restart and an administrator, so the first time:
  1. praxium admin bootstrap -name root -store file -store-data-dir /var/lib/praxium    (prints the first token, once)
  2. praxium api serve -store file -store-data-dir /var/lib/praxium
The users and tokens are kept beside the KG: in the data folder (-store file) or the database (-store postgres). Production
refuses the memory store, the development token flags and PRAXIUM_DEV_TOKEN. Access tokens must not cross a network in
plain text: on an address that is not loopback it needs -tls-cert and -tls-key, or -behind-proxy when a reverse proxy in
front ends TLS (then it believes the last address in X-Forwarded-For as the caller's).

With -store postgres, production also checks the link to the database (ADR-0064): when the host in the connection string
is on another machine, sslmode must be verify-full or verify-ca, so the server certificate is checked (add sslrootcert=FILE
if the certificate is not from a CA this machine trusts). A database on this machine (127.0.0.1, ::1, localhost, a Unix
socket) needs nothing. If the link is protected in another way, -allow-unverified-db-tls lets the server start and
writes a warning to the start log. A connection string that does not say sslmode counts as require, which does not check.

To look around without any of this use "praxium local" (your own KG, built with praxium ingest run). For development use -mode development: it takes one access token from -dev-token-file (a file
with the token; the environment variable PRAXIUM_DEV_TOKEN also works, the file wins) and treats a caller with it as the
subject -dev-subject with the roles -dev-roles.

The read-only MCP interface (POST /mcp, ADR-0062) is off in production until you start with -mcp, and on in development
mode. It takes only the token of a service account that holds the viewer role and no other (in development mode the token
of the run). -mcp-allowed-origins lists the browser origins that may call it (none by default).

-store file keeps the KG in a data folder (-store-data-dir, ADR-0034); -store postgres reads the connection string
from a file (-store-dsn-file) so no secret is in the process list. The connections are three pools: writes (always the
primary server; -store-write-conns, 8), reads (-store-read-conns, 8, with -store-read-dsn-file for another connection
string) and free-form queries (-store-query-conns, 4); -store-new-conns-per-second (20) spaces new connections. The
server refuses to start on PostgreSQL older than 18, and while an environment variable of the driver (PGHOST, ...) is set.

example:
  praxium admin bootstrap -name root -store file -store-data-dir /var/lib/praxium
  praxium api serve -addr 127.0.0.1:8080 -store file -store-data-dir /var/lib/praxium
  praxium worldgen -scale 500 -seed 7 -out /tmp/world
  printf 'dev-token-not-a-secret' > /tmp/dev.tok
  praxium api serve -mode development -dev-token-file /tmp/dev.tok -dev-truth /tmp/world/truth.jsonl
`

// apiHelpEntries are the help entries of this file's commands.
func apiHelpEntries(out map[string]helpEntry) {
	out["api serve"] = helpEntry{usage: apiServeUsage, body: apiServeBody, flags: func() *flag.FlagSet { return newServeFlags(&serveOptions{}) }}
}
