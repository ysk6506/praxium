#!/usr/bin/env bash
# Try a release archive of this machine's kind the way a person who downloaded it would (ADR-0039): unpack it, ask
# `praxium version`, start the server and ask for the web UI at /, then do the same with `praxium demo`, and then
# with the way to one's own data: `praxium ingest init`, `praxium ingest run -data-dir` and `praxium local`.
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
kill "$server" 2>/dev/null || true; wait "$server" 2>/dev/null || true; server=""

# The demo: `praxium demo` on a free port prints an address with the token after the #; / must give the web UI.
"$dir/praxium" demo -addr 127.0.0.1:0 > "$work/demo.out" 2> "$work/demo.err" &
server=$!
url=""
for _ in $(seq 1 300); do
  url="$(grep -m1 '^http://' "$work/demo.out" || true)"
  [ -n "$url" ] && break
  kill -0 "$server" 2>/dev/null || fail "the demo stopped: $(cat "$work/demo.out" "$work/demo.err")"
  sleep 0.1
done
[ -n "$url" ] || fail "the demo did not print an address: $(cat "$work/demo.out" "$work/demo.err")"
status="$(curl -s -o "$work/demo.html" -w '%{http_code}' "${url%%#*}")"
[ "$status" = 200 ] || fail "demo: GET / answered $status"
grep -q '<div id="root"' "$work/demo.html" || fail "demo: GET / did not answer the web UI's page"
grep -q 'does not include the web UI' "$work/demo.out" && fail "demo: the program says the web UI is not inside"

kill "$server" 2>/dev/null || true; wait "$server" 2>/dev/null || true; server=""

# The way to one's own data (ADR-0041): the examples are written, read into a data folder, and `praxium local` serves
# what was read. The address carries the mark of local mode, the web UI is at /, and the API has the objects.
"$dir/praxium" --help | grep -q 'Fastest start:  praxium demo' || fail "the help does not lead with praxium demo"
"$dir/praxium" ingest init -out "$work/ex" > "$work/init.out" || fail "ingest init failed: $(cat "$work/init.out")"
book="$work/ex/excel/datacenter.xlsx"
"$dir/praxium" ingest inspect -input "$book" > /dev/null || fail "ingest inspect failed on the example workbook"
"$dir/praxium" ingest run -data-dir "$work/data" -source "$work/ex/excel/source.json" -mapping "$work/ex/excel/mapping.json" \
  -input ds_racks="$book" -input ds_servers="$book" > "$work/run-xlsx.out" || fail "ingest run (excel) failed: $(cat "$work/run-xlsx.out")"
c="$work/ex/csv"
"$dir/praxium" ingest run -data-dir "$work/data" -source "$c/source.json" -mapping "$c/mapping.json" -input ds_racks="$c/racks.csv" \
  -input ds_servers="$c/servers.csv" -input ds_hosts="$c/hosts.jsonl" -input ds_vms="$c/vms.jsonl" -input ds_vm_power="$c/vm-power.csv" \
  > "$work/run-csv.out" || fail "ingest run (csv) failed: $(cat "$work/run-csv.out")"
grep -q 'completed without problems' "$work/run-csv.out" || fail "the example run did not complete without problems"
"$dir/praxium" local -data-dir "$work/data" -addr 127.0.0.1:0 > "$work/local.out" 2> "$work/local.err" &
server=$!
url=""
for _ in $(seq 1 300); do
  url="$(grep -m1 '^http://' "$work/local.out" || true)"
  [ -n "$url" ] && break
  kill -0 "$server" 2>/dev/null || fail "local stopped: $(cat "$work/local.out" "$work/local.err")"
  sleep 0.1
done
[ -n "$url" ] || fail "local did not print an address: $(cat "$work/local.out" "$work/local.err")"
grep -q 'mode=local' <<< "$url" || fail "the address of local mode does not carry mode=local: $url"
status="$(curl -s -o "$work/local.html" -w '%{http_code}' "${url%%#*}")"
[ "$status" = 200 ] || fail "local: GET / answered $status"
grep -q '<div id="root"' "$work/local.html" || fail "local: GET / did not answer the web UI's page"
token="$(sed -n 's/.*token=\([0-9a-f]*\).*/\1/p' <<< "$url")"
curl -sg -H "Authorization: Bearer $token" "${url%%/#*}/v1/kgs/main/objects?filter[object_type][equals]=Rack" | grep -q 'rack-x1' || fail "local: the API does not serve the objects the ingest runs made"

echo "release-smoke: ok (version $want_version, GET / = 200 with the web UI, also in praxium demo, and in praxium local over the ingested examples)"
