#!/usr/bin/env bash
# Runs a command with a throwaway PostgreSQL server and PRAXIUM_TEST_PG_DSN set to it (docs/harness-guide.md).
#
#   scripts/with-postgres.sh pnpm check
#   scripts/with-postgres.sh go test ./...
#
# If PRAXIUM_TEST_PG_DSN is already set (CI gives a service container), the command runs as it is. Otherwise the
# script needs the server programs (initdb, pg_ctl) of an installed PostgreSQL: from PG_BIN, from pg_config, from
# the PATH, or from /usr/lib/postgresql/<version>/bin. The server listens on a socket in a temporary directory only,
# no port, and everything is removed when the command ends. As root, initdb refuses to run, so the server is run
# as an unprivileged user (PG_RUN_USER, default praxium-pg, made if missing); this is for sandboxes and containers.
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: scripts/with-postgres.sh <command> [args...]" >&2
  exit 2
fi
if [ -n "${PRAXIUM_TEST_PG_DSN:-}" ]; then
  exec "$@"
fi

find_bin() {
  if [ -n "${PG_BIN:-}" ]; then echo "$PG_BIN"; return; fi
  if command -v pg_config >/dev/null 2>&1; then pg_config --bindir; return; fi
  if command -v initdb >/dev/null 2>&1; then dirname "$(command -v initdb)"; return; fi
  local newest
  newest=$(ls -d /usr/lib/postgresql/*/bin 2>/dev/null | sort -V | tail -n 1 || true)
  [ -n "$newest" ] && echo "$newest"
}
bin=$(find_bin || true)
if [ -z "$bin" ] || [ ! -x "$bin/initdb" ] || [ ! -x "$bin/pg_ctl" ]; then
  echo "with-postgres: no PostgreSQL server programs found (initdb, pg_ctl); install PostgreSQL 16 or set PG_BIN, or set PRAXIUM_TEST_PG_DSN to a test server" >&2
  exit 1
fi

# The socket path is limited to about 100 bytes, so the directory name is short.
dir=$(mktemp -d /tmp/pgt.XXXXXX)
as=()
if [ "$(id -u)" -eq 0 ]; then
  user=${PG_RUN_USER:-praxium-pg}
  id "$user" >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin "$user"
  chown "$user" "$dir"
  as=(runuser -u "$user" --)
fi
stop() {
  "${as[@]}" "$bin/pg_ctl" -D "$dir/data" -m immediate -w stop >/dev/null 2>&1 || true
  rm -rf "$dir"
}
trap stop EXIT

"${as[@]}" "$bin/initdb" -D "$dir/data" -U praxium -A trust --no-sync -E UTF8 --locale=C.UTF-8 >/dev/null
"${as[@]}" "$bin/pg_ctl" -D "$dir/data" -w -l "$dir/log" \
  -o "-c listen_addresses= -c unix_socket_directories=$dir -c fsync=off -c synchronous_commit=off -c full_page_writes=off -c max_connections=200" start >/dev/null || {
  cat "$dir/log" >&2
  exit 1
}
export PRAXIUM_TEST_PG_DSN="host=$dir user=praxium dbname=postgres sslmode=disable"
"$@"
