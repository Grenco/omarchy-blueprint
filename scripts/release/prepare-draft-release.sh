#!/usr/bin/env bash
# Usage: prepare-draft-release.sh <tag> <version> <dist-dir>
#
# Prepares the DRAFT GitHub Release for a verified release tag with the
# verified source archive and SHA256SUMS in <dist-dir>, using the GitHub CLI.
# It never publishes a release and never replaces an existing asset:
#   no release        create a draft (generated notes are an editing aid)
#   existing draft    succeed only if its assets match byte for byte; upload
#                     an asset the draft is still missing
#   published release succeed only if its assets match; never modify it
# Prints a short summary for the workflow log and step summary.
set -euo pipefail

fail() {
  printf 'prepare-draft-release: %s\n' "$1" >&2
  exit 1
}

(( $# == 3 )) || fail "usage: prepare-draft-release.sh <tag> <version> <dist-dir>"
tag=$1 version=$2 dist=$3
[[ $tag =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
  fail "not a vMAJOR.MINOR.PATCH release tag: $(printf '%q' "$tag")"
[[ $version == "${tag#v}" ]] || fail "version $version does not match tag $tag"

archive="omarchy-blueprint-$version.tar.gz"
assets=("$archive" SHA256SUMS)
[[ $(find "$dist" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort) == "$(printf '%s\n' "${assets[@]}" | LC_ALL=C sort)" ]] ||
  fail "$dist must contain exactly: ${assets[*]}"
(cd "$dist" && sha256sum --check --strict SHA256SUMS) >&2 || fail "$dist/SHA256SUMS does not verify"
[[ $(wc -l < "$dist/SHA256SUMS") -eq 1 ]] || fail "SHA256SUMS must name exactly the source archive"

if ! draft=$(gh release view "$tag" --json isDraft --jq .isDraft 2>/dev/null); then
  gh release create "$tag" --draft --verify-tag --title "$tag" --generate-notes \
    "$dist/$archive" "$dist/SHA256SUMS" >&2
  echo "Created draft release $tag with $archive and SHA256SUMS."
  exit 0
fi

existing=$(gh release view "$tag" --json assets --jq '.assets[].name')
compare=$(mktemp -d)
trap 'rm -rf "$compare"' EXIT
for asset in "${assets[@]}"; do
  if grep -qxF -- "$asset" <<< "$existing"; then
    gh release download "$tag" --dir "$compare" --pattern "$asset" >&2
    cmp -s "$dist/$asset" "$compare/$asset" ||
      fail "release $tag already has a different $asset; it is never replaced"
    echo "Existing $asset on $tag matches the verified asset."
  elif [[ $draft == true ]]; then
    gh release upload "$tag" "$dist/$asset" >&2
    echo "Uploaded missing $asset to draft release $tag."
  else
    fail "published release $tag lacks $asset; published releases are never modified"
  fi
done

if [[ $draft == true ]]; then
  echo "Draft release $tag already holds the verified assets."
else
  echo "Release $tag is already published with the verified assets; nothing changed."
fi
