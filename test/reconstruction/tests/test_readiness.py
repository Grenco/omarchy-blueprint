"""Both machines become package-ready only through Omarchy's own update, to the same runtime."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


def harness(work: str, body: str) -> tuple[subprocess.CompletedProcess, str]:
    Path(work, "artifacts/source").mkdir(parents=True, exist_ok=True)
    Path(work, "artifacts/target").mkdir(parents=True, exist_ok=True)
    script = f"""
        set -euo pipefail
        source '{ROOT}/lib/common.sh'
        trap 'ra_finish "$?"' EXIT
        source '{ROOT}/scenario/stage-guest.sh'
        source '{ROOT}/scenario/readiness.sh'
        {body}
    """
    result = subprocess.run(["bash", "-c", script], capture_output=True, text=True, timeout=30,
                            env={**os.environ, "RA_WORK": work}, stdin=subprocess.DEVNULL)
    return result, Path(work, "artifacts/summary.txt").read_text()


def stubs(work: str, update_status: int = 0, version: str = "4.1.2-1", synced_after: str = "yes") -> str:
    return f"""
        ra_drive_terminal() {{ printf '%s\\n' "$*" >> '{work}/drives'; return {update_status}; }}
        ra_guest_reboot() {{ echo reboot >> '{work}/reboots'; echo reboot >> '{work}/order'; }}
        ra_guest_stage() {{ echo stage >> '{work}/order'; }}
        ra_guest_exec() {{
          case $2 in
            *'omarchy version'*) echo '{version}' ;;
            *'$repo.db'*) [[ '{synced_after}' == yes ]] || printf 'core ' ;;
            *'pacman -Q'*) echo 'alacritty 0.17.0-1' ;;
          esac
        }}
    """


class ReadinessTests(unittest.TestCase):
    def test_harness_never_syncs_package_metadata_itself(self):
        for path in [ROOT / "run.sh", *ROOT.glob("lib/*.sh"), *ROOT.glob("vm/*.sh"), *ROOT.glob("scenario/*.sh")]:
            with self.subTest(path=path.name):
                self.assertNotIn("-Sy", path.read_text())

    def test_readiness_runs_omarchy_update_once_at_a_terminal_and_records_the_runtime(self):
        with tempfile.TemporaryDirectory() as work:
            result, summary = harness(work, stubs(work) + """
                ra_phase SOURCE_READINESS
                ra_omarchy_ready source SOURCE_READINESS
                ra_pass SOURCE_READINESS
            """)
            self.assertEqual(result.returncode, 0, summary + result.stderr)
            drives = Path(work, "drives").read_text().splitlines()
            self.assertEqual(len(drives), 1)
            self.assertIn("omarchy update -y", drives[0])
            self.assertIn("--sudo-password-file", drives[0])
            self.assertIn("ssh -tt", drives[0])
            self.assertNotIn(" spike ", f" {drives[0]} ")  # the password is only ever in the file
            self.assertEqual(Path(work, "reboots").read_text(), "reboot\n")
            # The reboot clears /tmp, so staged inputs are restored after it.
            self.assertEqual(Path(work, "order").read_text(), "reboot\nstage\n")
            self.assertEqual(Path(work, "artifacts/source/ready-omarchy-version.txt").read_text().strip(), "4.1.2-1")
            self.assertIn("source ready Omarchy: 4.1.2-1", summary)
            password = Path(work, "sudo-password")
            self.assertEqual(password.stat().st_mode & 0o077, 0)

    def test_failed_update_is_not_retried(self):
        with tempfile.TemporaryDirectory() as work:
            result, summary = harness(work, stubs(work, update_status=1) + """
                ra_phase SOURCE_READINESS
                ra_omarchy_ready source SOURCE_READINESS
            """)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("SOURCE_READINESS FAIL", summary)
            self.assertIn("not retried", summary)
            self.assertEqual(len(Path(work, "drives").read_text().splitlines()), 1)

    def test_update_must_leave_every_configured_repository_synced(self):
        with tempfile.TemporaryDirectory() as work:
            result, summary = harness(work, stubs(work, synced_after="no") + """
                ra_phase TARGET_READINESS
                ra_omarchy_ready target TARGET_READINESS
            """)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("still has no sync database", summary)

    def test_machines_must_reach_the_same_ready_runtime(self):
        with tempfile.TemporaryDirectory() as work:
            Path(work, "artifacts/source").mkdir(parents=True)
            Path(work, "artifacts/source/ready-omarchy-version.txt").write_text("4.1.2-1\n")
            result, summary = harness(work, stubs(work, version="4.1.3-1") + """
                ra_phase TARGET_READINESS
                ra_omarchy_ready target TARGET_READINESS
                ra_require_same_ready_runtime
            """)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("TARGET_READINESS FAIL", summary)
            self.assertIn("4.1.2-1", summary)
            self.assertIn("4.1.3-1", summary)
            self.assertEqual(len(Path(work, "drives").read_text().splitlines()), 1)

    def test_matching_runtime_passes(self):
        with tempfile.TemporaryDirectory() as work:
            Path(work, "artifacts/source").mkdir(parents=True)
            Path(work, "artifacts/source/ready-omarchy-version.txt").write_text("4.1.2-1\n")
            result, summary = harness(work, stubs(work) + """
                ra_phase TARGET_READINESS
                ra_omarchy_ready target TARGET_READINESS
                ra_require_same_ready_runtime
                ra_pass TARGET_READINESS
            """)
            self.assertEqual(result.returncode, 0, summary + result.stderr)
            self.assertIn("target ready Omarchy: 4.1.2-1", summary)


if __name__ == "__main__":
    unittest.main()
