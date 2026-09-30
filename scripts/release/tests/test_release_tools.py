"""Offline tests for the release tooling: tags, source archives, AUR rendering and publication status."""

import hashlib
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import unittest


RELEASE = Path(__file__).resolve().parents[1]
REPO = RELEASE.parents[1]
WORKFLOWS = REPO / ".github" / "workflows"
SHA = "0123456789abcdef" * 4
URL = "https://github.com/Grenco/omarchy-blueprint/releases/download/v0.1.0/omarchy-blueprint-0.1.0.tar.gz"
FIXED_DATE = "2026-09-29T12:00:00+00:00"


def run(*args: str, cwd: Path, check: bool = True) -> subprocess.CompletedProcess:
    env = {**os.environ, "GIT_AUTHOR_DATE": FIXED_DATE, "GIT_COMMITTER_DATE": FIXED_DATE,
           "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1"}
    result = subprocess.run(list(args), cwd=cwd, capture_output=True, text=True, env=env, timeout=60)
    if check and result.returncode != 0:
        raise AssertionError(f"{args} failed: {result.stderr}")
    return result


def git(repo: Path, *args: str) -> str:
    return run("git", "-c", "user.name=Release Test", "-c", "user.email=release@example.test", *args, cwd=repo).stdout.strip()


def fixture_repo(root: Path) -> Path:
    """A repository with one commit on main and a divergent branch."""
    repo = root / "repo"
    repo.mkdir()
    git(repo, "init", "-q", "-b", "main")
    (repo / "go.mod").write_text("module example.test/blueprint\n")
    (repo / "cmd").mkdir()
    (repo / "cmd" / "main.go").write_text("package main\n\nfunc main() {}\n")
    git(repo, "add", ".")
    git(repo, "commit", "-q", "-m", "initial")
    git(repo, "tag", "-a", "v0.1.0", "-m", "v0.1.0")
    git(repo, "tag", "v0.1.1-light")
    git(repo, "checkout", "-q", "-b", "side")
    (repo / "side.txt").write_text("not on main\n")
    git(repo, "add", ".")
    git(repo, "commit", "-q", "-m", "side")
    git(repo, "tag", "-a", "v0.2.0", "-m", "v0.2.0")
    git(repo, "checkout", "-q", "main")
    return repo


def verify_tag(repo: Path, tag: str, main: str = "main") -> subprocess.CompletedProcess:
    return run("bash", str(RELEASE / "verify-release-tag.sh"), tag, main, cwd=repo, check=False)


def make_archive(repo: Path, ref: str, version: str, out: Path) -> subprocess.CompletedProcess:
    return run("bash", str(RELEASE / "make-source-archive.sh"), ref, version, str(out), cwd=repo, check=False)


