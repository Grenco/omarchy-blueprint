"""The approved Restore runs once at a real terminal and every failure keeps its phase."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
PROMPT = "Apply this restore? [y/N] "
JOURNAL = "/home/spike/.local/state/omarchy-blueprint/restores/20260926T100000Z.jsonl"
VERIFIED = '{"type":"VERIFY_COMPLETED","message":"ok=true"}\n'


def restore(transcript: str, status: int = 0, journal: str = VERIFIED) -> tuple[subprocess.CompletedProcess, str, Path]:
    work = Path(tempfile.mkdtemp())
    Path(work, "artifacts/target").mkdir(parents=True)
    Path(work, "transcript").write_text(transcript)
    Path(work, "journal").write_text(journal)
    script = f"""
        set -euo pipefail
        source '{ROOT}/lib/common.sh'
        source '{ROOT}/fixtures/expected.env'
        trap 'ra_finish "$?"' EXIT
        source '{ROOT}/scenario/stage-guest.sh'
        source '{ROOT}/scenario/readiness.sh'
        source '{ROOT}/scenario/restore-target.sh'
        ra_drive_terminal() {{
          printf '%s\\n' "$*" >> '{work}/drives'
          while [[ $1 != --transcript ]]; do shift; done
          cp '{work}/transcript' "$2"
          return {status}
        }}
        ra_guest_exec() {{ :; }}
        ra_guest_copy_from() {{ printf '%s\\n' "$2" > '{work}/copied-from'; cp '{work}/journal' "$3"; }}
        ra_apply_approved_restore
    """
    result = subprocess.run(["bash", "-c", script], capture_output=True, text=True, timeout=30,
                            env={**os.environ, "RA_WORK": str(work)}, stdin=subprocess.DEVNULL)
    return result, Path(work, "artifacts/summary.txt").read_text(), work


class ApprovedRestoreTests(unittest.TestCase):
    def test_verified_restore_passes_approval_apply_and_blueprint_verify_once(self):
        result, summary, work = restore(f"plan\n{PROMPT}yes\ninstalled\nRestore verified. Journal: {JOURNAL}\n")
        self.assertEqual(result.returncode, 0, summary + result.stderr)
        for phase in ("RESTORE_APPROVAL", "RESTORE_APPLY", "BLUEPRINT_VERIFY"):
            self.assertIn(f"{phase} PASS", summary)
        drives = Path(work, "drives").read_text().splitlines()
        self.assertEqual(len(drives), 1)
        for forbidden in ("--yes", "--json", "--force", "--exact", "--conflicts", "--convergence"):
            self.assertNotIn(forbidden, drives[0])
        self.assertIn("ssh -tt", drives[0])
        self.assertIn("--sudo-password-file", drives[0])
        self.assertIn("--machine target restore", drives[0])
        self.assertEqual(Path(work, "copied-from").read_text().strip(), JOURNAL)
        self.assertIn("VERIFY_COMPLETED", Path(work, "artifacts/target/restore-journal.jsonl").read_text())

    def test_failures_keep_their_phase_and_are_not_retried(self):
        cases = {
            "RESTORE_APPROVAL": ("plan without a prompt\n", 1),
            "RESTORE_APPROVAL ": (f"{PROMPT}yes\nError: restore plan changed after approval; inspect the new plan and approve again\n", 1),
            "BLUEPRINT_VERIFY": (f"{PROMPT}yes\nError: restore completed but verification failed: missing official:alacritty\n", 1),
            "RESTORE_APPLY": (f"{PROMPT}yes\nError: restore completed with 1 failed operation(s)\n", 1),
        }
        for phase, (transcript, status) in cases.items():
            with self.subTest(phase=phase):
                result, summary, work = restore(transcript, status)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(f"phase: {phase.strip()}", summary)
                self.assertEqual(len(Path(work, "drives").read_text().splitlines()), 1)

    def test_journal_must_be_blueprints_and_record_successful_verification(self):
        outside = f"{PROMPT}yes\nRestore verified. Journal: /tmp/fake.jsonl\n"
        result, summary, _ = restore(outside)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("journal", summary)
        result, summary, _ = restore(f"{PROMPT}yes\nRestore verified. Journal: {JOURNAL}\n",
                                     journal='{"type":"VERIFY_COMPLETED","message":"ok=false"}\n')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("phase: BLUEPRINT_VERIFY", summary)


if __name__ == "__main__":
    unittest.main()
