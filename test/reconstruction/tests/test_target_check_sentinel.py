"""The fresh Machine B check is a narrow known-gap sentinel, not an ignored result."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
FRESH_STDERR = ("Error: check packages: detect explicitly installed native packages: pacman -Qqen: "
                "exit status 1: warning: database file for 'core' does not exist (use '-Sy' to download)\n")


def sentinel(sync_databases: bool, status: int, stdout: str, stderr: str) -> tuple[subprocess.CompletedProcess, str]:
    with tempfile.TemporaryDirectory() as work:
        Path(work, "artifacts/target").mkdir(parents=True)
        Path(work, "stdout").write_text(stdout)
        Path(work, "stderr").write_text(stderr)
        script = f"""
            set -euo pipefail
            source '{ROOT}/lib/common.sh'
            trap 'ra_finish "$?"' EXIT
            source '{ROOT}/scenario/stage-guest.sh'
            source '{ROOT}/scenario/capture-source.sh'
            source '{ROOT}/scenario/preflight-target.sh'
            ra_guest_exec() {{ [[ $2 == *'$repo.db'* ]] && printf '%s' '{'core ' if sync_databases else ''}'; return 0; }}
            ra_blueprint() {{ cat '{work}/stdout'; cat '{work}/stderr' >&2; return {status}; }}
            ra_phase TARGET_PREFLIGHT
            ra_target_check_sentinel
            ra_pass TARGET_PREFLIGHT
        """
        result = subprocess.run(["bash", "-c", script], capture_output=True, text=True, timeout=30,
                                env={**os.environ, "RA_WORK": work}, stdin=subprocess.DEVNULL)
        summary = Path(work, "artifacts/summary.txt").read_text()
        return result, summary


class TargetCheckSentinelTests(unittest.TestCase):
    def test_known_gap_passes_preflight_and_is_reported(self):
        result, summary = sentinel(False, 1, "", FRESH_STDERR)
        self.assertEqual(result.returncode, 0, summary + result.stderr)
        self.assertIn("TARGET_PREFLIGHT PASS", summary)
        self.assertIn("known gap: packages.fresh-sync-database-readiness", summary)

    def test_unexpected_success_fails_and_says_to_update_the_harness(self):
        result, summary = sentinel(False, 0, '{"api_version": 1, "command": "check", "ok": true}', "")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("TARGET_PREFLIGHT FAIL", summary)
        self.assertIn("unexpectedly resolved", summary)
        self.assertNotIn("known gap:", summary)

    def test_other_check_failure_fails_preflight(self):
        result, summary = sentinel(False, 1, "", "Error: check themes: Omarchy did not report an active theme\n")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unexpected reason", summary)

    def test_refreshed_target_databases_invalidate_the_sentinel(self):
        result, summary = sentinel(True, 1, "", FRESH_STDERR)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("sync databases", summary)
        self.assertNotIn("known gap:", summary)


if __name__ == "__main__":
    unittest.main()
