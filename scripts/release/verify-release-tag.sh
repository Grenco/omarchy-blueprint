#!/usr/bin/env bash
# Usage: verify-release-tag.sh <tag> <main-ref>
#
# Accepts only a strict vMAJOR.MINOR.PATCH tag whose commit is contained in
# <main-ref>, and prints the application version (without the leading "v")
# on stdout. Diagnostics go to stderr.
set -euo pipefail

fail() {
  printf 'verify-release-tag: %s\n' "$1" >&2
  exit 1
}

(( $# == 2 )) || fail "usage: verify-release-tag.sh <tag> <main-ref>"
tag=$1 main_ref=$2
[[ $tag =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
  fail "not a vMAJOR.MINOR.PATCH release tag: $(printf '%q' "$tag")"

commit=$(git rev-parse --verify --quiet "refs/tags/$tag^{commit}") || fail "tag $tag does not exist"
main_commit=$(git rev-parse --verify --quiet --end-of-options "$main_ref^{commit}") || fail "main ref $main_ref does not exist"
git merge-base --is-ancestor "$commit" "$main_commit" ||
  fail "tag $tag ($commit) is not contained in $main_ref"

printf '%s\n' "${tag#v}"
