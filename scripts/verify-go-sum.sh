#!/usr/bin/env bash
# Checks backend/go.sum against the official checksum database (ADR-0032).
#
# The third-party module was first fetched straight from its repository, without the database, so the lines of
# go.sum were never confirmed by it. This script confirms them: it copies the module to a temporary folder, drops
# go.sum, and lets `go mod tidy` rebuild it in an empty module cache with the default proxy and database (a new
# line is accepted only if the database vouches for it). The rebuilt file must equal the committed one, and
# `go mod verify` must find the downloaded files as the lines say.
#
# GOPROXY, GOSUMDB and GOFLAGS may be set by the caller only for a test of this script; CI uses the defaults.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'chmod -R u+w "$work" 2>/dev/null; rm -rf "$work"' EXIT

cp -R "$root/backend" "$work/backend"
rm -f "$work/backend/go.sum"
cd "$work/backend"
export GOMODCACHE="$work/modcache"
export GOFLAGS="${GOFLAGS:--mod=mod}"
unset GONOSUMDB GONOSUMCHECK GOPRIVATE GONOPROXY GOINSECURE

go mod tidy
go mod verify
if ! diff -u "$root/backend/go.sum" go.sum; then
  echo "verify-go-sum: backend/go.sum is not what the checksum database vouches for" >&2
  exit 1
fi
if ! diff -u "$root/backend/go.mod" go.mod; then
  echo "verify-go-sum: backend/go.mod is not tidy" >&2
  exit 1
fi
echo "verify-go-sum: go.sum matches the checksum database"
