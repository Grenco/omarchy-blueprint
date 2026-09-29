#!/usr/bin/env bash
# Usage: validate-arch-package.sh <package-dir> <pkgver> <pkgrel>
#
# Proves a rendered omarchy-blueprint PKGBUILD in a disposable Arch
# environment. Must run as root there, only to create the unprivileged
# builder user and to install the built package; makepkg itself runs as
# builder. <package-dir> holds the rendered PKGBUILD and LICENSE, plus the
# source archive when pre-release validation seeds it (otherwise makepkg
# downloads the source URL). On success .SRCINFO, generated from the
# PKGBUILD with makepkg --printsrcinfo, is written back to <package-dir>.
#
# This proves packaging, installability and version identity only; it runs
# no Capture or Restore.
set -euo pipefail

fail() {
  printf 'validate-arch-package: %s\n' "$1" >&2
  exit 1
}

(( $# == 3 )) || fail "usage: validate-arch-package.sh <package-dir> <pkgver> <pkgrel>"
input=$1 pkgver=$2 pkgrel=$3
[[ $pkgver =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
  fail "not a MAJOR.MINOR.PATCH pkgver: $(printf '%q' "$pkgver")"
[[ $pkgrel =~ ^[1-9][0-9]*$ ]] || fail "not a positive integer pkgrel: $(printf '%q' "$pkgrel")"
[[ -f $input/PKGBUILD && -f $input/LICENSE ]] || fail "$input must hold the rendered PKGBUILD and LICENSE"
(( EUID == 0 )) || fail "run as root inside a disposable Arch container"
[[ -f /etc/arch-release ]] || fail "not an Arch Linux environment"

id builder >/dev/null 2>&1 || useradd --create-home builder
as_builder() {
  runuser -u builder -- "$@"
}

# Build in a directory only builder owns, from a copy of the inputs.
work=/home/builder/package-validation
rm -rf "$work"
install -d -o builder -g builder "$work"
cp -a "$input"/. "$work"/
chown -R builder:builder "$work"
cd "$work"

echo "== .SRCINFO (makepkg --printsrcinfo)"
as_builder makepkg --printsrcinfo > "$work/.SRCINFO.new"
mv "$work/.SRCINFO.new" "$work/.SRCINFO"
cat .SRCINFO

echo "== makepkg --cleanbuild --check"
as_builder makepkg --cleanbuild --check --noconfirm
package=""
while IFS= read -r candidate; do
  [[ ${candidate##*/} == omarchy-blueprint-"$pkgver"-"$pkgrel"-x86_64.pkg.tar.* ]] && package=$candidate
done < <(as_builder makepkg --packagelist)
[[ -n $package && -f $package ]] || fail "makepkg did not produce omarchy-blueprint-$pkgver-$pkgrel-x86_64"
echo "built $package"

echo "== namcap"
errors=0
for target in PKGBUILD "$package"; do
  report=$(namcap "$target" 2>&1) || fail "namcap could not check $target: $report"
  printf '%s\n' "${report:-(no findings for $target)}"
  if grep -q ' E: ' <<< "$report"; then
    errors=1
  fi
done
(( errors == 0 )) || fail "namcap reported errors"

echo "== package contents"
expected=$(printf '%s\n' /usr/ /usr/bin/ /usr/bin/omarchy-blueprint /usr/share/ /usr/share/licenses/ \
  /usr/share/licenses/omarchy-blueprint/ /usr/share/licenses/omarchy-blueprint/LICENSE)
contents=$(pacman -Qlpq "$package" | LC_ALL=C sort)
printf '%s\n' "$contents"
[[ $contents == "$(LC_ALL=C sort <<< "$expected")" ]] ||
  fail "package must contain only the binary and its license"

echo "== install and identify"
pacman -U --noconfirm "$package"
[[ $(pacman -Q omarchy-blueprint) == "omarchy-blueprint $pkgver-$pkgrel" ]] ||
  fail "installed package is not omarchy-blueprint $pkgver-$pkgrel"
[[ $(pacman -Qoq /usr/bin/omarchy-blueprint) == omarchy-blueprint ]] ||
  fail "/usr/bin/omarchy-blueprint is not owned by omarchy-blueprint"
[[ $(command -v omarchy-blueprint) == /usr/bin/omarchy-blueprint ]] ||
  fail "omarchy-blueprint on PATH is not the packaged binary"
version=$(cd / && as_builder omarchy-blueprint --version)
[[ $version == "omarchy-blueprint version $pkgver" ]] || fail "installed binary reports: $version"
echo "$version"

cp "$work/.SRCINFO" "$input/.SRCINFO"
echo "validated omarchy-blueprint $pkgver-$pkgrel"
