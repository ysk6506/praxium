#!/usr/bin/env bash
# Make the GitHub release from the files the build job made (ADR-0047). The workflow's `release` job calls this script
# from a checkout of the ref that was run, which is not necessarily the commit that was built.
#
#   scripts/release-publish.sh <folder with the archives and SHA256SUMS>
#
#   TAG      the tag (v0.1.0-alpha.1)
#   SHA      the commit that was built
#   MODE     tag-push (the tag exists: gh checks it) or manual (GitHub makes the tag at SHA)
#   GH_TOKEN, GH_REPO   for gh
#
# It checks the files against SHA256SUMS first. The notes are the body of docs/releases/<tag>.md of this checkout
# (without its front matter) or, when there is no such file, the notes GitHub generates; either way the SHA256SUMS
# lines follow in a code block, so the checksums can be read before anything is downloaded. A tag with a `-` in it is
# marked as a pre-release. After a manual release it reads the commit of the new tag and fails when that is not SHA. The title is the tag. Temporary files are removed when the script ends (ADR-0036).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fail() { echo "release-publish: $*" >&2; exit 1; }

dir="${1:?usage: release-publish.sh <folder>}"
tag="${TAG:-}"
sha="${SHA:-}"
mode="${MODE:-}"
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?$ ]] || fail "TAG '$tag' is not v1.2.3 or v1.2.3-alpha.1"
[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || fail "SHA is not a 40-digit lower-case commit"
case "$mode" in tag-push | manual) ;; *) fail "MODE '$mode' is not tag-push or manual" ;; esac
[ -f "$dir/SHA256SUMS" ] || fail "$dir has no SHA256SUMS"

(cd "$dir" && sha256sum -c SHA256SUMS) || fail "the files do not match SHA256SUMS; nothing is published"

work="$(mktemp -d "${TMPDIR:-/tmp}/praxium-publish.XXXXXX")"
trap 'rm -rf "$work"' EXIT

# A tag may have been pushed after the plan was made; GitHub would then use that tag and not the commit that was built.
# Only a 404 says the tag is not there: any other answer (an error, a timeout, no permission) is not an answer.
if [ "$mode" = manual ]; then
  repo="${GH_REPO:?GH_REPO is needed}"
  status=0
  answer="$(gh api -i "repos/$repo/git/ref/tags/$tag" 2>&1)" || status=$?
  if [ "$status" = 0 ]; then
    fail "the tag $tag exists now; nothing is published"
  elif ! grep -Eq '^HTTP/[0-9.]+ 404( |$)' <<< "$answer"; then
    fail "could not find out whether the tag $tag exists; nothing is published"
  fi
fi

notes="$work/notes.md"
note_file="$root/docs/releases/$tag.md"
generate=()
if [ -f "$note_file" ]; then
  if [ "$(head -n 1 "$note_file")" = "---" ]; then
    awk 'NR == 1 { next } !done && /^---[[:space:]]*$/ { done = 1; next } done' "$note_file" > "$notes"
    [ -s "$notes" ] || fail "$note_file has no text after its front matter"
  else
    cp "$note_file" "$notes"
  fi
else
  : > "$notes"
  generate=(--generate-notes)
fi
{
  [ -s "$notes" ] && echo
  echo '### SHA256SUMS'
  echo
  echo '```'
  cat "$dir/SHA256SUMS"
  echo '```'
} >> "$notes"

flags=()
if [[ "$tag" == *-* ]]; then flags+=(--prerelease); fi
if [ "$mode" = manual ]; then flags+=(--target "$sha"); else flags+=(--verify-tag); fi

files=()
for f in "$dir"/*; do files+=("$f"); done
gh release create "$tag" "${files[@]}" --title "$tag" --notes-file "$notes" ${generate[@]+"${generate[@]}"} "${flags[@]}"

# A manual release made the tag: it must point at the commit that was built (checked after the fact; nothing is deleted).
if [ "$mode" = manual ]; then
  at="$(gh api "repos/$repo/commits/$tag" --jq .sha)" || fail "the release $tag was made, but the commit of its tag could not be read; check it by hand"
  [ "$at" = "$sha" ] || fail "the release $tag is on $at, not on the built commit $sha. Delete the release and the tag (gh release delete $tag --cleanup-tag) and run again"
fi
