#!/usr/bin/env bash
# Usage: aur-content-status.sh <validated-publication-dir> <aur-checkout-dir> <pkgver> <pkgrel>
#
# Classifies a validated publication (exactly PKGBUILD, .SRCINFO and LICENSE)
# against a checkout of the AUR package repository and prints one status:
#   new     the AUR repository has no package files yet
#   same    it already holds exactly this revision with identical files
#   update  it holds an older revision
# Fails, printing nothing on stdout, for anything that needs a human: the
# same revision with different content, a newer revision, unexpected files,
# or inputs that disagree with each other.
set -euo pipefail

FILES=(PKGBUILD .SRCINFO LICENSE)

fail() {
  printf 'aur-content-status: %s\n' "$1" >&2
  exit 1
}

# Prints the value of a top-level pkgbase field from a .SRCINFO file.
srcinfo_field() {
  awk -v key="$2" -F ' = ' '$1 == "\t" key { print $2; exit }' "$1"
}

# Succeeds when revision $1-$2 is older than $3-$4 (strict numeric SemVer).
older() {
  local IFS=.
  local -a a=($1) b=($3)
  local i
  for i in 0 1 2; do
    (( 10#${a[i]} < 10#${b[i]} )) && return 0
    (( 10#${a[i]} > 10#${b[i]} )) && return 1
  done
  (( 10#$2 < 10#$4 ))
}

# Lists the non-Git entries of a directory, one per line, sorted.
entries() {
  find "$1" -mindepth 1 -maxdepth 1 ! -name .git -printf '%f\n' | LC_ALL=C sort
}

(( $# == 4 )) || fail "usage: aur-content-status.sh <validated-publication-dir> <aur-checkout-dir> <pkgver> <pkgrel>"
pub=$1 checkout=$2 pkgver=$3 pkgrel=$4
semver='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
[[ $pkgver =~ $semver ]] || fail "not a MAJOR.MINOR.PATCH pkgver: $(printf '%q' "$pkgver")"
[[ $pkgrel =~ ^[1-9][0-9]*$ ]] || fail "not a positive integer pkgrel: $(printf '%q' "$pkgrel")"
[[ -d $pub && -d $checkout/.git ]] || fail "need a publication directory and a Git checkout"

expected=$(printf '%s\n' "${FILES[@]}" | LC_ALL=C sort)
[[ $(entries "$pub") == "$expected" ]] ||
  fail "publication directory must contain exactly: ${FILES[*]}"
[[ $(srcinfo_field "$pub/.SRCINFO" pkgver) == "$pkgver" && $(srcinfo_field "$pub/.SRCINFO" pkgrel) == "$pkgrel" ]] ||
  fail "publication .SRCINFO does not declare $pkgver-$pkgrel"

present=$(entries "$checkout")
if [[ -z $present ]]; then
  echo new
  exit 0
fi
[[ $present == "$expected" ]] ||
  fail "AUR checkout holds files other than exactly ${FILES[*]}: $(tr '\n' ' ' <<< "$present")"
aur_pkgver=$(srcinfo_field "$checkout/.SRCINFO" pkgver)
aur_pkgrel=$(srcinfo_field "$checkout/.SRCINFO" pkgrel)
[[ $aur_pkgver =~ $semver && $aur_pkgrel =~ ^[1-9][0-9]*$ ]] ||
  fail "cannot read the published revision from the AUR .SRCINFO"

if [[ $aur_pkgver == "$pkgver" && $aur_pkgrel == "$pkgrel" ]]; then
  for file in "${FILES[@]}"; do
    cmp -s "$pub/$file" "$checkout/$file" ||
      fail "AUR already has $pkgver-$pkgrel with a different $file; investigate before publishing"
  done
  echo same
elif older "$aur_pkgver" "$aur_pkgrel" "$pkgver" "$pkgrel"; then
  echo update
else
  fail "AUR already has newer revision $aur_pkgver-$aur_pkgrel than $pkgver-$pkgrel"
fi