class ReleaseTagTests(unittest.TestCase):
    def test_verify_release_tag_accepts_main_ancestor_and_prints_version(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = fixture_repo(Path(tmp))
            result = verify_tag(repo, "v0.1.0")
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout, "0.1.0\n")

    def test_verify_release_tag_accepts_a_lightweight_tag_by_peeling_to_its_commit(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = fixture_repo(Path(tmp))
            git(repo, "tag", "v1.2.3")
            result = verify_tag(repo, "v1.2.3")
            self.assertEqual((result.returncode, result.stdout), (0, "1.2.3\n"), result.stderr)

    def test_verify_release_tag_rejects_malformed_tag(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = fixture_repo(Path(tmp))
            for tag in ("0.1.0", "v0.1", "v01.0.0", "v0.01.0", "v0.1.0-rc1", "v0.1.0+build", "v0.1.1-light",
                        "V0.1.0", "v0.1.0 ", "v0.1.0\nv0.2.0", "--help", ""):
                with self.subTest(tag=tag):
                    result = verify_tag(repo, tag)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(result.stdout, "")

    def test_verify_release_tag_rejects_tag_not_contained_in_main(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = fixture_repo(Path(tmp))
            result = verify_tag(repo, "v0.2.0")
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(result.stdout, "")
            self.assertIn("main", result.stderr)

    def test_verify_release_tag_rejects_a_missing_tag_and_wrong_arguments(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = fixture_repo(Path(tmp))
            self.assertNotEqual(verify_tag(repo, "v9.9.9").returncode, 0)
            self.assertNotEqual(run("bash", str(RELEASE / "verify-release-tag.sh"), "v0.1.0", cwd=repo,
                                    check=False).returncode, 0)


class SourceArchiveTests(unittest.TestCase):
    def test_source_archive_is_reproducible_for_same_ref(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = fixture_repo(Path(tmp))
            first, second = Path(tmp, "a"), Path(tmp, "b")
            for out, ref in ((first, "v0.1.0"), (second, "v0.1.0")):
                result = make_archive(repo, ref, "0.1.0", out)
                self.assertEqual(result.returncode, 0, result.stderr)
            name = "omarchy-blueprint-0.1.0.tar.gz"
            a, b = (first / name).read_bytes(), (second / name).read_bytes()
            self.assertEqual(a, b)
            digest = hashlib.sha256(a).hexdigest()
            self.assertEqual((first / "SHA256SUMS").read_text(), f"{digest}  {name}\n")
            self.assertEqual((first / "SHA256SUMS").read_bytes(), (second / "SHA256SUMS").read_bytes())
            self.assertEqual(run("sha256sum", "-c", "SHA256SUMS", cwd=first).returncode, 0)

    def test_source_archive_ignores_the_callers_git_archive_configuration(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = fixture_repo(Path(tmp))
            make_archive(repo, "v0.1.0", "0.1.0", Path(tmp, "a"))
            git(repo, "config", "tar.umask", "0077")
            result = make_archive(repo, "v0.1.0", "0.1.0", Path(tmp, "b"))
            self.assertEqual(result.returncode, 0, result.stderr)
            name = "omarchy-blueprint-0.1.0.tar.gz"
            self.assertEqual(Path(tmp, "a", name).read_bytes(), Path(tmp, "b", name).read_bytes())

    def test_source_archive_has_versioned_top_level_directory(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = fixture_repo(Path(tmp))
            out = Path(tmp, "dist")
            self.assertEqual(make_archive(repo, "v0.1.0", "0.1.0", out).returncode, 0)
            with tarfile.open(out / "omarchy-blueprint-0.1.0.tar.gz") as archive:
                names = [member.name for member in archive.getmembers() if member.name != "pax_global_header"]
            self.assertTrue(names)
            for name in names:
                self.assertTrue(name == "omarchy-blueprint-0.1.0" or name.startswith("omarchy-blueprint-0.1.0/"), name)
            self.assertIn("omarchy-blueprint-0.1.0/go.mod", names)
            self.assertNotIn("omarchy-blueprint-0.1.0/side.txt", names)

    def test_source_archive_rejects_invalid_version(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = fixture_repo(Path(tmp))
            for version in ("v0.1.0", "0.1", "01.0.0", "0.1.0-rc1", "../0.1.0", "0.1.0/x", ""):
                with self.subTest(version=version):
                    out = Path(tmp, "dist")
                    self.assertNotEqual(make_archive(repo, "v0.1.0", version, out).returncode, 0)
                    self.assertFalse(any(out.glob("*")) if out.exists() else False)

    def test_source_archive_rerun_is_idempotent_but_never_overwrites_different_bytes(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = fixture_repo(Path(tmp))
            out = Path(tmp, "dist")
            self.assertEqual(make_archive(repo, "v0.1.0", "0.1.0", out).returncode, 0)
            original = (out / "omarchy-blueprint-0.1.0.tar.gz").read_bytes()
            self.assertEqual(make_archive(repo, "v0.1.0", "0.1.0", out).returncode, 0)
            result = make_archive(repo, "v0.2.0", "0.1.0", out)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual((out / "omarchy-blueprint-0.1.0.tar.gz").read_bytes(), original)
            self.assertEqual(sorted(p.name for p in out.iterdir()), ["SHA256SUMS", "omarchy-blueprint-0.1.0.tar.gz"])


def render(out: Path, **overrides: str) -> subprocess.CompletedProcess:
    values = {"version": "0.1.0", "pkgrel": "1", "source-url": URL, "sha256": SHA,
              "maintainer-name": "Blueprint Maintainer", "maintainer-email": "maintainer@example.test", **overrides}
    args = ["python3", str(RELEASE / "render-aur-package.py")]
    for key, value in values.items():
        args += [f"--{key}", value]
    return run(*args, "--output-dir", str(out), cwd=REPO, check=False)


def rendered(**overrides: str) -> str:
    with tempfile.TemporaryDirectory() as tmp:
        result = render(Path(tmp), **overrides)
        if result.returncode != 0:
            raise AssertionError(result.stderr)
        return Path(tmp, "PKGBUILD").read_text()


def shell_array(pkgbuild: str, name: str) -> str:
    match = re.search(rf"^{name}=\((.*?)\)$", pkgbuild, re.M | re.S)
    return match.group(1) if match else ""


class AurRenderTests(unittest.TestCase):
    def test_render_aur_package_pins_release_identity(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = render(Path(tmp))
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(sorted(p.name for p in Path(tmp).iterdir()), ["LICENSE", "PKGBUILD"])
            self.assertEqual(Path(tmp, "LICENSE").read_bytes(), (REPO / "packaging/aur/LICENSE").read_bytes())
            pkgbuild = Path(tmp, "PKGBUILD").read_text()
        for line in ("pkgname=omarchy-blueprint", "pkgver=0.1.0", "pkgrel=1", "arch=('x86_64')",
                     "license=('MIT')", "makedepends=('go')", "checkdepends=('git')",
                     "# Maintainer: Blueprint Maintainer <maintainer@example.test>",
                     f"sha256sums=('{SHA}')"):
            with self.subTest(line=line):
                self.assertIn(line + "\n", pkgbuild)
        self.assertIn(f'source=("omarchy-blueprint-${{pkgver}}.tar.gz::{URL}")', pkgbuild)
        self.assertIn("omarchy-blueprint-0.1.0.tar.gz", URL)
        self.assertNotRegex(pkgbuild, r"@@[A-Z_]+@@")

    def test_render_aur_package_rejects_bad_version_pkgrel_url_or_sha(self):
        bad = {
            "version": ["v0.1.0", "0.1", "01.0.0", "0.1.0-rc1", "0.1.0\npkgrel=9", ""],
            "pkgrel": ["0", "-1", "01", "1.1", "x", "1\n", ""],
            "source-url": ["http://example.test/omarchy-blueprint-0.1.0.tar.gz",
                           "https://example.test/other-0.1.0.tar.gz",
                           "https://example.test/$(id)/omarchy-blueprint-0.1.0.tar.gz",
                           "https://example.test/a b/omarchy-blueprint-0.1.0.tar.gz",
                           "https://example.test/\"/omarchy-blueprint-0.1.0.tar.gz",
                           "https://example.test/omarchy-blueprint-0.1.0.tar.gz\n", ""],
            "sha256": ["SKIP", SHA.upper(), SHA[:-1], SHA + "0", "g" * 64, ""],
            "maintainer-name": ["", "Name\nsha256sums=('SKIP')", "Tab\there", "Name <x@y>"],
            "maintainer-email": ["", "not-an-email", "a@b\nc", "a b@example.test", "<a@b.test>"],
        }
        for key, values in bad.items():
            for value in values:
                with self.subTest(key=key, value=value), tempfile.TemporaryDirectory() as tmp:
                    result = render(Path(tmp), **{key: value})
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(list(Path(tmp).iterdir()), [])

    def test_render_aur_package_never_uses_skip_checksum(self):
        self.assertNotIn("SKIP", rendered())
        self.assertNotIn("SKIP", (REPO / "packaging/aur/PKGBUILD.template").read_text())

    def test_render_aur_package_injects_linker_version(self):
        pkgbuild = rendered()
        self.assertIn("-X github.com/Grenco/omarchy-blueprint/internal/buildinfo.Version=${pkgver}", pkgbuild)
        self.assertIn('GOFLAGS="-buildmode=pie -trimpath -mod=readonly -modcacherw"', pkgbuild)
        self.assertIn("go test ./...", pkgbuild)
        self.assertIn('install -Dm755 build/omarchy-blueprint "${pkgdir}/usr/bin/omarchy-blueprint"', pkgbuild)
        self.assertIn('install -Dm644 LICENSE "${pkgdir}/usr/share/licenses/${pkgname}/LICENSE"', pkgbuild)
        self.assertNotIn("/usr/local", pkgbuild)

    def test_render_aur_package_keeps_omarchy_as_documented_runtime_not_invented_dependency(self):
        pkgbuild = rendered()
        for array in ("depends", "makedepends", "checkdepends", "optdepends"):
            with self.subTest(array=array):
                self.assertNotRegex(shell_array(pkgbuild, array), r"'omarchy[<>=:']")
        self.assertIn("Omarchy 4", pkgbuild)
        self.assertEqual(shell_array(pkgbuild, "depends"), "'glibc' 'git'")  # PIE + external linker: dynamically linked against libc

    def test_render_aur_package_output_is_byte_stable(self):
        self.assertEqual(rendered(), rendered())

    def test_packaging_template_can_render_pr_validation_version(self):
        # Pre-release validation renders reviewed code as 0.0.0 from a local
        # archive: no public release has to exist.
        with tempfile.TemporaryDirectory() as tmp:
            repo = fixture_repo(Path(tmp))
            self.assertEqual(make_archive(repo, "HEAD", "0.0.0", Path(tmp, "dist")).returncode, 0)
            digest = Path(tmp, "dist", "SHA256SUMS").read_text().split()[0]
            url = "https://example.invalid/omarchy-blueprint/releases/omarchy-blueprint-0.0.0.tar.gz"
            result = render(Path(tmp, "package"), version="0.0.0", sha256=digest, **{"source-url": url})
            self.assertEqual(result.returncode, 0, result.stderr)
            pkgbuild = Path(tmp, "package", "PKGBUILD").read_text()
        self.assertIn("pkgver=0.0.0\n", pkgbuild)
        self.assertIn(f"sha256sums=('{digest}')\n", pkgbuild)
        self.assertIn(f"::{url}\")", pkgbuild)


def srcinfo(pkgver: str, pkgrel: str) -> str:
    return (f"pkgbase = omarchy-blueprint\n\tpkgver = {pkgver}\n\tpkgrel = {pkgrel}\n"
            "\tarch = x86_64\n\npkgname = omarchy-blueprint\n")


def publication(root: Path, pkgver: str = "0.1.0", pkgrel: str = "1", pkgbuild: str = "PKGBUILD body\n") -> Path:
    out = root / "publication"
    out.mkdir()
    (out / "PKGBUILD").write_text(pkgbuild)
    (out / ".SRCINFO").write_text(srcinfo(pkgver, pkgrel))
    (out / "LICENSE").write_text("0BSD\n")
    return out


def aur_checkout(root: Path, files: dict[str, str] | None = None) -> Path:
    checkout = root / "aur"
    checkout.mkdir()
    git(checkout, "init", "-q", "-b", "master")
    for name, text in (files or {}).items():
        (checkout / name).write_text(text)
    return checkout


def status(pub: Path, checkout: Path, pkgver: str = "0.1.0", pkgrel: str = "1") -> subprocess.CompletedProcess:
    return run("bash", str(RELEASE / "aur-content-status.sh"), str(pub), str(checkout), pkgver, pkgrel,
               cwd=pub.parent, check=False)


def published(pkgver: str, pkgrel: str, pkgbuild: str = "PKGBUILD body\n") -> dict[str, str]:
    return {"PKGBUILD": pkgbuild, ".SRCINFO": srcinfo(pkgver, pkgrel), "LICENSE": "0BSD\n"}


class AurContentStatusTests(unittest.TestCase):
    def test_aur_content_status_new_for_empty_checkout(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = status(publication(Path(tmp)), aur_checkout(Path(tmp)))
            self.assertEqual((result.returncode, result.stdout), (0, "new\n"), result.stderr)

    def test_aur_content_status_same_for_identical_publication(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = status(publication(Path(tmp)), aur_checkout(Path(tmp), published("0.1.0", "1")))
            self.assertEqual((result.returncode, result.stdout), (0, "same\n"), result.stderr)

    def test_aur_content_status_update_for_older_revision(self):
        for older in (("0.0.9", "3"), ("0.1.0", "1")):
            with self.subTest(older=older), tempfile.TemporaryDirectory() as tmp:
                pub = publication(Path(tmp), "0.1.0", "2") if older == ("0.1.0", "1") else publication(Path(tmp))
                pkgrel = "2" if older == ("0.1.0", "1") else "1"
                result = status(pub, aur_checkout(Path(tmp), published(*older, "old body\n")), "0.1.0", pkgrel)
                self.assertEqual((result.returncode, result.stdout), (0, "update\n"), result.stderr)

    def test_aur_content_status_rejects_same_revision_different_content(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = status(publication(Path(tmp)), aur_checkout(Path(tmp), published("0.1.0", "1", "other body\n")))
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(result.stdout, "")
            self.assertIn("0.1.0-1", result.stderr)

    def test_aur_content_status_rejects_a_newer_published_revision(self):
        for newer in (("0.1.0", "2"), ("0.2.0", "1"), ("0.1.10", "1")):
            with self.subTest(newer=newer), tempfile.TemporaryDirectory() as tmp:
                result = status(publication(Path(tmp), "0.1.9"), aur_checkout(Path(tmp), published(*newer)), "0.1.9")
                if newer == ("0.1.0", "2"):
                    self.assertEqual(result.stdout, "update\n", result.stderr)
                else:
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(result.stdout, "")

    def test_aur_content_status_rejects_unexpected_publication_files(self):
        with tempfile.TemporaryDirectory() as tmp:
            pub = publication(Path(tmp))
            (pub / "id_ed25519").write_text("not a publication file\n")
            result = status(pub, aur_checkout(Path(tmp)))
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(result.stdout, "")
        with tempfile.TemporaryDirectory() as tmp:
            pub = publication(Path(tmp))
            (pub / "LICENSE").unlink()
            self.assertNotEqual(status(pub, aur_checkout(Path(tmp))).returncode, 0)
        with tempfile.TemporaryDirectory() as tmp:
            files = {**published("0.0.9", "1"), "notes.txt": "extra\n"}
            self.assertNotEqual(status(publication(Path(tmp)), aur_checkout(Path(tmp), files)).returncode, 0)

    def test_aur_content_status_rejects_inconsistent_or_malformed_inputs(self):
        cases = [("0.1.0", "2"), ("v0.1.0", "1"), ("0.1.0", "0"), ("0.1.0", "1; rm -rf /")]
        for pkgver, pkgrel in cases:
            with self.subTest(pkgver=pkgver, pkgrel=pkgrel), tempfile.TemporaryDirectory() as tmp:
                result = status(publication(Path(tmp)), aur_checkout(Path(tmp)), pkgver, pkgrel)
                self.assertNotEqual(result.returncode, 0)
        with tempfile.TemporaryDirectory() as tmp:
            files = {"PKGBUILD": "x\n", "LICENSE": "0BSD\n"}  # no .SRCINFO to read the revision from
            self.assertNotEqual(status(publication(Path(tmp)), aur_checkout(Path(tmp), files)).returncode, 0)


# A stand-in for the GitHub CLI, backed by a directory: <state>/<tag>/draft
# holds "true" or "false" and <state>/<tag>/assets/ the release assets;
# <state>/refs/<tag> and <state>/tagobjs/<sha> hold "<type> <sha>" for the
# Git refs API. FAKE_GH_MOVE_ON_CREATE moves the tag while a release is created.
FAKE_GH = r"""#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$FAKE_GH_STATE/calls.log"
if [[ $1 == api ]]; then
  case $2 in
    */git/ref/tags/*) file="$FAKE_GH_STATE/refs/${2##*/}" ;;
    */git/tags/*) file="$FAKE_GH_STATE/tagobjs/${2##*/}" ;;
    *) exit 2 ;;
  esac
  [[ -f $file ]] || { echo "HTTP 404" >&2; exit 1; }
  cat "$file"; exit 0
fi
[[ $1 == release ]] || exit 2
action=$2 tag=$3; shift 3
rel="$FAKE_GH_STATE/$tag"
case $action in
  view)
    [[ -d $rel ]] || { echo "release not found" >&2; exit 1; }
    case $* in
      *isDraft*) cat "$rel/draft" ;;
      *assets*) ls "$rel/assets" ;;
    esac ;;
  create)
    [[ ! -d $rel ]] || { echo "already exists" >&2; exit 1; }
    mkdir -p "$rel/assets"; echo true > "$rel/draft"
    for arg in "$@"; do [[ -f $arg ]] && cp "$arg" "$rel/assets/"; done
    [[ -z ${FAKE_GH_MOVE_ON_CREATE:-} ]] || echo "commit $FAKE_GH_MOVE_ON_CREATE" > "$FAKE_GH_STATE/refs/$tag"
    true ;;
  upload)
    for arg in "$@"; do
      [[ $arg == --clobber ]] && exit 3
      [[ -f $arg && ! -e $rel/assets/${arg##*/} ]] && cp "$arg" "$rel/assets/"
    done; true ;;
  download)
    dir="" pattern=""
    while (( $# )); do
      case $1 in --dir) dir=$2; shift ;; --pattern) pattern=$2; shift ;; esac; shift
    done
    cp "$rel/assets/$pattern" "$dir/" ;;
  *) exit 2 ;;
esac
"""


class DraftReleaseTests(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(lambda: subprocess.run(["rm", "-rf", str(self.tmp)]))
        self.state = self.tmp / "github"
        self.state.mkdir()
        bin_dir = self.tmp / "bin"
        bin_dir.mkdir()
        (bin_dir / "gh").write_text(FAKE_GH)
        (bin_dir / "gh").chmod(0o755)
        self.env = {**os.environ, "PATH": f"{bin_dir}:{os.environ['PATH']}", "FAKE_GH_STATE": str(self.state)}
        repo = fixture_repo(self.tmp)
        self.dist = self.tmp / "dist"
        self.assertEqual(make_archive(repo, "v0.1.0", "0.1.0", self.dist).returncode, 0)
        # GitHub's view of v0.1.0: an annotated tag object peeling to the verified commit.
        self.commit = git(repo, "rev-parse", "v0.1.0^{commit}")
        self.other = git(repo, "rev-parse", "side")
        self.set_tag(annotated=True)

    def set_tag(self, commit: str = "", annotated: bool = False) -> None:
        commit = commit or self.commit
        for directory in ("refs", "tagobjs"):
            (self.state / directory).mkdir(exist_ok=True)
        if annotated:
            (self.state / "refs" / "v0.1.0").write_text("tag " + "a" * 40 + "\n")
            (self.state / "tagobjs" / ("a" * 40)).write_text(f"commit {commit}\n")
        else:
            (self.state / "refs" / "v0.1.0").write_text(f"commit {commit}\n")

    def prepare(self, tag: str = "v0.1.0", version: str = "0.1.0", commit: str = "") -> subprocess.CompletedProcess:
        return subprocess.run(["bash", str(RELEASE / "prepare-draft-release.sh"), tag, version,
                               commit or self.commit, str(self.dist)],
                              capture_output=True, text=True, env=self.env, timeout=60)

    def mutations(self) -> list[str]:
        return [line for line in self.calls().splitlines()
                if re.match(r"release (create|upload|edit|delete)", line)]

    def existing(self, draft: bool, assets: dict[str, bytes]) -> Path:
        rel = self.state / "v0.1.0"
        (rel / "assets").mkdir(parents=True)
        (rel / "draft").write_text("true\n" if draft else "false\n")
        for name, data in assets.items():
            (rel / "assets" / name).write_bytes(data)
        return rel

    def verified(self) -> dict[str, bytes]:
        return {p.name: p.read_bytes() for p in self.dist.iterdir()}

    def calls(self) -> str:
        log = self.state / "calls.log"
        return log.read_text() if log.exists() else ""

    def test_creates_a_draft_with_both_assets_when_no_release_exists(self):
        result = self.prepare()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Created draft release v0.1.0", result.stdout)
        self.assertEqual((self.state / "v0.1.0" / "draft").read_text().strip(), "true")
        self.assertEqual({p.name: p.read_bytes() for p in (self.state / "v0.1.0" / "assets").iterdir()}, self.verified())
        create = [line for line in self.calls().splitlines() if line.startswith("release create")]
        self.assertEqual(len(create), 1)
        self.assertIn("--draft", create[0])
        self.assertIn("--verify-tag", create[0])

    def test_rerun_on_a_matching_draft_changes_nothing(self):
        self.existing(True, self.verified())
        result = self.prepare()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("holds the verified assets", result.stdout)
        self.assertNotRegex(self.calls(), r"release (create|upload)")

    def test_draft_missing_an_asset_gets_only_that_asset_uploaded(self):
        verified = self.verified()
        rel = self.existing(True, {"SHA256SUMS": verified["SHA256SUMS"]})
        result = self.prepare()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((rel / "assets" / "omarchy-blueprint-0.1.0.tar.gz").read_bytes(),
                         verified["omarchy-blueprint-0.1.0.tar.gz"])
        self.assertNotIn("--clobber", self.calls())

    def test_draft_with_different_bytes_fails_without_replacing_them(self):
        rel = self.existing(True, {**self.verified(), "omarchy-blueprint-0.1.0.tar.gz": b"other bytes"})
        result = self.prepare()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("never replaced", result.stderr)
        self.assertEqual((rel / "assets" / "omarchy-blueprint-0.1.0.tar.gz").read_bytes(), b"other bytes")
        self.assertNotRegex(self.calls(), r"release (create|upload)")

    def test_published_release_is_never_modified(self):
        self.existing(False, self.verified())
        result = self.prepare()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("already published", result.stdout)
        for assets in ({"SHA256SUMS": self.verified()["SHA256SUMS"]},
                       {**self.verified(), "SHA256SUMS": b"different\n"}):
            with self.subTest(assets=sorted(assets)):
                subprocess.run(["rm", "-rf", str(self.state / "v0.1.0")])
                (self.state / "calls.log").unlink(missing_ok=True)
                self.existing(False, assets)
                result = self.prepare()
                self.assertNotEqual(result.returncode, 0)
                self.assertNotRegex(self.calls(), r"release (create|upload|edit|delete)")

    def test_lightweight_tag_at_the_verified_commit_is_accepted(self):
        self.set_tag()
        result = self.prepare()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(self.commit, result.stdout)

    def test_tag_moved_since_verification_changes_nothing(self):
        for annotated in (True, False):
            with self.subTest(annotated=annotated):
                self.set_tag(self.other, annotated)
                result = self.prepare()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("not the verified commit", result.stderr)
                self.assertEqual(self.mutations(), [])
        (self.state / "refs" / "v0.1.0").unlink()
        self.assertNotEqual(self.prepare().returncode, 0)
        self.assertEqual(self.mutations(), [])

    def test_existing_draft_whose_tag_moved_gets_no_upload(self):
        self.existing(True, {"SHA256SUMS": self.verified()["SHA256SUMS"]})
        self.set_tag(self.other)
        self.assertNotEqual(self.prepare().returncode, 0)
        self.assertEqual(self.mutations(), [])

    def test_tag_moved_while_creating_the_draft_is_reported(self):
        self.env["FAKE_GH_MOVE_ON_CREATE"] = self.other
        result = self.prepare()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not the verified commit", result.stderr)

    def test_missing_archive_with_divergent_checksums_fails_with_zero_uploads(self):
        rel = self.existing(True, {"SHA256SUMS": b"divergent\n"})
        result = self.prepare()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("never replaced", result.stderr)
        self.assertEqual(self.mutations(), [])
        self.assertEqual(sorted(p.name for p in (rel / "assets").iterdir()), ["SHA256SUMS"])

    def test_rejects_bad_inputs_before_calling_github(self):
        for tag, version in (("v0.1", "0.1"), ("v0.1.0", "0.1.1"), ("0.1.0", "0.1.0"), ("v0.1.0-rc1", "0.1.0-rc1")):
            with self.subTest(tag=tag, version=version):
                self.assertNotEqual(self.prepare(tag, version).returncode, 0)
        (self.dist / "extra").write_text("x\n")
        self.assertNotEqual(self.prepare().returncode, 0)
        (self.dist / "extra").unlink()
        for commit in ("abc123", self.commit.upper(), self.commit + "0"):
            with self.subTest(commit=commit):
                self.assertNotEqual(self.prepare(commit=commit).returncode, 0)
        (self.dist / "omarchy-blueprint-0.1.0.tar.gz").write_bytes(b"tampered")
        self.assertNotEqual(self.prepare().returncode, 0)
        self.assertEqual(self.calls(), "")


def workflow(name: str) -> str:
    return (WORKFLOWS / name).read_text()


class WorkflowBoundaryTests(unittest.TestCase):
    def test_no_workflow_uses_pull_request_target_or_disables_host_checking(self):
        for path in WORKFLOWS.glob("*.yml"):
            with self.subTest(workflow=path.name):
                text = path.read_text()
                self.assertNotIn("pull_request_target", text)
                self.assertNotIn("StrictHostKeyChecking=no", text)

    def test_release_and_package_validation_never_reference_aur_credentials(self):
        for name in ("release.yml", "aur-package-validation.yml"):
            with self.subTest(workflow=name):
                self.assertNotIn("AUR_", workflow(name))
                self.assertNotIn("secrets.", workflow(name))

    def test_release_workflow_actions_are_pinned_to_full_commit_shas(self):
        for name in ("release.yml", "aur-package-validation.yml"):
            uses = re.findall(r"uses:\s*(\S+)", workflow(name))
            self.assertTrue(uses)
            for action in uses:
                with self.subTest(workflow=name, action=action):
                    self.assertRegex(action, r"^[\w.-]+/[\w.-]+@[0-9a-f]{40}$")

    def test_release_workflow_only_prepares_a_draft_with_least_privilege(self):
        text = workflow("release.yml")
        self.assertRegex(text, r"(?m)^on:\n  push:\n    tags: \[\"v\*\"\]\n\n")
        self.assertRegex(text, r"(?m)^permissions:\n  contents: read\n")
        self.assertEqual(text.count("contents: write"), 1)
        draft_job = text.split("\n  draft:\n", 1)[1]
        self.assertIn("contents: write", draft_job)
        self.assertNotIn("contents: write", text.split("\n  draft:\n", 1)[0])
        self.assertIn("needs: verify", draft_job)
        self.assertIn("verify-release-tag.sh", text)
        self.assertIn("prepare-draft-release.sh", draft_job)
        # The verified commit, not just the version, reaches the write-capable job.
        self.assertIn("commit: ${{ steps.tag.outputs.commit }}", text)
        self.assertIn("COMMIT: ${{ needs.verify.outputs.commit }}", draft_job)
        self.assertIn('prepare-draft-release.sh "$TAG" "$VERSION" "$COMMIT" dist', draft_job)
        self.assertIn('make-source-archive.sh "$COMMIT"', text)
        for publish in ("--draft=false", "gh release edit", "gh release create"):
            self.assertNotIn(publish, text)

    def test_package_validation_is_read_only_and_builds_in_a_fresh_arch_container(self):
        text = workflow("aur-package-validation.yml")
        self.assertRegex(text, r"(?m)^permissions:\n  contents: read\n")
        self.assertNotIn("contents: write", text)
        self.assertIn("container: archlinux:base-devel", text)
        self.assertIn("validate-arch-package.sh", text)
        script = (RELEASE / "validate-arch-package.sh").read_text()
        self.assertIn("runuser -u builder -- \"$@\"", script)
        self.assertRegex(script, r"as_builder makepkg --cleanbuild --check")
        self.assertIn("namcap", script)


if __name__ == "__main__":
    unittest.main()
