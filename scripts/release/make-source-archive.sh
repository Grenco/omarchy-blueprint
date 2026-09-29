#!/usr/bin/env bash
# Usage: make-source-archive.sh <git-ref> <version> <output-dir>
#
# Writes <output-dir>/omarchy-blueprint-<version>.tar.gz, a deterministic
# archive of <git-ref> beneath omarchy-blueprint-<version>/, and
# <output-dir>/SHA256SUMS for it. Re-running with identical results succeeds;
# existing files are never replaced with different bytes.
set -euo pipefail

fail() {
  printf 'make-source-archive: %s\n' "$1" >&2
  exit 1
}

(( $# == 3 )) || fail "usage: make-source-archive.sh <git-ref> <version> <output-dir>"
ref=$1 version=$2 out=$3
[[ $version =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
  fail "not a MAJOR.MINOR.PATCH version: $(printf '%q' "$version")"
commit=$(git rev-parse --verify --quiet --end-of-options "$ref^{commit}") || fail "ref $ref does not name a commit"

name="omarchy-blueprint-$version"
archive="$name.tar.gz"
mkdir -p "$out"
staging=$(mktemp -d "$out/.staging.XXXXXX")
trap 'rm -rf "$staging"' EXIT

# Pin every configuration that affects the tar bytes rather than inheriting
# the caller's; gzip -n omits the name and timestamp from the gzip header.
git -c tar.umask=0022 -c core.autocrlf=false -c core.eol=lf \
  archive --format=tar --prefix="$name/" "$commit" | gzip -n -9 > "$staging/$archive"
(cd "$staging" && sha256sum "$archive") > "$staging/SHA256SUMS"

for file in "$archive" SHA256SUMS; do
  if [[ -e $out/$file ]] && ! cmp -s "$staging/$file" "$out/$file"; then
    fail "$out/$file already exists with different content"
  fi
done
for file in "$archive" SHA256SUMS; do
  [[ -e $out/$file ]] || mv "$staging/$file" "$out/$file"
done
printf '%s\n' "$out/$archive"
