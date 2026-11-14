#!/usr/bin/env bash
# Try a release archive of this machine's kind the way a person who downloaded it would (ADR-0039): unpack it, ask
# `praxium version`, start the server and ask for the web UI at /.
#
#   scripts/release-smoke.sh <praxium_<version>_linux_amd64.tar.gz>
#
# It fails when the version in the file name differs from the one the program reports, when the program says the web
# UI is not inside, or when / does not answer 200 with the web UI's page. Needs curl. The temporary folder and the
# server are removed when it ends (ADR-0036).
set -euo pipefail

archive="${1:?usage: release-smoke.sh <archive.tar.gz>}"
fail() { echo "release-smoke: $*" >&2; exit 1; }

name="$(basename "$archive")"
[[ "$name" =~ ^praxium_(.+)_(linux|darwin)_(amd64|arm64)\.tar\.gz$ ]] || fail "$name is not a praxium_<version>_<os>_<arch>.tar.gz"
want_version="${BASH_REMATCH[1]}"

work="$(mktemp -d "${TMPDIR:-/tmp}/praxium-smoke.XXXXXX")"
server=""
cleanup() {
  if [ -n "$server" ]; then kill "$server" 2>/dev/null || true; wait "$server" 2>/dev/null || true; fi
  rm -rf "$work"
}
trap cleanup EXIT

tar -xzf "$archive" -C "$work"
dir="$work/${name%.tar.gz}"
for f in praxium LICENSE NOTICE THIRD_PARTY_NOTICES.md; do [ -f "$dir/$f" ] || fail "the archive has no $f"; done

info="$("$dir/praxium" version)"
echo "$info"
grep -qx "praxium $want_version" <<< "$info" || fail "the program does not report version $want_version"
grep -qx 'web ui:  included' <<< "$info" || fail "the program does not have the web UI inside"

printf 'smoke-token-not-a-secret' > "$work/token"
"$dir/praxium" api serve -addr 127.0.0.1:0 -mode development -dev-token-file "$work/token" 2> "$work/server.log" &
server=$!
addr=""
for _ in $(seq 1 100); do
  addr="$(sed -n 's/.*"msg":"praxium api listening".*"addr":"\([^"]*\)".*/\1/p' "$work/server.log" | head -1)"
  [ -n "$addr" ] && break
  kill -0 "$server" 2>/dev/null || fail "the server stopped: $(cat "$work/server.log")"
  sleep 0.1
done
[ -n "$addr" ] || fail "the server did not say where it listens: $(cat "$work/server.log")"

status="$(curl -s -o "$work/index.html" -w '%{http_code}' "http://$addr/")"
[ "$status" = 200 ] || fail "GET / answered $status"
grep -q '<div id="root"' "$work/index.html" || fail "GET / did not answer the web UI's page"
echo "release-smoke: ok (version $want_version, GET / = 200 with the web UI)"
