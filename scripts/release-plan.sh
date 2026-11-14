#!/usr/bin/env bash
# Decide what a run of the release workflow does (ADR-0047): the tag, the commit to build, and whether to publish.
# The workflow's `plan` job calls this script after it checks out the ref that was run (all history and tags), and
# nothing else decides. Every value comes in as an environment variable and is checked here before it is used.
#
#   EVENT_NAME     push (a tag `v*` was pushed) or workflow_dispatch (a manual run)
#   REF_NAME       push: the pushed tag
#   RUN_SHA        the commit the workflow run is on (the pushed tag's commit, or the tip of the chosen branch)
#   INPUT_VERSION  manual run: the version (0.1.0-alpha.1 or v0.1.0-alpha.1); empty builds a snapshot, no release
#   INPUT_COMMIT   manual run: the 40-digit SHA to release; empty is RUN_SHA
#   MAIN_REF       the main branch (default refs/remotes/origin/main)
#   GITHUB_OUTPUT  where the answers go (default: standard output)
#
# Answers (name=value lines): mode (tag-push, manual or snapshot), tag (v...; empty for a snapshot), sha, publish.
# A manual release must be a commit of the main branch (an ancestor of its tip, or its tip), and its tag must not
# exist yet; a pushed tag is taken as it is. The tags that exist are those the checkout fetched.
set -euo pipefail

fail() { echo "release-plan: $*" >&2; exit 1; }

event="${EVENT_NAME:-}"
run_sha="${RUN_SHA:-}"
main_ref="${MAIN_REF:-refs/remotes/origin/main}"
semver='^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?$'

[[ "$run_sha" =~ ^[0-9a-fA-F]{40}$ ]] || fail "RUN_SHA is not a 40-digit commit"
run_sha="$(git rev-parse --verify --quiet "${run_sha}^{commit}")" || fail "the commit $run_sha of this run is not in the checkout"

mode="" tag="" sha="$run_sha" publish=false
case "$event" in
  push)
    tag="${REF_NAME:-}"
    [[ "$tag" == v* ]] || fail "the pushed tag '$tag' does not start with v"
    [[ "${tag#v}" =~ $semver ]] || fail "the tag '$tag' is not semantic versioning (v1.2.3 or v1.2.3-alpha.1)"
    mode=tag-push publish=true
    ;;
  workflow_dispatch)
    version="${INPUT_VERSION:-}"
    commit="${INPUT_COMMIT:-}"
    if [ -n "$commit" ]; then
      [[ "$commit" =~ ^[0-9a-fA-F]{40}$ ]] || fail "the commit must be the full 40-digit SHA, not '$commit'"
      sha="$(git rev-parse --verify --quiet "${commit}^{commit}")" || fail "the commit $commit is not in this repository"
    fi
    if [ -z "$version" ]; then
      mode=snapshot
    else
      version="${version#v}"
      [[ "$version" =~ $semver ]] || fail "the version '$version' is not semantic versioning (1.2.3 or 1.2.3-alpha.1)"
      tag="v$version"
      git rev-parse --verify --quiet "refs/tags/$tag" > /dev/null && fail "the tag $tag already exists; use the next version"
      git rev-parse --verify --quiet "$main_ref" > /dev/null || fail "the main branch ($main_ref) is not in the checkout"
      git merge-base --is-ancestor "$sha" "$main_ref" || fail "the commit $sha is not on the main branch; only a commit of main can be released"
      mode=manual publish=true
    fi
    ;;
  *) fail "the event '$event' is not push or workflow_dispatch" ;;
esac

{
  echo "mode=$mode"
  echo "tag=$tag"
  echo "sha=$sha"
  echo "publish=$publish"
} >> "${GITHUB_OUTPUT:-/dev/stdout}"
echo "release-plan: mode=$mode tag=${tag:-none} sha=$sha publish=$publish" >&2
