"""Offline tests for the release tooling: tags, source archives, AUR rendering and publication status."""

import hashlib
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest


RELEASE = Path(__file__).resolve().parents[1]
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


if __name__ == "__main__":
    unittest.main()
