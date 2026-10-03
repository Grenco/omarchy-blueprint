#!/usr/bin/env bash
# Usage: prepare-draft-release.sh <tag> <version> <commit> <dist-dir>
#
# Prepares the DRAFT GitHub Release for a verified release tag with the
# verified source archive, SHA256SUMS and the validated pkgrel 1 PKGBUILD in
# <dist-dir>, using the GitHub CLI. The PKGBUILD is the AUR recipe for this
# release; beta testers build it with makepkg while the AUR is unavailable.
# <commit> is the commit the tag was verified at and the assets were built
# from; nothing is changed unless the tag on GitHub still points at it.
#
# It never publishes a release and never replaces an existing asset:
#   no release        create a draft (generated notes are an editing aid)
#   existing draft    every existing asset must match byte for byte before
#                     any asset the draft is still missing is uploaded
#   published release succeed only if its assets match; never modify it
# Prints a short summary for the workflow log and step summary.
set -euo pipefail

fail() {
  printf 'prepare-draft-release: %s\n' "$1" >&2
  exit 1
}

# Prints the commit the tag currently points at on GitHub, peeling annotated
# tag objects.
remote_tag_commit() {
  local type sha
  read -r type sha < <(gh api "repos/{owner}/{repo}/git/ref/tags/$tag" --jq '"\(.object.type) \(.object.sha)"') ||
    return 1
  while [[ $type == tag ]]; do
    read -r type sha < <(gh api "repos/{owner}/{repo}/git/tags/$sha" --jq '"\(.object.type) \(.object.sha)"') ||
      return 1
  done
  [[ $type == commit ]] && printf '%s\n' "$sha"
}

require_tag_at_commit() {
  local current
  current=$(remote_tag_commit) || fail "cannot resolve tag $tag on GitHub"
  [[ $current == "$commit" ]] ||
    fail "tag $tag now points at $current, not the verified commit $commit; investigate before releasing"
}

(( $# == 4 )) || fail "usage: prepare-draft-release.sh <tag> <version> <commit> <dist-dir>"
tag=$1 version=$2 commit=$3 dist=$4
[[ $tag =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
  fail "not a vMAJOR.MINOR.PATCH release tag: $(printf '%q' "$tag")"
[[ $version == "${tag#v}" ]] || fail "version $version does not match tag $tag"
[[ $commit =~ ^[0-9a-f]{40}$ ]] || fail "not a full commit SHA: $(printf '%q' "$commit")"

archive="omarchy-blueprint-$version.tar.gz"
assets=("$archive" SHA256SUMS PKGBUILD)
[[ $(find "$dist" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort) == "$(printf '%s\n' "${assets[@]}" | LC_ALL=C sort)" ]] ||
  fail "$dist must contain exactly: ${assets[*]}"
(cd "$dist" && sha256sum --check --strict SHA256SUMS) >&2 || fail "$dist/SHA256SUMS does not verify"
[[ $(wc -l < "$dist/SHA256SUMS") -eq 1 ]] || fail "SHA256SUMS must name exactly the source archive"
read -r digest _ < "$dist/SHA256SUMS"
for line in "pkgname=omarchy-blueprint" "pkgver=$version" "pkgrel=1" "sha256sums=('$digest')"; do
  grep -qxF -- "$line" "$dist/PKGBUILD" || fail "PKGBUILD is not the pkgrel 1 recipe for $archive: missing $line"
done

require_tag_at_commit

if ! draft=$(gh release view "$tag" --json isDraft --jq .isDraft 2>/dev/null); then
  gh release create "$tag" --draft --verify-tag --title "$tag" --generate-notes \
    "${assets[@]/#/$dist/}" >&2
  require_tag_at_commit
  echo "Created draft release $tag at $commit with $archive, SHA256SUMS and PKGBUILD."
  exit 0
fi

# Pass 1: compare every asset the release already has; change nothing.
existing=$(gh release view "$tag" --json assets --jq '.assets[].name')
compare=$(mktemp -d)
trap 'rm -rf "$compare"' EXIT
missing=()
for asset in "${assets[@]}"; do
  if grep -qxF -- "$asset" <<< "$existing"; then
    gh release download "$tag" --dir "$compare" --pattern "$asset" >&2
    cmp -s "$dist/$asset" "$compare/$asset" ||
      fail "release $tag already has a different $asset; it is never replaced"
  else
    missing+=("$asset")
  fi
done
if (( ${#missing[@]} )) && [[ $draft != true ]]; then
  fail "published release $tag lacks ${missing[*]}; published releases are never modified"
fi

# Pass 2: the release is consistent with the verified assets; complete it.
for asset in "${missing[@]}"; do
  gh release upload "$tag" "$dist/$asset" >&2
  echo "Uploaded missing $asset to draft release $tag."
done
require_tag_at_commit

if [[ $draft == true ]]; then
  echo "Draft release $tag at $commit holds the verified assets."
else
  echo "Release $tag is already published with the verified assets; nothing changed."
fi
