#!/usr/bin/env bash
# Usage: fetch-release-source.sh <owner/repo> <version> <output-dir>
#
# Downloads the immutable source archive and SHA256SUMS of the published
# GitHub Release v<version> from their public URLs, which this script
# constructs itself, and verifies them. Prints the archive URL, which is the
# exact source the AUR package builds from.
set -euo pipefail

fail() {
  printf 'fetch-release-source: %s\n' "$1" >&2
  exit 1
}

(( $# == 3 )) || fail "usage: fetch-release-source.sh <owner/repo> <version> <output-dir>"
repo=$1 version=$2 out=$3
[[ $repo =~ ^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$ ]] || fail "not an owner/repo: $(printf '%q' "$repo")"
[[ $version =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
  fail "not a MAJOR.MINOR.PATCH version: $(printf '%q' "$version")"

archive="omarchy-blueprint-$version.tar.gz"
base="https://github.com/$repo/releases/download/v$version"
mkdir -p "$out"
[[ -z $(find "$out" -mindepth 1 -maxdepth 1) ]] || fail "$out must be empty"
for asset in "$archive" SHA256SUMS; do
  curl --fail --silent --show-error --location --proto '=https' --retry 3 \
    --output "$out/$asset" "$base/$asset"
done

[[ $(wc -l < "$out/SHA256SUMS") -eq 1 ]] || fail "SHA256SUMS must hold exactly one entry"
read -r digest name < "$out/SHA256SUMS"
[[ $digest =~ ^[0-9a-f]{64}$ && $name == "$archive" ]] || fail "SHA256SUMS does not name $archive"
(cd "$out" && sha256sum --check --strict SHA256SUMS) >&2 || fail "$archive does not match SHA256SUMS"
printf '%s\n' "$base/$archive"
