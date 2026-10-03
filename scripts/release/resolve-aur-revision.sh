#!/usr/bin/env bash
# Usage: resolve-aur-revision.sh release <tag> <draft> <prerelease>
#        resolve-aur-revision.sh dispatch <version> <pkgrel> <github-ref> <github-sha>
#
# Decides which AUR revision a publication run may produce, printing
# GITHUB_OUTPUT lines: version, pkgrel, tag and ref (what validation checks
# out for the package recipe).
#   release   a published, non-prerelease vMAJOR.MINOR.PATCH release: pkgrel 1,
#             packaged with the recipe reviewed in that release's tag
#   dispatch  a revision of an existing published release, dispatched from
#             main. pkgrel 1 publishes the release itself later (e.g. once AUR
#             access exists), packaged with the recipe in its tag exactly as a
#             release event would. pkgrel >= 2 is a packaging-only revision,
#             packaged with the recipe at the dispatch event's commit
#             (<github-sha>), never the moving branch
set -euo pipefail

fail() {
  printf 'resolve-aur-revision: %s\n' "$1" >&2
  exit 1
}

semver='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
mode=${1:-}
case $mode in
  release)
    (( $# == 4 )) || fail "usage: resolve-aur-revision.sh release <tag> <draft> <prerelease>"
    tag=$2 draft=$3 prerelease=$4
    [[ $tag == v* && ${tag#v} =~ $semver ]] || fail "not a vMAJOR.MINOR.PATCH release tag: $(printf '%q' "$tag")"
    [[ $draft == false ]] || fail "release $tag is a draft; only published releases reach the AUR"
    [[ $prerelease == false ]] || fail "release $tag is a prerelease; prereleases are not published to the AUR"
    version=${tag#v} pkgrel=1 ref="refs/tags/$tag"
    ;;
  dispatch)
    (( $# == 5 )) || fail "usage: resolve-aur-revision.sh dispatch <version> <pkgrel> <github-ref> <github-sha>"
    version=$2 pkgrel=$3 github_ref=$4 github_sha=$5
    [[ $version =~ $semver ]] || fail "not a MAJOR.MINOR.PATCH version: $(printf '%q' "$version")"
    [[ $pkgrel =~ ^[1-9][0-9]*$ ]] || fail "not a positive integer pkgrel: $(printf '%q' "$pkgrel")"
    [[ $github_ref == refs/heads/main ]] || fail "packaging-only revisions are dispatched from main, not $github_ref"
    [[ $github_sha =~ ^[0-9a-f]{40}$ ]] || fail "not a full commit SHA: $(printf '%q' "$github_sha")"
    tag="v$version"
    state=$(gh api "repos/{owner}/{repo}/releases/tags/$tag" --jq '"\(.draft) \(.prerelease)"') ||
      fail "no published GitHub Release $tag"
    [[ $state == "false false" ]] || fail "GitHub Release $tag is not a published, non-prerelease release"
    if (( pkgrel == 1 )); then
      ref="refs/tags/$tag"
    else
      ref=$github_sha
    fi
    ;;
  *) fail "usage: resolve-aur-revision.sh release|dispatch ..." ;;
esac

printf 'version=%s\npkgrel=%s\ntag=%s\nref=%s\n' "$version" "$pkgrel" "$tag" "$ref"
