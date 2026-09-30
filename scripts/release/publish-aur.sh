#!/usr/bin/env bash
# Usage: publish-aur.sh <publication-dir> <aur-remote> <work-dir> <pkgver> <pkgrel>
#
# Publishes a validated AUR publication (PKGBUILD, .SRCINFO, LICENSE and
# PUBLICATION_SHA256SUMS covering them) to the AUR package repository's
# master branch. The caller provides Git's SSH configuration and commit
# identity; this script renders and builds nothing.
#   new/update  commit exactly the three files as "omarchy-blueprint
#               <pkgver>-<pkgrel>" and push, never forcing
#   same        the AUR already holds this revision: report success, no commit
# Anything else (same revision with other content, a newer revision,
# unexpected files) fails before any commit or push.
set -euo pipefail

FILES=(PKGBUILD .SRCINFO LICENSE)

fail() {
  printf 'publish-aur: %s\n' "$1" >&2
  exit 1
}

(( $# == 5 )) || fail "usage: publish-aur.sh <publication-dir> <aur-remote> <work-dir> <pkgver> <pkgrel>"
pub=$1 remote=$2 work=$3 pkgver=$4 pkgrel=$5
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
[[ $pkgver =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || fail "bad pkgver: $(printf '%q' "$pkgver")"
[[ $pkgrel =~ ^[1-9][0-9]*$ ]] || fail "bad pkgrel: $(printf '%q' "$pkgrel")"
[[ -n ${GIT_AUTHOR_NAME:-} && -n ${GIT_AUTHOR_EMAIL:-} && -n ${GIT_COMMITTER_NAME:-} && -n ${GIT_COMMITTER_EMAIL:-} ]] ||
  fail "set GIT_AUTHOR_NAME/EMAIL and GIT_COMMITTER_NAME/EMAIL to the AUR maintainer identity"

# The validated artifact must be exactly the three files and their manifest.
[[ $(find "$pub" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort) == \
   "$(printf '%s\n' "${FILES[@]}" PUBLICATION_SHA256SUMS | LC_ALL=C sort)" ]] ||
  fail "$pub must contain exactly ${FILES[*]} PUBLICATION_SHA256SUMS"
[[ $(awk '{print $2}' "$pub/PUBLICATION_SHA256SUMS" | LC_ALL=C sort) == "$(printf '%s\n' "${FILES[@]}" | LC_ALL=C sort)" ]] ||
  fail "PUBLICATION_SHA256SUMS must cover exactly ${FILES[*]}"
(cd "$pub" && sha256sum --check --strict PUBLICATION_SHA256SUMS) >&2 || fail "publication files do not match their manifest"

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
for file in "${FILES[@]}"; do
  cp "$pub/$file" "$stage/$file"
done

[[ ! -e $work ]] || fail "$work already exists"
git -c init.defaultBranch=master clone --quiet -- "$remote" "$work"
if git -C "$work" rev-parse --verify --quiet HEAD >/dev/null; then
  [[ $(git -C "$work" symbolic-ref --short HEAD) == master ]] || fail "the AUR repository's branch is not master"
else
  git -C "$work" symbolic-ref HEAD refs/heads/master
fi

status=$(bash "$here/aur-content-status.sh" "$stage" "$work" "$pkgver" "$pkgrel")
if [[ $status == same ]]; then
  echo "The AUR already has omarchy-blueprint $pkgver-$pkgrel with identical files; nothing to publish."
  exit 0
fi

for file in "${FILES[@]}"; do
  cp "$stage/$file" "$work/$file"
done
git -C "$work" add -- "${FILES[@]}"
changes=$(git -C "$work" status --porcelain --untracked-files=all)
while IFS= read -r line; do
  [[ -z $line || ${line:3} == PKGBUILD || ${line:3} == .SRCINFO || ${line:3} == LICENSE ]] ||
    fail "unexpected change in the AUR checkout: $line"
done <<< "$changes"
git -C "$work" commit --quiet -m "omarchy-blueprint $pkgver-$pkgrel"
git -C "$work" push --quiet origin HEAD:refs/heads/master
echo "Published omarchy-blueprint $pkgver-$pkgrel to the AUR ($status): $(git -C "$work" rev-parse HEAD)"
