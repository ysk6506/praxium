#!/usr/bin/env bash
# Build the release files of praxium (ADR-0039): one program per target with the web UI inside, one archive per
# target, and SHA256SUMS. The release workflow calls this script and nothing else does the work, so the same command
# makes the same files on a developer machine.
#
#   scripts/release-build.sh [--version V] [--out DIR] [--targets os/arch,os/arch]
#   scripts/release-build.sh --compile-only [--version V] [--out DIR] [--targets ...]
#
# Full build (default): builds the web UI once (scripts/build-with-web.sh --web-only), compiles the program for every
# target with CGO_ENABLED=0, and writes into DIR (default dist/release, which must be empty or absent):
#   praxium_<version>_<os>_<arch>.tar.gz   (zip for windows) holding the program, LICENSE, NOTICE and
#                                          THIRD_PARTY_NOTICES.md in a folder of the same name
#   SHA256SUMS                             sha256sum format, one line per archive
# The work tree must be clean, so that the commit the program reports is the commit it was built from.
#
# --compile-only: compiles the targets without the web UI (the placeholder page stays embedded), no archives, no
# sums, no clean tree needed. It is the "every target compiles" check of CI and the test of the build settings. The
# programs are kept in DIR/<os>_<arch>/ when --out is given and thrown away when it is not.
#
# The version is the tag without the leading v (0.1.0, 0.1.0-alpha.1); without --version it is 0.0.0-snapshot.<sha>.
# The build date the program reports is the time of the commit, not of the build. With -trimpath, -buildvcs=false and
# no timestamps in the archives, one commit gives the same bytes every time (same Go version, same targets).
#
# Needs: bash, git, Go (the version of backend/go.mod), pnpm (full build), GNU tar, gzip, zip, sha256sum.
# Temporary files are made in a folder of this script's own and removed when it ends (ADR-0036).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
default_targets="linux/amd64,linux/arm64,darwin/arm64,darwin/amd64,windows/amd64"

version=""
out=""
targets="$default_targets"
compile_only=0
while [ $# -gt 0 ]; do
  case "$1" in
    --version) [ $# -ge 2 ] || { echo "release-build: --version needs a value" >&2; exit 2; }
               version="$2"; shift 2 ;;  # an empty value means the default
    --out) out="${2:?--out needs a folder}"; shift 2 ;;
    --targets) targets="${2:?--targets needs a list}"; shift 2 ;;
    --compile-only) compile_only=1; shift ;;
    -h|--help) sed -n '2,/^set -euo/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "release-build: unknown argument: $1" >&2; exit 2 ;;
  esac
done

fail() { echo "release-build: $*" >&2; exit 1; }

commit="$(git -C "$root" rev-parse HEAD)" || fail "this is not a git work tree"
if [ -z "$version" ]; then
  version="0.0.0-snapshot.${commit:0:7}"
fi
version="${version#v}"
semver='^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?$'
[[ "$version" =~ $semver ]] || fail "version '$version' is not semantic versioning (1.2.3 or 1.2.3-alpha.1)"

IFS=',' read -r -a target_list <<< "$targets"
for t in "${target_list[@]}"; do
  [[ "$t" =~ ^[a-z0-9]+/[a-z0-9]+$ ]] || fail "target '$t' is not os/arch"
done

if [ "$compile_only" = 0 ]; then
  [ -z "$(git -C "$root" status --porcelain)" ] || fail "the work tree has changes that are not committed; commit them first"
  [ -z "$out" ] && out="$root/dist/release"
  if [ -d "$out" ] && [ -n "$(ls -A "$out")" ]; then fail "$out is not empty"; fi
  tar --version 2>/dev/null | grep -q 'GNU tar' || fail "GNU tar is needed (on macOS: brew install gnu-tar, then put it first on PATH)"
  command -v zip >/dev/null || fail "zip is needed"
fi

# The time of the commit, in UTC, and as seconds (for the times inside the archives).
build_date="$(TZ=UTC git -C "$root" log -1 --date=format-local:%Y-%m-%dT%H:%M:%SZ --format=%cd)"
epoch="$(git -C "$root" log -1 --format=%ct)"

stage="$(mktemp -d "${TMPDIR:-/tmp}/praxium-release.XXXXXX")"
built_web=0
cleanup() {
  rm -rf "$stage"
  if [ "$built_web" = 1 ]; then "$root/scripts/build-with-web.sh" --clean >/dev/null || true; fi
}
trap cleanup EXIT

if [ "$compile_only" = 0 ]; then
  "$root/scripts/build-with-web.sh" --web-only
  built_web=1
fi

ldflags="-s -w -X main.version=$version -X main.commit=$commit -X main.buildDate=$build_date"

# compile <os> <arch> <destination folder>: prints the path of the program.
compile() {
  local goos="$1" goarch="$2" dest="$3" name=praxium
  [ "$goos" = windows ] && name=praxium.exe
  mkdir -p "$dest"
  (cd "$root/backend" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOFLAGS=-mod=readonly \
    go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$dest/$name" ./cmd/praxium)
  echo "$dest/$name"
}

if [ "$compile_only" = 1 ]; then
  dest_root="${out:-$stage/bin}"
  for t in "${target_list[@]}"; do
    compile "${t%/*}" "${t#*/}" "$dest_root/${t%/*}_${t#*/}" >/dev/null
    echo "compiled $t"
  done
  exit 0
fi

# Archives. Files are put in a fixed order with fixed owners, modes and times, and gzip and zip store no clock, so the
# same commit gives the same archive.
mkdir -p "$out"
sums=()
for t in "${target_list[@]}"; do
  goos="${t%/*}"
  goarch="${t#*/}"
  base="praxium_${version}_${goos}_${goarch}"
  folder="$stage/pack/$base"
  mkdir -p "$folder"
  prog="$(compile "$goos" "$goarch" "$stage/bin/${goos}_${goarch}")"
  cp "$prog" "$folder/"
  cp "$root/LICENSE" "$root/NOTICE" "$root/THIRD_PARTY_NOTICES.md" "$folder/"
  chmod 755 "$folder" "$folder/$(basename "$prog")"
  chmod 644 "$folder/LICENSE" "$folder/NOTICE" "$folder/THIRD_PARTY_NOTICES.md"
  files=("$base/$(basename "$prog")" "$base/LICENSE" "$base/NOTICE" "$base/THIRD_PARTY_NOTICES.md")
  if [ "$goos" = windows ]; then
    archive="$base.zip"
    (cd "$stage/pack" && TZ=UTC touch -d "@$epoch" "${files[@]}" && TZ=UTC zip -X -D -q "$out/$archive" "${files[@]}")
  else
    archive="$base.tar.gz"
    (cd "$stage/pack" && tar --owner=0 --group=0 --numeric-owner --mtime="@$epoch" --no-recursion -cf - "${files[@]}" | gzip -n -9 > "$out/$archive")
  fi
  sums+=("$archive")
  echo "packed $archive"
done

# The lines are in the order of the file names, so the list reads the same however the targets were given.
(cd "$out" && printf '%s\n' "${sums[@]}" | LC_ALL=C sort | xargs sha256sum > SHA256SUMS)
echo "wrote $out:"
(cd "$out" && ls -l --block-size=K "${sums[@]}" SHA256SUMS | awk '{print "  " $5 "  " $9}')
