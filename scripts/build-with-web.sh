#!/usr/bin/env bash
# Build the praxium program with the web UI inside (ADR-0035): build apps/web, copy the result into the folder the
# Go program embeds, build the program.
#
#   scripts/build-with-web.sh [output-file]   default output: dist/praxium
#   scripts/build-with-web.sh --clean         leave only the committed placeholder in the embedded folder
#
# Go and pnpm must be on the PATH, and `pnpm install` must have run. The copied files are not committed (.gitignore),
# so the backend still builds and passes its tests without the web UI.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
embedded="$root/backend/internal/webui/dist"

clean_embedded() {
  find "$embedded" -mindepth 1 ! -name placeholder.html -delete
}

if [ "${1:-}" = "--clean" ]; then
  clean_embedded
  echo "web UI removed from $embedded (the placeholder stays)"
  exit 0
fi

out="${1:-$root/dist/praxium}"

(cd "$root" && pnpm --filter @praxium/web build)
clean_embedded
cp -R "$root/apps/web/dist/." "$embedded/"
mkdir -p "$(dirname "$out")"
(cd "$root/backend" && go build -o "$out" ./cmd/praxium)
echo "built $out with the web UI ($(du -sk "$embedded" | cut -f1) KB embedded)"
