"""Host-only checks for the deterministic fixture Git remote; no VM required."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
REVISION = "e161ca52ec7604e6fb335ef15fc212bba03a2994"


def harness(work: str, script: str) -> subprocess.CompletedProcess:
    # An isolated HOME keeps user Git config (signing, templates) out of the hash.
    return subprocess.run(
        ["bash", "-c", f"source '{ROOT}/lib/common.sh'; "
         f"source '{ROOT}/scenario/build-fixtures.sh'; {script}"],
        env={**os.environ, "RA_WORK": work, "HOME": work, "RA_GIT_PORT": "19443"},
        capture_output=True, text=True, timeout=60,
    )


class FixtureTests(unittest.TestCase):
    def test_expected_contract_uses_a_portable_https_remote(self):
        values = dict(line.split("=", 1) for line in
                      (ROOT / "fixtures/expected.env").read_text().splitlines() if line)
        self.assertEqual(values["RA_GIT_REVISION"], REVISION)
        self.assertEqual(values["RA_GIT_REMOTE"], "https://10.0.2.2:9443/blueprint-ra.git")

    def test_executable_fixtures_are_mode_0755(self):
        for fixture in ("scripts/blueprint-ra-helper", "hooks/blueprint-ra"):
            with self.subTest(fixture=fixture):
                self.assertEqual((ROOT / "fixtures" / fixture).stat().st_mode & 0o777, 0o755)

    def test_git_fixture_has_pinned_revision_and_is_served_over_https(self):
        with tempfile.TemporaryDirectory() as work:
            result = harness(work, "ra_build_git_fixture && ra_start_git_server && "
                                   "git -c http.sslCAInfo=\"$RA_WORK/git-tls/cert.pem\" "
                                   "clone -q https://127.0.0.1:19443/blueprint-ra.git \"$RA_WORK/clone\"; "
                                   "status=$?; ra_stop_git_server; exit $status")
            self.assertEqual(result.returncode, 0, result.stderr)
            head = subprocess.run(["git", "-C", f"{work}/clone", "rev-parse", "HEAD"],
                                  capture_output=True, text=True, check=True).stdout.strip()
            self.assertEqual(head, REVISION)
            self.assertFalse(Path(work, "git-tls/key.pem").stat().st_mode & 0o077)

    def test_server_refuses_to_start_when_the_revision_is_not_pinned(self):
        with tempfile.TemporaryDirectory() as work:
            result = harness(work, "RA_GIT_REVISION=0000000000000000000000000000000000000000 "
                                   "ra_build_git_fixture")
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("fixture revision", result.stderr)


if __name__ == "__main__":
    unittest.main()
