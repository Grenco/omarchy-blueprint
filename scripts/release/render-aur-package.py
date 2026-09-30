#!/usr/bin/env python3
"""Render the concrete omarchy-blueprint PKGBUILD from explicit, validated release inputs.

Writes <output-dir>/PKGBUILD and copies the 0BSD package-source LICENSE beside
it. .SRCINFO is deliberately not produced here: it is generated from the
rendered PKGBUILD with `makepkg --printsrcinfo`.
"""

import argparse
from pathlib import Path
import re
import sys


REPO = Path(__file__).resolve().parents[2]
TEMPLATE = REPO / "packaging" / "aur" / "PKGBUILD.template"
LICENSE = REPO / "packaging" / "aur" / "LICENSE"

VERSION = re.compile(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")
PKGREL = re.compile(r"[1-9][0-9]*")
SHA256 = re.compile(r"[0-9a-f]{64}")
# Only characters that stay literal inside a double-quoted shell string.
URL = re.compile(r"https://[A-Za-z0-9.-]+(?::[0-9]+)?/[A-Za-z0-9._~/%+-]*")
NAME = re.compile(r"[^\x00-\x1f\x7f<>@]*[^\s\x00-\x1f\x7f<>@][^\x00-\x1f\x7f<>@]*")
EMAIL = re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}")
MARKER = re.compile(r"@@[A-Z_]+@@")


def validate(args: argparse.Namespace) -> None:
    checks = [
        ("version", VERSION, args.version, "MAJOR.MINOR.PATCH without a leading v"),
        ("pkgrel", PKGREL, args.pkgrel, "a positive integer"),
        ("sha256", SHA256, args.sha256, "64 lowercase hexadecimal characters"),
        ("source-url", URL, args.source_url, "an https URL without shell metacharacters"),
        ("maintainer-name", NAME, args.maintainer_name, "a single-line name without <, > or @"),
        ("maintainer-email", EMAIL, args.maintainer_email, "a single e-mail address"),
    ]
    for option, pattern, value, expected in checks:
        if not pattern.fullmatch(value):
            raise ValueError(f"--{option} must be {expected}: {value!r}")
    asset = f"omarchy-blueprint-{args.version}.tar.gz"
    if not args.source_url.endswith("/" + asset):
        raise ValueError(f"--source-url must name the release asset {asset}: {args.source_url!r}")


def render(args: argparse.Namespace) -> str:
    values = {
        "@@VERSION@@": args.version,
        "@@PKGREL@@": args.pkgrel,
        "@@SOURCE_URL@@": args.source_url,
        "@@SHA256@@": args.sha256,
        "@@MAINTAINER@@": f"{args.maintainer_name.strip()} <{args.maintainer_email}>",
    }
    text = TEMPLATE.read_text()
    for marker, value in values.items():
        if marker not in text:
            raise ValueError(f"template has no {marker} marker")
        text = text.replace(marker, value)
    leftover = MARKER.search(text)
    if leftover:
        raise ValueError(f"template marker left unresolved: {leftover.group(0)}")
    return text


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    for option in ("version", "pkgrel", "source-url", "sha256", "maintainer-name", "maintainer-email"):
        parser.add_argument(f"--{option}", required=True)
    parser.add_argument("--output-dir", required=True, type=Path)
    args = parser.parse_args()
    try:
        validate(args)
        pkgbuild = render(args)
    except ValueError as error:
        print(f"render-aur-package: {error}", file=sys.stderr)
        return 1
    args.output_dir.mkdir(parents=True, exist_ok=True)
    (args.output_dir / "PKGBUILD").write_text(pkgbuild)
    (args.output_dir / "LICENSE").write_bytes(LICENSE.read_bytes())
    return 0


if __name__ == "__main__":
    sys.exit(main())
