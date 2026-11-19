#!/usr/bin/env bash
# Installs the PostgreSQL 18 server programs on an Ubuntu CI runner from the PostgreSQL project's own apt
# repository (ADR-0082). The tests run them through scripts/with-postgres.sh with PG_BIN set to the printed path.
set -euo pipefail

sudo apt-get update
sudo apt-get install -y postgresql-common
sudo /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh -y
sudo apt-get install -y postgresql-18
# The package starts a default cluster that the tests do not use.
sudo systemctl stop postgresql 2>/dev/null || true

bin=/usr/lib/postgresql/18/bin
version=$("$bin/postgres" --version)
echo "$version"
case "$version" in
  *" 18."*) ;;
  *) echo "ci-install-postgres: expected PostgreSQL 18 in $bin" >&2; exit 1 ;;
esac
