#!/usr/bin/env bash
# Runs a command with a throwaway PostgreSQL server and PRAXIUM_TEST_PG_DSN set to it (docs/harness-guide.md).
#
#   scripts/with-postgres.sh pnpm check
#   scripts/with-postgres.sh go test ./...
#
# If PRAXIUM_TEST_PG_DSN is already set (your own test server), the command runs as it is. Otherwise the script
# needs the server programs (initdb, pg_ctl) of an installed PostgreSQL 18 or newer: from PG_BIN, from pg_config,
# from the PATH, or from /usr/lib/postgresql/<version>/bin. CI runs this script too, with the programs that
# scripts/ci-install-postgres.sh installs (ADR-0082). The server listens on a socket in a temporary directory, and
# on 127.0.0.1 at a free TCP port that is picked for each run. Socket connections are trusted, as before. TCP
# connections need the password of a test role that the script makes for each run; the command gets the connection
# through PRAXIUM_TEST_PG_TCP_HOST, PRAXIUM_TEST_PG_TCP_PORT, PRAXIUM_TEST_PG_TCP_USER and PRAXIUM_TEST_PG_TCP_PASSWORD
# (docs/harness-guide.md). Everything is removed when the command ends. As root, initdb refuses to run, so the server
# is run as an unprivileged user (PG_RUN_USER, default praxium-pg, made if missing); this is for sandboxes and containers.
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
  echo "with-postgres: no PostgreSQL server programs found (initdb, pg_ctl); install PostgreSQL 18 or set PG_BIN, or set PRAXIUM_TEST_PG_DSN to a test server" >&2
  exit 1
fi

major=$("$bin/postgres" --version | sed -E 's/^[^0-9]*([0-9]+).*/\1/')
if [ -z "$major" ] || [ "$major" -lt 18 ]; then
  echo "with-postgres: the server programs in $bin are PostgreSQL ${major:-of an unknown version}; the tests need 18 or newer (ADR-0082). Install PostgreSQL 18 and point PG_BIN at its bin directory" >&2
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
# initdb writes trust lines for TCP too. Drop those "all databases, all roles" lines and add one scram-sha-256 line for
# 127.0.0.1, so a TCP connection needs a password. The socket lines stay trust.
hba="$dir/data/pg_hba.conf"
"${as[@]}" sed -i '/^host[[:space:]]\+all[[:space:]]\+all[[:space:]]/d' "$hba"
printf '%s\n' 'host all all 127.0.0.1/32 scram-sha-256' | "${as[@]}" tee -a "$hba" >/dev/null

# The port must be free when the server starts. Another check on this machine may take it in the meantime, so a
# failed start picks another port, up to five times.
port=""
for _ in 1 2 3 4 5; do
  candidate=$((20000 + RANDOM % 40000))
  if (exec 3<>"/dev/tcp/127.0.0.1/$candidate") 2>/dev/null; then
    continue # something already listens on it
  fi
  if "${as[@]}" "$bin/pg_ctl" -D "$dir/data" -w -l "$dir/log" \
    -o "-c listen_addresses=127.0.0.1 -c port=$candidate -c unix_socket_directories=$dir -c fsync=off -c synchronous_commit=off -c full_page_writes=off -c max_connections=200" start >/dev/null 2>&1; then
    port=$candidate
    break
  fi
done
if [ -z "$port" ]; then
  cat "$dir/log" >&2
  exit 1
fi

# The test role: can log in and create databases, is not a superuser. Its password is random for this run and goes to
# the server through standard input, so it is in neither a command line nor a file.
[ -x "$bin/psql" ] || {
  echo "with-postgres: no psql in $bin; it is needed to make the TCP test role" >&2
  exit 1
}
tcp_user=praxium_test
tcp_password=$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')
"$bin/psql" -X -q -v ON_ERROR_STOP=1 -h "$dir" -p "$port" -U praxium -d postgres >/dev/null <<SQL
CREATE ROLE $tcp_user LOGIN PASSWORD '$tcp_password' CREATEDB NOSUPERUSER;
SQL

export PRAXIUM_TEST_PG_DSN="host=$dir port=$port user=praxium dbname=postgres sslmode=disable"
export PRAXIUM_TEST_PG_TCP_HOST=127.0.0.1
export PRAXIUM_TEST_PG_TCP_PORT="$port"
export PRAXIUM_TEST_PG_TCP_USER="$tcp_user"
export PRAXIUM_TEST_PG_TCP_PASSWORD="$tcp_password"
"$@"
